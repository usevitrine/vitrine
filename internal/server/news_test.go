package server

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/tag"
)

// The saved searches menu's NEW pills: a saved search or group tag counts the
// files imported since it was last opened; opening it says when that was.
func TestNews(t *testing.T) {
	e := newEnv(t)
	e.loginAs("me", "correct horse")
	ctx := context.Background()
	imp := func(seed byte) uint32 {
		f, err := e.lib.Import(ctx, bytes.NewReader(pngBytes(t, 50+int(seed), 40, seed)))
		if err != nil {
			t.Fatal(err)
		}
		return f.ID
	}
	f1 := imp(1)
	if err := e.lib.AddTags(ctx, []uint32{f1}, []tag.Tag{{Name: "blue_sky"}}); err != nil {
		t.Fatal(err)
	}
	if err := e.lib.AddParent(ctx, tag.Tag{Name: "blue_sky"}, tag.Tag{Name: "favorite_artists"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.lib.SaveSearch(ctx, "Sky", "blue_sky", "", ""); err != nil {
		t.Fatal(err)
	}
	type item struct {
		ID       int64  `json:"id"`
		Tag      string `json:"tag"`
		Children int    `json:"children"`
		New      int    `json:"new"`
		SeenAt   int64  `json:"seen_at"`
	}
	var news struct{ Saved, Groups []item }
	e.call("GET", "/api/news", nil, &news)
	// Never opened: marked now, nothing new.
	if len(news.Saved) != 1 || news.Saved[0].New != 0 || len(news.Groups) != 1 || news.Groups[0].Tag != "favorite_artists" || news.Groups[0].Children != 1 || news.Groups[0].New != 0 {
		t.Fatalf("first: %+v", news)
	}
	time.Sleep(5 * time.Millisecond)
	f2 := imp(2)
	if err := e.lib.AddTags(ctx, []uint32{f2}, []tag.Tag{{Name: "blue_sky"}}); err != nil {
		t.Fatal(err)
	}
	e.call("GET", "/api/news", nil, &news)
	if news.Saved[0].New != 1 || news.Groups[0].New != 1 { // the group counts its children's files
		t.Fatalf("after a new file: %+v", news)
	}
	seenBefore := news.Groups[0].SeenAt
	var prev struct{ Previous int64 }
	e.call("POST", "/api/seen", map[string]string{"key": "tag:favorite_artists"}, &prev)
	if prev.Previous != seenBefore {
		t.Fatalf("previous %d, want %d", prev.Previous, seenBefore)
	}
	e.call("GET", "/api/news", nil, &news)
	if news.Groups[0].New != 0 || news.Saved[0].New != 1 { // each has its own mark
		t.Fatalf("after opening the group: %+v", news)
	}
	if code := e.call("POST", "/api/seen", map[string]string{"key": "other"}, nil); code != 400 {
		t.Fatalf("bad key: %d", code)
	}
}
