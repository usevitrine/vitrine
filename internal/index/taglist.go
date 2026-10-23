package index

import (
	"cmp"
	"slices"
	"strings"
)

// TagCount is a tag in the tag browser.
type TagCount struct {
	ID        uint32 `json:"-"`
	Tag       string `json:"tag"`       // "namespace:name" or "name"
	Namespace string `json:"namespace"` // "" = none
	Count     int    `json:"count"`     // live files that have it themselves
	Ideal     string `json:"ideal,omitempty"`
}

// NoNamespace, as TagList's ns, lists only tags without a namespace.
const NoNamespace = "-"

// TagList lists tags with live files: those whose name contains filter (any
// case), in namespace ns ("" = all, NoNamespace = none), most used first or
// by name. Returns how many match and the slice [offset, offset+limit).
func (ix *Index) TagList(filter, ns string, byName bool, offset, limit int) (int, []TagCount) {
	filter = strings.ToLower(strings.TrimSpace(filter))
	ns = strings.ToLower(ns)
	ix.mu.RLock()
	defer ix.mu.RUnlock()

	wantNS, nsOK := uint32(0), true
	switch ns {
	case "":
	case NoNamespace:
	default:
		wantNS, nsOK = ix.nsByName[ns]
		if !nsOK {
			return 0, nil
		}
	}
	type cand struct {
		id    uint32
		count int
	}
	var cands []cand
	for _, id := range ix.sorted { // by name already
		e := ix.tags[id]
		if !e.known || ix.raw[id] == nil {
			continue
		}
		if ns != "" && e.ns != wantNS {
			continue
		}
		if filter != "" && !strings.Contains(e.lower, filter) && !strings.Contains(strings.ToLower(ix.tagName(id)), filter) {
			continue
		}
		n := int(ix.raw[id].GetCardinality())
		if n == 0 {
			continue
		}
		cands = append(cands, cand{id, n})
	}
	if !byName {
		slices.SortStableFunc(cands, func(a, b cand) int { return cmp.Compare(b.count, a.count) })
	}
	total := len(cands)
	offset = min(max(0, offset), total)
	end := min(total, offset+max(0, limit))
	out := make([]TagCount, 0, end-offset)
	for _, c := range cands[offset:end] {
		t := TagCount{ID: c.id, Tag: ix.tagName(c.id), Namespace: ix.nsNames[ix.tags[c.id].ns], Count: c.count}
		if ideal := ix.rel.idealOf(c.id); ideal != c.id {
			t.Ideal = ix.tagName(ideal)
		}
		out = append(out, t)
	}
	return total, out
}
