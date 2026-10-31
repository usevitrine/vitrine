package server

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/tag"
)

// The saved searches menu's tiles: each saved search's first files (in its order, else the page's), each group's
// files, first tags and first files.
func TestSavedLooks(t *testing.T) {
	e := newEnv(t)
	e.loginAs("me", "correct horse")
	ctx := context.Background()
	var ids []uint32
	for seed := byte(1); seed <= 4; seed++ {
		f, err := e.lib.Import(ctx, bytes.NewReader(pngBytes(t, 50+int(seed), 40, seed)))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, f.ID)
		time.Sleep(2 * time.Millisecond) // (each imported later than the one before)
	}
	if err := e.lib.AddTags(ctx, ids, []tag.Tag{{Name: "blue_sky"}}); err != nil {
		t.Fatal(err)
	}
	for _, child := range []string{"blue_sky", "red", "sunset", "zebra"} {
		if err := e.lib.AddParent(ctx, tag.Tag{Name: child}, tag.Tag{Name: "some_group"}); err != nil {
			t.Fatal(err)
		}
	}
	sky, err := e.lib.SaveSearch(ctx, "Sky", "blue_sky", "", "")
	if err != nil {
		t.Fatal(err)
	}
	type pic struct {
		ID    uint32 `json:"id"`
		Thumb string `json:"thumb"`
	}
	type look struct {
		Tag   string   `json:"tag"`
		Count int      `json:"count"`
		Kids  []string `json:"kids"`
		Pics  []pic    `json:"pics"`
	}
	var out struct {
		Saved  map[string]look `json:"saved"`
		Groups []look          `json:"groups"`
	}
	if code := e.call("GET", "/api/saved/looks?sort=imported", nil, &out); code != 200 {
		t.Fatalf("looks: %d", code)
	}
	s, ok := out.Saved[strconv.FormatInt(sky, 10)]
	if !ok || len(s.Pics) > 3 {
		t.Fatalf("saved: %+v", out.Saved)
	}
	// (the pictures: files with a thumbnail, newest imported first)
	for i, p := range s.Pics {
		if p.ID != ids[len(ids)-1-i] || !strings.HasPrefix(p.Thumb, "/thumbs/") {
			t.Fatalf("pics: %+v", s.Pics)
		}
	}
	if len(out.Groups) != 1 {
		t.Fatalf("groups: %+v", out.Groups)
	}
	g := out.Groups[0]
	if g.Tag != "some_group" || g.Count != 4 || strings.Join(g.Kids, ",") != "blue_sky,red,sunset" || len(g.Pics) > 3 {
		t.Fatalf("group: %+v", g)
	}
}

