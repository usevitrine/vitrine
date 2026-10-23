package index

import (
	"cmp"
	"slices"
	"strings"

	"github.com/RoaringBitmap/roaring/v2"

	"github.com/usevitrine/vitrine/internal/tag"
)

// Suggestion is one autocomplete entry.
type Suggestion struct {
	Tag   string `json:"tag"`   // as typed into a search, "namespace:name" or "name"
	Count int    `json:"count"` // live files it finds (including siblings and children)
	// Ideal is set when Tag is a sibling: searching it searches Ideal.
	Ideal string `json:"ideal,omitempty"`
}

// Autocomplete suggests tags for what the user is typing: names starting with
// it first, then names with a word starting with it ("modern_warfare" finds
// call_of_duty_modern_warfare), then names with it anywhere (from two
// characters on), each most used first (in any namespace, or in one with
// "ns:text"); a '*' in it makes it a wildcard pattern instead. Tags with no
// live files are left out. sites (a comma-separated list, as in system:site=):
// only tags of files from those sites, counted on those files - none of them
// with files yet (a site not downloaded from): every tag.
func (ix *Index) Autocomplete(text string, limit int, sites string) []Suggestion {
	text = tag.Normalize(text)
	if text == "" || limit <= 0 {
		return nil
	}
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	var scope *roaring.Bitmap
	if strings.TrimSpace(sites) != "" {
		scope = ix.sitesBitmap(sites) // (nil: no files from them - every tag)
	}

	var ids []uint32
	var tiers map[uint32]uint8 // (how well each matched: 0 = the start, 1 = a word's start, 2 = inside one)
	if strings.Contains(text, "*") {
		ids = ix.matchPattern(text)
	} else {
		ids, tiers = ix.matchText(text)
	}

	// Keep the top `limit` by count in a small sorted list (most candidates
	// fall below its last entry), and only build names for the winners.
	type cand struct {
		id, ideal uint32
		count     int
		tier      uint8
	}
	better := func(a, b cand) int { // < 0 if a ranks before b
		if c := cmp.Compare(a.tier, b.tier); c != 0 {
			return c
		}
		if c := cmp.Compare(b.count, a.count); c != 0 {
			return c
		}
		return cmp.Or(strings.Compare(ix.tags[a.id].lower, ix.tags[b.id].lower), cmp.Compare(a.id, b.id))
	}
	top := make([]cand, 0, limit+1)
	for _, id := range ids {
		ideal := ix.rel.idealOf(id)
		c := cand{id, ideal, int(ix.eff[ideal].GetCardinality()), tiers[id]}
		if c.count == 0 || (len(top) == limit && better(c, top[limit-1]) >= 0) {
			continue // (with sites: its count there can only be smaller - out either way)
		}
		if scope != nil {
			if c.count = int(ix.eff[ideal].AndCardinality(scope)); c.count == 0 || (len(top) == limit && better(c, top[limit-1]) >= 0) {
				continue
			}
		}
		i, _ := slices.BinarySearchFunc(top, c, better)
		top = slices.Insert(top, i, c)
		if len(top) > limit {
			top = top[:limit]
		}
	}

	out := make([]Suggestion, len(top))
	for i, c := range top {
		out[i] = Suggestion{Tag: ix.tagName(c.id), Count: c.count}
		if c.ideal != c.id {
			out[i].Ideal = ix.tagName(c.ideal)
		}
	}
	return out
}

// matchText finds tags whose name has the text in it ("ns:text" limits it to a
// namespace), and how well each matched (see Autocomplete). One character:
// only names starting with it (anywhere would be nearly every tag).
func (ix *Index) matchText(text string) ([]uint32, map[uint32]uint8) {
	ids := ix.matchPrefix(text)
	tiers := make(map[uint32]uint8, len(ids))
	for _, id := range ids {
		tiers[id] = 0
	}
	t := tag.Split(text, ix.isNamespace)
	p := strings.ToLower(t.Name)
	if len([]rune(p)) < 2 {
		return ids, tiers
	}
	ns, anyNS := uint32(0), t.Namespace == ""
	if !anyNS {
		ns = ix.nsByName[strings.ToLower(t.Namespace)]
	}
	for _, id := range ix.sorted {
		e := ix.tags[id]
		if _, done := tiers[id]; done || (!anyNS && e.ns != ns) {
			continue
		}
		i := strings.Index(e.lower, p)
		if i <= 0 {
			continue // (none - or the start: matchPrefix has those)
		}
		tier := uint8(2)
		if wordStart(e.lower, i, p) {
			tier = 1
		}
		tiers[id] = tier
		ids = append(ids, id)
	}
	return ids, tiers
}

// wordStart says whether p is somewhere in name at the start of a word (after '_', '-', '(', ...), its first match
// at i.
func wordStart(name string, i int, p string) bool {
	for i >= 0 {
		if r := name[i-1]; !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r >= 0x80) {
			return true
		}
		j := strings.Index(name[i+1:], p)
		if j < 0 {
			return false
		}
		i += 1 + j
	}
	return false
}

// matchPrefix finds tags whose name starts with the prefix ("ns:prefix" limits
// it to a namespace).
func (ix *Index) matchPrefix(text string) []uint32 {
	t := tag.Split(text, ix.isNamespace)
	if ns, rest, ok := strings.Cut(text, ":"); ok && rest == "" && ix.isNamespace(ns) {
		// "artist:" alone: every tag in the namespace.
		t = tag.Tag{Namespace: ns}
	}
	ns, anyNS := uint32(0), t.Namespace == ""
	if !anyNS {
		ns = ix.nsByName[strings.ToLower(t.Namespace)]
	}
	p := strings.ToLower(t.Name)
	lo, _ := slices.BinarySearchFunc(ix.sorted, p, func(id uint32, s string) int {
		return strings.Compare(ix.tags[id].lower, s)
	})
	var out []uint32
	for _, id := range ix.sorted[lo:] {
		e := ix.tags[id]
		if !strings.HasPrefix(e.lower, p) {
			break
		}
		if anyNS || e.ns == ns {
			out = append(out, id)
		}
	}
	return out
}
