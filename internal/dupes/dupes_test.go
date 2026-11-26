package dupes

import (
	"image"
	"image/color"
	"math"
	"testing"

	"golang.org/x/image/draw"
)

// scene draws a picture with shapes (seed picks which), w x h.
func scene(seed, w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			fx, fy := float64(x)/float64(w), float64(y)/float64(h)
			v := 128 + 100*math.Sin(fx*float64(3+seed)*2+fy*float64(1+seed%3)*4)*math.Cos(fy*float64(2+seed%5)*3)
			img.Set(x, y, color.RGBA{uint8(v), uint8(255 - v), uint8(int(v*fx) % 256), 255})
		}
	}
	return img
}

func resized(src image.Image, w, h int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	return dst
}

func TestHash(t *testing.T) {
	a, ok := Hash(scene(1, 400, 300))
	if !ok {
		t.Fatal("no hash")
	}
	small, _ := Hash(resized(scene(1, 400, 300), 160, 120))
	if d := Distance(a, small); d > 2 {
		t.Errorf("a smaller copy is %d apart, want <= 2", d)
	}
	other, _ := Hash(scene(4, 400, 300))
	if d := Distance(a, other); d < 16 {
		t.Errorf("a different picture is only %d apart", d)
	}
	flat := image.NewRGBA(image.Rect(0, 0, 50, 50))
	draw.Draw(flat, flat.Bounds(), image.NewUniform(color.Black), image.Point{}, draw.Src)
	if _, ok := Hash(flat); ok {
		t.Error("a flat picture got a hash")
	}
}

func TestAlikeAndFind(t *testing.T) {
	img := Item{ID: 1, Hash: 0xFF00FF00, Width: 1000, Height: 800}
	near := Item{ID: 2, Hash: 0xFF00FF03, Width: 500, Height: 400}
	wide := Item{ID: 3, Hash: 0xFF00FF00, Width: 1600, Height: 800}
	far := Item{ID: 4, Hash: ^uint64(0xFF00FF00), Width: 1000, Height: 800}
	vid := Item{ID: 5, Hash: 0xFF00FF00, Width: 1000, Height: 800, Video: true, DurationMS: 10000}
	vidSame := Item{ID: 6, Hash: 0xFF00FF01, Width: 1000, Height: 800, Video: true, DurationMS: 10200}
	vidLong := Item{ID: 7, Hash: 0xFF00FF00, Width: 1000, Height: 800, Video: true, DurationMS: 30000}

	if d, ok := Alike(img, near); !ok || d != 2 {
		t.Errorf("near: %d %v", d, ok)
	}
	for _, o := range []Item{wide, far, vid} {
		if _, ok := Alike(img, o); ok {
			t.Errorf("item %d counted as alike", o.ID)
		}
	}
	if _, ok := Alike(vid, vidSame); !ok {
		t.Error("videos of about the same length not alike")
	}
	if _, ok := Alike(vid, vidLong); ok {
		t.Error("videos of different lengths alike")
	}

	all := []Item{img, near, wide, far, vid, vidSame, vidLong}
	pairs := Find([]Item{near, vidSame}, all)
	if len(pairs) != 2 || pairs[0] != (Pair{1, 2, 2}) || pairs[1] != (Pair{5, 6, 1}) {
		t.Errorf("pairs %+v", pairs)
	}
	// Two new items alike: found once.
	if p := Find([]Item{img, near}, all); len(p) != 1 {
		t.Errorf("new pair found %d times", len(p))
	}
}
