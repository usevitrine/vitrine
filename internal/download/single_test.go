package download

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/plugin"
)

func TestSinglePostOnce(t *testing.T) {
	e := newEnv(t, 5)
	ctx := context.Background()
	if _, err := e.dl.Add(ctx, "fake", "https://other.test/post/3", 0); !plugin.IsPostRef(err) {
		t.Fatalf("another site's link: %v", err)
	}
	id, err := e.dl.Add(ctx, "fake", "ID:3", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.dl.RunQueue(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	s, err := e.dl.Get(ctx, id)
	if err != nil || s.State != StateDone || s.Query != "id:3" {
		t.Fatalf("%+v %v", s, err)
	}
	if n := e.count(t, ""); n != 1 {
		t.Fatalf("%d files, want 1", n)
	}
	if e.fb.fileHits[3] != 1 {
		t.Fatalf("file hits: %v", e.fb.fileHits)
	}
}

func TestSinglePostMissing(t *testing.T) {
	e := newEnv(t, 2)
	ctx := context.Background()
	id, err := e.dl.Add(ctx, "fake", "id:99", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.dl.RunQueue(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	s, _ := e.dl.Get(ctx, id)
	if s.State != StateFailed || s.Error == "" {
		t.Fatalf("%+v", s)
	}
	if _, err := e.dl.CheckSingle(ctx, "fake", "id:99"); !plugin.IsNoPost(err) {
		t.Fatalf("check: %v", err)
	}
	res, err := e.dl.CheckSingle(ctx, "fake", "id:1")
	if err != nil || !res.Single || res.Query != "id:1" || res.Files != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	if res, err := e.dl.CheckSingle(ctx, "fake", "blue_sky"); err != nil || res.Single {
		t.Fatalf("a search: %+v %v", res, err)
	}
}

func TestSinglePostNoSubscription(t *testing.T) {
	e := newEnv(t, 2)
	ctx := context.Background()
	if _, err := e.dl.Subscribe(ctx, "fake", "id:1", time.Hour, 0); !errors.Is(err, ErrSinglePost) {
		t.Fatalf("subscribe: %v", err)
	}
	id, _ := e.dl.Add(ctx, "fake", "id:1", 0)
	if _, err := e.dl.SubscribeSearch(ctx, id, time.Hour); !errors.Is(err, ErrSinglePost) {
		t.Fatalf("subscribe search: %v", err)
	}
}
