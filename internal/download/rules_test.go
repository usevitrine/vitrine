package download

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestDownloadRules(t *testing.T) {
	e := newEnv(t, 5) // 8x8 images
	ctx := context.Background()
	// Images over 50 pixels are skipped (64 > 50).
	if err := e.lib.SetSettings(ctx, map[string]json.RawMessage{"skipImageMP": json.RawMessage(`0.00005`)}); err != nil {
		t.Fatal(err)
	}
	id, _ := e.dl.Add(ctx, "fake", "common", 0)
	e.dl.RunQueue(ctx, "fake")
	s, _ := e.dl.Get(ctx, id)
	if s.Downloaded != 0 || s.Skipped != 5 || len(e.fb.fileHits) != 0 {
		t.Fatalf("with the rule: %+v, %d files fetched", s, len(e.fb.fileHits))
	}
	// The rule loosened: the same posts download on the next run.
	e.lib.SetSettings(ctx, map[string]json.RawMessage{"skipImageMP": json.RawMessage(`0`)})
	id2, _ := e.dl.Add(ctx, "fake", "common", 0)
	e.dl.RunQueue(ctx, "fake")
	if s, _ = e.dl.Get(ctx, id2); s.Downloaded != 5 {
		t.Fatalf("without the rule: %+v", s)
	}

	// Videos over the size limit: known from the size before the body is read.
	st := e.dl.Site("fake")
	if _, _, err := e.dl.fetchFile(ctx, st, e.fb.srv.URL+"/img/3.png", [16]byte{}, 10, true); !errors.Is(err, errTooBig) {
		t.Fatalf("over the limit: %v", err)
	}
	for u, want := range map[string]bool{"https://x.test/a/b.MP4": true, "https://x.test/v.webm?x=1": true, "https://x.test/i.png": false, "https://x.test/mp4/i.jpg": false} {
		if isVideoURL(u) != want {
			t.Errorf("isVideoURL(%q) != %v", u, want)
		}
	}
	if err := e.lib.SetSettings(ctx, map[string]json.RawMessage{"skipVideoMB": json.RawMessage(`-1`)}); err == nil {
		t.Error("a negative limit was accepted")
	}
}

// A day-only upload time (a page that says "Oct, 21 2026") takes its moment from the file's Last-Modified on that
// day; a moment already, another day or none stays as it is.
func TestRefinePosted(t *testing.T) {
	day := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	moment := time.Date(2026, 10, 21, 20, 4, 25, 0, time.UTC)
	for _, c := range []struct {
		posted, modified, want time.Time
	}{
		{day, moment, moment},
		{day, moment.AddDate(0, 0, 3), day},                      // re-saved later: not the upload
		{moment.Add(-time.Hour), moment, moment.Add(-time.Hour)}, // the page had the moment
		{day, time.Time{}, day},
		{time.Time{}, moment, time.Time{}},
	} {
		if got := refinePosted(c.posted, c.modified); !got.Equal(c.want) {
			t.Errorf("refinePosted(%v, %v) = %v, want %v", c.posted, c.modified, got, c.want)
		}
	}
}
