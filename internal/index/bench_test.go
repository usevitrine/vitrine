package index

import (
	"fmt"
	"math/rand/v2"
	"os"
	"strconv"
	"sync"
	"testing"

	"github.com/usevitrine/vitrine/internal/media"
	"github.com/usevitrine/vitrine/internal/search"
)

// Benchmarks run on a synthetic library shaped like a real booru collection:
// Zipf-distributed tags (a few very common, most rare), ~55 tags per file.
// Default 100k files / 85k tags; set VITRINE_BENCH_FILES=1000000 for the
// million-file target (search < 20 ms, autocomplete < 10 ms).

var (
	benchOnce sync.Once
	benchIx   *Index
	benchTags []string // tag names by popularity rank
)

func benchIndex(b *testing.B) *Index {
	benchOnce.Do(func() {
		files := 100_000
		if v, err := strconv.Atoi(os.Getenv("VITRINE_BENCH_FILES")); err == nil && v > 0 {
			files = v
		}
		nTags := 85_000 * max(1, files/100_000) / max(1, files/300_000+1) // ~85k at 100k, ~300k at 1M
		if files >= 1_000_000 {
			nTags = 300_000
		}
		benchIx, benchTags = synthetic(files, nTags, 1)
	})
	return benchIx
}

func synthetic(files, nTags int, seed uint64) (*Index, []string) {
	r := rand.New(rand.NewPCG(seed, 2))
	ix := New()
	ix.AddNamespace(1, "creator")
	names := make([]string, nTags)
	tags := make([]Tag, nTags)
	syllables := []string{"ka", "ri", "to", "mu", "sen", "ra", "vo", "ne", "li", "an", "go", "shi", "bel", "da", "or"}
	for i := range names {
		n := ""
		for j := 0; j < 2+r.IntN(3); j++ {
			n += syllables[r.IntN(len(syllables))]
		}
		names[i] = n + "_" + strconv.Itoa(i)
		ns := uint32(0)
		if i%20 == 0 {
			ns = 1
		}
		tags[i] = Tag{ID: uint32(i + 1), NamespaceID: ns, Name: names[i]}
	}
	ix.AddTags(tags...)

	zipf := rand.NewZipf(r, 1.1, 8, uint64(nTags-1))
	now := now.UnixMilli()
	fileTags := make([]uint32, 0, 80)
	for id := 1; id <= files; id++ {
		fileTags = fileTags[:0]
		for range 20 + r.IntN(70) {
			fileTags = append(fileTags, uint32(zipf.Uint64())+1)
		}
		typ := media.JPEG
		var dur uint32
		audio := false
		if r.IntN(10) < 6 {
			typ, dur, audio = media.MP4, uint32(r.IntN(600_000)), r.IntN(10) < 9
		}
		f := File{ID: uint32(id), Size: int64(r.IntN(200 << 20)), Type: typ, DurationMS: dur, HasAudio: audio,
			Width: uint32(r.IntN(4000)), Height: uint32(r.IntN(4000)),
			ImportedAt: now - int64(files-id)*60_000, ModifiedAt: now - int64(r.IntN(files))*60_000}
		ix.AddFile(f, dedupe(fileTags))
	}
	// A group tag over 30 artists (a parent tag used as a group).
	ix.AddTags(Tag{ID: uint32(nTags + 1), Name: "group_artists"})
	var rels []Relation
	for i := 0; i < 30; i++ {
		rels = append(rels, Relation{From: uint32(100 + i*20 + 1), To: uint32(nTags + 1)})
	}
	if err := ix.SetRelations(rels, nil); err != nil {
		panic(err)
	}
	return ix, names
}

func dedupe(ids []uint32) []uint32 {
	seen := make(map[uint32]bool, len(ids))
	out := ids[:0]
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func benchSearch(b *testing.B, text string) {
	ix := benchIndex(b)
	q, err := search.Parse(text)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	var total int
	for b.Loop() {
		total = ix.Search(q, now).Total
	}
	b.ReportMetric(float64(total), "results")
}

func BenchmarkSearch(b *testing.B) {
	benchIndex(b)
	t := benchTags
	cases := []struct{ name, q string }{
		{"everything_newest", "limit:1000"},
		{"popular_tag", t[0] + " limit:1000"},
		{"two_tags", t[0] + " " + t[1] + " limit:1000"},
		{"tag_not_tag", t[0] + " -" + t[1] + " limit:1000"},
		{"or_group", fmt.Sprintf("( %s ~ %s ~ %s ) limit:1000", t[50], t[60], t[70])},
		{"group_audio_5d", "group_artists system:has_audio system:imported<5d"},
		{"modified_5d_newest", "system:modified<5d"},
		{"wildcard_infix", "*senra* limit:1000"},
		{"popular_by_size_all", t[0] + " sort:size"},
		{"popular_by_size_1000", t[0] + " sort:size limit:1000"},
		{"everything_by_size_all", "sort:size"},
		{"everything_random", "sort:random_1"},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) { benchSearch(b, c.q) })
	}
}

func BenchmarkAutocomplete(b *testing.B) {
	ix := benchIndex(b)
	for _, prefix := range []string{"k", "kari", "creator:", "*senra*"} {
		b.Run(prefix, func(b *testing.B) {
			var n int
			for b.Loop() {
				n = len(ix.Autocomplete(prefix, 20, ""))
			}
			b.ReportMetric(float64(n), "results")
		})
	}
}

func BenchmarkUpdateTags(b *testing.B) {
	ix := benchIndex(b)
	add, remove := []uint32{5, 6, 7}, []uint32{5, 6, 7}
	i := uint32(0)
	for b.Loop() {
		id := i%50_000 + 1
		ix.UpdateTags(id, add, nil, 0)
		ix.UpdateTags(id, nil, remove, 0)
		i++
	}
}
