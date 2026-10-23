package search

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/media"
)

func mustParse(t *testing.T, s string) Query {
	t.Helper()
	q, err := parseAt(s, time.UTC)
	if err != nil {
		t.Fatalf("Parse(%q): %v", s, err)
	}
	return q
}

func TestParseTags(t *testing.T) {
	q := mustParse(t, "  Blue_Sky -cloud artist:some_artist *sky* foo_(artist) 9:16 /Some_Title* -/x / ")
	want := []Atom{
		{Kind: KindTag, Text: "blue_sky"},
		{Kind: KindTag, Text: "cloud", Negate: true},
		{Kind: KindTag, Text: "artist:some_artist"},
		{Kind: KindWildcard, Text: "*sky*"},
		{Kind: KindTag, Text: "foo_(artist)"},
		{Kind: KindTag, Text: "9:16"},
		{Kind: KindTitle, Text: "some_title*"},
		{Kind: KindTitle, Text: "x", Negate: true},
		{Kind: KindTag, Text: "/"}, // (a lone slash: a tag)
	}
	if len(q.Terms) != len(want) {
		t.Fatalf("got %d terms", len(q.Terms))
	}
	for i, w := range want {
		if got := q.Terms[i].Any; len(got) != 1 || !reflect.DeepEqual(got[0], w) {
			t.Errorf("term %d = %+v, want %+v", i, got, w)
		}
	}
	if q.Sort != DefaultSort || q.Limit != 0 {
		t.Errorf("sort/limit = %+v/%d", q.Sort, q.Limit)
	}
}

func TestParseOrGroup(t *testing.T) {
	q := mustParse(t, "a ( smiling ~ -quiet ~ system:has_audio ) b")
	if len(q.Terms) != 3 || len(q.Terms[1].Any) != 3 {
		t.Fatalf("got %+v", q.Terms)
	}
	g := q.Terms[1].Any
	if g[0].Text != "smiling" || !g[1].Negate || g[2].Kind != KindSystem || g[2].Sys.Field != FieldHasAudio {
		t.Fatalf("group = %+v", g)
	}

	// A one-member group is a plain term; an empty group disappears.
	q = mustParse(t, "( solo ) ( )")
	if len(q.Terms) != 1 || q.Terms[0].Any[0].Text != "solo" {
		t.Fatalf("got %+v", q.Terms)
	}
}

func TestParseSystem(t *testing.T) {
	cases := map[string]System{
		"system:size>=1.5GB":      {Field: FieldSize, Op: OpGe, Num: 1610612736},
		"system:size<100k":        {Field: FieldSize, Op: OpLt, Num: 102400},
		"system:filesize=10":      {Field: FieldSize, Op: OpEq, Num: 10},
		"system:duration>30s":     {Field: FieldDuration, Op: OpGt, Num: 30000},
		"system:duration<=1m30s":  {Field: FieldDuration, Op: OpLe, Num: 90000},
		"system:length>2.5":       {Field: FieldDuration, Op: OpGt, Num: 2500},
		"system:width>=1920":      {Field: FieldWidth, Op: OpGe, Num: 1920},
		"system:tags<5":           {Field: FieldTagCount, Op: OpLt, Num: 5},
		"system:audio":            {Field: FieldHasAudio},
		"system:imported<5d":      {Field: FieldImported, Op: OpLt, Age: 5 * 24 * time.Hour, AgeText: "5d"},
		"system:modified>=1.5h":   {Field: FieldModified, Op: OpGe, Age: 90 * time.Minute, AgeText: "1.5h"},
		"system:rating!=explicit": {Field: FieldRating, Op: OpNe, Rating: media.RatingExplicit},
		"system:type=video,gif": {Field: FieldType, Op: OpEq, TypeText: "video,gif",
			Types: []media.Type{media.MP4, media.QuickTime, media.WebM, media.Matroska, media.GIF}},
	}
	for text, want := range cases {
		q := mustParse(t, text)
		if got := q.Terms[0].Any[0].Sys; !reflect.DeepEqual(got, want) {
			t.Errorf("%s:\n got %+v\nwant %+v", text, got, want)
		}
	}

	q := mustParse(t, "system:imported=2026-01-31")
	s := q.Terms[0].Any[0].Sys
	if !s.Time.Equal(time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)) || !s.Until.Equal(s.Time.AddDate(0, 0, 1)) {
		t.Errorf("date span %v..%v", s.Time, s.Until)
	}
	q = mustParse(t, "system:imported>2026-01-31T15:04")
	s = q.Terms[0].Any[0].Sys
	if s.Until.Sub(s.Time) != time.Minute {
		t.Errorf("minute span %v..%v", s.Time, s.Until)
	}
}

func TestParseSortLimit(t *testing.T) {
	q := mustParse(t, "a sort:size_asc limit:1000")
	if q.Sort != (Sort{Field: FieldSize, Asc: true}) || q.Limit != 1000 {
		t.Fatalf("got %+v %d", q.Sort, q.Limit)
	}
	q = mustParse(t, "order:duration system:limit=5 sort:random_42")
	if q.Sort != (Sort{Field: FieldRandom, Seed: 42}) || q.Limit != 5 {
		t.Fatalf("got %+v %d", q.Sort, q.Limit)
	}
}

func TestParseErrors(t *testing.T) {
	for _, s := range []string{
		"( a ~ b", "a )", "~", "( a b )", "( a ~ )", "( ~ a )", "( a ~ ( b ) )",
		"system:bogus>1", "system:size", "system:size>abc", "system:width>-1",
		"system:audio=1", "system:type>video", "system:type=pdf", "system:rating>safe",
		"system:rating=s", "system:imported=5d", "system:imported<5x", "system:imported<d",
		"sort:has_audio", "sort:bogus", "limit:0", "limit:x",
	} {
		if _, err := parseAt(s, time.UTC); err == nil {
			t.Errorf("Parse(%q) accepted", s)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	for _, s := range []string{
		"a -b ns:c *wild* /some_title -/other*",
		"( a ~ -b ~ system:has_audio ) -system:has_audio",
		"system:imported<5d system:modified>=2026-01-31 system:imported=2026-01-31T15:04",
		"system:size>=1.5GB system:size<100KB system:size=7B system:duration>1m30s system:duration<90ms",
		"system:width>=1920 system:tags<5 system:type!=video,gif system:rating=general",
		"x sort:size_asc limit:1000",
		"sort:random_7",
	} {
		q := mustParse(t, s)
		text := q.String()
		q2 := mustParse(t, text)
		if !reflect.DeepEqual(q, q2) {
			t.Errorf("round trip of %q via %q differs:\n%+v\n%+v", s, text, q, q2)
		}
		if strings.Contains(s, "1.5GB") && !strings.Contains(text, "1.5GB") {
			t.Errorf("size not kept readable: %q", text)
		}
	}
}
