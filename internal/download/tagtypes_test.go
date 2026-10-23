package download

import (
	"context"
	"testing"
)

const typesManifest = "tags: {types: tag_info, namespaces: {artist: creator}}\n"

func TestTagTypesFromSearch(t *testing.T) {
	e := newEnvWith(t, 25, typesManifest)
	e.fb.types = true
	ctx := context.Background()
	if got := e.lib.Namespaces(); len(got) != 1 || got[0] != "creator" {
		t.Fatalf("namespaces at start: %v", got)
	}
	e.dl.Add(ctx, "fake", "common", 0)
	if err := e.dl.RunQueue(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, "creator:tag_7"); n != 1 {
		t.Fatalf("creator:tag_7: %d", n)
	}
	if n := e.count(t, "common"); n != 24 {
		t.Fatalf("common: %d", n)
	}
	// Nothing left to look up.
	if more, err := e.dl.LearnTypes(ctx, "fake"); more || err != nil {
		t.Fatalf("LearnTypes: %v %v", more, err)
	}
}

func TestLearnTypesForOlderFiles(t *testing.T) {
	e := newEnvWith(t, 25, typesManifest)
	ctx := context.Background()
	// Downloaded while the site sent no types: plain tags.
	e.dl.Add(ctx, "fake", "common", 0)
	if err := e.dl.RunQueue(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, "creator:tag_7"); n != 0 {
		t.Fatalf("typed without types: %d", n)
	}
	if more, err := e.dl.LearnTypes(ctx, "fake"); !more || err == nil {
		t.Fatalf("LearnTypes without types: %v %v", more, err) // fails, tried later
	}

	// Types available: every file's post is looked up once (each has its own artist tag).
	e.fb.types = true
	steps := 0
	for {
		more, err := e.dl.LearnTypes(ctx, "fake")
		if err != nil {
			t.Fatal(err)
		}
		if !more {
			break
		}
		if steps++; steps > 30 {
			t.Fatal("LearnTypes doesn't finish")
		}
	}
	if steps != 24 {
		t.Errorf("%d lookups, want 24", steps)
	}
	for _, q := range []string{"creator:tag_7", "tag_7", "creator:tag_25"} {
		if n := e.count(t, q); n != 1 {
			t.Errorf("%s: %d", q, n)
		}
	}
	if n := e.count(t, "common"); n != 24 {
		t.Errorf("common: %d", n)
	}

	// A post the site no longer has: its tags count as looked up, no loop.
	e2 := newEnvWith(t, 5, typesManifest)
	e2.dl.Add(ctx, "fake", "common", 0)
	e2.dl.RunQueue(ctx, "fake")
	e2.fb.types = true
	e2.fb.mu.Lock()
	e2.fb.n = 3 // posts 4 and 5 are gone
	e2.fb.mu.Unlock()
	steps = 0
	for more := true; more; steps++ {
		var err error
		if more, err = e2.dl.LearnTypes(ctx, "fake"); err != nil {
			t.Fatal(err)
		}
		if steps > 10 {
			t.Fatal("loops")
		}
	}
	if n := e2.count(t, "creator:tag_2"); n != 1 {
		t.Errorf("creator:tag_2: %d", n)
	}
	if n := e2.count(t, "tag_5"); n != 1 {
		t.Errorf("tag_5 (post gone) should stay: %d", n)
	}
}
