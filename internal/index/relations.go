package index

import (
	"fmt"
	"slices"

	"github.com/RoaringBitmap/roaring/v2"
)

// Relation is a pair of tag IDs: child -> parent, or sibling -> ideal tag.
type Relation struct {
	From, To uint32
}

// relations holds the parent/sibling graph and what it implies for bitmaps.
//
// A sibling is replaced by its ideal tag: searching either finds files with
// both. A parent is implied by its children: searching the parent finds files
// with any descendant. Parents apply to ideal tags (a sibling's parents
// belong to its ideal tag).
type relations struct {
	parents  []Relation
	siblings []Relation
	ideal    map[uint32]uint32   // tag -> ideal tag, for tags that have a sibling
	members  map[uint32][]uint32 // T -> every tag whose files count as T (incl. T), for T with its own eff bitmap
	up       map[uint32][]uint32 // t -> every T whose eff bitmap includes t's files
}

func (r *relations) reset() {
	*r = relations{ideal: map[uint32]uint32{}, members: map[uint32][]uint32{}, up: map[uint32][]uint32{}}
}

// idealOf follows sibling links to the final tag.
func (r *relations) idealOf(t uint32) uint32 {
	if i, ok := r.ideal[t]; ok {
		return i
	}
	return t
}

// resolve finds the ideal of every tag in the sibling graph, rejecting
// cycles and tags with two different ideals.
func resolveSiblings(ix *Index, siblings []Relation) (map[uint32]uint32, error) {
	next := map[uint32]uint32{}
	for _, s := range siblings {
		if s.From == s.To {
			return nil, fmt.Errorf("%q can't be its own sibling", ix.tagName(s.From))
		}
		if old, ok := next[s.From]; ok && old != s.To {
			return nil, fmt.Errorf("%q already has the ideal tag %q", ix.tagName(s.From), ix.tagName(old))
		}
		next[s.From] = s.To
	}
	ideal := make(map[uint32]uint32, len(next))
	for from := range next {
		t, steps := from, 0
		for {
			n, ok := next[t]
			if !ok {
				break
			}
			t = n
			if steps++; steps > len(next) {
				return nil, fmt.Errorf("siblings form a loop through %q", ix.tagName(from))
			}
		}
		ideal[from] = t
	}
	return ideal, nil
}

// buildRelations computes the derived maps, or explains why the relations
// are invalid (loops).
func buildRelations(ix *Index, parents, siblings []Relation) (relations, error) {
	var r relations
	r.reset()
	for _, rel := range append(slices.Clone(parents), siblings...) {
		if err := ix.checkTags([]uint32{rel.From, rel.To}); err != nil {
			return r, err
		}
	}
	ideal, err := resolveSiblings(ix, siblings)
	if err != nil {
		return r, err
	}
	r.ideal = ideal
	r.parents, r.siblings = slices.Clone(parents), slices.Clone(siblings)

	// Parent graph between ideal tags: children[parent] = child tags.
	children := map[uint32][]uint32{}
	for _, p := range parents {
		c, pa := r.idealOf(p.From), r.idealOf(p.To)
		if c == pa {
			return r, fmt.Errorf("%q can't be a parent of %q (they are the same tag or siblings)",
				ix.tagName(p.To), ix.tagName(p.From))
		}
		if !slices.Contains(children[pa], c) {
			children[pa] = append(children[pa], c)
		}
	}

	// Loop check: depth-first search with colors.
	const (
		white = iota
		gray
		black
	)
	color := map[uint32]int{}
	var visit func(t uint32) error
	visit = func(t uint32) error {
		color[t] = gray
		for _, c := range children[t] {
			switch color[c] {
			case gray:
				return fmt.Errorf("tag parents form a loop: %q is an ancestor of itself", ix.tagName(c))
			case white:
				if err := visit(c); err != nil {
					return err
				}
			}
		}
		color[t] = black
		return nil
	}
	for t := range children {
		if color[t] == white {
			if err := visit(t); err != nil {
				return r, err
			}
		}
	}

	// Tags that share an ideal: byIdeal[i] = i and its siblings.
	byIdeal := map[uint32][]uint32{}
	for from, to := range r.ideal {
		if len(byIdeal[to]) == 0 {
			byIdeal[to] = []uint32{to}
		}
		byIdeal[to] = append(byIdeal[to], from)
	}
	group := func(i uint32) []uint32 {
		if g, ok := byIdeal[i]; ok {
			return g
		}
		return []uint32{i}
	}

	// Every ideal with children or siblings gets its own bitmap: the union
	// of its group and all descendants' groups.
	need := map[uint32]bool{}
	for t := range children {
		need[t] = true
	}
	for t := range byIdeal {
		need[t] = true
	}
	for T := range need {
		seen := map[uint32]bool{}
		var walk func(t uint32)
		walk = func(t uint32) {
			if seen[t] {
				return
			}
			seen[t] = true
			r.members[T] = append(r.members[T], group(t)...)
			for _, c := range children[t] {
				walk(c)
			}
		}
		walk(T)
		slices.Sort(r.members[T])
		for _, m := range r.members[T] {
			r.up[m] = append(r.up[m], T)
		}
	}
	return r, nil
}

// CheckRelations reports whether the parent and sibling lists are valid (no
// loops), without changing the index. The library calls it before saving.
func (ix *Index) CheckRelations(parents, siblings []Relation) error {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	_, err := buildRelations(ix, parents, siblings)
	return err
}

// SetRelations replaces all parent and sibling relations.
func (ix *Index) SetRelations(parents, siblings []Relation) error {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	return ix.setRelations(parents, siblings)
}

func (ix *Index) setRelations(parents, siblings []Relation) error {
	r, err := buildRelations(ix, parents, siblings)
	if err != nil {
		return err
	}
	for T := range ix.rel.members {
		ix.eff[T] = ix.raw[T]
	}
	ix.rel = r
	for T, ms := range r.members {
		bms := make([]*roaring.Bitmap, len(ms))
		for i, m := range ms {
			bms[i] = ix.raw[m]
		}
		ix.eff[T] = roaring.FastOr(bms...)
	}
	return nil
}

// Relations returns the current parent and sibling lists.
func (ix *Index) Relations() (parents, siblings []Relation) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return slices.Clone(ix.rel.parents), slices.Clone(ix.rel.siblings)
}

// RelationNames returns the parent and sibling lists as display names
// ([child, parent] and [tag, ideal]).
func (ix *Index) RelationNames() (parents, siblings [][2]string) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	for _, r := range ix.rel.parents {
		parents = append(parents, [2]string{ix.tagName(r.From), ix.tagName(r.To)})
	}
	for _, r := range ix.rel.siblings {
		siblings = append(siblings, [2]string{ix.tagName(r.From), ix.tagName(r.To)})
	}
	return parents, siblings
}
