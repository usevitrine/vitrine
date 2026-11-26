package download

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestGroups(t *testing.T) {
	e := newEnv(t, 3)
	ctx := context.Background()
	a, _ := e.dl.Subscribe(ctx, "fake", "artist_a", 24*time.Hour, 0)
	b, _ := e.dl.Subscribe(ctx, "fake", "artist_b", 24*time.Hour, 0)
	c, _ := e.dl.Subscribe(ctx, "fake", "other", 24*time.Hour, 0)

	g, err := e.dl.CreateGroup(ctx, "fake", "  Artists  ")
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := e.dl.CreateGroup(ctx, "fake", "artists"); again != g {
		t.Fatalf("the same name made a second group: %d vs %d", again, g)
	}
	if _, err := e.dl.CreateGroup(ctx, "fake", " "); !errors.Is(err, ErrGroupName) {
		t.Fatalf("empty name: %v", err)
	}
	if _, err := e.dl.CreateGroup(ctx, "nope", "x"); !errors.Is(err, ErrUnknownSite) {
		t.Fatalf("unknown site: %v", err)
	}
	for _, id := range []int64{a, b} {
		if err := e.dl.SetGroup(ctx, id, g); err != nil {
			t.Fatal(err)
		}
	}
	if s := e.sub(t, a); s.GroupID != g {
		t.Fatalf("group not set: %+v", s)
	}
	list, _ := e.dl.Groups(ctx)
	if len(list) != 1 || list[0].Name != "Artists" || list[0].Site != "fake" {
		t.Fatalf("groups: %+v", list)
	}

	// Group actions reach every member, and only them.
	if err := e.dl.GroupAction(ctx, g, "pause"); err != nil {
		t.Fatal(err)
	}
	if !e.sub(t, a).Paused || !e.sub(t, b).Paused || e.sub(t, c).Paused {
		t.Fatal("pause all: wrong subscriptions paused")
	}
	if err := e.dl.GroupAction(ctx, g, "resume"); err != nil {
		t.Fatal(err)
	}
	if e.sub(t, a).Paused {
		t.Fatal("resume all")
	}
	if err := e.dl.SetGroupInterval(ctx, g, 7*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if e.sub(t, b).Interval != 604800 || e.sub(t, c).Interval != 86400 {
		t.Fatalf("interval for all: %d %d", e.sub(t, b).Interval, e.sub(t, c).Interval)
	}
	if gg, _ := e.dl.GetGroup(ctx, g); gg.Interval != 604800 {
		t.Fatalf("the group's usual schedule: %+v", gg)
	}

	// Rename (not onto another group's name); delete keeps the subscriptions.
	g2, _ := e.dl.CreateGroup(ctx, "fake", "Others")
	if err := e.dl.RenameGroup(ctx, g2, "ARTISTS"); !errors.Is(err, ErrGroupExists) {
		t.Fatalf("rename onto an existing name: %v", err)
	}
	if err := e.dl.RenameGroup(ctx, g, "Favorite artists"); err != nil {
		t.Fatal(err)
	}
	if err := e.dl.SetGroup(ctx, a, 0); err != nil {
		t.Fatal(err)
	}
	if e.sub(t, a).GroupID != 0 {
		t.Fatal("ungroup")
	}
	if err := e.dl.DeleteGroup(ctx, g); err != nil {
		t.Fatal(err)
	}
	if s := e.sub(t, b); s.GroupID != 0 || s.Query != "artist_b" {
		t.Fatalf("after deleting the group: %+v", s)
	}
	if err := e.dl.GroupAction(ctx, g, "check"); !errors.Is(err, ErrNoGroup) {
		t.Fatalf("a deleted group: %v", err)
	}
}
