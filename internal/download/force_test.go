package download

import (
	"context"
	"errors"
	"testing"
)

// One post "anyway": a post whose file was deleted (in the trash, then gone for good) comes back - from the trash, or
// downloaded again - where an ordinary download of it is refused. Only one post: never a whole search.
func TestForcedPost(t *testing.T) {
	e := newEnv(t, 8)
	ctx := context.Background()
	run := func(id int64) Search {
		t.Helper()
		if err := e.dl.RunQueue(ctx, "fake"); err != nil {
			t.Fatal(err)
		}
		s, err := e.dl.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	id, err := e.dl.Add(ctx, "fake", "id:5", 0)
	if err != nil {
		t.Fatal(err)
	}
	if s := run(id); s.Downloaded != 1 {
		t.Fatalf("first download: %+v", s)
	}
	if e.count(t, "") != 1 {
		t.Fatal("not in the library")
	}
	files := e.lib.Index().Search(mustParse(t, ""), e.dl.now()).IDs
	if err := e.lib.Delete(ctx, files, "uploaded in or before 2020"); err != nil {
		t.Fatal(err)
	}
	// An ordinary download of it: refused (seen, and deleted).
	id, _ = e.dl.Add(ctx, "fake", "id:5", 0)
	if s := run(id); s.Downloaded != 0 || e.count(t, "") != 0 {
		t.Fatalf("an ordinary download brought a deleted file back: %+v", s)
	}
	// Anyway: back from the trash.
	id, err = e.dl.AddForced(ctx, "fake", "id:5")
	if err != nil {
		t.Fatal(err)
	}
	if s := run(id); s.Failed != 0 || e.count(t, "") != 1 {
		t.Fatalf("anyway, from the trash: %+v, %d in the library", s, e.count(t, ""))
	}
	// Deleted again and gone for good: anyway downloads it again.
	if err := e.lib.Delete(ctx, files, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.lib.EmptyTrash(ctx); err != nil {
		t.Fatal(err)
	}
	id, _ = e.dl.AddForced(ctx, "fake", "id:5")
	if s := run(id); s.Downloaded != 1 || e.count(t, "") != 1 {
		t.Fatalf("anyway, gone for good: %+v, %d in the library", s, e.count(t, ""))
	}
	// A whole search: no.
	if _, err := e.dl.AddForced(ctx, "fake", "common"); !errors.Is(err, ErrNotOnePost) {
		t.Fatalf("a whole search anyway: %v", err)
	}
}
