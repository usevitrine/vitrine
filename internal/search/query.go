// Package search describes library searches and reads them from booru-style
// text.
//
// A Query is a list of Terms that must all match (AND). Each Term is one chip
// in the web client: a single Atom, or an OR group of Atoms. An Atom is a tag,
// a wildcard pattern or a system filter, optionally negated.
//
// Text syntax (see Parse):
//
//	tag -excluded ns:tag *wild*card ( a ~ -b ~ system:has_audio )
//	system:imported<5d system:size>100MB system:type=video,gif sort:size_desc limit:1000
package search

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/usevitrine/vitrine/internal/media"
)

// Query is a whole search.
type Query struct {
	Terms []Term // all must match; no terms = every file
	Sort  Sort
	Limit int // keep only the first Limit results in sort order; 0 = all
}

// Term matches if any of its atoms matches. One atom = a plain chip; several
// = an OR group.
type Term struct {
	Any []Atom
}

// Kind is what an Atom tests.
type Kind uint8

const (
	KindTag      Kind = iota // a tag (or every namespace's tag of that name)
	KindWildcard             // tags matching a pattern with '*'
	KindSystem               // a system filter
	KindTitle                // "/words": comics whose title has the words (never a tag); elsewhere the tag "/words"
)

// Atom is one test.
type Atom struct {
	Negate bool
	Kind   Kind
	// Text is the tag ("name" or "namespace:name"), the wildcard pattern, or the title's words (without the "/").
	Text string
	// Sys is the filter for KindSystem.
	Sys System
}

// Field is what a system filter or sort looks at.
type Field uint8

const (
	FieldNone Field = iota
	FieldImported
	FieldModified
	FieldSize
	FieldWidth
	FieldHeight
	FieldDuration
	FieldTagCount
	FieldHasAudio
	FieldType
	FieldRating
	FieldPosted   // when posted on its site (upload time), else import time
	FieldSite     // the site a file came from (system:site=safebooru.org)
	FieldFavorite // a favorite (filter: system:favorite; sort: when it became one)
	FieldClimax   // times counted with the viewer's "I came" button (system:came>0, sort:came)
	FieldPages    // comics: how many pages (system:pages>20, sort:pages)
	FieldRead     // comics: unread / reading / read (system:unread ...; sort: last read)
	FieldFoundBy  // the subscription that downloaded a file (system:found_by=12; Num: its ID)
	FieldTitle    // comics: sort only, by title
	FieldRandom   // sort only; keep last (sizes per-field arrays)
)

// Op compares a field with a value.
type Op uint8

const (
	OpNone Op = iota
	OpLt
	OpLe
	OpGt
	OpGe
	OpEq
	OpNe
)

var opText = [...]string{OpNone: "", OpLt: "<", OpLe: "<=", OpGt: ">", OpGe: ">=", OpEq: "=", OpNe: "!="}

func (o Op) String() string { return opText[o] }

// System is a system filter.
type System struct {
	Field Field
	Op    Op
	// Num is the value for size (bytes), width/height (px), duration (ms) and
	// tag count.
	Num int64
	// For imported/modified: either Age (relative: "imported<5d" = less than
	// 5 days ago - the operator compares the age) or Time (a point in time:
	// "imported>2026-01-01" = after that day; "=" means that whole day).
	// Time..Until is the span the written date covers: a whole day for
	// "2026-01-01", one minute for "2026-01-01T15:04".
	Age         time.Duration
	Time, Until time.Time
	// AgeText / TimeText keep the value as written, for String.
	AgeText, TimeText string
	// Types for FieldType (any of them).
	Types []media.Type
	// TypeText keeps the type list as written, e.g. "video,gif".
	TypeText string
	// Rating for FieldRating.
	Rating media.Rating
	// For FieldRead, Num is ReadUnread, ReadReading or ReadDone.
	// Site for FieldSite: a site's address or plugin ID, lower case; several
	// separated by commas (from any of them).
	Site string
}

// Comics' read states (System.Num for FieldRead).
const (
	ReadUnread  = 0 // never opened
	ReadReading = 1 // opened, not finished
	ReadDone    = 2 // read to the last page
)

var readStates = [...]string{ReadUnread: "unread", ReadReading: "reading", ReadDone: "read"}

// Sort is a result order.
type Sort struct {
	Field Field // FieldImported (default), FieldModified, FieldSize, ... or FieldRandom
	Asc   bool  // default is descending (newest, biggest, longest first)
	Seed  int64 // for FieldRandom: the same seed gives the same order
}

// DefaultSort is newest imports first.
var DefaultSort = Sort{Field: FieldImported}

// fieldNames are the canonical names used in text; aliases are in parse.go.
var fieldNames = map[Field]string{
	FieldImported: "imported",
	FieldModified: "modified",
	FieldSize:     "size",
	FieldWidth:    "width",
	FieldHeight:   "height",
	FieldDuration: "duration",
	FieldTagCount: "tags",
	FieldHasAudio: "has_audio",
	FieldType:     "type",
	FieldRating:   "rating",
	FieldSite:     "site",
	FieldPosted:   "uploaded",
	FieldFavorite: "favorite",
	FieldClimax:   "came",
	FieldPages:    "pages",
	FieldRead:     "read",
	FieldFoundBy:  "found_by",
	FieldTitle:    "title",
	FieldRandom:   "random",
}

// String writes the query in the text syntax; Parse reads it back.
func (q Query) String() string {
	var parts []string
	for _, t := range q.Terms {
		parts = append(parts, t.String())
	}
	if q.Sort != DefaultSort && q.Sort.Field != FieldNone {
		parts = append(parts, q.Sort.String())
	}
	if q.Limit > 0 {
		parts = append(parts, "limit:"+strconv.Itoa(q.Limit))
	}
	return strings.Join(parts, " ")
}

func (t Term) String() string {
	if len(t.Any) == 1 {
		return t.Any[0].String()
	}
	parts := make([]string, len(t.Any))
	for i, a := range t.Any {
		parts[i] = a.String()
	}
	return "( " + strings.Join(parts, " ~ ") + " )"
}

func (a Atom) String() string {
	s := a.Text
	switch a.Kind {
	case KindSystem:
		s = a.Sys.String()
	case KindTitle:
		s = "/" + a.Text
	}
	if a.Negate {
		return "-" + s
	}
	return s
}

func (s System) String() string {
	name := "system:" + fieldNames[s.Field]
	switch s.Field {
	case FieldHasAudio, FieldFavorite:
		return name
	case FieldRead:
		return "system:" + readStates[s.Num]
	case FieldImported, FieldModified, FieldPosted:
		if s.AgeText != "" {
			return name + s.Op.String() + s.AgeText
		}
		return name + s.Op.String() + s.TimeText
	case FieldSize:
		return name + s.Op.String() + formatSize(s.Num)
	case FieldDuration:
		return name + s.Op.String() + formatDuration(s.Num)
	case FieldType:
		return name + s.Op.String() + s.TypeText
	case FieldRating:
		return name + s.Op.String() + s.Rating.String()
	case FieldSite:
		return name + s.Op.String() + s.Site
	case FieldFoundBy:
		return name + s.Op.String() + strconv.FormatInt(s.Num, 10)
	}
	return name + s.Op.String() + strconv.FormatInt(s.Num, 10)
}

func (s Sort) String() string {
	out := "sort:" + fieldNames[s.Field]
	if s.Field == FieldRandom {
		if s.Seed != 0 {
			out += "_" + strconv.FormatInt(s.Seed, 10)
		}
		return out
	}
	if s.Asc {
		return out + "_asc"
	}
	return out
}

// formatSize writes bytes with the largest unit that reads back exactly
// (at most 3 decimals), e.g. 1.5GB.
func formatSize(n int64) string {
	for _, u := range sizeUnits {
		if n < u.mult {
			continue
		}
		v := float64(n) / float64(u.mult)
		s := strconv.FormatFloat(v, 'f', -1, 64)
		if _, frac, _ := strings.Cut(s, "."); len(frac) <= 3 && int64(math.Round(v*float64(u.mult))) == n {
			return s + u.name
		}
	}
	return strconv.FormatInt(n, 10) + "B"
}

var sizeUnits = []struct {
	name string
	mult int64
}{{"GB", 1 << 30}, {"MB", 1 << 20}, {"KB", 1 << 10}}

func formatDuration(ms int64) string {
	if ms%1000 != 0 {
		return strconv.FormatInt(ms, 10) + "ms"
	}
	s := ms / 1000
	switch {
	case s >= 3600 && s%3600 == 0:
		return fmt.Sprintf("%dh", s/3600)
	case s >= 60 && s%60 == 0:
		return fmt.Sprintf("%dm", s/60)
	}
	return fmt.Sprintf("%ds", s)
}
