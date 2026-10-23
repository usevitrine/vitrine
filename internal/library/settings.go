package library

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/usevitrine/vitrine/internal/i18n"
	"github.com/usevitrine/vitrine/internal/search"
)

// Settings shared by every device. Each key has a checker for its value, so
// only known, sensible values are stored.
var settingCheck = map[string]func(json.RawMessage) error{
	"defaultSearch": func(v json.RawMessage) error {
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			return err
		}
		if len(s) > 2000 {
			return fmt.Errorf("too long")
		}
		_, err := search.Parse(s)
		return err
	},
	"defaultSort":   isOrder,
	"favoritesSort": isOrder, // the Favorites page's own default order
	"startMuted":    isBool,
	// The FlareSolverr service that passes Cloudflare challenges ("" = none).
	"flaresolverrURL": func(v json.RawMessage) error {
		var s string
		if json.Unmarshal(v, &s) != nil || len(s) > 300 {
			return fmt.Errorf("not an address")
		}
		if s != "" && !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
			return fmt.Errorf("must start with http:// or https://")
		}
		return nil
	},
	// New files: everything imported after seenAt (Unix ms, set by "Mark all as
	// seen") is new; shown first in newOrder; newMode "auto" adds arrivals at
	// once, "button" shows the N new files button.
	"seenAt":      numberIn(0, 1e15),
	"newOrder":    oneOf("imported", "imported_asc", "uploaded"),
	"newFromBulk": isBool,           // one-off downloads, first checks and imports count as new (missing = yes)
	"backupAuto":  isBool,           // a backup every day (missing = on)
	"backupKeep":  numberIn(1, 365), // how many backups to keep
	// The trash empties itself of files deleted more than this many days ago (0 / missing = never).
	"trashDays":    numberIn(0, 3650),
	"newMode":      oneOf("auto", "button"),
	"hoverPreview": isBool,
	"cameButton":   isBool, // the viewer's "I came" button and the Rankings page (missing = off)
	// The app's sections: Media (pictures and videos) and Comics, each can be switched off (missing = on) - never both.
	"mediaOff":  isBool,
	"comicsOff": isBool,
	// SFW mode: nothing adult in the app - the "I came" button and Rankings off, no adult sites offered, no adult
	// wording (missing = off).
	"sfwMode": isBool,
	// Signed out, the server shows a plain sign-in page that says nothing about what it's for (no name, logo, icon,
	// title) and none of the app's files (missing = off).
	"anonymousLogin": isBool,
	// Check depth: how many posts a subscription's check goes on past the newest one the last
	// check saw (0 / missing = stops there), for all, and per site (site ID -> posts).
	"checkDepth": numberIn(0, MaxCheckDepth),
	// The app's language for every device: "auto" (each browser's own) or a language tag.
	"language":        isLanguage,
	"siteCheckDepths": siteNumbers(0, MaxCheckDepth),
	// The welcome steps after the first account: the step to show ("finished" = closed; missing = never
	// started, e.g. a setup from before them).
	"welcome": oneOf("", "sections", "storage", "sites", "look", "app", "notify", "rules", "library", "done", "finished"),
	// Download rules: skip videos over N MB / images over N megapixels (0 = no limit).
	"skipVideoMB": numberIn(0, 1_000_000),
	"skipImageMP": numberIn(0, 100_000),
	// Push notifications by kind (missing = on).
	"notifyNew":         isBool,
	"notifyFailed":      isBool,
	"notifyDone":        isBool,
	"notifyBlacklisted": isBool,
	// Where each kind of notice goes (the bell, on screen, nowhere): kind -> "bell" | "screen" | "off"; a kind left
	// out has its default (Settings > Notifications).
	"noticeModes": func(v json.RawMessage) error {
		var m map[string]string
		if err := json.Unmarshal(v, &m); err != nil || len(m) > 100 {
			return fmt.Errorf("not a list of kinds")
		}
		for k, mode := range m {
			if len(k) > 40 || (mode != "bell" && mode != "screen" && mode != "off") {
				return fmt.Errorf("%q: not bell, screen or off", k)
			}
		}
		return nil
	},
	// Connection failures in a row that pause a site's downloads until it answers again (missing = 5; 0 = never).
	"pauseAfter":  numberIn(0, 100),
	"checksFirst": isBool, // subscription checks go ahead of downloads, a running one stepping aside (missing = on)
	// Sites (written by the downloader): switched off, and built-in ones removed.
	"sitesOff":     idList,
	"removedSites": idList,
	"loop":         isBool,
	"speed": func(v json.RawMessage) error {
		var f float64
		if err := json.Unmarshal(v, &f); err != nil {
			return err
		}
		if f < 0.25 || f > 4 {
			return fmt.Errorf("out of range")
		}
		return nil
	},
}

func oneOf(values ...string) func(json.RawMessage) error {
	return func(v json.RawMessage) error {
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			return err
		}
		for _, x := range values {
			if s == x {
				return nil
			}
		}
		return fmt.Errorf("not one of %v", values)
	}
}

func numberIn(lo, hi float64) func(json.RawMessage) error {
	return func(v json.RawMessage) error {
		var f float64
		if err := json.Unmarshal(v, &f); err != nil {
			return err
		}
		if f < lo || f > hi {
			return fmt.Errorf("out of range")
		}
		return nil
	}
}

// isLanguage is "auto" or a language there's a file for.
func isLanguage(v json.RawMessage) error {
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return err
	}
	if s != "auto" && !slices.Contains(i18n.Available(), s) {
		return fmt.Errorf("no such language")
	}
	return nil
}

// MaxCheckDepth is the deepest a check may go past the last check's newest post.
const MaxCheckDepth = 100_000

// siteNumbers is a map of plugin IDs to numbers in a range.
func siteNumbers(lo, hi float64) func(json.RawMessage) error {
	return func(v json.RawMessage) error {
		var m map[string]float64
		if err := json.Unmarshal(v, &m); err != nil {
			return err
		}
		if len(m) > 1000 {
			return fmt.Errorf("too many")
		}
		for _, n := range m {
			if n < lo || n > hi {
				return fmt.Errorf("out of range")
			}
		}
		return nil
	}
}

// CheckDepths are the check depths set for all and per site (nil = not set).
func (l *Library) CheckDepths(ctx context.Context) (all *int, sites map[string]int, err error) {
	set, err := l.Settings(ctx)
	if err != nil {
		return nil, nil, err
	}
	var n float64
	if json.Unmarshal(set["checkDepth"], &n) == nil {
		v := int(n)
		all = &v
	}
	var m map[string]float64
	json.Unmarshal(set["siteCheckDepths"], &m)
	sites = map[string]int{}
	for k, v := range m {
		sites[k] = int(v)
	}
	return all, sites, nil
}

// DownloadRules are the size limits for downloads (0 = none).
type DownloadRules struct {
	VideoBytes  int64 // skip videos larger than this
	ImagePixels int64 // skip images with more pixels than this
}

// DownloadRules reads the download rules from the settings.
func (l *Library) DownloadRules(ctx context.Context) (DownloadRules, error) {
	set, err := l.Settings(ctx)
	if err != nil {
		return DownloadRules{}, err
	}
	num := func(k string) float64 {
		var f float64
		json.Unmarshal(set[k], &f)
		return f
	}
	return DownloadRules{VideoBytes: int64(num("skipVideoMB") * 1024 * 1024), ImagePixels: int64(num("skipImageMP") * 1e6)}, nil
}

// idList is a list of plugin IDs.
func idList(v json.RawMessage) error {
	var ids []string
	if err := json.Unmarshal(v, &ids); err != nil {
		return err
	}
	if len(ids) > 1000 {
		return fmt.Errorf("too many")
	}
	return nil
}

func isBool(v json.RawMessage) error {
	var b bool
	return json.Unmarshal(v, &b)
}

// Settings returns the shared settings that have been set.
func (l *Library) Settings(ctx context.Context) (map[string]json.RawMessage, error) {
	rows, err := l.db.R.QueryContext(ctx, "SELECT key, value FROM settings")
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	defer rows.Close()
	out := map[string]json.RawMessage{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		if _, ok := settingCheck[k]; ok {
			out[k] = json.RawMessage(v)
		}
	}
	return out, rows.Err()
}

// SetSettings stores the given settings (all or none: one bad value rejects
// the lot).
func (l *Library) SetSettings(ctx context.Context, values map[string]json.RawMessage) error {
	for k, v := range values {
		check, ok := settingCheck[k]
		if !ok {
			return fmt.Errorf("library: %w: unknown setting %q", ErrInvalid, k)
		}
		if err := check(v); err != nil {
			return fmt.Errorf("library: %w: setting %q: %v", ErrInvalid, k, err)
		}
	}
	// (Media and Comics: one of them stays on - the app is useless with neither)
	if values["mediaOff"] != nil || values["comicsOff"] != nil {
		cur, err := l.Settings(ctx)
		if err != nil {
			return err
		}
		off := func(k string) bool {
			if v, ok := values[k]; ok {
				return string(v) == "true"
			}
			return string(cur[k]) == "true"
		}
		if off("mediaOff") && off("comicsOff") {
			return fmt.Errorf("library: %w: Media and Comics can't both be off", ErrInvalid)
		}
	}
	tx, err := l.db.W.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	defer tx.Rollback()
	now := l.now().UnixMilli()
	for k, v := range values {
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
			ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
			k, string(v), now); err != nil {
			return fmt.Errorf("library: %w", err)
		}
	}
	return tx.Commit()
}

// isOrder checks a result order ("imported", "favorite_asc").
func isOrder(v json.RawMessage) error {
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return err
	}
	if s == "" || len(s) > 40 || strings.ContainsAny(s, " :") {
		return fmt.Errorf("not an order")
	}
	return nil
}
