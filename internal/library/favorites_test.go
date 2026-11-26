package library

import (
	"context"
	"testing"
	"time"
)

func TestFavorites(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	ids := importN(t, l, 4)
	clock := time.Now()
	l.now = func() time.Time { return clock }
	if n, err := l.SetFavorite(ctx, []uint32{ids[2]}, true); err != nil || n != 1 {
		t.Fatalf("%d, %v", n, err)
	}
	clock = clock.Add(time.Minute)
	if n, err := l.SetFavorite(ctx, []uint32{ids[0], ids[2]}, true); err != nil || n != 1 { // ids[2] already is one
		t.Fatalf("%d, %v", n, err)
	}
	want(t, l, "system:favorite", ids[2], ids[0])
	want(t, l, "system:favourite sort:favorite", ids[0], ids[2]) // the newest favorite first
	want(t, l, "system:fav sort:favorite_asc", ids[2], ids[0])
	want(t, l, "-system:favorite", ids[3], ids[1])
	if rows, _ := l.Files(ctx, ids[:2]); !rows[0].Favorite || rows[1].Favorite {
		t.Fatalf("rows %+v", rows)
	}
	// Deleted: out of the search; undone: a favorite again.
	if err := l.Delete(ctx, ids[:1], ""); err != nil {
		t.Fatal(err)
	}
	want(t, l, "system:favorite", ids[2])
	if err := l.Undelete(ctx, ids[:1]); err != nil {
		t.Fatal(err)
	}
	want(t, l, "system:favorite", ids[2], ids[0])
	if n, err := l.SetFavorite(ctx, []uint32{ids[2]}, false); err != nil || n != 1 {
		t.Fatalf("%d, %v", n, err)
	}
	want(t, l, "system:favorite", ids[0])
	if st, _ := l.Stats(ctx, 0, 0); st.Favorites != 1 {
		t.Fatalf("stats: %d favorites", st.Favorites)
	}
	checkReload(t, l)
}
