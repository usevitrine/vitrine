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

func TestParseMD5(t *testing.T) {
	good := "00112233445566778899aabbccddeeff"
	if got := ParseMD5(good); got[0] != 0x00 || got[1] != 0x11 || got[15] != 0xff {
		t.Errorf("ParseMD5(%q) = %x", good, got)
	}
	for _, bad := range []string{"", "bad", good[:30], good + "00", good[:31], "zz" + good[2:]} {
		if got := ParseMD5(bad); got != [16]byte{} {
			t.Errorf("ParseMD5(%q) = %x, want zero", bad, got)
		}
	}
}

// Shimmie2's two md5 reads had no length check of their own (only their
// patterns' {32}); through ParseMD5 a malformed name gives no md5 either way.
func TestShimmieMalformedMD5(t *testing.T) {
	m := Manifest{Site: "shimmie_test"}
	for _, md := range []string{"0123456789abcdef0123456789abcde", "0123456789abcdef0123456789abcdef0", "0123456789abcdef0123456789abcdeg"} {
		page := `<html><body><img alt='main image' class='shm-main-image' id='main_image' src='/_images/` + md + `/1%20-%20tagme.png'></body></html>`
		p := parseShimmiePost(m, 1, page)
		if p == nil || p.FileURL == "" {
			t.Fatalf("md5 %s: no post from the page", md)
		}
		if p.MD5 != [16]byte{} {
			t.Errorf("md5 %s: read as %x, want none", md, p.MD5)
		}
	}
}
