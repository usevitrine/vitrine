// Package dupes finds files that look alike: a perceptual hash of each
// file's thumbnail (for videos, the frame at 10% of the length), compared by
// how many of its 64 bits differ. Resized, recompressed or re-encoded copies
// of a picture come out the same or nearly; different pictures don't.
//
// The hash is the usual DCT one: the picture in 32x32 grays, its 8x8
// coarsest cosine patterns, one bit per pattern - above or below their
// median.
package dupes

import (
	"image"
	"math"
	"math/bits"
	"sort"
)

// MaxStored is the largest distance kept as a possible duplicate (the review
// screen narrows it down further).
const MaxStored = 10

const side = 32

var cosines = func() (c [8][side]float64) {
	for u := range 8 {
		for x := range side {
			c[u][x] = math.Cos(float64(2*x+1) * float64(u) * math.Pi / (2 * side))
		}
	}
	return
}()

// Hash returns a picture's perceptual hash. ok is false for a flat picture
// (all one color, e.g. a black frame): it would look like every other one.
func Hash(img image.Image) (h uint64, ok bool) {
	g := grays(img)
	var sum, sq float64
	for _, v := range g {
		sum += v
		sq += v * v
	}
	n := float64(len(g))
	if sq/n-(sum/n)*(sum/n) < 4 { // standard deviation under 2 of 255
		return 0, false
	}
	// The 8x8 lowest frequencies of the 2D DCT, separably: rows, then columns.
	var rows [side][8]float64
	for y := range side {
		for u := range 8 {
			var s float64
			for x := range side {
				s += g[y*side+x] * cosines[u][x]
			}
			rows[y][u] = s
		}
	}
	var coef [64]float64
	for v := range 8 {
		for u := range 8 {
			var s float64
			for y := range side {
				s += rows[y][u] * cosines[v][y]
			}
			coef[v*8+u] = s
		}
	}
	ac := append([]float64(nil), coef[1:]...) // the median leaves out the average brightness
	sort.Float64s(ac)
	median := (ac[31] + ac[32]) / 2
	for i, c := range coef {
		if c > median {
			h |= 1 << i
		}
	}
	return h, true
}

// grays shrinks a picture to side x side gray levels (each the average of
// the pixels it covers).
func grays(img image.Image) []float64 {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	out := make([]float64, side*side)
	if w == 0 || h == 0 {
		return out
	}
	var count [side * side]float64
	for y := range h {
		cy := y * side / h
		for x := range w {
			r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			lum := (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(bl)) / 257
			i := cy*side + x*side/w
			out[i] += lum
			count[i]++
		}
	}
	for i := range out {
		if count[i] > 0 {
			out[i] /= count[i]
		}
	}
	return out
}

// Distance is how many bits two hashes differ in (0 = the same).
func Distance(a, b uint64) int { return bits.OnesCount64(a ^ b) }

// Item is what's compared of a file.
type Item struct {
	ID         uint32
	Hash       uint64
	Video      bool
	Width      int
	Height     int
	DurationMS int64
}

// Alike reports whether two files may be duplicates, and how far apart
// their hashes are: both pictures or both videos (of about the same length),
// about the same shape, hashes at most MaxStored apart.
func Alike(a, b Item) (int, bool) {
	if a.Video != b.Video || a.Width <= 0 || a.Height <= 0 || b.Width <= 0 || b.Height <= 0 {
		return 0, false
	}
	ra := float64(a.Width) / float64(a.Height)
	rb := float64(b.Width) / float64(b.Height)
	if math.Abs(ra-rb)/max(ra, rb) > 0.05 {
		return 0, false
	}
	if a.Video {
		diff := a.DurationMS - b.DurationMS
		if diff < 0 {
			diff = -diff
		}
		if a.DurationMS <= 0 || b.DurationMS <= 0 || diff > max(500, max(a.DurationMS, b.DurationMS)*3/100) {
			return 0, false
		}
	}
	d := Distance(a.Hash, b.Hash)
	return d, d <= MaxStored
}

// Pair is two alike files (A < B).
type Pair struct {
	A, B     uint32
	Distance int
}

// Find compares each of the new items with all of them (new ones included).
func Find(fresh, all []Item) []Pair {
	isNew := make(map[uint32]bool, len(fresh))
	for _, f := range fresh {
		isNew[f.ID] = true
	}
	var out []Pair
	for _, f := range fresh {
		for _, o := range all {
			if o.ID == f.ID || (isNew[o.ID] && o.ID < f.ID) { // each new pair once
				continue
			}
			if d, ok := Alike(f, o); ok {
				p := Pair{A: min(f.ID, o.ID), B: max(f.ID, o.ID), Distance: d}
				out = append(out, p)
			}
		}
	}
	return out
}
