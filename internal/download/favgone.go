package download

import (
	"context"
	"database/sql"
	"fmt"
)

// Favorites removed on the site: a subscription to a user's favorites keeps
// which of its posts are in the library (fav_members). A complete check - from
// the newest post to the end, not resumed, not cut short - that no longer
// finds one marks it gone; Downloads then asks whether to delete those files
// (FavGoneDelete) or keep them (FavGoneKeep). A post favorited again is no
// longer gone. A check that found no posts at all marks nothing (an empty
// list is more likely the site failing than every favorite removed).

// noteFavMember records that a favorites subscription's post is in the library.
func (d *Downloader) noteFavMember(ctx context.Context, subID, postID int64, fileID uint32) {
	if _, err := d.db.W.ExecContext(ctx, `
		INSERT INTO fav_members (subscription_id, post_id, file_id) VALUES (?, ?, ?)
		ON CONFLICT (subscription_id, post_id) DO UPDATE SET file_id = excluded.file_id, gone_at = NULL`,
		subID, postID, fileID); err != nil {
		d.log.Warn("noting a favorite", "subscription", subID, "post", postID, "err", err)
	}
}

// markFavGone marks the subscription's posts not in seen (the whole list a
// complete check went through) as gone; it returns how many became gone.
func (d *Downloader) markFavGone(ctx context.Context, subID int64, seen map[int64]bool) (int, error) {
	if len(seen) == 0 {
		return 0, nil
	}
	rows, err := d.db.R.QueryContext(ctx, "SELECT post_id FROM fav_members WHERE subscription_id = ? AND gone_at IS NULL", subID)
	if err != nil {
		return 0, fmt.Errorf("download: %w", err)
	}
	var gone []int64
	for rows.Next() {
		var p int64
		if err := rows.Scan(&p); err != nil {
			rows.Close()
			return 0, fmt.Errorf("download: %w", err)
		}
		if !seen[p] {
			gone = append(gone, p)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(gone) == 0 {
		return 0, err
	}
	tx, err := d.db.W.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("download: %w", err)
	}
	defer tx.Rollback()
	now := d.now().UnixMilli()
	for _, p := range gone {
		if _, err := tx.ExecContext(ctx, "UPDATE fav_members SET gone_at = ? WHERE subscription_id = ? AND post_id = ?", now, subID, p); err != nil {
			return 0, fmt.Errorf("download: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("download: %w", err)
	}
	d.Changed()
	return len(gone), nil
}

// FavGone lists the files of a subscription's posts no longer in the
// favorites (live files only), newest mark first.
func (d *Downloader) FavGone(ctx context.Context, subID int64) ([]uint32, error) {
	rows, err := d.db.R.QueryContext(ctx, `
		SELECT m.file_id FROM fav_members m JOIN files f ON f.id = m.file_id AND f.deleted_at IS NULL
		WHERE m.subscription_id = ? AND m.gone_at IS NOT NULL ORDER BY m.gone_at DESC, m.post_id DESC`, subID)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	defer rows.Close()
	out := []uint32{}
	for rows.Next() {
		var id uint32
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("download: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// favGoneCounts is each subscription's number of files no longer in its favorites.
func (d *Downloader) favGoneCounts(ctx context.Context) (map[int64]int, error) {
	rows, err := d.db.R.QueryContext(ctx, `
		SELECT m.subscription_id, count(*) FROM fav_members m JOIN files f ON f.id = m.file_id AND f.deleted_at IS NULL
		WHERE m.gone_at IS NOT NULL GROUP BY m.subscription_id`)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, fmt.Errorf("download: %w", err)
		}
		out[id] = n
	}
	return out, rows.Err()
}

// FavGoneDelete deletes the files no longer in the subscription's favorites
// (with the library's undo window); it returns how many.
func (d *Downloader) FavGoneDelete(ctx context.Context, subID int64) (int, error) {
	ids, err := d.FavGone(ctx, subID)
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	if err := d.lib.Delete(ctx, ids, "removed from the site's favorites"); err != nil {
		return 0, err
	}
	if _, err := d.db.W.ExecContext(ctx, "DELETE FROM fav_members WHERE subscription_id = ? AND gone_at IS NOT NULL", subID); err != nil {
		return 0, fmt.Errorf("download: %w", err)
	}
	d.Changed()
	return len(ids), nil
}

// FavGoneKeep keeps them: they're no longer asked about (favorited again, they
// are followed again).
func (d *Downloader) FavGoneKeep(ctx context.Context, subID int64) error {
	var res sql.Result
	res, err := d.db.W.ExecContext(ctx, "DELETE FROM fav_members WHERE subscription_id = ? AND gone_at IS NOT NULL", subID)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		d.Changed()
	}
	return nil
}
