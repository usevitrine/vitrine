package download

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/usevitrine/vitrine/internal/library"
)

// Subscription groups: a name the user picks, on one site (like "Artists" on
// a booru). A subscription is in at most one group; group actions (check now,
// pause, resume, the check interval) apply to every subscription in it.

// Group is a named set of subscriptions on one site.
type Group struct {
	ID       int64  `json:"id"`
	Site     string `json:"site"`
	Name     string `json:"name"`
	Interval int64  `json:"interval,omitempty"` // its usual schedule in seconds (0 = none set)
	// CheckDepth: its subscriptions' check depth, if set on the group (nil = from the site or
	// the setting for all; a subscription's own wins).
	CheckDepth *int  `json:"check_depth,omitempty"`
	Depth      int   `json:"depth"` // the one its subscriptions get when they set none
	CreatedAt  int64 `json:"created_at"`
	siteID     uint32
}

var (
	// ErrNoGroup means there is no group with that ID.
	ErrNoGroup = errors.New("no such group")
	// ErrGroupExists means the site has a group with that name already.
	ErrGroupExists = errors.New("that site already has a group with that name")
	// ErrGroupName means a group name is empty or too long.
	ErrGroupName = errors.New("a group name needs 1 to 60 characters")
	// ErrGroupSite means a subscription and a group are on different sites.
	ErrGroupSite = errors.New("the group is for another site")
)

func groupName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" || utf8.RuneCountInString(name) > 60 {
		return "", ErrGroupName
	}
	return name, nil
}

const groupColumns = `g.id, si.name, g.site_id, g.name, coalesce(g.interval_s, 0), g.created_at, g.check_depth`

func scanGroup(row interface{ Scan(...any) error }) (Group, error) {
	var g Group
	err := row.Scan(&g.ID, &g.Site, &g.siteID, &g.Name, &g.Interval, &g.CreatedAt, &g.CheckDepth)
	return g, err
}

// Groups lists every group by site and name.
func (d *Downloader) Groups(ctx context.Context) ([]Group, error) {
	rows, err := d.db.R.QueryContext(ctx, "SELECT "+groupColumns+
		" FROM sub_groups g JOIN sites si ON si.id = g.site_id ORDER BY si.name, g.name COLLATE NOCASE")
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	defer rows.Close()
	out := []Group{}
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if ds, err := d.depthSource(ctx); err == nil {
		for i := range out {
			out[i].Depth = ds.ofGroup(out[i].ID, out[i].Site)
		}
	}
	return out, nil
}

// GetGroup returns one group.
func (d *Downloader) GetGroup(ctx context.Context, id int64) (Group, error) {
	g, err := scanGroup(d.db.R.QueryRowContext(ctx, "SELECT "+groupColumns+
		" FROM sub_groups g JOIN sites si ON si.id = g.site_id WHERE g.id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return g, ErrNoGroup
	}
	if err != nil {
		return g, fmt.Errorf("download: %w", err)
	}
	if ds, err := d.depthSource(ctx); err == nil {
		g.Depth = ds.ofGroup(g.ID, g.Site)
	}
	return g, nil
}

// SetGroupCheckDepth sets a group's check depth (n < 0: none, its site's or the setting for all applies).
func (d *Downloader) SetGroupCheckDepth(ctx context.Context, id int64, n int) error {
	if _, err := d.GetGroup(ctx, id); err != nil {
		return err
	}
	var v any
	if n >= 0 {
		v = min(n, library.MaxCheckDepth)
	}
	if _, err := d.db.W.ExecContext(ctx, "UPDATE sub_groups SET check_depth = ? WHERE id = ?", v, id); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	return nil
}

// CreateGroup makes a group on a site; a group of that name there already is
// returned instead (so "new group" with an existing name just uses it).
func (d *Downloader) CreateGroup(ctx context.Context, site, name string) (int64, error) {
	st, ok := d.all()[site]
	if !ok {
		return 0, fmt.Errorf("%w: %q", ErrUnknownSite, site)
	}
	name, err := groupName(name)
	if err != nil {
		return 0, err
	}
	var id int64
	err = d.db.W.QueryRowContext(ctx, "SELECT id FROM sub_groups WHERE site_id = ? AND name = ? COLLATE NOCASE", st.ID, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("download: %w", err)
	}
	res, err := d.db.W.ExecContext(ctx, "INSERT INTO sub_groups (site_id, name, created_at) VALUES (?, ?, ?)",
		st.ID, name, d.now().UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("download: %w", err)
	}
	return res.LastInsertId()
}

// RenameGroup renames a group.
func (d *Downloader) RenameGroup(ctx context.Context, id int64, name string) error {
	g, err := d.GetGroup(ctx, id)
	if err != nil {
		return err
	}
	name, err = groupName(name)
	if err != nil {
		return err
	}
	var other int64
	err = d.db.W.QueryRowContext(ctx, "SELECT id FROM sub_groups WHERE site_id = ? AND name = ? COLLATE NOCASE AND id != ?",
		g.siteID, name, id).Scan(&other)
	if err == nil {
		return ErrGroupExists
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("download: %w", err)
	}
	_, err = d.db.W.ExecContext(ctx, "UPDATE sub_groups SET name = ? WHERE id = ?", name, id)
	return err
}

// DeleteGroup removes a group; its subscriptions stay, ungrouped.
func (d *Downloader) DeleteGroup(ctx context.Context, id int64) error {
	tx, err := d.db.W.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE subscriptions SET group_id = NULL WHERE group_id = ?", id); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	res, err := tx.ExecContext(ctx, "DELETE FROM sub_groups WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoGroup
	}
	return tx.Commit()
}

// SetGroup moves a subscription into a group (0 = no group).
func (d *Downloader) SetGroup(ctx context.Context, subID, groupID int64) error {
	sub, err := d.GetSubscription(ctx, subID)
	if err != nil {
		return err
	}
	var group any
	if groupID != 0 {
		g, err := d.GetGroup(ctx, groupID)
		if err != nil {
			return err
		}
		if g.siteID != sub.siteID {
			return ErrGroupSite
		}
		group = groupID
	}
	_, err = d.db.W.ExecContext(ctx, "UPDATE subscriptions SET group_id = ? WHERE id = ?", group, subID)
	return err
}

// members lists a group's subscription IDs.
func (d *Downloader) members(ctx context.Context, groupID int64) ([]int64, error) {
	if _, err := d.GetGroup(ctx, groupID); err != nil {
		return nil, err
	}
	rows, err := d.db.R.QueryContext(ctx, "SELECT id FROM subscriptions WHERE group_id = ? ORDER BY id", groupID)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GroupAction checks, pauses or resumes every subscription in a group
// (action: "check", "pause" or "resume").
func (d *Downloader) GroupAction(ctx context.Context, groupID int64, action string) error {
	ids, err := d.members(ctx, groupID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		switch action {
		case "check":
			err = d.CheckNow(ctx, id)
		case "pause", "resume":
			p := action == "pause"
			err = d.UpdateSubscription(ctx, id, SubscriptionChange{Paused: &p})
		default:
			return fmt.Errorf("%w: unknown group action %q", ErrState, action)
		}
		if err != nil && !errors.Is(err, ErrNoSubscription) {
			return err
		}
	}
	return nil
}

// SetGroupInterval sets how often every subscription in a group is checked,
// and makes it the group's usual schedule.
func (d *Downloader) SetGroupInterval(ctx context.Context, groupID int64, every time.Duration) error {
	iv, err := checkInterval(every)
	if err != nil {
		return err
	}
	ids, err := d.members(ctx, groupID)
	if err != nil {
		return err
	}
	if _, err := d.db.W.ExecContext(ctx, "UPDATE sub_groups SET interval_s = ? WHERE id = ?", iv, groupID); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	for _, id := range ids {
		if err := d.UpdateSubscription(ctx, id, SubscriptionChange{Interval: &every}); err != nil && !errors.Is(err, ErrNoSubscription) {
			return err
		}
	}
	return nil
}
