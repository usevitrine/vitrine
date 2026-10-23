package plugin

import (
	"html"
	"net/url"
	"strings"
)

// SourceLinks picks the links out of a post's source text. Sites keep it as
// free text: usually one URL, but also several, labeled ("Twitter:
// https://... Patreon: https://..."), or a bare "x.com/..." without its scheme.
// Labels and other words are dropped; a bare address gets https://. Each link
// once, in order.
func SourceLinks(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, w := range strings.Fields(text) {
		w = strings.Trim(w, `"'<>()[]{},;`)
		if !strings.Contains(w, "://") {
			// A bare address: a host with a dot, then a path ("x.com/someone";
			// a lone "x.com" says nothing).
			host, path, _ := strings.Cut(w, "/")
			if path == "" || !strings.Contains(host, ".") || strings.HasSuffix(host, ".") || strings.ContainsAny(host, ":@") {
				continue
			}
			w = "https://" + w
		}
		u, err := url.Parse(w)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || !strings.Contains(u.Host, ".") {
			continue
		}
		if !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	return out
}

// Tags splits a post's tag list. Some sites send tags with HTML character
// codes in them (some Gelbooru 0.2 APIs: "kai&#039;sa" for "kai'sa"); they're decoded.
func Tags(list string) []string {
	out := strings.Fields(list)
	for i, t := range out {
		out[i] = CleanTag(t)
	}
	return out
}

// CleanTag decodes HTML character codes in a site's tag ("&#039;" -> "'").
func CleanTag(t string) string {
	if strings.ContainsRune(t, '&') {
		return html.UnescapeString(t)
	}
	return t
}
