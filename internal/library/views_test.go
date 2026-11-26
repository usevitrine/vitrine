package library

import (
	"context"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/index"
)

func TestViews(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	ids := importN(t, l, 3)
	min := int64(60 * 1000)
	now := time.Now().UnixMilli()
	day := 24 * 60 * min
	add := func(vs ...View) {
		t.Helper()
		if _, err := l.AddViews(ctx, vs); err != nil {
			t.Fatal(err)
		}
	}
	// ids[0]: viewed 10 days ago, then twice today - the second within half an hour of the first: one view, longer.
	add(View{File: ids[0], At: now - 10*day, MS: 5000})
	add(View{File: ids[0], At: now - 60*min, MS: 3000}, View{File: ids[0], At: now - 50*min, MS: 4000})
	// ids[1]: once, 20 days ago, a long one.
	add(View{File: ids[1], At: now - 20*day, MS: 20 * min})
	// A file gone for good is skipped, not an error.
	add(View{File: 99999, At: now, MS: 3000})

	if v := l.ix.Views(ids[0]); v.Count != 2 || v.MS != 12000 || v.Last != now-60*min {
		t.Fatalf("ids[0]: %+v", v)
	}
	if rows, _ := l.Files(ctx, ids[:1]); rows[0].Views.Count != 2 {
		t.Fatalf("file row: %+v", rows[0].Views)
	}
	want(t, l, "system:views>0 sort:views", ids[0], ids[1])
	want(t, l, "system:views=0", ids[2])
	want(t, l, "sort:viewtime system:viewtime>10m", ids[1])
	want(t, l, "system:viewed<7d", ids[0])
	// Not viewed in the last two weeks: ids[1] (20 days ago) and ids[2] (never).
	want(t, l, "system:viewed>14d", ids[2], ids[1])
	want(t, l, "system:views>0 sort:viewed", ids[0], ids[1])

	// The index read back from the database has the same counts.
	fresh, err := index.Load(ctx, l.db.R)
	if err != nil {
		t.Fatal(err)
	}
	if v := fresh.Views(ids[0]); v.Count != 2 || v.MS != 12000 {
		t.Fatalf("after reload: %+v", v)
	}

	if n, err := l.ClearViews(ctx); err != nil || n != 3 {
		t.Fatalf("clear: %d, %v", n, err)
	}
	want(t, l, "system:views>0")
}
