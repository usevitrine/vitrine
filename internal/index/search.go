package index

import (
	"cmp"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"github.com/RoaringBitmap/roaring/v2"

	"github.com/usevitrine/vitrine/internal/search"
	"github.com/usevitrine/vitrine/internal/tag"
)

// Result is a search's outcome.
type Result struct {
	IDs   []uint32 // matching file IDs in sort order (after the limit)
	Total int      // matches before the limit
	// Unknown lists searched tags the library doesn't have (they match nothing).
	Unknown []string
	// Seed is the random sort's seed, so the same order can be asked for again.
	Seed int64
}

// Search runs q. now is the reference time for ages like "imported<5d".
func (ix *Index) Search(q search.Query, now time.Time) Result {
	ix.mu.RLock()
	defer ix.mu.RUnlock()

	var res Result
	bm := ix.eval(q, now, &res)
	res.Total = int(bm.GetCardinality())
	res.IDs, res.Seed = ix.sortIDs(bm, q.Sort, q.Limit)
	return res
}

// Count returns how many files match q, without sorting.
func (ix *Index) Count(q search.Query, now time.Time) int {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	var res Result
	return int(ix.eval(q, now, &res).GetCardinality())
}

// eval returns a new bitmap of the files matching every term.
func (ix *Index) eval(q search.Query, now time.Time, res *Result) *roaring.Bitmap {
	var pos, neg []*roaring.Bitmap // index-owned bitmaps: never modify
	var filters []search.System
	for _, term := range q.Terms {
		if len(term.Any) == 1 {
			a := term.Any[0]
			switch {
			case a.Kind == search.KindSystem && a.Sys.Field == search.FieldHasAudio && !a.Negate:
				pos = append(pos, ix.hasAudio) // a bitmap already: cheaper as an AND
			case a.Kind == search.KindSystem && a.Sys.Field == search.FieldFavorite && !a.Negate:
				pos = append(pos, ix.favorite)
			case a.Kind == search.KindSystem && !a.Negate:
				filters = append(filters, a.Sys) // applied to the (smaller) candidates at the end
			case a.Negate:
				neg = append(neg, ix.atomBitmap(a, now, res, false))
			default:
				pos = append(pos, ix.atomBitmap(a, now, res, false))
			}
			continue
		}
		// OR group.
		parts := make([]*roaring.Bitmap, 0, len(term.Any))
		for _, a := range term.Any {
			parts = append(parts, ix.atomBitmap(a, now, res, true))
		}
		pos = append(pos, roaring.FastOr(parts...))
	}

	var out *roaring.Bitmap
	switch {
	case len(pos) == 0 && len(filters) > 0:
		// Only filters (e.g. "modified in the last 5 days"): the first one
		// scans its column directly, the rest check its matches.
		out = ix.sysBitmap(filters[0], now)
		filters = filters[1:]
	case len(pos) == 0:
		out = ix.alive.Clone()
	case len(pos) == 1:
		out = roaring.And(pos[0], ix.alive) // (has_audio and favorites hold deleted files too)
	default:
		slices.SortFunc(pos, func(a, b *roaring.Bitmap) int {
			return cmp.Compare(a.GetCardinality(), b.GetCardinality())
		})
		out = roaring.FastAnd(append(pos, ix.alive)...)
	}
	for _, n := range neg {
		if out.IsEmpty() {
			break
		}
		out.AndNot(n)
	}
	if len(filters) > 0 && !out.IsEmpty() {
		match := make([]func(uint32) bool, len(filters))
		for i, f := range filters {
			match[i] = ix.matcher(f, now)
		}
		kept := roaring.New()
		kept.AddMany(slices.DeleteFunc(out.ToArray(), func(id uint32) bool {
			for _, m := range match {
				if !m(id) {
					return true
				}
			}
			return false
		}))
		out = kept
	}
	return out
}

// atomBitmap returns the files matching one atom. With applyNegate (inside an
// OR group) a negated atom becomes "live files without it"; otherwise the
// caller handles negation. The result may be an index-owned bitmap.
func (ix *Index) atomBitmap(a search.Atom, now time.Time, res *Result, applyNegate bool) *roaring.Bitmap {
	var bm *roaring.Bitmap
	switch a.Kind {
	case search.KindTag:
		bm = ix.tagBitmap(a.Text, res)
	case search.KindWildcard:
		bm = ix.wildcardBitmap(a.Text)
	case search.KindSystem:
		bm = ix.sysBitmap(a.Sys, now)
	case search.KindTitle:
		if ix.comic != nil {
			bm = ix.comic.titleBitmap(a.Text)
		} else {
			bm = ix.tagBitmap("/"+a.Text, res) // (no titles here: a tag that starts with "/")
		}
	}
	if a.Negate && applyNegate {
		return roaring.AndNot(ix.alive, bm)
	}
	return bm
}

// lookupTags finds tag IDs for a typed tag: "ns:name" in that namespace, a
// plain name in every namespace. "ns:name" also finds a tag stored as that
// whole text, from before "ns" was a namespace.
func (ix *Index) lookupTags(text string) []uint32 {
	t := tag.Split(text, ix.isNamespace)
	ids := ix.byName[strings.ToLower(t.Name)]
	if t.Namespace == "" {
		return ids
	}
	ns := ix.nsByName[strings.ToLower(t.Namespace)]
	var out []uint32
	for _, id := range ids {
		if ix.tags[id].ns == ns {
			out = append(out, id)
		}
	}
	for _, id := range ix.byName[strings.ToLower(t.Namespace+":"+t.Name)] {
		if ix.tags[id].ns == 0 {
			out = append(out, id)
		}
	}
	return out
}

func (ix *Index) tagBitmap(text string, res *Result) *roaring.Bitmap {
	ids := ix.lookupTags(text)
	switch len(ids) {
	case 0:
		if ix.comic != nil && !strings.Contains(text, ":") {
			// Comics: a word that isn't a tag searches their titles.
			if bm := ix.comic.titleBitmap(text); !bm.IsEmpty() {
				return bm
			}
		}
		res.Unknown = append(res.Unknown, text)
		return roaring.New()
	case 1:
		return ix.eff[ix.rel.idealOf(ids[0])]
	}
	return ix.unionOf(ids)
}

// unionOf ORs the effective bitmaps of tags (each resolved to its ideal).
func (ix *Index) unionOf(ids []uint32) *roaring.Bitmap {
	seen := make(map[uint32]bool, len(ids))
	bms := make([]*roaring.Bitmap, 0, len(ids))
	for _, id := range ids {
		i := ix.rel.idealOf(id)
		if !seen[i] {
			seen[i] = true
			bms = append(bms, ix.eff[i])
		}
	}
	if len(bms) > 256 {
		return roaring.ParOr(0, bms...)
	}
	return roaring.FastOr(bms...)
}

func (ix *Index) wildcardBitmap(pattern string) *roaring.Bitmap {
	return ix.unionOf(ix.matchPattern(pattern))
}

// matchPattern finds the tags matching a '*' pattern, optionally "ns:pattern".
func (ix *Index) matchPattern(pattern string) []uint32 {
	t := tag.Split(pattern, ix.isNamespace)
	ns, anyNS := uint32(0), t.Namespace == ""
	if !anyNS {
		ns = ix.nsByName[strings.ToLower(t.Namespace)]
	}
	p := strings.ToLower(t.Name)
	pieces := strings.Split(p, "*")
	ok := func(id uint32) bool {
		e := ix.tags[id]
		return e.known && (anyNS || e.ns == ns) && globMatch(pieces, e.lower)
	}

	var out []uint32
	switch {
	case pieces[0] != "":
		// Fixed start: a range of the sorted names.
		lo, _ := slices.BinarySearchFunc(ix.sorted, pieces[0], func(id uint32, s string) int {
			return strings.Compare(ix.tags[id].lower, s)
		})
		for _, id := range ix.sorted[lo:] {
			if !strings.HasPrefix(ix.tags[id].lower, pieces[0]) {
				break
			}
			if ok(id) {
				out = append(out, id)
			}
		}
	default:
		// Narrow by trigrams where a piece is long enough, else scan all.
		var bms []*roaring.Bitmap
		for _, piece := range pieces {
			for _, g := range trigramsOf(piece) {
				b := ix.trigrams[g]
				if b == nil {
					return nil
				}
				bms = append(bms, b)
			}
		}
		if len(bms) == 0 {
			for id := range ix.tags {
				if ok(uint32(id)) {
					out = append(out, uint32(id))
				}
			}
			return out
		}
		cand := roaring.FastAnd(bms...)
		cand.Iterate(func(id uint32) bool {
			if ok(id) {
				out = append(out, id)
			}
			return true
		})
	}
	return out
}

// globMatch matches s against a pattern split at its '*'s.
func globMatch(pieces []string, s string) bool {
	if len(pieces) == 1 {
		return s == pieces[0]
	}
	if !strings.HasPrefix(s, pieces[0]) {
		return false
	}
	s = s[len(pieces[0]):]
	last := pieces[len(pieces)-1]
	for _, p := range pieces[1 : len(pieces)-1] {
		i := strings.Index(s, p)
		if i < 0 {
			return false
		}
		s = s[i+len(p):]
	}
	return len(s) >= len(last) && strings.HasSuffix(s, last)
}

// sortKey returns a function giving each file's value for a sort field.
func (ix *Index) sortKey(f search.Field) func(id uint32) int64 {
	switch f {
	case search.FieldModified:
		return func(id uint32) int64 { return ix.modified[id] }
	case search.FieldPosted:
		return func(id uint32) int64 { return ix.posted[id] }
	case search.FieldSize:
		return func(id uint32) int64 { return ix.size[id] }
	case search.FieldWidth:
		return func(id uint32) int64 { return int64(ix.width[id]) }
	case search.FieldHeight:
		return func(id uint32) int64 { return int64(ix.height[id]) }
	case search.FieldDuration:
		return func(id uint32) int64 { return int64(ix.duration[id]) }
	case search.FieldTagCount:
		return func(id uint32) int64 { return int64(ix.tagCount[id]) }
	case search.FieldFavorite:
		return func(id uint32) int64 { return ix.favAt[id] }
	case search.FieldClimax:
		return func(id uint32) int64 { return int64(ix.climaxes[id]) }
	case search.FieldPages, search.FieldRead, search.FieldTitle:
		if c := ix.comic; c != nil {
			return c.sortKey(f)
		}
	}
	return func(id uint32) int64 { return int64(id) } // imported = ID order
}

// sortIDs orders the matches and applies the limit. Equal values are ordered
// by ID in the same direction, so every path gives the same order.
func (ix *Index) sortIDs(bm *roaring.Bitmap, s search.Sort, limit int) ([]uint32, int64) {
	n := int(bm.GetCardinality())
	want := n
	if limit > 0 && limit < n {
		want = limit
	}
	switch s.Field {
	case search.FieldImported, search.FieldNone:
		if s.Asc {
			ids := bm.ToArray()
			return ids[:want], 0
		}
		ids := make([]uint32, 0, want)
		it := bm.ReverseIterator()
		for it.HasNext() && len(ids) < want {
			ids = append(ids, it.Next())
		}
		return ids, 0
	case search.FieldRandom:
		seed := s.Seed
		if seed == 0 {
			seed = rand.Int64N(1<<53) + 1
		}
		// Each file gets its own place from the seed (a hash of seed and ID), so
		// a file added later lands somewhere in the order without reshuffling
		// the others (a shuffle of the whole list would).
		// Key and ID packed in one number (the key's top half, then the ID): a
		// plain sort of numbers, and ties broken by ID.
		ids := bm.ToArray()
		packed := make([]uint64, len(ids))
		for i, id := range ids {
			packed[i] = mix64(uint64(seed)^uint64(id)*0x9e3779b97f4a7c15)&^0xffffffff | uint64(id)
		}
		slices.Sort(packed)
		for i, v := range packed {
			ids[i] = uint32(v)
		}
		return ids[:want], seed
	}

	key := ix.sortKey(s.Field)
	// Small results: sort them directly. Large ones: walk the presorted list
	// of all files and keep those in the result (a dense bitset makes the
	// membership test a single shift and mask).
	if n*16 < len(ix.size) || n < 4096 {
		ids := bm.ToArray()
		slices.SortFunc(ids, func(a, b uint32) int {
			c := cmp.Compare(key(a), key(b))
			if c == 0 {
				c = cmp.Compare(a, b)
			}
			if !s.Asc {
				c = -c
			}
			return c
		})
		return ids[:want], 0
	}

	order := ix.presorted(s.Field, key)
	dense := bm.ToDense()
	in := func(id uint32) bool {
		w := int(id >> 6)
		return w < len(dense) && dense[w]&(1<<(id&63)) != 0
	}
	ids := make([]uint32, 0, want)
	if s.Asc {
		for _, id := range order {
			if in(id) {
				if ids = append(ids, id); len(ids) == want {
					break
				}
			}
		}
	} else {
		for i := len(order) - 1; i >= 0; i-- {
			if id := order[i]; in(id) {
				if ids = append(ids, id); len(ids) == want {
					break
				}
			}
		}
	}
	return ids, 0
}

// presorted returns every file ID ordered by the field (ascending, ties by
// ID), building it if an update invalidated it.
func (ix *Index) presorted(f search.Field, key func(uint32) int64) []uint32 {
	if p := ix.sortCache[f].Load(); p != nil {
		return *p
	}
	ix.sortMu.Lock()
	defer ix.sortMu.Unlock()
	if p := ix.sortCache[f].Load(); p != nil {
		return *p
	}
	order := make([]uint32, len(ix.size))
	for i := range order {
		order[i] = uint32(i)
	}
	slices.SortFunc(order, func(a, b uint32) int {
		if c := cmp.Compare(key(a), key(b)); c != 0 {
			return c
		}
		return cmp.Compare(a, b)
	})
	ix.sortCache[f].Store(&order)
	return order
}

// GlobMatch reports whether s matches a pattern where '*' stands for any
// text (e.g. "ai_*"). Both are compared as given; lower-case them first.
func GlobMatch(pattern, s string) bool {
	return globMatch(strings.Split(pattern, "*"), s)
}

// NewOpts says which files are new: imported after Since, and - with SkipBulk -
// not ones that came in bulk (one-off downloads, first checks, imports). A file
// opened in the viewer is never new (2026-10-31, the user: "viewing an image
// marked as new should remove the new status").
type NewOpts struct {
	Since    int64
	SkipBulk bool
}

func (ix *Index) isNew(id uint32, o NewOpts) bool {
	return int(id) < len(ix.imported) && ix.imported[id] > o.Since && !(o.SkipBulk && ix.bulk.Contains(id)) && !ix.seen.Contains(id)
}

// PinNew moves the new files to the front of ids (a result in sort order),
// ordered by s; the rest keep their order. It returns the new order and how
// many files are in front. ("New files first", whatever the sort.)
func (ix *Index) PinNew(ids []uint32, o NewOpts, s search.Sort) ([]uint32, int) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	var fresh, rest []uint32
	for _, id := range ids {
		if ix.isNew(id, o) {
			fresh = append(fresh, id)
		} else {
			rest = append(rest, id)
		}
	}
	if len(fresh) == 0 {
		return ids, 0
	}
	key := ix.sortKey(s.Field)
	slices.SortFunc(fresh, func(a, b uint32) int {
		c := cmp.Compare(key(a), key(b))
		if c == 0 {
			c = cmp.Compare(a, b)
		}
		if !s.Asc {
			c = -c
		}
		return c
	})
	return append(fresh, rest...), len(fresh)
}

// CountNew counts the new files in ids.
func (ix *Index) CountNew(ids []uint32, o NewOpts) int {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	n := 0
	for _, id := range ids {
		if ix.isNew(id, o) {
			n++
		}
	}
	return n
}

// mix64 is SplitMix64's finalizer: a fast, well-spread hash of x.
func mix64(x uint64) uint64 {
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	return x ^ (x >> 31)
}
