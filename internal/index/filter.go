package index

import (
	"math"
	"slices"
	"time"

	"github.com/RoaringBitmap/roaring/v2"

	"github.com/usevitrine/vitrine/internal/search"
)

// bounds turns a numeric or time filter into a range: a value v matches if
// lo <= v < hi, or - with negate - if it falls outside. ok is false for
// fields that aren't ranges (type, rating, audio).
func bounds(s search.System, now time.Time) (lo, hi int64, negate, ok bool) {
	lo, hi = math.MinInt64, math.MaxInt64
	switch s.Field {
	case search.FieldImported, search.FieldModified, search.FieldPosted:
		if s.AgeText != "" {
			// age = now - t, so "age < A" is "t > now - A".
			edge := now.UnixMilli() - s.Age.Milliseconds()
			switch s.Op {
			case search.OpLt:
				lo = edge + 1
			case search.OpLe:
				lo = edge
			case search.OpGt:
				hi = edge
			case search.OpGe:
				hi = edge + 1
			default:
				return 0, 0, false, false
			}
			return lo, hi, false, true
		}
		from, until := s.Time.UnixMilli(), s.Until.UnixMilli()
		switch s.Op {
		case search.OpLt:
			hi = from
		case search.OpLe:
			hi = until
		case search.OpGt:
			lo = until
		case search.OpGe:
			lo = from
		case search.OpEq, search.OpNe:
			lo, hi, negate = from, until, s.Op == search.OpNe
		}
		return lo, hi, negate, true
	case search.FieldSize, search.FieldWidth, search.FieldHeight, search.FieldDuration, search.FieldTagCount, search.FieldClimax, search.FieldPages:
		x := s.Num
		switch s.Op {
		case search.OpLt:
			hi = x
		case search.OpLe:
			hi = x + 1
		case search.OpGt:
			lo = x + 1
		case search.OpGe:
			lo = x
		case search.OpEq, search.OpNe:
			lo, hi, negate = x, x+1, s.Op == search.OpNe
		}
		return lo, hi, negate, true
	}
	return 0, 0, false, false
}

type integer interface{ ~int64 | ~uint32 | ~uint16 }

// column returns the file values a range filter compares; exactly one of the
// results is non-nil.
func (ix *Index) column(f search.Field) (i64 []int64, u32 []uint32, u16 []uint16) {
	switch f {
	case search.FieldImported:
		return ix.imported, nil, nil
	case search.FieldModified:
		return ix.modified, nil, nil
	case search.FieldPosted:
		return ix.posted, nil, nil
	case search.FieldSize:
		return ix.size, nil, nil
	case search.FieldWidth:
		return nil, ix.width, nil
	case search.FieldHeight:
		return nil, ix.height, nil
	case search.FieldDuration:
		return nil, ix.duration, nil
	case search.FieldTagCount:
		return nil, nil, ix.tagCount
	case search.FieldFavorite:
		return ix.favAt, nil, nil
	case search.FieldClimax:
		return nil, nil, ix.climaxes
	case search.FieldPages:
		if ix.comic != nil {
			return nil, ix.comic.pages, nil
		}
	}
	return nil, nil, nil
}

// matcher returns a test for one filter, with its bounds worked out once.
func (ix *Index) matcher(s search.System, now time.Time) func(id uint32) bool {
	switch s.Field {
	case search.FieldHasAudio:
		return ix.hasAudio.Contains
	case search.FieldFavorite:
		return ix.favorite.Contains
	case search.FieldType:
		want := s.Op == search.OpEq
		return func(id uint32) bool { return slices.Contains(s.Types, ix.typ[id]) == want }
	case search.FieldRating:
		want := s.Op == search.OpEq
		return func(id uint32) bool { return (ix.rating[id] == s.Rating) == want }
	case search.FieldSite:
		bm, want := ix.sitesBitmap(s.Site), s.Op == search.OpEq
		return func(id uint32) bool { return (bm != nil && bm.Contains(id)) == want }
	case search.FieldFoundBy:
		bm, want := ix.subFiles[uint32(s.Num)], s.Op == search.OpEq
		return func(id uint32) bool { return (bm != nil && bm.Contains(id)) == want }
	case search.FieldRead:
		if ix.comic == nil {
			return func(uint32) bool { return false }
		}
		return func(id uint32) bool { return ix.comic.readState(id) == s.Num }
	}
	lo, hi, negate, ok := bounds(s, now)
	if !ok {
		return func(uint32) bool { return false }
	}
	in := func(v int64) bool { return (lo <= v && v < hi) != negate }
	i64, u32, u16 := ix.column(s.Field)
	switch {
	case i64 != nil:
		return func(id uint32) bool { return in(i64[id]) }
	case u32 != nil:
		return func(id uint32) bool { return in(int64(u32[id])) }
	case u16 != nil:
		return func(id uint32) bool { return in(int64(u16[id])) }
	}
	return func(uint32) bool { return false }
}

// sysBitmap returns the live files matching a filter. Range filters scan
// their column in one tight loop. The result is new, except for has_audio
// (the index's own bitmap: don't modify it).
func (ix *Index) sysBitmap(s search.System, now time.Time) *roaring.Bitmap {
	if s.Field == search.FieldHasAudio {
		return ix.hasAudio
	}
	if s.Field == search.FieldFavorite {
		return roaring.And(ix.favorite, ix.alive)
	}
	if s.Field == search.FieldSite || s.Field == search.FieldFoundBy {
		out := ix.alive.Clone()
		bm := ix.sitesBitmap(s.Site)
		if s.Field == search.FieldFoundBy {
			bm = ix.subFiles[uint32(s.Num)]
		}
		switch {
		case s.Op == search.OpEq && bm == nil:
			return roaring.New()
		case s.Op == search.OpEq:
			out.And(bm)
		case bm != nil:
			out.AndNot(bm)
		}
		return out
	}
	alive := ix.alive.ToDense()
	out := roaring.New()
	if lo, hi, negate, ok := bounds(s, now); ok {
		i64, u32, u16 := ix.column(s.Field)
		switch {
		case i64 != nil:
			out.AddMany(scanRange(i64, alive, lo, hi, negate))
		case u32 != nil:
			out.AddMany(scanRange(u32, alive, lo, hi, negate))
		case u16 != nil:
			out.AddMany(scanRange(u16, alive, lo, hi, negate))
		}
		return out
	}
	match := ix.matcher(s, now)
	ids := ix.alive.ToArray()
	out.AddMany(slices.DeleteFunc(ids, func(id uint32) bool { return !match(id) }))
	return out
}

// scanRange lists live file IDs whose value is in [lo, hi) (outside with
// negate). alive is the live-file bitmap as dense words.
func scanRange[T integer](col []T, alive []uint64, lo, hi int64, negate bool) []uint32 {
	var out []uint32
	n := min(len(col), len(alive)*64)
	for id := 0; id < n; id++ {
		v := int64(col[id])
		if (lo <= v && v < hi) != negate && alive[id>>6]&(1<<(id&63)) != 0 {
			out = append(out, uint32(id))
		}
	}
	return out
}
