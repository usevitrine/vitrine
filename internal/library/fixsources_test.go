package library

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// The source links fix: offered only where a link has "&amp;" in it; run, each becomes its decoded form, merged with
// it where a file has both, and the coded rows go. Post links aren't touched.
func TestSourceLinksRepair(t *testing.T) {
	l := newTest(t)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	l.rep.wait = time.Millisecond
	for i := 1; i <= 3; i++ {
		if _, err := l.db.W.ExecContext(ctx, `INSERT INTO files (id, sha256, md5, size, type, imported_at, modified_at, flags)
			VALUES (?, ?, ?, 1, 1, 1, 1, 0)`, i, []byte{byte(i), 1}, []byte{byte(i), 2}); err != nil {
			t.Fatal(err)
		}
	}
	setup := `
INSERT INTO urls (id, url) VALUES (1, 'https://x.example/s?a=1&amp;b=2'), (2, 'https://x.example/s?a=1&b=2'),
  (3, 'https://y.example/t?c=3&amp;d=4'), (4, 'https://post.example/p?id=9&amp;x=1');
-- file 1: both forms (a refresh's duplicate); file 2: the coded one only; file 3: another coded one, and a post link.
INSERT INTO file_urls (file_id, url_id, kind, site_id) VALUES (1, 1, 2, 0), (1, 2, 2, 0), (2, 1, 2, 0), (3, 3, 2, 0), (3, 4, 1, 0);`
	if _, err := l.db.W.ExecContext(ctx, setup); err != nil {
		t.Fatal(err)
	}
	l.AddSourceLinksRepair()
	if err := l.StartRepairs(ctx); err != nil {
		t.Fatal(err)
	}
	if r := waitRepair(t, l, sourceLinksRepair, RepairOffered); r.Total != 3 {
		t.Fatalf("offered: %+v (3 coded links on files)", r)
	}
	if err := l.RunRepair(ctx, sourceLinksRepair); err != nil {
		t.Fatal(err)
	}
	if r := waitRepair(t, l, sourceLinksRepair, RepairDone); r.Done != 3 || r.Changed != 3 {
		t.Fatalf("done: %+v", r)
	}
	rows, err := l.db.R.QueryContext(ctx, `SELECT fu.file_id, u.url, fu.kind FROM file_urls fu JOIN urls u ON u.id = fu.url_id ORDER BY 1, 2`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var f, k int
		var u string
		rows.Scan(&f, &u, &k)
		got = append(got, fmt.Sprintf("%d %s %d", f, u, k))
	}
	want := []string{
		"1 https://x.example/s?a=1&b=2 2",
		"2 https://x.example/s?a=1&b=2 2",
		"3 https://post.example/p?id=9&amp;x=1 1", // (a post link: not this fix's)
		"3 https://y.example/t?c=3&d=4 2",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("after the fix:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	var left int
	l.db.R.QueryRowContext(ctx, `SELECT count(*) FROM urls WHERE id IN (1, 3)`).Scan(&left)
	if left != 0 {
		t.Fatalf("%d coded source rows left in urls", left)
	}

	// A library with nothing to fix never hears of it.
	l2 := newTest(t)
	l2.AddSourceLinksRepair()
	if err := l2.StartRepairs(ctx); err != nil {
		t.Fatal(err)
	}
	if list, _ := l2.Repairs(); len(list) != 0 {
		t.Fatalf("offered with nothing to fix: %+v", list)
	}
}
