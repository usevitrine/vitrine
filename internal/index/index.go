// Package index is the in-memory search index.
//
// Files are addressed by their dense uint32 ID. Each tag has a roaring bitmap
// of the files that have it; per-file values (size, times, ...) are column
// arrays indexed by file ID. Searches combine bitmaps and scan columns; the
// database is never queried.
//
// Only live files are in the index: a deleted file (in its undo window) is
// removed and put back on undo, so every bitmap and count covers live files.
//
// The index is safe for concurrent use: searches share a read lock, updates
// take the write lock. Updates are applied by the library after each database
// commit.
package index

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/RoaringBitmap/roaring/v2"

	"github.com/usevitrine/vitrine/internal/media"
	"github.com/usevitrine/vitrine/internal/search"
	"github.com/usevitrine/vitrine/internal/tag"
)

// File is one file's searchable values.
type File struct {
	ID         uint32
	Size       int64
	Type       media.Type
	Width      uint32
	Height     uint32
	DurationMS uint32
	HasAudio   bool
	ImportedAt int64 // Unix ms
	ModifiedAt int64 // Unix ms
	PostedAt   int64 // Unix ms when posted on its site; 0 = unknown
	PostID     int64 // its post on the site (only for a day-only PostedAt: the order within that day)
	Rating     media.Rating
	Bulk       bool // came in bulk (a one-off download, a first check, an import)
}

// Tag is a tag as stored in the database.
type Tag struct {
	ID          uint32
	NamespaceID uint32
	Name        string
}

type tagEntry struct {
	ns    uint32
	name  string // verbatim
	lower string // lower-cased name, used for lookups
	known bool
}

// Index is the search index. The zero value is not usable; use New or Load.
type Index struct {
	mu    sync.RWMutex
	trash bool // the trash's index (LoadTrash): deleted files

	// Files, indexed by file ID.
	alive    *roaring.Bitmap
	hasAudio *roaring.Bitmap
	bulk     *roaring.Bitmap // files that came in bulk (File.Bulk)
	favorite *roaring.Bitmap // favorites (deleted ones too: they're favorites again on undo)
	favAt    []int64         // when each became a favorite (Unix ms; 0 = not one)
	climaxes []uint16        // times counted with the "I came" button
	size     []int64
	typ      []media.Type
	width    []uint32
	height   []uint32
	duration []uint32
	imported []int64
	modified []int64
	posted   []int64 // posted time, or the import time when unknown (so sorting
	// and filtering by upload time still place every file)
	rating   []media.Rating
	tagCount []uint16

	// Tags, indexed by tag ID.
	tags     []tagEntry
	nsNames  []string          // namespace ID -> name ("" for 0)
	nsByName map[string]uint32 // namespace name -> ID
	byName   map[string][]uint32
	sorted   []uint32 // tag IDs ordered by lower-cased name
	trigrams map[string]*roaring.Bitmap
	raw      []*roaring.Bitmap // tag -> files that have it themselves
	eff      []*roaring.Bitmap // tag -> files that have it, a sibling or a child; == raw for most tags

	rel relations

	// Sites (see sites.go).
	siteAlias map[string]uint32          // plugin ID or address -> site ID
	siteFiles map[uint32]*roaring.Bitmap // site ID -> files from it
	subFiles  map[uint32]*roaring.Bitmap // subscription ID -> files it downloaded (system:found_by=)

	// comic: the comics index's own columns (nil in the library's; see comics.go).
	comic *comicColumns

	// Presorted file IDs per sort field (nil = needs rebuilding).
	sortMu    sync.Mutex
	sortCache [search.FieldRandom]atomic.Pointer[[]uint32]
}

// New returns an empty index.
func New() *Index {
	ix := &Index{
		alive:     roaring.New(),
		hasAudio:  roaring.New(),
		bulk:      roaring.New(),
		favorite:  roaring.New(),
		nsNames:   []string{""},
		nsByName:  map[string]uint32{},
		byName:    map[string][]uint32{},
		trigrams:  map[string]*roaring.Bitmap{},
		siteAlias: map[string]uint32{},
		siteFiles: map[uint32]*roaring.Bitmap{},
		subFiles:  map[uint32]*roaring.Bitmap{},
	}
	ix.rel.reset()
	return ix
}

// growFiles makes the column arrays long enough for id.
func (ix *Index) growFiles(id uint32) {
	n := int(id) + 1
	if n <= len(ix.size) {
		return
	}
	c := max(n, len(ix.size)*5/4) // grow by 25% to avoid copying on every import
	ix.size = slices.Grow(ix.size, c-len(ix.size))[:n]
	ix.typ = slices.Grow(ix.typ, c-len(ix.typ))[:n]
	ix.width = slices.Grow(ix.width, c-len(ix.width))[:n]
	ix.height = slices.Grow(ix.height, c-len(ix.height))[:n]
	ix.duration = slices.Grow(ix.duration, c-len(ix.duration))[:n]
	ix.imported = slices.Grow(ix.imported, c-len(ix.imported))[:n]
	ix.modified = slices.Grow(ix.modified, c-len(ix.modified))[:n]
	ix.posted = slices.Grow(ix.posted, c-len(ix.posted))[:n]
	ix.rating = slices.Grow(ix.rating, c-len(ix.rating))[:n]
	ix.tagCount = slices.Grow(ix.tagCount, c-len(ix.tagCount))[:n]
	ix.favAt = slices.Grow(ix.favAt, c-len(ix.favAt))[:n]
	ix.climaxes = slices.Grow(ix.climaxes, c-len(ix.climaxes))[:n]
}

// AddNamespace registers a namespace.
func (ix *Index) AddNamespace(id uint32, name string) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.addNamespace(id, name)
}

func (ix *Index) addNamespace(id uint32, name string) {
	for int(id) >= len(ix.nsNames) {
		ix.nsNames = append(ix.nsNames, "")
	}
	ix.nsNames[id] = name
	if name != "" {
		ix.nsByName[strings.ToLower(name)] = id
	}
}

// AddTags registers tags (new ones, or ones the index hasn't seen). Tags must
// be known before files use them.
func (ix *Index) AddTags(tags ...Tag) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	for _, t := range tags {
		ix.addTag(t, true)
	}
}

// addTag registers one tag; keepSorted inserts it into the sorted name list
// (the bulk loader sorts once at the end instead).
func (ix *Index) addTag(t Tag, keepSorted bool) {
	for int(t.ID) >= len(ix.tags) {
		ix.tags = append(ix.tags, tagEntry{})
		ix.raw = append(ix.raw, nil)
		ix.eff = append(ix.eff, nil)
	}
	if old := ix.tags[t.ID]; old.known {
		if old.ns == t.NamespaceID && old.name == t.Name {
			return
		}
		// The ID now names another tag (a rolled-back transaction let SQLite
		// reuse it): drop the old name. Its bitmaps stay.
		ix.unname(t.ID)
	}
	lower := strings.ToLower(t.Name)
	ix.tags[t.ID] = tagEntry{ns: t.NamespaceID, name: t.Name, lower: lower, known: true}
	if ix.raw[t.ID] == nil {
		bm := roaring.New()
		ix.raw[t.ID], ix.eff[t.ID] = bm, bm
	}
	ix.byName[lower] = append(ix.byName[lower], t.ID)
	for _, g := range trigramsOf(lower) {
		b := ix.trigrams[g]
		if b == nil {
			b = roaring.New()
			ix.trigrams[g] = b
		}
		b.Add(t.ID)
	}
	if keepSorted {
		i, _ := slices.BinarySearchFunc(ix.sorted, lower, func(id uint32, s string) int {
			return strings.Compare(ix.tags[id].lower, s)
		})
		ix.sorted = slices.Insert(ix.sorted, i, t.ID)
	}
}

// unname removes a tag's name from the lookup structures.
func (ix *Index) unname(id uint32) {
	lower := ix.tags[id].lower
	ix.byName[lower] = slices.DeleteFunc(ix.byName[lower], func(x uint32) bool { return x == id })
	if len(ix.byName[lower]) == 0 {
		delete(ix.byName, lower)
	}
	for _, g := range trigramsOf(lower) {
		if b := ix.trigrams[g]; b != nil {
			b.Remove(id)
		}
	}
	ix.sorted = slices.DeleteFunc(ix.sorted, func(x uint32) bool { return x == id })
}

func (ix *Index) sortTags() {
	slices.SortFunc(ix.sorted, func(a, b uint32) int {
		if c := strings.Compare(ix.tags[a].lower, ix.tags[b].lower); c != 0 {
			return c
		}
		return int(a) - int(b)
	})
}

// trigramsOf lists the distinct 3-byte pieces of s.
func trigramsOf(s string) []string {
	if len(s) < 3 {
		return nil
	}
	out := make([]string, 0, len(s)-2)
	for i := 0; i+3 <= len(s); i++ {
		if g := s[i : i+3]; !slices.Contains(out, g) {
			out = append(out, g)
		}
	}
	return out
}

// tagName is the display name, "namespace:name" or "name".
func (ix *Index) tagName(id uint32) string {
	e := ix.tags[id]
	return tag.Tag{Namespace: ix.nsNames[e.ns], Name: e.name}.String()
}

// IsNamespace reports whether the library has a namespace by this name.
func (ix *Index) IsNamespace(s string) bool {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return ix.isNamespace(s)
}

func (ix *Index) isNamespace(s string) bool {
	_, ok := ix.nsByName[strings.ToLower(s)]
	return ok
}

func (ix *Index) checkTags(ids []uint32) error {
	for _, t := range ids {
		if int(t) >= len(ix.tags) || !ix.tags[t].known {
			return fmt.Errorf("index: unknown tag %d", t)
		}
	}
	return nil
}

// AddFile puts a live file into the index with its tags: a new import, or a
// file brought back by undo.
func (ix *Index) AddFile(f File, tags []uint32) error {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	return ix.addFile(f, tags)
}

func (ix *Index) addFile(f File, tags []uint32) error {
	if err := ix.checkTags(tags); err != nil {
		return err
	}
	if int(f.ID) >= len(ix.size) {
		ix.invalidateSorts() // a new ID must enter every presorted list
	}
	ix.setColumns(f)
	if f.Bulk {
		ix.bulk.Add(f.ID)
	}
	ix.alive.Add(f.ID)
	ix.tagCount[f.ID] = uint16(min(len(tags), 65535))
	for _, t := range tags {
		ix.linkTag(f.ID, t)
	}
	return nil
}

// UpdateFile changes a live file's values (e.g. after probing its media).
func (ix *Index) UpdateFile(f File) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if int(f.ID) >= len(ix.size) || !ix.alive.Contains(f.ID) {
		return // (not in the library - deleted, or only a comic page: AddFile sets its values)
	}
	ix.setColumns(f)
	ix.invalidateSorts()
}

func (ix *Index) setColumns(f File) {
	ix.growFiles(f.ID)
	id := f.ID
	ix.size[id] = f.Size
	ix.typ[id] = f.Type
	ix.width[id] = f.Width
	ix.height[id] = f.Height
	ix.duration[id] = f.DurationMS
	ix.imported[id] = f.ImportedAt
	ix.modified[id] = f.ModifiedAt
	ix.posted[id] = postedKey(f)
	ix.rating[id] = f.Rating
	if f.HasAudio {
		ix.hasAudio.Add(id)
	} else {
		ix.hasAudio.Remove(id)
	}
}

// SetFavorite makes a file a favorite since at (Unix ms), or not one (at = 0).
func (ix *Index) SetFavorite(id uint32, at int64) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.growFiles(id)
	ix.favAt[id] = at
	if at > 0 {
		ix.favorite.Add(id)
	} else {
		ix.favorite.Remove(id)
	}
	ix.invalidateSort(search.FieldFavorite)
}

// SetClimaxes sets how many times a file was counted with the "I came" button.
func (ix *Index) SetClimaxes(id uint32, n int) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.growFiles(id)
	ix.climaxes[id] = uint16(min(max(n, 0), 65535))
	ix.invalidateSort(search.FieldClimax)
}

// Climaxes returns how many times a file was counted.
func (ix *Index) Climaxes(id uint32) int {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	if int(id) >= len(ix.climaxes) {
		return 0
	}
	return int(ix.climaxes[id])
}

// IsFavorite tells whether a file is a favorite.
func (ix *Index) IsFavorite(id uint32) bool {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return ix.favorite.Contains(id)
}

// Favorites counts the live favorites.
func (ix *Index) Favorites() int {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return int(roaring.And(ix.favorite, ix.alive).GetCardinality())
}

// RemoveFile takes a file out of every search (it was deleted). tags are the
// file's current tags.
func (ix *Index) RemoveFile(id uint32, tags []uint32) error {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if err := ix.checkTags(tags); err != nil {
		return err
	}
	ix.alive.Remove(id)
	for _, t := range tags {
		ix.raw[t].Remove(id)
		for _, T := range ix.rel.up[t] {
			ix.eff[T].Remove(id)
		}
	}
	return nil
}

// UpdateTags applies a change to a file's tags. added and removed are tag IDs
// the file gained or lost (a tag counts once, whatever its origins);
// modifiedAt is the file's new modified time.
func (ix *Index) UpdateTags(id uint32, added, removed []uint32, modifiedAt int64) error {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if err := ix.checkTags(added); err != nil {
		return err
	}
	if err := ix.checkTags(removed); err != nil {
		return err
	}
	if !ix.alive.Contains(id) {
		return nil // deleted files aren't indexed; undo re-adds them with their tags
	}
	for _, t := range removed {
		if ix.raw[t].CheckedRemove(id) && ix.tagCount[id] > 0 {
			ix.tagCount[id]--
		}
		for _, T := range ix.rel.up[t] {
			if !ix.hasMember(id, T) {
				ix.eff[T].Remove(id)
			}
		}
	}
	for _, t := range added {
		if !ix.raw[t].Contains(id) && ix.tagCount[id] < 65535 {
			ix.tagCount[id]++
		}
		ix.linkTag(id, t)
	}
	ix.modified[id] = modifiedAt
	ix.invalidateSort(search.FieldModified)
	ix.invalidateSort(search.FieldTagCount)
	return nil
}

func (ix *Index) linkTag(id, t uint32) {
	ix.raw[t].Add(id)
	for _, T := range ix.rel.up[t] {
		ix.eff[T].Add(id)
	}
}

// hasMember reports whether the file still has any tag that implies T.
func (ix *Index) hasMember(id, T uint32) bool {
	for _, m := range ix.rel.members[T] {
		if ix.raw[m].Contains(id) {
			return true
		}
	}
	return false
}

func (ix *Index) invalidateSorts() {
	for i := range ix.sortCache {
		ix.sortCache[i].Store(nil)
	}
}

func (ix *Index) invalidateSort(f search.Field) { ix.sortCache[f].Store(nil) }

// Stats are index totals.
type Stats struct {
	Files int // live files
	Tags  int // tags with at least one live file
}

// Stats counts live files and used tags.
func (ix *Index) Stats() Stats {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	s := Stats{Files: int(ix.alive.GetCardinality())}
	for _, bm := range ix.raw {
		if bm != nil && !bm.IsEmpty() {
			s.Tags++
		}
	}
	return s
}

// SizeOf adds up the files' sizes in bytes.
func (ix *Index) SizeOf(ids []uint32) int64 {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	var n int64
	for _, id := range ids {
		if int(id) < len(ix.size) {
			n += ix.size[id]
		}
	}
	return n
}

// Namespaces lists the namespace names, sorted (without the empty one).
func (ix *Index) Namespaces() []string {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	out := make([]string, 0, len(ix.nsByName))
	for _, n := range ix.nsNames {
		if n != "" {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return out
}

// postedKey is a file's upload time as the index sorts it: the import time when unknown; a day only (midnight UTC -
// the site says no more, e.g. older realbooru posts) plus its post ID in ms, so files of one day come in post order
// (newer posts later) and still fall on that day (2026-10-23, the user's choice: "order by post ID only").
func postedKey(f File) int64 {
	switch {
	case f.PostedAt == 0:
		return f.ImportedAt
	case f.PostedAt%dayMS == 0 && f.PostID > 0 && f.PostID < dayMS:
		return f.PostedAt + f.PostID
	}
	return f.PostedAt
}

const dayMS = 24 * 60 * 60 * 1000

// Times returns each file's import time (posted = false) or upload time
// (posted = true; the import time when unknown), in ms.
func (ix *Index) Times(ids []uint32, posted bool) []int64 {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	col := ix.imported
	if posted {
		col = ix.posted
	}
	out := make([]int64, len(ids))
	for i, id := range ids {
		if int(id) < len(col) {
			out[i] = col[id]
		}
	}
	return out
}

// Shapes returns each file's width/height as a uint16 (ratio x 1024, clamped;
// 1024 = square when the size isn't known) - the masonry grid lays a whole
// result out from these.
func (ix *Index) Shapes(ids []uint32) []uint16 {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	out := make([]uint16, len(ids))
	for i, id := range ids {
		out[i] = 1024
		if int(id) < len(ix.width) && ix.width[id] > 0 && ix.height[id] > 0 {
			r := float64(ix.width[id]) / float64(ix.height[id]) * 1024
			out[i] = uint16(min(max(r, 16), 65535))
		}
	}
	return out
}
