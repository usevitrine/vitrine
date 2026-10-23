package server

import (
	"bytes"
	"context"
	"net/url"
	"slices"
	"testing"

	"github.com/usevitrine/vitrine/internal/library"
	"github.com/usevitrine/vitrine/internal/tag"
)

func TestComicsAPI(t *testing.T) {
	e := newEnv(t)
	e.loginAs("me", "a long password")
	ctx := context.Background()
	id, _, err := e.lib.AddComic(ctx, library.ComicInfo{Title: "Some comic", Tags: []tag.Tag{{Namespace: "creator", Name: "some_artist"}}})
	if err != nil {
		t.Fatal(err)
	}
	for p := range 3 {
		if _, err := e.lib.AddComicPage(ctx, id, p, bytes.NewReader(pngBytes(t, 40, 60, byte(p)))); err != nil {
			t.Fatal(err)
		}
	}
	var list struct {
		Comics []comicJSON `json:"comics"`
		Total  int         `json:"total"`
	}
	if code := e.call("GET", "/api/comics?q=some_artist", nil, &list); code != 200 || list.Total != 1 || list.Comics[0].Have != 3 {
		t.Fatalf("list: %d %+v", code, list)
	}
	var one struct {
		Comic comicJSON  `json:"comic"`
		Pages []fileJSON `json:"pages"`
	}
	if code := e.call("GET", "/api/comics/"+itoa(uint32(id)), nil, &one); code != 200 || len(one.Pages) != 3 || one.Pages[0].URL == "" {
		t.Fatalf("one: %d %+v", code, one)
	}
	// The library's search doesn't see the pages.
	var res struct {
		Total int `json:"total"`
	}
	e.call("GET", "/api/search?q=", nil, &res)
	if res.Total != 0 {
		t.Fatalf("the library found %d files", res.Total)
	}
	// Reading position and the user's tags.
	if code := e.call("POST", "/api/comics/"+itoa(uint32(id)), map[string]any{"read_page": 2, "tags": []string{"favorite_artists"}}, nil); code != 200 {
		t.Fatalf("update: %d", code)
	}
	e.call("GET", "/api/comics/"+itoa(uint32(id)), nil, &one)
	if one.Comic.ReadPage != 2 || len(one.Comic.Tags) != 1 || one.Comic.Tags[0] != "favorite_artists" { // (an imported comic's tags are the user's: replaced)
		t.Fatalf("after the update: %+v", one.Comic)
	}
	// Delete and undo.
	if code := e.call("POST", "/api/comics/delete", map[string]any{"ids": []int64{id}}, nil); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	e.call("GET", "/api/comics", nil, &list)
	if list.Total != 0 {
		t.Fatalf("after the delete: %d", list.Total)
	}
	e.call("POST", "/api/comics/undelete", map[string]any{"ids": []int64{id}}, nil)
	e.call("GET", "/api/comics", nil, &list)
	if list.Total != 1 {
		t.Fatalf("after undo: %d", list.Total)
	}
	if code := e.call("GET", "/api/comics/999", nil, nil); code != 404 {
		t.Fatalf("unknown comic: %d", code)
	}
}

func TestComicSearch(t *testing.T) {
	e := newEnv(t)
	e.loginAs("me", "a long password")
	ctx := context.Background()
	add := func(title string, pages int, tags ...tag.Tag) int64 {
		t.Helper()
		id, _, err := e.lib.AddComic(ctx, library.ComicInfo{Title: title, Tags: tags})
		if err != nil {
			t.Fatal(err)
		}
		for p := range pages {
			if _, err := e.lib.AddComicPage(ctx, id, p, bytes.NewReader(pngBytes(t, 40, 60, byte(int(id)*10+p)))); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	a := add("Blue sky", 3, tag.Tag{Namespace: "female", Name: "glasses"}, tag.Tag{Namespace: "language", Name: "english"})
	b := add("Another one", 5, tag.Tag{Namespace: "female", Name: "glasses"})
	c := add("Third", 1)
	type result struct {
		Total int      `json:"total"`
		IDs   []uint32 `json:"ids"`
	}
	find := func(q string) []uint32 {
		t.Helper()
		var res result
		if code := e.call("GET", "/api/search?scope=comics&q="+url.QueryEscape(q), nil, &res); code != 200 {
			t.Fatalf("search %q: %d", q, code)
		}
		return res.IDs
	}
	ids := func(list ...int64) []uint32 {
		out := []uint32{}
		for _, id := range list {
			out = append(out, uint32(id))
		}
		return out
	}
	check := func(q string, want ...int64) {
		t.Helper()
		if got := find(q); !slices.Equal(orEmpty(got), ids(want...)) {
			t.Fatalf("%q: %v, want %v", q, got, want)
		}
	}
	check("", c, b, a) // newest added first
	check("female:glasses", b, a)
	check("sky", a) // (not a tag: the titles)
	check("another_one", b)
	check("-language:english female:glasses", b)
	check("system:pages>2 sort:pages", b, a)
	check("sort:title_asc", b, a, c)
	// The library's own search sees none of it.
	var res result
	e.call("GET", "/api/search?q=", nil, &res)
	if res.Total != 0 {
		t.Fatalf("the library found %d files", res.Total)
	}
	// Read state, favorites.
	e.call("POST", "/api/comics/"+itoa(uint32(a)), map[string]any{"read_page": 1}, nil) // (left past the cover: being read)
	if code := e.call("POST", "/api/comics/do/read", map[string]any{"ids": []int64{c}, "read": true}, nil); code != 200 {
		t.Fatalf("read: %d", code)
	}
	check("system:reading", a)
	check("system:read", c)
	check("system:unread", b)
	e.call("POST", "/api/comics/do/favorite", map[string]any{"ids": []int64{b}, "on": true}, nil)
	check("system:favorite", b)
	// Tags for a selection, autocomplete, namespaces, details.
	e.call("POST", "/api/comics/do/tags", map[string]any{"ids": []int64{a, c}, "add": []string{"favorite_artists"}, "remove": []string{"female:glasses"}}, nil)
	check("favorite_artists", a, c) // (a is being read: first)
	check("female:glasses", b)
	var sugg struct {
		Suggestions []struct {
			Tag   string `json:"tag"`
			Count int    `json:"count"`
		} `json:"suggestions"`
	}
	e.call("GET", "/api/autocomplete?scope=comics&q=fav", nil, &sugg)
	if len(sugg.Suggestions) != 1 || sugg.Suggestions[0].Tag != "favorite_artists" || sugg.Suggestions[0].Count != 2 {
		t.Fatalf("autocomplete: %+v", sugg)
	}
	var ns struct {
		Namespaces []string `json:"namespaces"`
	}
	e.call("GET", "/api/namespaces?scope=comics", nil, &ns)
	if !slices.Contains(ns.Namespaces, "female") || !slices.Contains(ns.Namespaces, "language") {
		t.Fatalf("comic namespaces: %v", ns.Namespaces)
	}
	var info struct {
		Comics []comicJSON `json:"comics"`
	}
	e.call("GET", "/api/comics/info?ids="+itoa(uint32(b))+","+itoa(uint32(a)), nil, &info)
	if len(info.Comics) != 2 || info.Comics[0].ID != b || !info.Comics[0].Favorite || info.Comics[1].Have != 3 {
		t.Fatalf("info: %+v", info.Comics)
	}
	// The user's own title (searched; the site's too) and a crop of the cover.
	if code := e.call("POST", "/api/comics/"+itoa(uint32(b)), map[string]any{"title": "My Better Title", "crop": []float64{0, 0.1, 1, 0.5}}, nil); code != 200 {
		t.Fatalf("title, crop: %d", code)
	}
	check("better", b)
	check("another_one", b)
	e.call("GET", "/api/comics/info?ids="+itoa(uint32(b)), nil, &info)
	if got := info.Comics[0]; got.Title != "My Better Title" || got.SiteTitle != "Another one" || len(got.Crop) != 4 || got.Crop[3] != 0.5 {
		t.Fatalf("title, crop: %+v", got)
	}
	e.call("POST", "/api/comics/"+itoa(uint32(b)), map[string]any{"title": "", "crop": []float64{}}, nil)
	info.Comics = nil
	e.call("GET", "/api/comics/info?ids="+itoa(uint32(b)), nil, &info)
	if got := info.Comics[0]; got.Title != "Another one" || got.SiteTitle != "" || got.Crop != nil {
		t.Fatalf("title, crop reset: %+v", got)
	}
	if code := e.call("POST", "/api/comics/"+itoa(uint32(b)), map[string]any{"crop": []float64{0.5, 0, 0.8, 1}}, nil); code != 400 {
		t.Fatalf("a crop past the cover: %d", code)
	}
	// Saved searches: the Comics page's own list.
	e.call("POST", "/api/saved?scope=comics", map[string]any{"name": "Glasses", "query": "female:glasses"}, nil)
	var saved struct {
		Saved []library.SavedSearch `json:"saved"`
	}
	e.call("GET", "/api/saved?scope=comics", nil, &saved)
	if len(saved.Saved) != 1 || saved.Saved[0].Count != 1 {
		t.Fatalf("comics' saved searches: %+v", saved.Saved)
	}
	e.call("GET", "/api/saved", nil, &saved)
	if len(saved.Saved) != 0 {
		t.Fatalf("the library's saved searches: %+v", saved.Saved)
	}
}
