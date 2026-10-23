package library

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// What outlives a restart besides the library itself: the bell's notices (the downloader's events, the last
// NoticesKept) and the sites paused because they kept failing to answer.

// NoticesKept is how many notices are kept.
const NoticesKept = 100

// Notice is one of the downloader's events for the bell, numbered.
type Notice struct {
	ID           int64  `json:"id"`
	At           int64  `json:"at"` // Unix ms
	Kind         string `json:"kind"`
	Site         string `json:"site,omitempty"`
	Query        string `json:"query,omitempty"`
	Subscription bool   `json:"subscription,omitempty"`
	New          int    `json:"new,omitempty"`
	Error        string `json:"error,omitempty"`
	Tag          string `json:"tag,omitempty"`
	First        bool   `json:"first,omitempty"`
	// Done in the app (kind "app"): its words, tone (done, info, removed, error) and kind of notice (its setting).
	Text     string `json:"text,omitempty"`
	Tone     string `json:"tone,omitempty"`
	Category string `json:"category,omitempty"`
}

// AddNotice stores a notice (the oldest beyond NoticesKept go) and returns its ID.
func (l *Library) AddNotice(ctx context.Context, n Notice) (int64, error) {
	res, err := l.db.W.ExecContext(ctx, `INSERT INTO notices (at, kind, site, query, subscription, new, error, tag, first, text, tone, category)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, n.At, n.Kind, n.Site, n.Query, n.Subscription, n.New, n.Error, n.Tag, n.First, n.Text, n.Tone, n.Category)
	if err != nil {
		return 0, fmt.Errorf("library: %w", err)
	}
	id, _ := res.LastInsertId()
	if _, err := l.db.W.ExecContext(ctx, "DELETE FROM notices WHERE id <= ?", id-NoticesKept); err != nil {
		return id, fmt.Errorf("library: %w", err)
	}
	return id, nil
}

// Notices returns the kept notices, oldest first.
func (l *Library) Notices(ctx context.Context) ([]Notice, error) {
	rows, err := l.db.R.QueryContext(ctx, `SELECT id, at, kind, site, query, subscription, new, error, tag, first, text, tone, category
		FROM notices ORDER BY id DESC LIMIT ?`, NoticesKept)
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	defer rows.Close()
	var out []Notice
	for rows.Next() {
		var n Notice
		if err := rows.Scan(&n.ID, &n.At, &n.Kind, &n.Site, &n.Query, &n.Subscription, &n.New, &n.Error, &n.Tag, &n.First, &n.Text, &n.Tone, &n.Category); err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		out = append(out, n)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}

// NoticesBoot names this library's run of notice IDs (made once): a page tells notices it has seen by it and their
// ID, so it stays the same over restarts.
func (l *Library) NoticesBoot(ctx context.Context) (string, error) {
	b := make([]byte, 6)
	rand.Read(b)
	if _, err := l.db.W.ExecContext(ctx, "INSERT INTO meta (key, value) VALUES ('notices_boot', ?) ON CONFLICT (key) DO NOTHING",
		hex.EncodeToString(b)); err != nil {
		return "", fmt.Errorf("library: %w", err)
	}
	var boot string
	if err := l.db.R.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = 'notices_boot'").Scan(&boot); err != nil {
		return "", fmt.Errorf("library: %w", err)
	}
	return boot, nil
}

// SitePause is a site paused because it kept failing to answer (times in Unix ms).
type SitePause struct {
	Since, Until int64
	Reason       string
	Tries        int
}

// SitePauses returns the paused sites, by plugin ID.
func (l *Library) SitePauses(ctx context.Context) (map[string]SitePause, error) {
	rows, err := l.db.R.QueryContext(ctx, "SELECT site, since, until, reason, tries FROM site_pauses")
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	defer rows.Close()
	out := map[string]SitePause{}
	for rows.Next() {
		var site string
		var p SitePause
		if err := rows.Scan(&site, &p.Since, &p.Until, &p.Reason, &p.Tries); err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		out[site] = p
	}
	return out, rows.Err()
}

// SetSitePause keeps a site's pause (nil: it isn't paused any more).
func (l *Library) SetSitePause(ctx context.Context, site string, p *SitePause) error {
	var err error
	if p == nil {
		_, err = l.db.W.ExecContext(ctx, "DELETE FROM site_pauses WHERE site = ?", site)
	} else {
		_, err = l.db.W.ExecContext(ctx, `INSERT INTO site_pauses (site, since, until, reason, tries) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (site) DO UPDATE SET since = excluded.since, until = excluded.until, reason = excluded.reason, tries = excluded.tries`,
			site, p.Since, p.Until, p.Reason, p.Tries)
	}
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	return nil
}

// NoticeMarks are how far the bell's notices are read and cleared (notice IDs; 0: none) - for every device.
func (l *Library) NoticeMarks(ctx context.Context) (read, cleared int64, err error) {
	rows, err := l.db.R.QueryContext(ctx, "SELECT key, value FROM meta WHERE key IN ('notices_read', 'notices_cleared')")
	if err != nil {
		return 0, 0, fmt.Errorf("library: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var v int64
		if err := rows.Scan(&k, &v); err != nil {
			return 0, 0, fmt.Errorf("library: %w", err)
		}
		if k == "notices_read" {
			read = v
		} else {
			cleared = v
		}
	}
	return read, cleared, rows.Err()
}

// SetNoticeMarks keeps how far the bell's notices are read and cleared.
func (l *Library) SetNoticeMarks(ctx context.Context, read, cleared int64) error {
	_, err := l.db.W.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES ('notices_read', ?), ('notices_cleared', ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, read, cleared)
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	return nil
}
