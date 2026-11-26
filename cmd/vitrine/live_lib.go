package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/usevitrine/vitrine/internal/config"
	"github.com/usevitrine/vitrine/internal/download"
)

// The library commands (search, tag, blacklist, setting, sites, credentials) through the running server: it sees
// the change at once (its search index, settings it holds, the open web app). Without a running server they work on
// the library's files themselves (cmd_search.go and the others) - fine while nothing else has them open.

// withLive runs fn on the running server; ran = false when there is none (the caller works offline).
func withLive(ctx context.Context, c config.Config, fn func(*live) error) (ran bool, err error) {
	l, err := dialLive(ctx, c)
	if errors.Is(err, errNotRunning) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	return true, fn(l)
}

func liveSearch(ctx context.Context, l *live, query string, n int, showTags bool, stdout io.Writer) error {
	var res struct {
		Total   int      `json:"total"`
		IDs     []uint32 `json:"ids"`
		Unknown []string `json:"unknown"`
		TookMS  float64  `json:"took_ms"`
	}
	if err := l.get(ctx, "/api/search?"+url.Values{"q": {query}, "limit": {strconv.Itoa(max(n, 1))}}.Encode(), &res); err != nil {
		return err
	}
	for _, u := range res.Unknown {
		fmt.Fprintf(stdout, "note: no tag %q in the library\n", u)
	}
	took := fmt.Sprintf("%.1f ms", res.TookMS)
	if res.TookMS < 1 {
		took = "under 1 ms"
	}
	fmt.Fprintf(stdout, "%d files (search took %s)\n", res.Total, took)
	shown := res.IDs[:min(n, len(res.IDs))]
	if len(shown) == 0 {
		return nil
	}
	parts := make([]string, len(shown))
	for i, id := range shown {
		parts[i] = strconv.FormatUint(uint64(id), 10)
	}
	var files struct {
		Files []struct {
			ID         uint32 `json:"id"`
			Type       string `json:"type"`
			Size       int64  `json:"size"`
			Width      int    `json:"width"`
			Height     int    `json:"height"`
			DurationMS int    `json:"duration_ms"`
			HasAudio   bool   `json:"has_audio"`
			Favorite   bool   `json:"favorite"`
		} `json:"files"`
	}
	if err := l.get(ctx, "/api/files?ids="+strings.Join(parts, ","), &files); err != nil {
		return err
	}
	for _, f := range files.Files {
		desc := fmt.Sprintf("%dx%d", f.Width, f.Height)
		if f.DurationMS > 0 {
			desc += fmt.Sprintf(" %.1fs", float64(f.DurationMS)/1000)
		}
		if f.HasAudio {
			desc += " audio"
		}
		fav := ""
		if f.Favorite {
			fav = "  ♥"
		}
		fmt.Fprintf(stdout, "#%-7d %-5s %-22s %8s%s\n", f.ID, f.Type, desc, size(f.Size), fav)
		if showTags {
			var ft struct {
				Tags []struct {
					Tag string `json:"tag"`
				} `json:"tags"`
			}
			if err := l.get(ctx, fmt.Sprintf("/api/files/%d/tags", f.ID), &ft); err != nil {
				return err
			}
			names := make([]string, len(ft.Tags))
			for i, t := range ft.Tags {
				names[i] = t.Tag
			}
			fmt.Fprintf(stdout, "         tags: %s\n", strings.Join(names, " "))
		}
	}
	return nil
}

func liveTag(ctx context.Context, l *live, ids []uint32, add, remove []string, stdout io.Writer) error {
	var res struct {
		Updated int `json:"updated"`
	}
	if err := l.post(ctx, "/api/tags", map[string]any{"ids": ids, "add": orNone(add), "remove": orNone(remove)}, &res); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "updated %d files\n", len(ids))
	return nil
}

func orNone(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func liveBlacklist(ctx context.Context, l *live, action string, tags []string, stdout io.Writer) error {
	if action == "matches" {
		var m struct {
			Total int   `json:"total"`
			Size  int64 `json:"size"`
			Tags  []struct {
				Tag   string `json:"tag"`
				Files int    `json:"files"`
			} `json:"tags"`
		}
		if err := l.get(ctx, "/api/blacklist/matches", &m); err != nil {
			return err
		}
		for _, t := range m.Tags {
			if t.Files > 0 {
				fmt.Fprintf(stdout, "%-30s %d\n", t.Tag, t.Files)
			}
		}
		fmt.Fprintf(stdout, "%d files with blacklisted tags (%.1f MB)\n", m.Total, float64(m.Size)/1e6)
		return nil
	}
	var cur struct {
		Tags []string `json:"tags"`
	}
	if err := l.get(ctx, "/api/blacklist", &cur); err != nil {
		return err
	}
	if action != "list" {
		next := slices.Clone(cur.Tags)
		if action == "add" {
			next = append(next, tags...)
		} else {
			next = slices.DeleteFunc(next, func(p string) bool { return slices.Contains(tags, p) })
		}
		if err := l.post(ctx, "/api/blacklist", map[string]any{"tags": orNone(next)}, &cur); err != nil {
			return err
		}
	}
	for _, p := range cur.Tags {
		fmt.Fprintln(stdout, p)
	}
	fmt.Fprintf(stdout, "%d blacklisted tags\n", len(cur.Tags))
	return nil
}

func liveSetting(ctx context.Context, l *live, rest []string, stdout io.Writer) error {
	var set map[string]json.RawMessage
	if len(rest) == 2 {
		v := json.RawMessage(rest[1])
		if !json.Valid(v) {
			v, _ = json.Marshal(rest[1]) // (a bare word: a string)
		}
		if err := l.post(ctx, "/api/settings", map[string]json.RawMessage{rest[0]: v}, &set); err != nil {
			return err
		}
	} else if err := l.get(ctx, "/api/settings", &set); err != nil {
		return err
	}
	if len(rest) > 0 {
		v, ok := set[rest[0]]
		if !ok {
			return fmt.Errorf("%q isn't set", rest[0])
		}
		fmt.Fprintln(stdout, string(v))
		return nil
	}
	if len(set) == 0 {
		fmt.Fprintln(stdout, "none set (the defaults apply)")
	}
	for _, k := range slices.Sorted(func(yield func(string) bool) {
		for k := range set {
			if !yield(k) {
				return
			}
		}
	}) {
		fmt.Fprintf(stdout, "%s = %s\n", k, set[k])
	}
	return nil
}

// liveSites lists the sites as the running server has them (switched off, paused, credentials), or acts on one:
// on, off (switch it), try (a paused one: try it now).
func liveSites(ctx context.Context, l *live, rest []string, asJSON bool, stdout io.Writer) error {
	var res struct {
		Sites []download.SiteInfo `json:"sites"`
	}
	if len(rest) > 0 {
		if len(rest) != 2 {
			return fmt.Errorf("vitrine sites on|off|try <site>")
		}
		switch rest[0] {
		case "on", "off":
			if err := l.post(ctx, "/api/sites/"+esc(rest[1])+"/off", map[string]bool{"off": rest[0] == "off"}, &res); err != nil {
				return err
			}
		case "try":
			if err := l.post(ctx, "/api/sites/"+esc(rest[1])+"/try", nil, &res); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown action %q (on, off, try)", rest[0])
		}
		fmt.Fprintf(stdout, "%s: %s\n", rest[1], map[string]string{"on": "switched on", "off": "switched off", "try": "trying it now"}[rest[0]])
		return nil
	}
	if err := l.get(ctx, "/api/sites", &res); err != nil {
		return err
	}
	if asJSON {
		return printJSON(stdout, res.Sites)
	}
	for _, s := range res.Sites {
		var notes []string
		switch {
		case s.Off:
			notes = append(notes, "switched off")
		case s.Paused != nil:
			notes = append(notes, fmt.Sprintf("paused (%s; next try %s)", s.Paused.Reason, ago(s.Paused.Until.UnixMilli())))
		}
		if len(s.Fields) > 0 {
			switch {
			case s.SignedIn:
				notes = append(notes, "credentials set")
			case s.Required:
				notes = append(notes, "credentials missing (vitrine credentials "+s.ID+")")
			default:
				notes = append(notes, "no account set (optional)")
			}
		}
		if s.Subscriptions+s.Downloads > 0 {
			notes = append(notes, fmt.Sprintf("%d subscriptions, %d downloads", s.Subscriptions, s.Downloads))
		}
		line := fmt.Sprintf("%-16s %-22s %s %s", s.ID, s.Name, s.Engine, s.Version)
		if len(notes) > 0 {
			line += " - " + strings.Join(notes, ", ")
		}
		fmt.Fprintln(stdout, line)
	}
	return nil
}
