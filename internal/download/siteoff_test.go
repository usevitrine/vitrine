package download

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A site switched off: no new downloads, no subscription checks, its worker
// stops; remembered after a restart; back on, it carries on.
func TestSiteOff(t *testing.T) {
	e := newEnv(t, 3)
	ctx := context.Background()
	if _, err := e.dl.Subscribe(ctx, "fake", "common", time.Hour, 0); err != nil {
		t.Fatal(err)
	}
	if err := e.dl.SetOff(ctx, "fake", true); err != nil {
		t.Fatal(err)
	}
	if _, err := e.dl.Add(ctx, "fake", "common", 0); !errors.Is(err, ErrSiteOff) {
		t.Fatalf("a download on a site that's off: %v", err)
	}
	if err := e.dl.StartDue(ctx); err != nil {
		t.Fatal(err)
	}
	if list, _ := e.dl.List(ctx); len(list) != 0 {
		t.Fatalf("a check started on a site that's off: %+v", list)
	}
	off := false
	for _, s := range e.dl.SiteInfos() {
		if s.ID == "fake" {
			off = s.Off
		}
	}
	if !off {
		t.Fatal("Settings doesn't show it off")
	}
	if again, _ := New(ctx, e.lib, e.db, nil, nil, e.dl.log); !again.IsOff("fake") {
		t.Fatal("not off after a restart")
	}

	if err := e.dl.SetOff(ctx, "fake", false); err != nil {
		t.Fatal(err)
	}
	if err := e.dl.StartDue(ctx); err != nil {
		t.Fatal(err)
	}
	if list, _ := e.dl.List(ctx); len(list) != 1 {
		t.Fatalf("back on, the due check should start: %+v", list)
	}
	if _, err := e.dl.Add(ctx, "fake", "common", 0); err != nil {
		t.Fatalf("back on: %v", err)
	}
	if err := e.dl.SetOff(ctx, "nope", true); !errors.Is(err, ErrUnknownSite) {
		t.Fatalf("unknown site: %v", err)
	}
}
