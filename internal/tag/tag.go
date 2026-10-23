// Package tag names tags: an optional namespace plus a name, written
// "namespace:name".
//
// Site tags are stored verbatim, so a name may itself contain colons (e.g.
// "9:16"). A prefix before a colon is only a namespace if the library knows a
// namespace by that name - callers resolve that with Split.
package tag

import "strings"

// Tag is a namespace and a name. An empty namespace means "no namespace".
type Tag struct {
	Namespace string
	Name      string
}

// String writes the tag as "namespace:name", or just "name".
func (t Tag) String() string {
	if t.Namespace == "" {
		return t.Name
	}
	return t.Namespace + ":" + t.Name
}

// Normalize cleans up a tag typed by the user: surrounding space trimmed and
// lower case. Site tags are not normalized (they are stored verbatim).
func Normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// Split reads "namespace:name". The part before the first colon is taken as
// the namespace only if isNamespace says it is one; otherwise the whole text
// is the name. isNamespace may be nil (no namespaces known).
func Split(s string, isNamespace func(string) bool) Tag {
	if ns, name, ok := strings.Cut(s, ":"); ok && ns != "" && name != "" && isNamespace != nil && isNamespace(ns) {
		return Tag{Namespace: ns, Name: name}
	}
	return Tag{Name: s}
}
