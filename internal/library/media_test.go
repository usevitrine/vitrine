package library

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"slices"
	"testing"
)

func realPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = byte(i * 7)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestProcessMedia(t *testing.T) {
	l := newTest(t)
	if err := l.tools.Check(); err != nil {
		t.Skip(err)
	}
	ctx := context.Background()

	good, err := l.Import(ctx, bytes.NewReader(realPNG(t, 1200, 800)))
	if err != nil {
		t.Fatal(err)
	}
	bad, err := l.Import(ctx, bytes.NewReader(fakePNG(1))) // PNG signature, garbage after
	if err != nil {
		t.Fatal(err)
	}

	pending, err := l.PendingMedia(ctx, false)
	if err != nil || !slices.Equal(pending, []uint32{bad.ID, good.ID}) {
		t.Fatalf("pending = %v, %v", pending, err)
	}
	var errs int
	l.ProcessMany(ctx, pending, 2, func(id uint32, err error) {
		if (err != nil) != (id == bad.ID) {
			t.Errorf("file %d: %v", id, err)
		}
		if err != nil {
			errs++
		}
	})
	if errs != 1 {
		t.Fatalf("%d errors, want 1", errs)
	}

	// The good file is searchable by its size; the bad one isn't pending
	// any more unless retried.
	want(t, l, "system:width=1200 system:height=800", good.ID)
	if p, _ := l.PendingMedia(ctx, false); len(p) != 0 {
		t.Errorf("pending after processing: %v", p)
	}
	if p, _ := l.PendingMedia(ctx, true); !slices.Equal(p, []uint32{bad.ID}) {
		t.Errorf("pending with retry: %v", p)
	}

	th, err := l.thumbs.Get(ctx, good.ID)
	if err != nil || th.Width != 600 || th.Height != 400 || len(th.Data) == 0 {
		t.Fatalf("thumbnail %dx%d (%d bytes), %v", th.Width, th.Height, len(th.Data), err)
	}
	rows, err := l.Files(ctx, []uint32{good.ID})
	if err != nil || len(rows) != 1 || len(rows[0].Placeholder) != 48 || rows[0].Width != 1200 {
		t.Fatalf("file row %+v, %v", rows, err)
	}
	checkReload(t, l)

	// Purging removes the thumbnail too.
	l.Delete(ctx, []uint32{good.ID}, "")
	if _, err := l.Purge(ctx, l.now().Add(1e12)); err != nil {
		t.Fatal(err)
	}
	if n, _ := l.thumbs.Count(ctx); n != 0 {
		t.Errorf("%d thumbnails left after purge", n)
	}
}
