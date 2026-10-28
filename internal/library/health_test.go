package library

import (
	"context"
	"os"
	"slices"
	"testing"
)

// The health check: a sound library passes; an original removed from the disk is found by the weekly look, one
// changed on the disk by the read-back - each reported once, not again at the next check.
func TestHealthCheck(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	ids := importN(t, l, 4)
	var found []HealthFinding
	note := func(f HealthFinding) { found = append(found, f) }

	h, err := l.HealthCheck(ctx, 1<<30, 0, note)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 || h.DBProblem != "" || h.DBCheckedAt == 0 || h.FilesCheckedAt == 0 || h.LastPassAt == 0 || h.Verified != 4 {
		t.Fatalf("a sound library: %+v, found %+v", h, found)
	}

	gone, changed := l.pathOf(ctx, ids[1]), l.pathOf(ctx, ids[2])
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(changed)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)/2] ^= 0xff // a bit flipped on the disk
	if err := os.WriteFile(changed, data, 0o644); err != nil {
		t.Fatal(err)
	}
	// Due again (a week on for the look, a day for the read-back).
	h.DBCheckedAt, h.FilesCheckedAt, h.ReadAt = 1, 1, 1
	if err := l.saveHealth(ctx, h); err != nil {
		t.Fatal(err)
	}
	h, err = l.HealthCheck(ctx, 1<<30, 0, note)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 || !found[0].Missing || found[0].ID != ids[1] || found[1].Missing || found[1].ID != ids[2] || found[1].Path != changed {
		t.Fatalf("found: %+v", found)
	}
	if h.MissingCount != 1 || !slices.Equal(h.Missing, []uint32{ids[1]}) || h.DamagedCount != 1 || !slices.Equal(h.Damaged, []uint32{ids[2]}) {
		t.Fatalf("health: %+v", h)
	}

	// Checked again: known already - not reported a second time.
	h.FilesCheckedAt, h.ReadAt = 1, 1
	l.saveHealth(ctx, h)
	if _, err := l.HealthCheck(ctx, 1<<30, 0, note); err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 {
		t.Fatalf("reported again: %+v", found)
	}

	// The read-back is daily: run again at once, it reads nothing.
	before, _ := l.Health(ctx)
	after, _ := l.HealthCheck(ctx, 1<<30, 0, note)
	if after.Verified != before.Verified || after.Cursor != before.Cursor {
		t.Fatalf("read again the same day: %+v -> %+v", before, after)
	}
}
