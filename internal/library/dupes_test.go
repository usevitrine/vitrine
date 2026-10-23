package library

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"math"
	"testing"

	"github.com/usevitrine/vitrine/internal/tag"
)

// scenePNG draws a picture (seed picks which) at w x h: the same seed at
// another size is the same picture resized.
func scenePNG(t *testing.T, seed, w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			fx, fy := float64(x)/float64(w), float64(y)/float64(h)
			v := 128 + 100*math.Sin(fx*float64(3+seed)*2+fy*float64(1+seed%3)*4)*math.Cos(fy*float64(2+seed%5)*3)
			img.Set(x, y, color.RGBA{uint8(v), uint8(255 - v), uint8(int(v*fx) % 256), 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDupes(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	a, _ := l.Import(ctx, bytes.NewReader(scenePNG(t, 1, 800, 600)))
	b, _ := l.Import(ctx, bytes.NewReader(scenePNG(t, 1, 400, 300)))
	c, _ := l.Import(ctx, bytes.NewReader(scenePNG(t, 5, 800, 600)))
	for _, f := range []File{a, b, c} {
		if err := l.ProcessMedia(ctx, f.ID); err != nil {
			t.Skip("media tools needed:", err)
		}
	}
	pending, _ := l.PendingPHash(ctx, 10)
	if len(pending) != 3 {
		t.Fatalf("pending %v", pending)
	}
	n, err := l.HashFiles(ctx, pending)
	if err != nil || n != 1 {
		t.Fatalf("new pairs %d, %v; want 1", n, err)
	}
	if again, _ := l.PendingPHash(ctx, 10); len(again) != 0 {
		t.Fatalf("still pending: %v", again)
	}
	total, pairs, prog, err := l.DupePairs(ctx, 6, 0, 10)
	if err != nil || total != 1 || pairs[0].A != a.ID || pairs[0].B != b.ID || prog.Checked != 3 || prog.Total != 3 {
		t.Fatalf("pairs %d %+v %+v %v", total, pairs, prog, err)
	}

	// Keep a: it gets b's tags, b is deleted, the pair isn't listed.
	if err := l.AddTags(ctx, []uint32{b.ID}, []tag.Tag{{Name: "blue_sky"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.SetFavorite(ctx, []uint32{b.ID}, true); err != nil {
		t.Fatal(err)
	}
	if err := l.KeepOne(ctx, a.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	want(t, l, "system:favorite", a.ID) // b was a favorite: the kept file is one now
	tags, _ := l.FileTags(ctx, a.ID)
	if len(tags) != 1 || tags[0].Tag != "blue_sky" {
		t.Fatalf("kept file's tags %+v", tags)
	}
	want(t, l, "blue_sky", a.ID)
	if total, _, _, _ := l.DupePairs(ctx, 6, 0, 10); total != 0 {
		t.Fatalf("%d pairs after keeping one", total)
	}

	// Undo: b is back and the pair is to review again; then keep both.
	if err := l.UndoDupe(ctx, a.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	if total, _, _, _ := l.DupePairs(ctx, 6, 0, 10); total != 1 {
		t.Fatalf("%d pairs after undo", total)
	}
	if tags, _ := l.FileTags(ctx, a.ID); len(tags) != 0 {
		t.Fatalf("the copied tags stayed after undo: %+v", tags)
	}
	want(t, l, "blue_sky", b.ID)
	want(t, l, "system:favorite", b.ID) // (taken back from a)
	if err := l.KeepBoth(ctx, a.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	if total, _, _, _ := l.DupePairs(ctx, 6, 0, 10); total != 0 {
		t.Fatalf("%d pairs after keeping both", total)
	}
	facts, _ := l.Facts(ctx, []uint32{b.ID})
	if facts[b.ID].Tags != 1 {
		t.Fatalf("facts %+v", facts)
	}
}
