package library

import (
	"context"
	"database/sql"
	"fmt"
	"html"
)

// Source links kept with an HTML code in them (2026-11-13, the user: "sometimes when i refresh tags on an image it
// grabs the same source again"): a site's JSON API sends a post's source text as the site stores it ("...?a=1&amp;s=46"),
// its XML API decoded, so a tag refresh added a link the download had already given. plugin.SourceLinks decodes them
// since; this fix (the fixes menu: Run or Skip) turns the ones already kept into their decoded form, merged with that
// one where a file has both.

const sourceLinksRepair = "source_links"

// codedSourceSQL: the files' source links with "&amp;" in them.
const codedSourceSQL = `FROM file_urls fu JOIN urls u ON u.id = fu.url_id WHERE fu.kind = 2 AND u.url LIKE '%&amp;%'`

// AddSourceLinksRepair offers RunSourceLinks where there's a coded link to fix.
func (l *Library) AddSourceLinksRepair() {
	l.AddRepair(sourceLinksRepair, func(ctx context.Context) (int64, int64, error) {
		n, err := l.codedSources(ctx)
		return 0, n, err
	}, l.RunSourceLinks)
}

func (l *Library) codedSources(ctx context.Context) (int64, error) {
	var n int64
	if err := l.db.R.QueryRowContext(ctx, "SELECT count(*) "+codedSourceSQL).Scan(&n); err != nil {
		return 0, fmt.Errorf("library: %w", err)
	}
	return n, nil
}

// RunSourceLinks decodes the coded source links, a batch of links at a time (stopped half-way, it carries on: what's
// done is done). Progress counts the files' links.
func (l *Library) RunSourceLinks(ctx context.Context) error {
	n, err := l.codedSources(ctx)
	if err != nil {
		return err
	}
	l.repairLeft(ctx, sourceLinksRepair, n)
	for {
		var fixed int64
		more := false
		err := l.write(ctx, func(tx *sql.Tx) (func() error, error) {
			rows, err := tx.QueryContext(ctx, "SELECT DISTINCT u.id, u.url "+codedSourceSQL+" LIMIT 200")
			if err != nil {
				return nil, err
			}
			type coded struct {
				id  int64
				url string
			}
			var list []coded
			for rows.Next() {
				var c coded
				if err := rows.Scan(&c.id, &c.url); err != nil {
					rows.Close()
					return nil, err
				}
				list = append(list, c)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return nil, err
			}
			more = len(list) > 0
			for _, c := range list {
				var to int64
				if err := tx.QueryRowContext(ctx, `INSERT INTO urls (url) VALUES (?) ON CONFLICT (url) DO UPDATE SET url = excluded.url
					RETURNING id`, html.UnescapeString(c.url)).Scan(&to); err != nil {
					return nil, err
				}
				if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO file_urls (file_id, url_id, kind, site_id)
					SELECT file_id, ?, 2, site_id FROM file_urls WHERE url_id = ? AND kind = 2`, to, c.id); err != nil {
					return nil, err
				}
				res, err := tx.ExecContext(ctx, "DELETE FROM file_urls WHERE url_id = ? AND kind = 2", c.id)
				if err != nil {
					return nil, err
				}
				k, _ := res.RowsAffected()
				fixed += k
				if _, err := tx.ExecContext(ctx, "DELETE FROM urls WHERE id = ? AND NOT EXISTS (SELECT 1 FROM file_urls WHERE url_id = ?)", c.id, c.id); err != nil {
					return nil, err
				}
			}
			return nil, nil
		})
		if err != nil {
			return fmt.Errorf("library: source links: %w", err)
		}
		if !more {
			return nil
		}
		l.stepRepair(ctx, sourceLinksRepair, fixed, fixed)
	}
}
