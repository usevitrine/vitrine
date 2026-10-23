// indexbench - Vitrine speed experiment (build order step 0, throwaway code).
//
// Builds the planned in-memory search index on a synthetic library and times typical searches:
//   - per-tag file sets as roaring bitmaps (AND / OR / NOT = bitmap operations)
//   - file IDs dense and sequential, so ID order = import order (newest first for free)
//   - column arrays per file (size, duration) and a has-audio bitmap for filters and sorting
//   - parent tags as precomputed "effective" bitmaps
//   - autocomplete: tags sorted by name (prefix = binary search) + a trigram index for *wildcards*
//
// Tag popularity follows a Zipf distribution (a few huge tags, a long tail), like booru tags.
//
// Run:  go run . [-files 100000] [-tags 85000] [-min 20] [-max 90] [-iters 2000]  (times = per search, from
//
//	batches of 20 - the Windows clock is too coarse for single runs)
//
// Targets (docs/architecture/stack-and-storage.md): search < 20 ms at 1M files, autocomplete < 10 ms.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"math/rand"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/RoaringBitmap/roaring"
)

type index struct {
	files      int
	tagFiles   []*roaring.Bitmap          // tag ID -> file IDs
	tagCount   []int                      // tag ID -> number of files
	names      []string                   // tag ID -> name
	byName     []uint32                   // tag IDs sorted by name
	trigrams   map[string]*roaring.Bitmap // 3-letter piece -> tag IDs whose name contains it
	parents    []*roaring.Bitmap          // effective bitmaps of parent ("group") tags
	hasAudio   *roaring.Bitmap
	size       []uint64 // file ID -> bytes
	duration   []uint32 // file ID -> ms (0 = still image)
	bySizeDesc []uint32 // file IDs sorted by size, largest first (presorted for "sort by size")
	mappings   int
}

var sink uint64 // keeps results "used" so the compiler can't skip work

func main() {
	files := flag.Int("files", 100000, "number of files")
	tags := flag.Int("tags", 85000, "number of distinct tags")
	minPer := flag.Int("min", 20, "min tags per file")
	maxPer := flag.Int("max", 90, "max tags per file")
	iters := flag.Int("iters", 2000, "timed runs per query type")
	seed := flag.Int64("seed", 1, "random seed")
	flag.Parse()

	fmt.Printf("Vitrine index experiment - %d files, %d tags, %d-%d tags per file, Go %s, %d CPUs\n\n",
		*files, *tags, *minPer, *maxPer, runtime.Version(), runtime.NumCPU())

	t0 := time.Now()
	ix := build(*files, *tags, *minPer, *maxPer, *seed)
	buildTime := time.Since(t0)
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	fmt.Printf("Index built in %v: %d tag links (avg %.1f per file)\n", buildTime.Round(time.Millisecond),
		ix.mappings, float64(ix.mappings)/float64(ix.files))
	fmt.Printf("Memory: Go heap in use %.1f MB (tag bitmaps %.1f MB, trigram index %.1f MB, columns %.1f MB)\n\n",
		mb(ms.HeapAlloc), mb(bitmapBytes(ix.tagFiles)), mb(trigramBytes(ix.trigrams)),
		mb(uint64(ix.files)*(8+4+4)))

	snapshot(ix)

	r := rand.New(rand.NewSource(*seed + 1))
	n := *iters
	pick := func(lo, hi int) []uint32 { // n random tag IDs from a popularity band (low ID = popular)
		if hi > len(ix.tagFiles) {
			hi = len(ix.tagFiles)
		}
		out := make([]uint32, n)
		for i := range out {
			out[i] = uint32(lo + r.Intn(hi-lo))
		}
		return out
	}
	popular, mid, rare := pick(0, 50), pick(50, 2000), pick(2000, len(ix.tagFiles))
	popular2, mid2, mid3 := pick(0, 50), pick(50, 2000), pick(50, 2000)
	parentIdx := make([]int, n)
	for i := range parentIdx {
		parentIdx[i] = r.Intn(len(ix.parents))
	}
	recent := roaring.New() // "imported in the last 5 days" = the newest IDs (library spans ~2 years)
	cutoff := ix.files - ix.files*5/730
	recent.AddRange(uint64(cutoff), uint64(ix.files))

	fmt.Println("Searches (server-side, including picking the first 1000 results to show):")
	fmt.Printf("  %-58s %9s %9s %9s %10s\n", "query", "median", "p99", "max", "results")
	bench("1 popular tag, newest first", n, func(i int) uint64 {
		return newest(ix.tagFiles[popular[i]], 1000)
	})
	bench("1 rare tag, newest first", n, func(i int) uint64 {
		return newest(ix.tagFiles[rare[i]], 1000)
	})
	bench("popular AND mid tag", n, func(i int) uint64 {
		return newest(roaring.And(ix.tagFiles[popular[i]], ix.tagFiles[mid[i]]), 1000)
	})
	bench("2 popular AND mid, NOT mid", n, func(i int) uint64 {
		res := roaring.FastAnd(ix.tagFiles[popular[i]], ix.tagFiles[popular2[i]], ix.tagFiles[mid[i]])
		res.AndNot(ix.tagFiles[mid2[i]])
		return newest(res, 1000)
	})
	bench("OR group of 3 mid tags AND popular tag", n, func(i int) uint64 {
		or := roaring.FastOr(ix.tagFiles[mid[i]], ix.tagFiles[mid2[i]], ix.tagFiles[mid3[i]])
		or.And(ix.tagFiles[popular[i]])
		return newest(or, 1000)
	})
	bench("parent tag AND has-audio AND imported last 5 days", n, func(i int) uint64 {
		res := roaring.FastAnd(ix.parents[parentIdx[i]], ix.hasAudio, recent)
		return newest(res, 1000)
	})
	bench("popular tag, sorted by size (largest 1000)", n, func(i int) uint64 {
		return bySize(ix, ix.tagFiles[popular[i]], 1000)
	})
	bench("popular tag, FULL result sorted by size", n, func(i int) uint64 {
		return bySize(ix, ix.tagFiles[popular[i]], 0)
	})
	bench("popular tag, FULL result newest-first ID list", n, func(i int) uint64 {
		a := ix.tagFiles[popular[i]].ToArray()
		slices.Reverse(a)
		return uint64(len(a))
	})
	wild := wildcardTerms(ix, r, n)
	bench("wildcard *xyz* as a search term (OR of all matching tags)", n, func(i int) uint64 {
		ids := matchSubstring(ix, wild[i])
		bms := make([]*roaring.Bitmap, 0, len(ids))
		for _, t := range ids {
			bms = append(bms, ix.tagFiles[t])
		}
		return newest(roaring.FastOr(bms...), 1000)
	})

	fmt.Println("\nAutocomplete (top 10 suggestions by file count):")
	fmt.Printf("  %-58s %9s %9s %9s %10s\n", "input", "median", "p99", "max", "matches")
	for _, plen := range []int{1, 2, 3, 5} {
		prefixes := make([]string, n)
		for i := range prefixes {
			name := ix.names[r.Intn(len(ix.names))]
			if len(name) > plen {
				name = name[:plen]
			}
			prefixes[i] = name
		}
		bench(fmt.Sprintf("prefix, %d letter(s)", plen), n, func(i int) uint64 {
			return uint64(len(top10(ix, prefixMatches(ix, prefixes[i]))))
		})
	}
	bench("substring *xyz* (3 letters)", n, func(i int) uint64 {
		return uint64(len(top10(ix, matchSubstring(ix, wild[i]))))
	})
	fmt.Printf("\n(checksum %d)\n", sink%1000)
}

// ---------------------------------------------------------------- building the synthetic library + index

func build(files, tags, minPer, maxPer int, seed int64) *index {
	r := rand.New(rand.NewSource(seed))
	z := rand.NewZipf(r, 1.07, 2, uint64(tags-1)) // tag ID 0 = most popular
	lists := make([][]uint32, tags)
	seen := make(map[uint32]struct{}, maxPer)
	ix := &index{files: files, size: make([]uint64, files), duration: make([]uint32, files), hasAudio: roaring.New()}
	for f := 0; f < files; f++ {
		k := minPer + r.Intn(maxPer-minPer+1)
		clear(seen)
		for len(seen) < k {
			t := uint32(z.Uint64())
			if _, dup := seen[t]; dup {
				continue
			}
			seen[t] = struct{}{}
			lists[t] = append(lists[t], uint32(f)) // files are added in ID order, so lists stay sorted
		}
		if r.Float64() < 0.64 { // ~64% videos (a video-heavy library), most with audio
			ix.duration[f] = uint32(5000 + r.Intn(600000))
			ix.size[f] = uint64(r.ExpFloat64() * 25e6)
			if r.Float64() < 0.95 {
				ix.hasAudio.Add(uint32(f))
			}
		} else {
			ix.size[f] = uint64(r.ExpFloat64() * 3e6)
		}
	}
	ix.tagFiles = make([]*roaring.Bitmap, tags)
	ix.tagCount = make([]int, tags)
	for t, l := range lists {
		bm := roaring.New()
		bm.AddMany(l)
		bm.RunOptimize()
		ix.tagFiles[t] = bm
		ix.tagCount[t] = len(l)
		ix.mappings += len(l)
	}
	ix.hasAudio.RunOptimize()

	// parent ("group") tags: each the union of 10 mid-popularity child tags, precomputed
	for p := 0; p < 7; p++ {
		kids := make([]*roaring.Bitmap, 10)
		for i := range kids {
			kids[i] = ix.tagFiles[100+r.Intn(5000)]
		}
		ix.parents = append(ix.parents, roaring.FastOr(kids...))
	}

	// presorted order for "sort by size"
	ix.bySizeDesc = make([]uint32, files)
	for i := range ix.bySizeDesc {
		ix.bySizeDesc[i] = uint32(i)
	}
	slices.SortFunc(ix.bySizeDesc, func(a, b uint32) int {
		if ix.size[a] > ix.size[b] {
			return -1
		}
		if ix.size[a] < ix.size[b] {
			return 1
		}
		return 0
	})

	// tag names + autocomplete structures
	ix.names = make([]string, tags)
	used := make(map[string]bool, tags)
	for t := range ix.names {
		name := genName(r)
		for used[name] {
			name += "_" + genWord(r)
		}
		used[name] = true
		ix.names[t] = name
	}
	ix.byName = make([]uint32, tags)
	for i := range ix.byName {
		ix.byName[i] = uint32(i)
	}
	sort.Slice(ix.byName, func(a, b int) bool { return ix.names[ix.byName[a]] < ix.names[ix.byName[b]] })
	ix.trigrams = make(map[string]*roaring.Bitmap)
	for t, name := range ix.names {
		for i := 0; i+3 <= len(name); i++ {
			g := name[i : i+3]
			bm := ix.trigrams[g]
			if bm == nil {
				bm = roaring.New()
				ix.trigrams[g] = bm
			}
			bm.Add(uint32(t))
		}
	}
	return ix
}

func genWord(r *rand.Rand) string {
	const cons, vows = "bcdfghjklmnprstvwz", "aeiouy"
	n := 2 + r.Intn(4)
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteByte(cons[r.Intn(len(cons))])
		b.WriteByte(vows[r.Intn(len(vows))])
	}
	return b.String()
}

func genName(r *rand.Rand) string { // booru-like: 1-3 words joined by underscores
	words := 1 + r.Intn(3)
	parts := make([]string, words)
	for i := range parts {
		parts[i] = genWord(r)
	}
	return strings.Join(parts, "_")
}

// ---------------------------------------------------------------- query helpers

// newest returns the number of the first `limit` results, newest first (highest IDs = newest imports).
func newest(bm *roaring.Bitmap, limit int) uint64 {
	it := bm.ReverseIterator()
	var n uint64
	for it.HasNext() && n < uint64(limit) {
		sink += uint64(it.Next())
		n++
	}
	return bm.GetCardinality()
}

// bySize walks the presorted size order and keeps files in the result (limit 0 = the whole result).
func bySize(ix *index, bm *roaring.Bitmap, limit int) uint64 {
	want := int(bm.GetCardinality())
	if limit > 0 && limit < want {
		want = limit
	}
	out := make([]uint32, 0, want)
	for _, f := range ix.bySizeDesc {
		if bm.Contains(f) {
			out = append(out, f)
			if len(out) == want {
				break
			}
		}
	}
	sink += uint64(len(out))
	return bm.GetCardinality()
}

func prefixMatches(ix *index, p string) []uint32 {
	lo := sort.Search(len(ix.byName), func(i int) bool { return ix.names[ix.byName[i]] >= p })
	var out []uint32
	for i := lo; i < len(ix.byName) && strings.HasPrefix(ix.names[ix.byName[i]], p); i++ {
		out = append(out, ix.byName[i])
	}
	return out
}

func matchSubstring(ix *index, s string) []uint32 {
	var bms []*roaring.Bitmap
	for i := 0; i+3 <= len(s); i++ {
		bm := ix.trigrams[s[i:i+3]]
		if bm == nil {
			return nil
		}
		bms = append(bms, bm)
	}
	cand := roaring.FastAnd(bms...)
	if len(bms) == 1 {
		cand = bms[0]
	}
	out := make([]uint32, 0, cand.GetCardinality())
	it := cand.Iterator()
	for it.HasNext() {
		t := it.Next()
		if strings.Contains(ix.names[t], s) { // trigrams can match without the whole string matching
			out = append(out, t)
		}
	}
	return out
}

func top10(ix *index, ids []uint32) []uint32 {
	slices.SortFunc(ids, func(a, b uint32) int { return ix.tagCount[b] - ix.tagCount[a] })
	if len(ids) > 10 {
		ids = ids[:10]
	}
	return ids
}

func wildcardTerms(ix *index, r *rand.Rand, n int) []string {
	out := make([]string, n)
	for i := range out {
		for {
			name := ix.names[r.Intn(len(ix.names))]
			if len(name) >= 5 {
				j := r.Intn(len(name) - 2)
				if !strings.Contains(name[j:j+3], "_") {
					out[i] = name[j : j+3]
					break
				}
			}
		}
	}
	return out
}

// ---------------------------------------------------------------- measuring

// bench times batches of 20 runs and divides (the Windows clock only ticks in ~0.5-1 ms steps, too coarse for
// single sub-millisecond searches). Columns: median batch average, slowest 1% of batch averages, slowest batch.
func bench(name string, iters int, fn func(i int) uint64) {
	const batch = 20
	var d []time.Duration
	var results uint64
	for i := 0; i+batch <= iters; i += batch {
		start := time.Now()
		for j := i; j < i+batch; j++ {
			results += fn(j)
		}
		d = append(d, time.Since(start)/batch)
	}
	slices.Sort(d)
	fmt.Printf("  %-58s %9s %9s %9s %10.0f\n", name, show(d[len(d)/2]), show(d[len(d)*99/100]), show(d[len(d)-1]),
		float64(results)/float64(len(d)*batch))
}

func show(d time.Duration) string {
	switch {
	case d < time.Microsecond:
		return fmt.Sprintf("%dns", d.Nanoseconds())
	case d < time.Millisecond:
		return fmt.Sprintf("%.1fµs", float64(d.Nanoseconds())/1e3)
	default:
		return fmt.Sprintf("%.2fms", float64(d.Nanoseconds())/1e6)
	}
}

func mb(b uint64) float64 { return float64(b) / (1 << 20) }

func bitmapBytes(bms []*roaring.Bitmap) uint64 {
	var n uint64
	for _, bm := range bms {
		n += bm.GetSizeInBytes()
	}
	return n
}

func trigramBytes(m map[string]*roaring.Bitmap) uint64 {
	var n uint64
	for _, bm := range m {
		n += bm.GetSizeInBytes()
	}
	return n
}

// snapshot measures saving all tag bitmaps to one blob and loading them back - the "instant start-up" path.
func snapshot(ix *index) {
	var buf bytes.Buffer
	t0 := time.Now()
	for _, bm := range ix.tagFiles {
		if _, err := bm.WriteTo(&buf); err != nil {
			panic(err)
		}
	}
	save := time.Since(t0)
	size := buf.Len()
	t1 := time.Now()
	rd := bytes.NewReader(buf.Bytes())
	for range ix.tagFiles {
		bm := roaring.New()
		if _, err := bm.ReadFrom(rd); err != nil {
			panic(err)
		}
		sink += bm.GetCardinality()
	}
	load := time.Since(t1)
	fmt.Printf("Index snapshot (all tag bitmaps): %.1f MB, saved in %v, loaded in %v\n\n", mb(uint64(size)),
		save.Round(time.Millisecond), load.Round(time.Millisecond))
}
