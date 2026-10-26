//go:build js && wasm

// Command demowasm is the website's demo search engine (GOOS=js GOARCH=wasm): this program's own index and query
// parser in the browser, built from a demo.Snapshot through the index's ordinary calls (as files are imported). The
// web app's demo backend (web/src/demo/backend.ts) asks it what the server's handlers would answer: a search and its
// slices, suggestions, tag edits, favorites, deletes - each in and out as JSON. Nothing is kept beyond the page.
package main

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"syscall/js"
	"time"

	"github.com/usevitrine/vitrine/internal/demo"
	"github.com/usevitrine/vitrine/internal/index"
	"github.com/usevitrine/vitrine/internal/search"
	"github.com/usevitrine/vitrine/internal/tag"
)

var (
	ix      *index.Index
	files   = map[uint32]*demo.File{}
	deleted = map[uint32]bool{}
	tagKey  = map[string]uint32{} // namespace ID + "|" + lower-cased name -> tag ID
	tagText = map[uint32]string{} // tag ID -> "namespace:name" (as the app shows it)
	nsID    = map[string]uint32{}
	nextTag uint32
	results = map[string][]uint32{}
	nextRes int
)

func key(ns uint32, name string) string { return strconv.Itoa(int(ns)) + "|" + strings.ToLower(name) }

func load(data string) error {
	var s demo.Snapshot
	if err := json.Unmarshal([]byte(data), &s); err != nil {
		return err
	}
	ix = index.New()
	nsNames := map[uint32]string{}
	for _, n := range s.Namespaces {
		ix.AddNamespace(n.ID, n.Name)
		nsNames[n.ID] = n.Name
		nsID[strings.ToLower(n.Name)] = n.ID
	}
	ix.AddTags(s.Tags...)
	for _, t := range s.Tags {
		tagKey[key(t.NamespaceID, t.Name)] = t.ID
		tagText[t.ID] = tag.Tag{Namespace: nsNames[t.NamespaceID], Name: t.Name}.String()
		nextTag = max(nextTag, t.ID)
	}
	for _, x := range s.Sites {
		ix.AddSiteAlias(x.Name, x.ID)
	}
	for i := range s.Files {
		f := &s.Files[i]
		if err := ix.AddFile(f.Index, f.Tags); err != nil {
			return err
		}
		for _, site := range f.Sites {
			ix.AddFileSite(f.Index.ID, site)
		}
		files[f.Index.ID] = f
	}
	return nil
}

// searchFiles is GET /api/search (internal/server/api.go's search): the result kept for its slices.
func searchFiles(p struct {
	Q      string `json:"q"`
	Limit  int    `json:"limit"`
	Shapes bool   `json:"shapes"`
	Find   string `json:"find"`
}) (map[string]any, error) {
	q, err := search.Parse(p.Q)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	res := ix.Search(q, start)
	nextRes++
	id := "d" + strconv.Itoa(nextRes)
	results[id] = res.IDs
	if len(results) > 30 { // (the oldest results go, as the server's expire)
		delete(results, "d"+strconv.Itoa(nextRes-30))
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 200
	}
	out := map[string]any{
		"result": id, "total": len(res.IDs), "matched": res.Total, "ids": orEmpty(res.IDs[:min(limit, len(res.IDs))]),
		"unknown": orEmpty(res.Unknown), "seed": res.Seed, "took_ms": float64(time.Since(start).Microseconds()) / 1000,
		"query": q.String(), "now": start.UnixMilli(), "new": 0, "pinned": 0,
	}
	if p.Find != "" {
		want := map[uint32]bool{}
		for _, s := range strings.Split(p.Find, ",") {
			if n, err := strconv.ParseUint(strings.TrimSpace(s), 10, 32); err == nil && len(want) < 8 {
				want[uint32(n)] = true
			}
		}
		found := map[string]int{}
		for i, id := range res.IDs {
			if want[id] {
				found[strconv.FormatUint(uint64(id), 10)] = i
			}
		}
		out["found"] = found
	}
	if p.Shapes {
		sh := ix.Shapes(res.IDs)
		b := make([]byte, 2*len(sh))
		for i, v := range sh {
			binary.LittleEndian.PutUint16(b[2*i:], v)
		}
		out["shapes"] = base64.StdEncoding.EncodeToString(b)
	}
	return out, nil
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// tagID finds a tag typed in the app ("namespace:name" or "name"), adding it to the index if it's new.
func tagID(text string) (uint32, string, bool) {
	t := tag.Split(tag.Normalize(text), ix.IsNamespace)
	if t.Name == "" {
		return 0, "", false
	}
	ns := nsID[t.Namespace]
	if id, ok := tagKey[key(ns, t.Name)]; ok {
		return id, tagText[id], true
	}
	nextTag++
	ix.AddTags(index.Tag{ID: nextTag, NamespaceID: ns, Name: t.Name})
	tagKey[key(ns, t.Name)] = nextTag
	tagText[nextTag] = t.String()
	return nextTag, tagText[nextTag], true
}

// editTags is POST /api/tags: the files' own tags changed - in the index, and in their details (added ones as the
// user's).
func editTags(ids []uint32, add, remove []string) error {
	now := time.Now().UnixMilli()
	for _, id := range ids {
		f := files[id]
		if f == nil {
			continue
		}
		var details struct {
			Tags    []map[string]any `json:"tags"`
			URLs    json.RawMessage  `json:"urls"`
			Sources json.RawMessage  `json:"sources"`
		}
		if err := json.Unmarshal(f.Details, &details); err != nil {
			return err
		}
		var added, removed []uint32
		for _, text := range add {
			t, shown, ok := tagID(text)
			if !ok || slices.Contains(f.Tags, t) {
				continue
			}
			f.Tags = append(f.Tags, t)
			added = append(added, t)
			details.Tags = append(details.Tags, map[string]any{"tag": shown, "origins": []string{"user"}})
		}
		for _, text := range remove {
			t, shown, ok := tagID(text)
			if !ok || !slices.Contains(f.Tags, t) {
				continue
			}
			f.Tags = slices.DeleteFunc(f.Tags, func(x uint32) bool { return x == t })
			removed = append(removed, t)
			details.Tags = slices.DeleteFunc(details.Tags, func(m map[string]any) bool { return m["tag"] == shown })
		}
		if len(added)+len(removed) == 0 {
			continue
		}
		slices.SortFunc(details.Tags, func(a, b map[string]any) int { return strings.Compare(a["tag"].(string), b["tag"].(string)) })
		if !deleted[id] {
			if err := ix.UpdateTags(id, added, removed, now); err != nil {
				return err
			}
		}
		b, err := json.Marshal(details)
		if err != nil {
			return err
		}
		f.Details = b
	}
	return nil
}

// tagsOf is POST /api/tags/of: the tags the files have, each with how many of them.
func tagsOf(ids []uint32) map[string]any {
	count := map[uint32]int{}
	for _, id := range ids {
		if f := files[id]; f != nil {
			for _, t := range f.Tags {
				count[t]++
			}
		}
	}
	type tc struct {
		Tag   string `json:"tag"`
		Count int    `json:"count"`
	}
	out := make([]tc, 0, len(count))
	for t, n := range count {
		out = append(out, tc{tagText[t], n})
	}
	slices.SortFunc(out, func(a, b tc) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return strings.Compare(a.Tag, b.Tag)
	})
	return map[string]any{"files": len(ids), "tags": out}
}

// info is a file as GET /api/files gives it (favorite as it is now).
func info(id uint32) (map[string]any, bool) {
	f := files[id]
	if f == nil {
		return nil, false
	}
	var m map[string]any
	if json.Unmarshal(f.Info, &m) != nil {
		return nil, false
	}
	if ix.IsFavorite(id) {
		m["favorite"] = true
	}
	return m, true
}

func ids(v js.Value) []uint32 {
	var out []uint32
	json.Unmarshal([]byte(v.String()), &out)
	return out
}

func strs(v js.Value) []string {
	var out []string
	json.Unmarshal([]byte(v.String()), &out)
	return out
}

// reply turns an answer into JSON for the page ({error} when it failed).
func reply(v any, err error) any {
	if err != nil {
		v = map[string]string{"error": err.Error()}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func main() {
	api := map[string]func(args []js.Value) any{
		"load": func(a []js.Value) any { return reply(map[string]bool{"ok": true}, load(a[0].String())) },
		"search": func(a []js.Value) any {
			var p struct {
				Q      string `json:"q"`
				Limit  int    `json:"limit"`
				Shapes bool   `json:"shapes"`
				Find   string `json:"find"`
			}
			if err := json.Unmarshal([]byte(a[0].String()), &p); err != nil {
				return reply(nil, err)
			}
			return reply(searchFiles(p))
		},
		"slice": func(a []js.Value) any {
			list, ok := results[a[0].String()]
			if !ok {
				return reply(nil, fmt.Errorf("result expired, search again"))
			}
			off := min(max(a[1].Int(), 0), len(list))
			end := min(off+max(a[2].Int(), 0), len(list))
			return reply(map[string]any{"total": len(list), "offset": off, "ids": orEmpty(list[off:end])}, nil)
		},
		// GET /api/results/{id}/dates (the fast scroller's): n files spread over the result, each one's date.
		"dates": func(a []js.Value) any {
			list, ok := results[a[0].String()]
			if !ok {
				return reply(nil, fmt.Errorf("result expired, search again"))
			}
			n := min(max(a[1].Int(), 2), 2000, len(list))
			sample := make([]uint32, n)
			for k := range n {
				pos := 0
				if n > 1 {
					pos = k * (len(list) - 1) / (n - 1)
				}
				sample[k] = list[pos]
			}
			return reply(map[string]any{"total": len(list), "dates": orEmpty(ix.Times(sample, a[2].Bool()))}, nil)
		},
		"autocomplete": func(a []js.Value) any {
			return reply(map[string]any{"suggestions": orEmpty(ix.Autocomplete(a[0].String(), a[1].Int(), "")), "took_ms": 0}, nil)
		},
		"files": func(a []js.Value) any {
			out := []map[string]any{}
			for _, id := range ids(a[0]) {
				if m, ok := info(id); ok {
					out = append(out, m)
				}
			}
			return reply(map[string]any{"files": out}, nil)
		},
		"details": func(a []js.Value) any {
			f := files[uint32(a[0].Int())]
			if f == nil {
				return reply(nil, fmt.Errorf("no such file"))
			}
			return string(f.Details)
		},
		"editTags": func(a []js.Value) any {
			list := ids(a[0])
			return reply(map[string]int{"updated": len(list)}, editTags(list, strs(a[1]), strs(a[2])))
		},
		"tagsOf": func(a []js.Value) any { return reply(tagsOf(ids(a[0])), nil) },
		"favorite": func(a []js.Value) any {
			n, at := 0, int64(0)
			if a[1].Bool() {
				at = time.Now().UnixMilli()
			}
			for _, id := range ids(a[0]) {
				if files[id] != nil && ix.IsFavorite(id) != a[1].Bool() {
					ix.SetFavorite(id, at)
					n++
				}
			}
			return reply(map[string]int{"changed": n}, nil)
		},
		"remove": func(a []js.Value) any {
			list := ids(a[0])
			for _, id := range list {
				if f := files[id]; f != nil && !deleted[id] {
					if err := ix.RemoveFile(id, f.Tags); err != nil {
						return reply(nil, err)
					}
					deleted[id] = true
				}
			}
			return reply(map[string]int{"deleted": len(list)}, nil)
		},
		"restore": func(a []js.Value) any {
			list := ids(a[0])
			for _, id := range list {
				if f := files[id]; f != nil && deleted[id] {
					if err := ix.AddFile(f.Index, f.Tags); err != nil {
						return reply(nil, err)
					}
					for _, site := range f.Sites {
						ix.AddFileSite(id, site)
					}
					delete(deleted, id)
				}
			}
			return reply(map[string]int{"restored": len(list)}, nil)
		},
		"namespaces": func(a []js.Value) any { return reply(map[string]any{"namespaces": orEmpty(ix.Namespaces())}, nil) },
	}
	obj := js.Global().Get("Object").New()
	for name, fn := range api {
		obj.Set(name, js.FuncOf(func(this js.Value, args []js.Value) any { return fn(args) }))
	}
	js.Global().Set("vitrineDemo", obj)
	js.Global().Call("dispatchEvent", js.Global().Get("Event").New("vitrine-demo-ready"))
	select {} // (answers calls from the page for as long as it's open)
}
