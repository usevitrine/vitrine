package server

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"math"
	"net/http"
	"strconv"
	"sync"

	xdraw "golang.org/x/image/draw"
)

// Launch images for the iPhone home-screen app. Without one at the device's
// exact screen size, iOS shows an empty screen from the tap until the page
// first draws. Each is the page's own splash (index.html): the icon on the
// theme color, centered in the area below the status bar, so the page takes
// over without a jump. index.html lists one per screen size (media queries);
// they're drawn here on first request rather than kept as files.

var startupBg = color.NRGBA{0x1e, 0x1e, 0x2e, 0xff} // the theme color (index.html, manifest)

type startupKey struct{ w, h, top, scale int }

type startupImages struct {
	web   fs.FS
	mu    sync.Mutex
	cache map[startupKey][]byte
}

// serve: /startup.png?w=&h=&top=&scale= - the screen in device pixels, the
// status bar's height in device pixels and the pixel ratio.
func (si *startupImages) serve(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	num := func(k string, lo, hi int) (int, bool) {
		n, err := strconv.Atoi(q.Get(k))
		return n, err == nil && n >= lo && n <= hi
	}
	width, ok1 := num("w", 100, 3000)
	height, ok2 := num("h", 100, 3000)
	top, ok3 := num("top", 0, 400)
	scale, ok4 := num("scale", 1, 4)
	if !ok1 || !ok2 || !ok3 || !ok4 {
		http.Error(w, "bad size", http.StatusBadRequest)
		return
	}
	key := startupKey{width, height, top, scale}
	si.mu.Lock()
	b, ok := si.cache[key]
	si.mu.Unlock()
	if !ok {
		b = si.draw(key)
		si.mu.Lock()
		if len(si.cache) < 64 {
			si.cache[key] = b
		}
		si.mu.Unlock()
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(b)
}

func (si *startupImages) draw(k startupKey) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, k.w, k.h))
	xdraw.Draw(img, img.Bounds(), image.NewUniform(startupBg), image.Point{}, xdraw.Src)
	if icon := si.icon(); icon != nil {
		// As in the splash: 64 px, rounded corners of 14 px, 90 % opaque.
		size := 64 * k.scale
		x := (k.w - size) / 2
		y := k.top + (k.h-k.top-size)/2
		dst := image.Rect(x, y, x+size, y+size)
		scaled := image.NewNRGBA(image.Rect(0, 0, size, size))
		xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), icon, icon.Bounds(), xdraw.Src, nil)
		mask := roundedMask(size, 14*k.scale, 0.9)
		xdraw.DrawMask(img, dst, scaled, image.Point{}, mask, image.Point{}, xdraw.Over)
	}
	var buf bytes.Buffer
	(&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&buf, img)
	return buf.Bytes()
}

func (si *startupImages) icon() image.Image {
	f, err := si.web.Open("icon-180.png")
	if err != nil {
		return nil
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil
	}
	return img
}

// roundedMask: a size x size square with rounded corners (radius r, smoothed
// edges) at the given opacity.
func roundedMask(size, r int, opacity float64) *image.Alpha {
	m := image.NewAlpha(image.Rect(0, 0, size, size))
	fr := float64(r)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			// Distance outside the inner rectangle the corners are rounded around.
			cx := clampF(float64(x)+0.5, fr, float64(size)-fr)
			cy := clampF(float64(y)+0.5, fr, float64(size)-fr)
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			d := math.Sqrt(dx*dx+dy*dy) - fr
			a := clampF(0.5-d, 0, 1) * opacity
			m.SetAlpha(x, y, color.Alpha{uint8(a*255 + 0.5)})
		}
	}
	return m
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
