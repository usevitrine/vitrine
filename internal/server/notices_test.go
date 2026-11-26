package server

import (
	"io"
	"log/slog"
	"testing"

	"github.com/usevitrine/vitrine/internal/download"
	"github.com/usevitrine/vitrine/internal/library"
)

// The bell's notices are kept over a restart: a new server (the same library) lists them, under the same boot and
// IDs (a page doesn't take them for new ones), and numbers on from the last; only the last NoticesKept stay.
func TestNoticesKeptOverRestart(t *testing.T) {
	e := newEnv(t)
	e.loginAs("me", "a long password")
	e.h.Notice(download.Event{Kind: download.EventPaused, Site: "safebooru.org", Error: "503"})
	e.h.Notice(download.Event{Kind: download.EventNewFiles, Site: "safebooru.org", Query: "blue_sky", New: 3, Subscription: true})
	var before struct {
		Boot    string           `json:"boot"`
		Notices []library.Notice `json:"notices"`
	}
	e.call("GET", "/api/notices", nil, &before)
	if len(before.Notices) != 2 || before.Boot == "" {
		t.Fatalf("before: %+v", before)
	}

	again := newNotices(e.lib, slog.New(slog.NewTextHandler(io.Discard, nil))) // (what a restarted server starts with)
	if again.boot != before.Boot {
		t.Fatalf("boot changed over a restart: %q -> %q", before.Boot, again.boot)
	}
	got := again.after(0)
	if len(got) != 2 || got[0] != before.Notices[0] || got[1] != before.Notices[1] || got[1].Query != "blue_sky" || got[1].New != 3 {
		t.Fatalf("after a restart: %+v, want %+v", got, before.Notices)
	}
	again.add(download.Event{Kind: download.EventBack, Site: "safebooru.org"})
	if l := again.after(0); l[2].ID != got[1].ID+1 {
		t.Fatalf("numbering: %+v", l)
	}
	for range library.NoticesKept + 5 {
		again.add(download.Event{Kind: download.EventDone, Site: "safebooru.org"})
	}
	if kept, _ := e.lib.Notices(t.Context()); len(kept) != library.NoticesKept || len(again.after(0)) != library.NoticesKept {
		t.Fatalf("kept %d / %d, want %d", len(kept), len(again.after(0)), library.NoticesKept)
	}
}
