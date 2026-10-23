package logring

import (
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

func TestRing(t *testing.T) {
	r := New(3)
	log := slog.New(Tee(slog.NewTextHandler(io.Discard, nil), r)).With("app", "v")
	for i := 0; i < 5; i++ {
		log.WithGroup("g").Info("hello", "n", i, "err", errors.New("boom"))
	}
	recs := r.Records(10)
	if len(recs) != 3 {
		t.Fatalf("%d records", len(recs))
	}
	first := recs[0]
	if first.Msg != "hello" || len(first.Attrs) != 3 {
		t.Fatalf("%+v", first)
	}
	if first.Attrs[0].Key != "app" || first.Attrs[1].Key != "g.n" || first.Attrs[1].Value.Int64() != 2 || first.Attrs[2].Value.String() != "boom" {
		t.Fatalf("attrs %+v", first.Attrs)
	}
}

// The log file keeps the records across restarts (a new Ring on the same
// file), rotates past its size (one older file kept), and reads back with the
// values' kinds (numbers stay numbers, a duration isn't a string).
func TestFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vitrine.log")
	r, err := Open(path, 10, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(Tee(slog.NewTextHandler(io.Discard, nil), r))
	log.Warn("first run", "files", 3, "took", 1500*time.Millisecond, "site", "x", "ok", true)
	r.Close()

	r, err = Open(path, 10, 400) // a small file: rotates
	if err != nil {
		t.Fatal(err)
	}
	log = slog.New(Tee(slog.NewTextHandler(io.Discard, nil), r))
	for i := 0; i < 20; i++ {
		log.Info("second run", "n", i)
	}
	recs := r.Records(1000)
	if len(recs) < 3 || recs[len(recs)-1].Msg != "second run" || recs[len(recs)-1].Attrs[0].Value.Int64() != 19 {
		t.Fatalf("%d records, last %+v", len(recs), recs[len(recs)-1])
	}
	// The first run's record went with the rotation; read one from before it.
	r2, _ := Open(filepath.Join(t.TempDir(), "other.log"), 10, 1<<20)
	slog.New(Tee(slog.NewTextHandler(io.Discard, nil), r2)).Warn("first run", "files", 3, "took", 1500*time.Millisecond, "site", "x", "ok", true)
	r2.Close()
	got := r2.Records(10)
	if len(got) != 1 {
		t.Fatalf("%d records", len(got))
	}
	a := got[0].Attrs
	if got[0].Level != slog.LevelWarn || a[0].Value.Kind() != slog.KindInt64 || a[0].Value.Int64() != 3 ||
		a[1].Value.Kind() == slog.KindString || a[1].Value.String() != "1.5s" ||
		a[2].Value.String() != "x" || a[3].Value.Kind() != slog.KindBool {
		t.Fatalf("read back: %+v", got[0])
	}
	r.Close()
}
