package download

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Managing searches: pause, resume, remove, clear finished, reorder.
// A running search notices a pause or removal between posts, so the file in
// flight always finishes (never a half-written file) and nothing is skipped:
// resuming continues from the search's low_id.

// ErrNoSearch means there is no search with that ID.
var ErrNoSearch = errors.New("no such search")

// ErrState means the search can't do that in its current state (e.g.
// resuming one that is already done).
var ErrState = errors.New("not possible in the search's current state")

// stop asks a running search to stop before its next post; a page request
// it's waiting for is canceled at once (files already downloading finish).
func (d *Downloader) stop(id int64) {
	d.mu.Lock()
	d.stops[id] = true
	cancel := d.pageCancel[id]
	d.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// trackPage notes (cancel set) or forgets (nil) a search's page request in
// flight. Asked to stop already: canceled straight away.
func (d *Downloader) trackPage(id int64, cancel context.CancelFunc) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if cancel == nil {
		delete(d.pageCancel, id)
		return
	}
	d.pageCancel[id] = cancel
	if d.stops[id] {
		cancel()
	}
}

// stopped reports (and clears, when done is true) a stop request.
func (d *Downloader) stopped(id int64, done bool) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.stops[id]
	if done {
		delete(d.stops, id)
	}
	return s
}

func (d *Downloader) change(ctx context.Context, id int64, query string, args ...any) error {
	res, err := d.db.W.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := d.Get(ctx, id); errors.Is(err, sql.ErrNoRows) {
			return ErrNoSearch
		}
		return ErrState
	}
	return nil
}

// Pause stops a queued or running search; a running one stops after the
// file in flight.
func (d *Downloader) Pause(ctx context.Context, id int64) error {
	if err := d.change(ctx, id,
		"UPDATE searches SET state = 'paused' WHERE id = ? AND state IN ('queued', 'running')", id); err != nil {
		return err
	}
	d.stop(id)
	return nil
}

// Resume puts a paused or failed search back in its queue (at its place);
// it continues from where it stopped.
func (d *Downloader) Resume(ctx context.Context, id int64) error {
	if err := d.change(ctx, id, `UPDATE searches SET state = 'queued', error = NULL, finished_at = NULL, retry_at = NULL
		WHERE id = ? AND state IN ('paused', 'failed')`, id); err != nil {
		return err
	}
	d.stopped(id, true) // a stop for a pause that's being undone no longer applies
	if s, err := d.Get(ctx, id); err == nil {
		d.wake(s.Site)
	}
	return nil
}

// Remove takes a search off the list (a running one stops after the file in
// flight). Its downloaded files stay in the library. Removing a
// subscription's check skips that check: the next one is a full interval away.
func (d *Downloader) Remove(ctx context.Context, id int64) error {
	d.stop(id)
	var sub sql.NullInt64
	err := d.db.W.QueryRowContext(ctx, "DELETE FROM searches WHERE id = ? RETURNING subscription_id", id).Scan(&sub)
	if errors.Is(err, sql.ErrNoRows) {
		d.stopped(id, true)
		return ErrNoSearch
	}
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	if sub.Valid {
		if _, err := d.db.W.ExecContext(ctx, "UPDATE subscriptions SET next_check_at = ? + interval_s * 1000 + CAST(min(interval_s * 100, 1800000) * ? AS INTEGER) WHERE id = ?",
			d.now().UnixMilli(), d.jitter(), sub.Int64); err != nil {
			return fmt.Errorf("download: %w", err)
		}
	}
	return nil
}

// ClearFinished removes every finished (done) search; returns how many.
func (d *Downloader) ClearFinished(ctx context.Context) (int, error) {
	res, err := d.db.W.ExecContext(ctx, "DELETE FROM searches WHERE state = 'done'")
	if err != nil {
		return 0, fmt.Errorf("download: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// Reorder sets the order of a site's queue: ids first, in that order, then
// the site's other searches in their current order. A change above a running
// search takes effect when it finishes its current file... in practice, when
// the worker next picks a search.
func (d *Downloader) Reorder(ctx context.Context, site string, ids []int64) error {
	st, ok := d.all()[site]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownSite, site)
	}
	tx, err := d.db.W.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT id FROM searches WHERE site_id = ? ORDER BY position", st.ID)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	var rest []int64
	in := map[int64]bool{}
	for _, id := range ids {
		in[id] = true
	}
	known := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		known[id] = true
		if !in[id] {
			rest = append(rest, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		if !known[id] {
			return fmt.Errorf("download: search %d on %s: %w", id, site, ErrNoSearch)
		}
	}
	for i, id := range append(append([]int64{}, ids...), rest...) {
		if _, err := tx.ExecContext(ctx, "UPDATE searches SET position = ? WHERE id = ?", i+1, id); err != nil {
			return fmt.Errorf("download: %w", err)
		}
	}
	return tx.Commit()
}

func retryWaitsJSON(w []time.Duration) string {
	ms := make([]string, len(w))
	for i, d := range w {
		ms[i] = strconv.FormatInt(d.Milliseconds(), 10)
	}
	return "[" + strings.Join(ms, ",") + "]"
}

// retryFailed puts failed searches whose automatic retry is due back in their
// queues (subscription checks have their own schedule).
func (d *Downloader) retryFailed(ctx context.Context) error {
	rows, err := d.db.W.QueryContext(ctx, `UPDATE searches SET state = 'queued', error = NULL, finished_at = NULL, retry_at = NULL
		WHERE state = 'failed' AND subscription_id IS NULL AND retry_at IS NOT NULL AND retry_at <= ?
		RETURNING (SELECT name FROM sites WHERE id = searches.site_id)`, d.now().UnixMilli())
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	var sites []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			rows.Close()
			return err
		}
		sites = append(sites, s)
	}
	rows.Close()
	for _, s := range sites {
		if _, ok := d.all()[s]; ok {
			d.wake(s)
		}
	}
	return rows.Err()
}
