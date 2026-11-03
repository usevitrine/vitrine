package server

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/library"
)

// Old files through the API: the counts first, the delete in the background, then what's in the trash - restored, or
// removed for good.
func TestOldFilesAPI(t *testing.T) {
	e := newEnv(t)
	e.loginAs("me", "correct horse")
	ctx := context.Background()
	for i := range 3 {
		f, err := e.lib.Import(ctx, bytes.NewReader(pngBytes(t, 30+i, 30, byte(40+i))))
		if err != nil {
			t.Fatal(err)
		}
		if i < 2 { // (the third has no upload date)
			at := time.Date(2015+i*5, 6, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
			if _, err := e.db.W.Exec("UPDATE files SET posted_at = ? WHERE id = ?", at, f.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	type info struct {
		Years   []library.YearFiles `json:"years"`
		Undated library.SectionSize `json:"undated"`
		InTrash []library.YearFiles `json:"in_trash"`
		Job     *cleanupJob         `json:"job"`
	}
	read := func() info {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for {
			var v info
			if code := e.call("GET", "/api/old-files", nil, &v); code != 200 {
				t.Fatalf("old files: %d", code)
			}
			if v.Job == nil {
				return v
			}
			if time.Now().After(deadline) {
				t.Fatalf("still working: %+v", v.Job)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	v := read()
	if len(v.Years) != 2 || v.Years[0].Year != 2015 || v.Years[1].Year != 2020 || v.Undated.Items != 1 || len(v.InTrash) != 0 {
		t.Fatalf("%+v", v)
	}
	for _, y := range []int{0, 1969, time.Now().UTC().Year() + 1} {
		if code := e.call("POST", "/api/old-files/delete", map[string]any{"year": y}, nil); code != 400 {
			t.Fatalf("year %d: %d", y, code)
		}
	}
	if code := e.call("POST", "/api/old-files/delete", map[string]any{"year": 2015}, nil); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	v = read()
	if len(v.Years) != 1 || v.Years[0].Year != 2020 || len(v.InTrash) != 1 || v.InTrash[0].Year != 2015 || v.InTrash[0].Files != 1 {
		t.Fatalf("after the delete: %+v", v)
	}
	if code := e.call("POST", "/api/old-files/restore", map[string]any{"year": 2015}, nil); code != 200 {
		t.Fatalf("restore: %d", code)
	}
	if v = read(); len(v.Years) != 2 || len(v.InTrash) != 0 {
		t.Fatalf("after the restore: %+v", v)
	}
	// Forever: out of the trash too.
	if code := e.call("POST", "/api/old-files/delete", map[string]any{"year": 2020, "forever": true}, nil); code != 200 {
		t.Fatalf("forever: %d", code)
	}
	if v = read(); len(v.Years) != 0 || len(v.InTrash) != 0 || v.Undated.Items != 1 {
		t.Fatalf("after forever: %+v", v)
	}
	var left int
	e.db.R.QueryRow("SELECT count(*) FROM files").Scan(&left)
	if left != 1 {
		t.Fatalf("%d files left, want the one with no date", left)
	}
}
