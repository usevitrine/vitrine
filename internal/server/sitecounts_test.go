package server

import (
	"bytes"
	"context"
	"testing"

	"github.com/usevitrine/vitrine/internal/library"
)

// Show files from lists each site the search has files from - a removed site too (no plugin: its address from a
// post link of its files), so its files can still be picked (2026-11-17, the user). The trash's totals come apart
// from the library's other figures (Empty trash asks at once).
func TestSiteCountsAndTrash(t *testing.T) {
	e := newEnv(t)
	e.loginAs("me", "correct horse")
	ctx := context.Background()
	gone, _ := e.lib.Site(ctx, "gone-site")
	var ids []uint32
	for i := range 3 {
		f, err := e.lib.Import(ctx, bytes.NewReader(pngBytes(t, 64+i, 48, byte(i))))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, f.ID)
	}
	for i, id := range ids[:2] {
		if err := e.lib.SetSitePost(ctx, id, gone, library.SitePost{PostURL: "https://www.gone.test/post/" + string(rune('1'+i))}); err != nil {
			t.Fatal(err)
		}
	}
	var res struct {
		Total int `json:"total"`
		Sites []struct {
			Site    string `json:"site"`
			Count   int    `json:"count"`
			Address string `json:"address"`
		} `json:"sites"`
	}
	if code := e.call("GET", "/api/search/sites?q=", nil, &res); code != 200 {
		t.Fatalf("site counts: %d", code)
	}
	var found bool
	for _, s := range res.Sites {
		if s.Site == "gone-site" {
			found = true
			if s.Count != 2 || s.Address != "gone.test" {
				t.Fatalf("the removed site: %+v", s)
			}
		}
	}
	if !found || res.Total != 3 {
		t.Fatalf("counts: %+v", res)
	}

	if err := e.lib.Delete(ctx, ids[2:], ""); err != nil {
		t.Fatal(err)
	}
	var tr struct {
		Files int   `json:"files"`
		Bytes int64 `json:"bytes"`
	}
	if code := e.call("GET", "/api/trash", nil, &tr); code != 200 || tr.Files != 1 || tr.Bytes <= 0 {
		t.Fatalf("trash: %d %+v", code, tr)
	}
}
