package plugin

import (
	"slices"
	"testing"
)

func TestSourceLinks(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"https://example.com/a", []string{"https://example.com/a"}},
		{"Twitter: https://twitter.com/some_artist/status/1 Patreon: https://patreon.com/some_artist",
			[]string{"https://twitter.com/some_artist/status/1", "https://patreon.com/some_artist"}},
		{"x.com/some_artist/status/2", []string{"https://x.com/some_artist/status/2"}},
		{"(https://example.com/b), https://example.com/b", []string{"https://example.com/b"}},
		{"Twitter: Patreon:", nil},
		{"x.com", nil},
		{"some words only", nil},
		{"ftp://example.com/c javascript:alert(1) mailto:someone@example.com", nil},
		{"", nil},
	}
	for _, c := range cases {
		if got := SourceLinks(c.in); !slices.Equal(got, c.want) {
			t.Errorf("SourceLinks(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTags(t *testing.T) {
	got := Tags("kai&#039;sa  &lt;1_second_video blue_sky :&gt;=")
	want := []string{"kai'sa", "<1_second_video", "blue_sky", ":>="}
	if !slices.Equal(got, want) {
		t.Errorf("Tags = %q, want %q", got, want)
	}
}
