package logring

import (
	"errors"
	"io"
	"log/slog"
	"os"
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

func TestForget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vitrine.log")
	r, err := Open(path, 10, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	log := slog.New(Tee(slog.NewTextHandler(io.Discard, nil), r))
	log.Info("search done", "site", "blue_site")
	log.Info("started")
	log.Info("Blue_Site paused")
	if n := r.Forget("blue_site"); n != 2 {
		t.Errorf("forgot %d, want 2", n)
	}
	log.Info("after")
	for _, recs := range [][]Record{r.Records(10), r.memory()} {
		if len(recs) != 2 || recs[0].Msg != "started" || recs[1].Msg != "after" {
			t.Errorf("records = %+v", recs)
		}
	}
	r.Close()
	r2, _ := Open(path, 10, 1<<20) // (the file, read again)
	defer r2.Close()
	if recs := r2.Records(10); len(recs) != 2 {
		t.Errorf("file = %+v", recs)
	}
}

// Warnings and errors outlast a flood of debug lines: they're also kept in the warnings' log (a quarter of the
// main one's size, but only them), and asked for by level they're picked out of the whole log, not out of the newest
// records. A site wiped goes from both files.
func TestWarningsKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vitrine.log")
	r, err := Open(path, 10, 8000)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	log := slog.New(Tee(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), r))
	log.Warn("an old warning", "site", "blue_site")
	log.Error("an old error")
	for i := 0; i < 500; i++ { // far more than the main log holds
		log.Debug("post", "n", i)
	}
	if recs := r.Records(1000); len(recs) == 0 || recs[0].Msg == "an old warning" {
		t.Fatalf("the main log kept everything (%d records): the test needs it to rotate", len(recs))
	}
	recs := r.RecordsAt(10, slog.LevelWarn)
	if len(recs) != 2 || recs[0].Msg != "an old warning" || recs[1].Level != slog.LevelError {
		t.Fatalf("warnings: %+v", recs)
	}
	if recs := r.RecordsAt(10, slog.LevelError); len(recs) != 1 {
		t.Fatalf("errors: %+v", recs)
	}
	if _, err := os.Stat(WarningsPath(path)); err != nil || filepath.Base(WarningsPath(path)) != "vitrine-warnings.log" {
		t.Fatalf("the warnings' log: %v", err)
	}
	r.Forget("blue_site")
	if recs := r.RecordsAt(10, slog.LevelWarn); len(recs) != 1 {
		t.Fatalf("after forgetting the site: %+v", recs)
	}
}
