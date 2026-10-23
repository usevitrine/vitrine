package search

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/usevitrine/vitrine/internal/media"
	"github.com/usevitrine/vitrine/internal/tag"
)

// Parse reads a search typed as text. Words are separated by spaces:
//
//	tag             files with the tag (in any namespace, unless written ns:tag)
//	-tag            files without it
//	*part*          any tag matching the pattern ('*' = any text); ns:* = any tag in ns
//	( a ~ b ~ -c )  an OR group: files matching any member
//	system:...      a filter: imported, uploaded, modified, size, width, height, duration,
//	                tags (tag count), has_audio, favorite, type, rating - e.g.
//	                system:imported<5d  system:size>=1.5GB  system:duration>30s
//	                system:type=video,gif  system:rating!=explicit
//	sort:field      result order: imported, uploaded (when posted on its
//	                site), modified, size, width, height, duration, tags,
//	                favorite (when it became one), random; add _asc for ascending
//	limit:N         only the first N results
//
// Dates for imported/modified are either an age (5d = 5 days ago; units s, m,
// h, d, w, mo, y) where the operator compares the age, or a date
// (2026-01-31 or 2026-01-31T15:04, local time) where it compares the time.
func Parse(text string) (Query, error) {
	return parseAt(text, time.Local)
}

func parseAt(text string, loc *time.Location) (Query, error) {
	q := Query{Sort: DefaultSort}
	toks := strings.Fields(text)
	for i := 0; i < len(toks); i++ {
		tok := toks[i]
		switch {
		case tok == "(":
			var group []Atom
			want := true // expecting an atom
			closed := false
			for i++; i < len(toks); i++ {
				t := toks[i]
				if t == ")" {
					closed = true
					break
				}
				if want {
					if t == "~" || t == "(" {
						return q, fmt.Errorf("unexpected %q in OR group", t)
					}
					a, err := parseAtom(t, loc)
					if err != nil {
						return q, err
					}
					group = append(group, a)
				} else if t != "~" {
					return q, fmt.Errorf("expected ~ or ) after %q in OR group", toks[i-1])
				}
				want = !want
			}
			if !closed {
				return q, fmt.Errorf("OR group is missing its closing )")
			}
			if want && len(group) > 0 {
				return q, fmt.Errorf("OR group ends with ~")
			}
			if len(group) > 0 {
				q.Terms = append(q.Terms, Term{Any: group})
			}
		case tok == ")" || tok == "~":
			return q, fmt.Errorf("unexpected %q outside an OR group", tok)
		case hasPrefixFold(tok, "sort:") || hasPrefixFold(tok, "order:"):
			_, v, _ := strings.Cut(tok, ":")
			s, err := parseSort(strings.ToLower(v))
			if err != nil {
				return q, err
			}
			q.Sort = s
		case hasPrefixFold(tok, "limit:") || hasPrefixFold(tok, "system:limit="):
			v := tok[strings.LastIndexAny(tok, ":=")+1:]
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return q, fmt.Errorf("bad limit %q", v)
			}
			q.Limit = n
		default:
			a, err := parseAtom(tok, loc)
			if err != nil {
				return q, err
			}
			q.Terms = append(q.Terms, Term{Any: []Atom{a}})
		}
	}
	return q, nil
}

func hasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

func parseAtom(tok string, loc *time.Location) (Atom, error) {
	var a Atom
	if len(tok) > 1 && tok[0] == '-' {
		a.Negate = true
		tok = tok[1:]
	}
	switch {
	case hasPrefixFold(tok, "system:"):
		s, err := parseSystem(strings.ToLower(tok[len("system:"):]), loc)
		if err != nil {
			return a, err
		}
		a.Kind, a.Sys = KindSystem, s
	case len(tok) > 1 && tok[0] == '/':
		a.Kind, a.Text = KindTitle, tag.Normalize(tok[1:])
	case strings.Contains(tok, "*"):
		a.Kind, a.Text = KindWildcard, tag.Normalize(tok)
	default:
		a.Kind, a.Text = KindTag, tag.Normalize(tok)
	}
	return a, nil
}

var fieldAliases = map[string]Field{
	"imported": FieldImported, "import": FieldImported, "import_time": FieldImported,
	"modified": FieldModified, "modified_time": FieldModified,
	"size": FieldSize, "filesize": FieldSize,
	"width": FieldWidth, "height": FieldHeight,
	"duration": FieldDuration, "length": FieldDuration,
	"tags": FieldTagCount, "tag_count": FieldTagCount, "tagcount": FieldTagCount,
	"has_audio": FieldHasAudio, "audio": FieldHasAudio,
	"type": FieldType, "filetype": FieldType,
	"rating":   FieldRating,
	"site":     FieldSite,
	"found_by": FieldFoundBy,
	"uploaded": FieldPosted, "upload": FieldPosted, "posted": FieldPosted, "source_time": FieldPosted,
	"random":   FieldRandom,
	"favorite": FieldFavorite, "favourite": FieldFavorite, "fav": FieldFavorite, "favorites": FieldFavorite,
	"favourites": FieldFavorite, "favorited": FieldFavorite, "favourited": FieldFavorite,
	"came": FieldClimax, "climax": FieldClimax, "climaxes": FieldClimax,
	"pages": FieldPages, "page_count": FieldPages,
	"read": FieldRead, "title": FieldTitle,
}

// readAliases are the comics' read-state filters (FieldRead).
var readAliases = map[string]int64{
	"unread": ReadUnread, "reading": ReadReading, "in_progress": ReadReading, "read": ReadDone, "finished": ReadDone,
}

// splitOp splits "imported<=5d" into "imported", OpLe, "5d".
func splitOp(s string) (string, Op, string) {
	i := strings.IndexAny(s, "<>=!")
	if i < 0 {
		return s, OpNone, ""
	}
	name, rest := s[:i], s[i:]
	for _, c := range []struct {
		text string
		op   Op
	}{{"<=", OpLe}, {">=", OpGe}, {"!=", OpNe}, {"<", OpLt}, {">", OpGt}, {"=", OpEq}} {
		if strings.HasPrefix(rest, c.text) {
			return name, c.op, rest[len(c.text):]
		}
	}
	return name, OpNone, rest
}

func parseSystem(s string, loc *time.Location) (System, error) {
	name, op, val := splitOp(s)
	f, ok := fieldAliases[name]
	if state, ok := readAliases[name]; ok && op == OpNone {
		// system:unread, system:reading, system:read (comics)
		return System{Field: FieldRead, Op: OpEq, Num: state}, nil
	}
	if !ok || f == FieldRandom || f == FieldTitle || f == FieldRead {
		return System{}, fmt.Errorf("unknown system filter %q", "system:"+s)
	}
	sys := System{Field: f, Op: op}
	if f == FieldHasAudio || f == FieldFavorite {
		if op != OpNone {
			return sys, fmt.Errorf("system:%s takes no value (use -system:%s for files without)", fieldNames[f], fieldNames[f])
		}
		return sys, nil
	}
	if op == OpNone || val == "" {
		return sys, fmt.Errorf("system:%s needs a comparison, e.g. system:%s>10", name, name)
	}

	var err error
	switch f {
	case FieldImported, FieldModified, FieldPosted:
		err = parseTimeValue(&sys, val, loc)
	case FieldSize:
		sys.Num, err = parseSize(val)
	case FieldDuration:
		sys.Num, err = parseDurationMS(val)
	case FieldWidth, FieldHeight, FieldTagCount, FieldClimax, FieldPages:
		sys.Num, err = strconv.ParseInt(val, 10, 64)
		if err == nil && sys.Num < 0 {
			err = fmt.Errorf("negative")
		}
	case FieldType:
		if op != OpEq && op != OpNe {
			return sys, fmt.Errorf("system:type only takes = or !=")
		}
		for _, v := range strings.Split(val, ",") {
			ts, ok := typeNames[v]
			if !ok {
				return sys, fmt.Errorf("unknown file type %q", v)
			}
			sys.Types = append(sys.Types, ts...)
		}
		sys.TypeText = val
	case FieldRating:
		if op != OpEq && op != OpNe {
			return sys, fmt.Errorf("system:rating only takes = or !=")
		}
		r, ok := media.ParseRating(val)
		if !ok {
			return sys, fmt.Errorf("unknown rating %q (general, sensitive, questionable, explicit)", val)
		}
		sys.Rating = r
	case FieldSite:
		if op != OpEq && op != OpNe {
			return sys, fmt.Errorf("system:site only takes = or !=")
		}
		sys.Site = strings.ToLower(val)
	case FieldFoundBy:
		if op != OpEq && op != OpNe {
			return sys, fmt.Errorf("system:found_by only takes = or !=")
		}
		sys.Num, err = strconv.ParseInt(val, 10, 64)
	}
	if err != nil {
		return sys, fmt.Errorf("bad value in system:%s: %q", name, val)
	}
	return sys, nil
}

var typeNames = func() map[string][]media.Type {
	m := map[string][]media.Type{"image": nil, "video": nil}
	for t := media.JPEG; t <= media.Matroska; t++ {
		m[t.String()] = []media.Type{t}
		m[t.Ext()] = []media.Type{t}
		if t.IsImage() {
			m["image"] = append(m["image"], t)
		} else {
			m["video"] = append(m["video"], t)
		}
	}
	m["jpeg"] = []media.Type{media.JPEG}
	m["mov"] = []media.Type{media.QuickTime}
	m["mkv"] = []media.Type{media.Matroska}
	return m
}()

var ageUnits = map[string]time.Duration{
	"s": time.Second, "m": time.Minute, "min": time.Minute, "h": time.Hour,
	"d": 24 * time.Hour, "w": 7 * 24 * time.Hour, "mo": 30 * 24 * time.Hour, "y": 365 * 24 * time.Hour,
}

func parseTimeValue(sys *System, val string, loc *time.Location) error {
	for _, layout := range []struct {
		layout string
		span   func(time.Time) time.Time
	}{
		{"2006-01-02", func(t time.Time) time.Time { return t.AddDate(0, 0, 1) }},
		{"2006-01-02T15:04", func(t time.Time) time.Time { return t.Add(time.Minute) }},
	} {
		upper := strings.ToUpper(val) // the query is lower-cased; layouts use "T"
		if t, err := time.ParseInLocation(layout.layout, upper, loc); err == nil {
			sys.Time, sys.Until, sys.TimeText = t, layout.span(t), upper
			return nil
		}
	}
	// An age: a number and a unit.
	i := strings.IndexFunc(val, func(r rune) bool { return (r < '0' || r > '9') && r != '.' })
	if i <= 0 {
		return fmt.Errorf("bad age")
	}
	n, err := strconv.ParseFloat(val[:i], 64)
	unit, ok := ageUnits[val[i:]]
	if err != nil || !ok || n < 0 {
		return fmt.Errorf("bad age")
	}
	if sys.Op == OpEq || sys.Op == OpNe {
		return fmt.Errorf("= needs a date, not an age")
	}
	sys.Age, sys.AgeText = time.Duration(n*float64(unit)), val
	return nil
}

// sizeSuffixes are tried in order: "1.5GB", "1.5G", ..., "100B", "100".
var sizeSuffixes = []struct {
	suffix string
	mult   int64
}{{"GB", 1 << 30}, {"G", 1 << 30}, {"MB", 1 << 20}, {"M", 1 << 20}, {"KB", 1 << 10}, {"K", 1 << 10}, {"B", 1}}

func parseSize(val string) (int64, error) {
	v := strings.ToUpper(val)
	mult := int64(1)
	for _, s := range sizeSuffixes {
		if strings.HasSuffix(v, s.suffix) {
			v, mult = strings.TrimSuffix(v, s.suffix), s.mult
			break
		}
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("bad size")
	}
	return int64(math.Round(n * float64(mult))), nil
}

func parseDurationMS(val string) (int64, error) {
	if n, err := strconv.ParseFloat(val, 64); err == nil && n >= 0 {
		return int64(math.Round(n * 1000)), nil // plain number = seconds
	}
	d, err := time.ParseDuration(val)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("bad duration")
	}
	return d.Milliseconds(), nil
}

func parseSort(v string) (Sort, error) {
	s := Sort{}
	name := v
	switch {
	case strings.HasSuffix(v, "_asc"):
		name, s.Asc = strings.TrimSuffix(v, "_asc"), true
	case strings.HasSuffix(v, "_desc"):
		name = strings.TrimSuffix(v, "_desc")
	case strings.HasPrefix(v, "random_"):
		seed, err := strconv.ParseInt(strings.TrimPrefix(v, "random_"), 10, 64)
		if err != nil {
			return s, fmt.Errorf("bad sort %q", v)
		}
		name, s.Seed = "random", seed
	}
	f, ok := fieldAliases[name]
	switch {
	case !ok, f == FieldHasAudio, f == FieldType, f == FieldRating, f == FieldSite, f == FieldFoundBy:
		return s, fmt.Errorf("can't sort by %q", v)
	}
	s.Field = f
	return s, nil
}
