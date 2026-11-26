// Package i18n gives the server's own texts (push notifications) in the
// user's language, from the same language files as the web app (package
// locales). A text is a string with {name} placeholders, or, when it depends
// on a number, an object of plural forms ({"one": "...", "other": "..."}).
package i18n

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/usevitrine/vitrine/locales"
)

// Main is the language every other one falls back to.
const Main = "en-US"

var (
	once  sync.Once
	langs map[string]map[string]any // language tag -> flattened key -> text
	def   = Main
)

func load() {
	langs = map[string]map[string]any{}
	files, _ := fs.Glob(locales.FS, "*.json")
	for _, f := range files {
		b, err := fs.ReadFile(locales.FS, f)
		if err != nil {
			continue
		}
		var tree map[string]any
		if err := json.Unmarshal(b, &tree); err != nil {
			slog.Error("i18n: a language file doesn't read", "file", f, "err", err)
			continue
		}
		flat := map[string]any{}
		flatten("", tree, flat)
		langs[strings.TrimSuffix(f, ".json")] = flat
	}
}

// flatten turns nested objects into dotted keys; an object of plural forms
// (only one/other/... keys with strings) stays one text.
func flatten(prefix string, tree map[string]any, out map[string]any) {
	for k, v := range tree {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		switch v := v.(type) {
		case string:
			out[key] = v
		case map[string]any:
			if isPlural(v) {
				out[key] = v
			} else {
				flatten(key, v, out)
			}
		}
	}
}

var pluralForms = map[string]bool{"zero": true, "one": true, "two": true, "few": true, "many": true, "other": true}

func isPlural(m map[string]any) bool {
	if _, ok := m["other"]; !ok {
		return false
	}
	for k, v := range m {
		if _, ok := v.(string); !ok || !pluralForms[k] {
			return false
		}
	}
	return true
}

// Available lists the language tags there are files for.
func Available() []string {
	once.Do(load)
	return slices.Sorted(maps.Keys(langs))
}

// SetDefault picks the language used when a device hasn't said its own
// (the VITRINE_LANGUAGE setting). An unknown tag is refused.
func SetDefault(tag string) error {
	once.Do(load)
	if tag == "" {
		def = Main
		return nil
	}
	if _, ok := langs[tag]; !ok {
		return fmt.Errorf("no language file for %q (there are: %s)", tag, strings.Join(Available(), ", "))
	}
	def = tag
	return nil
}

// Default is the language used when a device hasn't said its own.
func Default() string {
	once.Do(load)
	return def
}

// Lang gives texts in one language.
type Lang struct{ tag string }

// For returns a language's texts ("" or an unknown tag: the default).
func For(tag string) Lang {
	once.Do(load)
	if _, ok := langs[tag]; !ok {
		tag = def
	}
	return Lang{tag}
}

// Tag is the language's tag.
func (l Lang) Tag() string { return l.tag }

// T returns a text with its placeholders filled in: vars are name, value
// pairs. A number named "n" picks the plural form. A missing text is its key.
func (l Lang) T(key string, vars ...any) string {
	v, ok := langs[l.tag][key]
	if !ok {
		v, ok = langs[Main][key]
	}
	if !ok {
		return key
	}
	values := map[string]string{}
	n := -1
	for i := 0; i+1 < len(vars); i += 2 {
		name := fmt.Sprint(vars[i])
		switch x := vars[i+1].(type) {
		case int:
			values[name] = l.number(x)
			if name == "n" {
				n = x
			}
		default:
			values[name] = fmt.Sprint(x)
		}
	}
	var s string
	switch v := v.(type) {
	case string:
		s = v
	case map[string]any:
		form := "other"
		if n == 1 { // (the server's texts so far: languages with one/other plurals)
			form = "one"
		}
		if f, ok := v[form].(string); ok {
			s = f
		} else {
			s, _ = v["other"].(string)
		}
	}
	for name, value := range values {
		s = strings.ReplaceAll(s, "{"+name+"}", value)
	}
	return s
}

// number writes n with the language's thousands separator ("_group" in its file; 1,234).
func (l Lang) number(n int) string {
	sep := ","
	if g, ok := langs[l.tag]["_group"].(string); ok {
		sep = g
	}
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + sep + s[i:]
	}
	if neg {
		s = "-" + s
	}
	return s
}
