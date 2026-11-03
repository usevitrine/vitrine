package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
)

// Repairs (2026-11-03, the user): fixes an update brings for what's already in a library - videos read again, tags put
// back. Each is offered once there's something for it to do, with what it does (the page's words): the user runs it or
// skips it, and follows it in the top bar until they've acknowledged that it finished. One that had begun goes on after
// a restart; one that failed waits to be tried again or skipped. A library with nothing to fix never hears of it. Kept
// in meta (repair:<id>), the same for every device; how far each pass got is its own business (its own meta key).

// A repair's states.
const (
	RepairOffered = "offered"
	RepairRunning = "running"
	RepairDone    = "done"
	RepairFailed  = "failed"
	RepairSkipped = "skipped"
	repairNone    = "none" // nothing to fix when it was looked for
)

// repairResumeAfter: one that had begun carries on this long after a start, after the start's own work.
const repairResumeAfter = 5 * time.Minute

// ErrNoRepair: no such repair, or it can't do that now (only a finished one is acknowledged, say).
var ErrNoRepair = errors.New("no such fix waiting for that")

// Repair is one fix as the page shows it.
type Repair struct {
	ID         string `json:"id"`
	State      string `json:"state"`
	Done       int64  `json:"done"`
	Total      int64  `json:"total"`
	Changed    int64  `json:"changed"`
	Error      string `json:"error,omitempty"`
	OfferedAt  int64  `json:"offered_at"` // Unix ms
	FinishedAt int64  `json:"finished_at,omitempty"`
	Acked      bool   `json:"acked,omitempty"`
}

type repairDef struct {
	id   string
	find func(ctx context.Context) (done, total int64, err error) // done > 0: it had begun (before fixes were offered)
	run  func(ctx context.Context) error
}

type repairs struct {
	mu      sync.Mutex
	defs    []repairDef
	list    map[string]*Repair
	version int64
	ctx     context.Context // (what a run started from the page lives under: the server's)
	wait    time.Duration
}

// AddRepair names a fix: find says how much it has to do, run does it (and can be run again after a stop).
func (l *Library) AddRepair(id string, find func(ctx context.Context) (done, total int64, err error), run func(ctx context.Context) error) {
	l.rep.mu.Lock()
	defer l.rep.mu.Unlock()
	l.rep.defs = append(l.rep.defs, repairDef{id, find, run})
}

// StartRepairs reads what's kept, offers what's new with something to do and lets go on what had begun. ctx: the
// server's life.
func (l *Library) StartRepairs(ctx context.Context) error {
	l.rep.mu.Lock()
	l.rep.ctx = ctx
	if l.rep.wait == 0 {
		l.rep.wait = repairResumeAfter
	}
	defs := slices.Clone(l.rep.defs)
	l.rep.mu.Unlock()
	if err := l.loadRepairs(ctx); err != nil {
		return err
	}
	for _, d := range defs {
		l.rep.mu.Lock()
		r := l.rep.list[d.id]
		l.rep.mu.Unlock()
		if r != nil {
			if r.State == RepairRunning {
				go l.runRepair(ctx, d, l.rep.wait)
			}
			continue
		}
		done, total, err := d.find(ctx)
		if err != nil {
			return fmt.Errorf("library: looking for what %s would fix: %w", d.id, err)
		}
		n := &Repair{ID: d.id, Done: done, Total: total, OfferedAt: l.now().UnixMilli()}
		switch {
		case total == 0:
			n.State, n.Acked = repairNone, true
		case done > 0:
			n.State = RepairRunning
		default:
			n.State = RepairOffered
		}
		if err := l.putRepair(ctx, n); err != nil {
			return err
		}
		switch n.State {
		case RepairRunning:
			go l.runRepair(ctx, d, l.rep.wait)
		case RepairOffered:
			l.tellRepair(*n)
		}
	}
	return nil
}

func (l *Library) loadRepairs(ctx context.Context) error {
	rows, err := l.db.R.QueryContext(ctx, "SELECT value FROM meta WHERE key >= 'repair:' AND key < 'repair;'")
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	defer rows.Close()
	list := map[string]*Repair{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return fmt.Errorf("library: %w", err)
		}
		var r Repair
		if json.Unmarshal([]byte(v), &r) == nil && r.ID != "" {
			list[r.ID] = &r
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("library: %w", err)
	}
	l.rep.mu.Lock()
	l.rep.list = list
	l.rep.mu.Unlock()
	return nil
}

// putRepair keeps a repair as it is now and lets pages know.
func (l *Library) putRepair(ctx context.Context, r *Repair) error {
	b, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	l.mu.Lock()
	_, err = l.db.W.ExecContext(ctx, "INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value", "repair:"+r.ID, string(b))
	l.mu.Unlock()
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	l.rep.mu.Lock()
	if l.rep.list == nil {
		l.rep.list = map[string]*Repair{}
	}
	c := *r
	l.rep.list[r.ID] = &c
	l.rep.version++
	l.rep.mu.Unlock()
	return nil
}

func (l *Library) tellRepair(r Repair) {
	if l.OnRepair != nil {
		l.OnRepair(r)
	}
}

// Repairs lists the fixes that have something to show: offered, running, failed, or finished and not yet acknowledged.
func (l *Library) Repairs() (out []Repair, version int64) {
	l.rep.mu.Lock()
	defer l.rep.mu.Unlock()
	out = []Repair{}
	for _, r := range l.rep.list {
		if !r.Acked && r.State != RepairSkipped && r.State != repairNone {
			out = append(out, *r)
		}
	}
	slices.SortFunc(out, func(a, b Repair) int {
		if a.OfferedAt != b.OfferedAt {
			return int(a.OfferedAt - b.OfferedAt)
		}
		if a.ID < b.ID {
			return -1
		}
		return 1
	})
	return out, l.rep.version
}

// repairNow is a copy of a repair that's in one of the states, and its definition.
func (l *Library) repairNow(id string, states ...string) (*Repair, repairDef, error) {
	l.rep.mu.Lock()
	defer l.rep.mu.Unlock()
	r := l.rep.list[id]
	if r == nil || !slices.Contains(states, r.State) || r.Acked {
		return nil, repairDef{}, ErrNoRepair
	}
	c := *r
	for _, d := range l.rep.defs {
		if d.id == id {
			return &c, d, nil
		}
	}
	return &c, repairDef{}, nil
}

// RunRepair starts a fix that's offered, or one that failed again.
func (l *Library) RunRepair(ctx context.Context, id string) error {
	r, d, err := l.repairNow(id, RepairOffered, RepairFailed)
	if err != nil {
		return err
	}
	l.rep.mu.Lock()
	life := l.rep.ctx
	l.rep.mu.Unlock()
	if d.run == nil || life == nil {
		return ErrNoRepair
	}
	r.State, r.Error = RepairRunning, ""
	if err := l.putRepair(ctx, r); err != nil {
		return err
	}
	go l.runRepair(life, d, 0)
	return nil
}

// SkipRepair leaves a fix undone: it isn't offered again.
func (l *Library) SkipRepair(ctx context.Context, id string) error {
	r, _, err := l.repairNow(id, RepairOffered, RepairFailed)
	if err != nil {
		return err
	}
	r.State = RepairSkipped
	return l.putRepair(ctx, r)
}

// AckRepair takes a fix that finished off the list.
func (l *Library) AckRepair(ctx context.Context, id string) error {
	r, _, err := l.repairNow(id, RepairDone)
	if err != nil {
		return err
	}
	r.Acked = true
	return l.putRepair(ctx, r)
}

func (l *Library) runRepair(ctx context.Context, d repairDef, wait time.Duration) {
	if wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return
		}
	}
	err := d.run(ctx)
	if ctx.Err() != nil {
		return // (stopping: it goes on at the next start)
	}
	r, _, e := l.repairNow(d.id, RepairRunning)
	if e != nil {
		return
	}
	r.FinishedAt = l.now().UnixMilli()
	if err != nil {
		r.State, r.Error = RepairFailed, err.Error()
	} else {
		r.State = RepairDone
		r.Total = r.Done
	}
	if e := l.putRepair(context.WithoutCancel(ctx), r); e != nil {
		return
	}
	l.tellRepair(*r)
}

// stepRepair adds to how far a running fix got (none running by that name: nothing).
func (l *Library) stepRepair(ctx context.Context, id string, done, changed int64) {
	r, _, err := l.repairNow(id, RepairRunning)
	if err != nil {
		return
	}
	r.Done += done
	r.Changed += changed
	r.Total = max(r.Total, r.Done) // (new files came meanwhile)
	l.putRepair(ctx, r)
}

// repairLeft says how much a fix that's about to work still has (it had done some before a restart, say).
func (l *Library) repairLeft(ctx context.Context, id string, left int64) {
	r, _, err := l.repairNow(id, RepairRunning)
	if err != nil {
		return
	}
	r.Total = r.Done + left
	l.putRepair(ctx, r)
}

// AddVideoShapesRepair offers RunVideoShapes.
func (l *Library) AddVideoShapesRepair() {
	l.AddRepair("video_shapes", func(ctx context.Context) (int64, int64, error) {
		if l.tools.Check() != nil {
			return 0, 0, nil
		}
		var last int64
		err := l.db.R.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = 'video_shapes'").Scan(&last)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return 0, 0, err
		}
		if last < 0 {
			return 0, 0, nil
		}
		var done, total int64
		err = l.db.R.QueryRowContext(ctx, `SELECT count(*), coalesce(sum(id <= ?), 0) FROM files
			WHERE type BETWEEN 6 AND 9 AND deleted_at IS NULL AND flags & ? != 0`, last, FlagProbed).Scan(&total, &done)
		return done, total, err
	}, func(ctx context.Context) error { return l.RunVideoShapes(ctx, nil) })
}

// AddGeneralTagsRepair offers RunGeneralTags.
func (l *Library) AddGeneralTagsRepair(general func(site uint32, typ string) bool) {
	l.AddRepair("general_tags", func(ctx context.Context) (int64, int64, error) {
		var mark string
		err := l.db.R.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = 'general_tags'").Scan(&mark)
		if err == nil {
			return 0, 0, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, 0, err
		}
		names, err := l.generalTags(ctx, general)
		return 0, int64(len(names)), err
	}, func(ctx context.Context) error { return l.RunGeneralTags(ctx, general, nil) })
}
