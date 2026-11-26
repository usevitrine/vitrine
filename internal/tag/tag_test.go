package tag

import "testing"

func TestSplit(t *testing.T) {
	isNS := func(s string) bool { return s == "creator" || s == "series" }
	cases := map[string]Tag{
		"creator:some_artist": {"creator", "some_artist"},
		"9:16":                {"", "9:16"},
		"rating:safe":         {"", "rating:safe"}, // not a namespace: verbatim
		"creator:":            {"", "creator:"},
		":x":                  {"", ":x"},
		"plain":               {"", "plain"},
		"series:a:b":          {"series", "a:b"},
	}
	for in, want := range cases {
		if got := Split(in, isNS); got != want {
			t.Errorf("Split(%q) = %+v, want %+v", in, got, want)
		}
		if got := Split(in, isNS); got.String() != in {
			t.Errorf("String of Split(%q) = %q", in, got.String())
		}
	}
	if got := Split("creator:x", nil); got != (Tag{Name: "creator:x"}) {
		t.Errorf("nil namespace func: %+v", got)
	}
	if Normalize("  Blue_SKY ") != "blue_sky" {
		t.Error("Normalize")
	}
}
