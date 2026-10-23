package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"maps"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/usevitrine/vitrine/internal/download"
	"github.com/usevitrine/vitrine/internal/library"
	"github.com/usevitrine/vitrine/internal/plugin"
)

// Commands that work with the running server (live.go): what it's doing, its queue and subscriptions, failed posts,
// notices and logs, and any API call. Each prints plain text; -json prints the server's answer as it is.

// downloadsInfo is GET /api/downloads.
type downloadsInfo struct {
	Sites         []string                `json:"sites"`
	SitesOff      []string                `json:"sites_off"`
	SitesPaused   map[string]plugin.Pause `json:"sites_paused"`
	Searches      []download.Search       `json:"searches"`
	Subscriptions []download.Subscription `json:"subscriptions"`
	Groups        []download.Group        `json:"groups"`
}

// newLiveFlags: a command's flags (the shared ones, plus -json) and how it reaches the running server.
func newLiveFlags(name string, stderr io.Writer, usage string) (*flag.FlagSet, *bool, func(context.Context) (*live, error)) {
	fset := flag.NewFlagSet(name, flag.ContinueOnError)
	fset.SetOutput(stderr)
	fset.Usage = func() {
		fmt.Fprintln(stderr, strings.TrimSpace(usage))
		fmt.Fprintln(stderr, "\nFlags:")
		fset.PrintDefaults()
	}
	load := configFlags(fset)
	asJSON := fset.Bool("json", false, "print the server's answer as JSON")
	connect := func(ctx context.Context) (*live, error) {
		c, err := load()
		if err != nil {
			return nil, err
		}
		return needLive(ctx, c)
	}
	return fset, asJSON, connect
}

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// ids reads search, subscription or group IDs ("12", "#12").
func ids(args []string) ([]int64, error) {
	var out []int64
	for _, a := range args {
		id, err := strconv.ParseInt(strings.TrimPrefix(a, "#"), 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("bad ID %q", a)
		}
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("which one? give its ID (see the list)")
	}
	return out, nil
}

// ---- status

func cmdStatus(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fset, asJSON, connect := newLiveFlags("status", stderr, `
Usage: vitrine status [flags]

What the running Vitrine is doing: version and uptime, the library, the download
queue, subscriptions, sites that are paused or off, unread notices.`)
	if _, err := parseMixed(fset, args); err != nil {
		return err
	}
	l, err := connect(ctx)
	if err != nil {
		return err
	}
	var st struct {
		Files         int   `json:"files"`
		Bytes         int64 `json:"bytes"`
		Tags          int   `json:"tags"`
		Favorites     int   `json:"favorites"`
		Subscriptions struct {
			Total   int `json:"total"`
			Paused  int `json:"paused"`
			Failing int `json:"failing"`
		} `json:"subscriptions"`
		Server struct {
			Version string `json:"version"`
			Uptime  int64  `json:"uptime_s"`
			Docker  bool   `json:"in_docker"`
		} `json:"server"`
		Storage struct {
			Disks []struct {
				For   string `json:"for"`
				Total int64  `json:"total"`
				Free  int64  `json:"free"`
			} `json:"disks"`
		} `json:"storage"`
	}
	if err := l.get(ctx, "/api/stats", &st); err != nil {
		return err
	}
	var dl downloadsInfo
	dlErr := l.get(ctx, "/api/downloads", &dl)
	var nt struct {
		Notices []library.Notice `json:"notices"`
		Read    int64            `json:"read"`
	}
	l.get(ctx, "/api/notices", &nt)
	if *asJSON {
		return printJSON(stdout, map[string]any{"stats": st, "downloads": dl, "notices": nt})
	}
	fmt.Fprintf(stdout, "Vitrine %s, up %s%s\n", st.Server.Version, short(time.Duration(st.Server.Uptime)*time.Second), map[bool]string{true: " (Docker)"}[st.Server.Docker])
	fmt.Fprintf(stdout, "Library    %d files (%s), %d tags, %d favorites\n", st.Files, size(st.Bytes), st.Tags, st.Favorites)
	for _, d := range st.Storage.Disks {
		fmt.Fprintf(stdout, "Disk       %s: %s free of %s\n", d.For, size(d.Free), size(d.Total))
	}
	if dlErr != nil {
		fmt.Fprintln(stdout, "Downloads  off:", dlErr)
	} else {
		var running, waiting, paused, failed int
		for _, s := range dl.Searches {
			switch s.State {
			case download.StateRunning:
				running++
			case download.StateQueued:
				waiting++
			case download.StatePaused:
				paused++
			case download.StateFailed:
				failed++
			}
		}
		fmt.Fprintf(stdout, "Queue      %d running, %d waiting, %d paused, %d failed\n", running, waiting, paused, failed)
		fmt.Fprintf(stdout, "Subs       %d (%d paused, %d failing)\n", st.Subscriptions.Total, st.Subscriptions.Paused, st.Subscriptions.Failing)
		var sites []string
		for _, id := range dl.SitesOff {
			sites = append(sites, id+" (off)")
		}
		for id, p := range dl.SitesPaused {
			sites = append(sites, fmt.Sprintf("%s (paused: %s; next try %s)", id, p.Reason, ago(p.Until.UnixMilli())))
		}
		slices.Sort(sites)
		if len(sites) > 0 {
			fmt.Fprintf(stdout, "Sites      %s\n", strings.Join(sites, ", "))
		}
	}
	unread := 0
	for _, n := range nt.Notices {
		if n.ID > nt.Read {
			unread++
		}
	}
	fmt.Fprintf(stdout, "Notices    %d unread (vitrine notices)\n", unread)
	return nil
}

// ---- queue

func cmdQueue(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fset, asJSON, connect := newLiveFlags("queue", stderr, `
Usage: vitrine queue [flags] [list | add <site> <search> | pause <id>... | resume <id>... | remove <id>... | clear]

The download queue of the running Vitrine (Downloads > Queue and History).
  list      what's downloading and waiting (with -a: finished ones too); -w follows along
  add       download a search once, e.g. vitrine queue add safebooru "blue_sky -cloud" (-max: at most N files)
  pause, resume, remove   one or more downloads by ID (the first column)
  clear     remove the finished ones

Examples:
  vitrine queue -w
  vitrine queue add safebooru blue_sky -max 50
  vitrine queue pause 12`)
	all := fset.Bool("a", false, "list: finished downloads too")
	watch := fset.Bool("w", false, "list: keep the list up to date (Ctrl+C stops)")
	maxPosts := fset.Int("max", 0, "add: at most this many files (0: all)")
	label := fset.String("label", "", "add: a name for it in the lists")
	words, err := parseMixed(fset, args)
	if err != nil {
		return err
	}
	action, rest := "list", words
	if len(rest) > 0 {
		action, rest = rest[0], rest[1:]
	}
	l, err := connect(ctx)
	if err != nil {
		return err
	}
	switch action {
	case "list", "ls":
		for {
			var dl downloadsInfo
			if err := l.get(ctx, "/api/downloads", &dl); err != nil {
				return err
			}
			if *asJSON {
				return printJSON(stdout, dl.Searches)
			}
			if *watch && isTerminal(stdout) {
				fmt.Fprint(stdout, "\033[H\033[2J") // (the screen cleared: the list in place)
			}
			printQueue(stdout, dl, *all)
			if !*watch {
				return nil
			}
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(2 * time.Second):
			}
		}
	case "add":
		if len(rest) < 2 {
			fset.Usage()
			return flag.ErrHelp
		}
		var s download.Search
		if err := l.post(ctx, "/api/downloads", map[string]any{"site": rest[0], "query": strings.Join(rest[1:], " "), "max_posts": *maxPosts, "label": *label}, &s); err != nil {
			return err
		}
		if *asJSON {
			return printJSON(stdout, s)
		}
		fmt.Fprintf(stdout, "queued #%d on %s: %s\n", s.ID, s.Site, s.Query)
		return nil
	case "pause", "resume", "remove":
		list, err := ids(rest)
		if err != nil {
			return err
		}
		for _, id := range list {
			if err := l.post(ctx, fmt.Sprintf("/api/downloads/%d/%s", id, action), nil, nil); err != nil {
				return fmt.Errorf("#%d: %w", id, err)
			}
			fmt.Fprintf(stdout, "#%d: %s\n", id, map[string]string{"pause": "paused", "resume": "resumed", "remove": "removed"}[action])
		}
		return nil
	case "clear":
		var res struct {
			Removed int `json:"removed"`
		}
		if err := l.post(ctx, "/api/downloads/clear", nil, &res); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "removed %d finished downloads\n", res.Removed)
		return nil
	}
	fset.Usage()
	return fmt.Errorf("unknown action %q", action)
}

func printQueue(w io.Writer, dl downloadsInfo, all bool) {
	shown := 0
	for _, s := range dl.Searches {
		if !all && s.State == download.StateDone {
			continue
		}
		shown++
		what := strconv.Quote(s.Query)
		if s.Label != "" {
			what = s.Label
		}
		if s.SubscriptionID != 0 {
			kind := "check"
			if s.FirstCheck {
				kind = "first check"
			}
			what = fmt.Sprintf("%s of subscription #%d: %s", kind, s.SubscriptionID, what)
		}
		progress := fmt.Sprintf("%d posts", s.Handled)
		if s.SiteTotal > 0 {
			progress = fmt.Sprintf("%d/%d posts", s.Handled, s.SiteTotal)
		}
		facts := []string{progress}
		for _, f := range []struct {
			n    int
			what string
		}{{s.Downloaded, "new"}, {s.Already, "had"}, {s.Deleted, "deleted before"}, {s.Blacklisted, "blacklisted"}, {s.Skipped, "skipped"}, {s.Failed, "failed"}} {
			if f.n > 0 {
				facts = append(facts, fmt.Sprintf("%d %s", f.n, f.what))
			}
		}
		state := s.State
		if s.State == download.StateFailed && s.RetryAt > 0 {
			state = "failed, retry " + ago(s.RetryAt)
		}
		fmt.Fprintf(w, "#%-6d %-12s %-8s %s\n         %s\n", s.ID, s.Site, state, what, strings.Join(facts, " · "))
		if s.Error != "" {
			fmt.Fprintf(w, "         error: %s\n", s.Error)
		}
	}
	if shown == 0 {
		fmt.Fprintln(w, "nothing in the queue")
	}
	for id, p := range dl.SitesPaused {
		fmt.Fprintf(w, "(%s is paused: %s; next try %s)\n", id, p.Reason, ago(p.Until.UnixMilli()))
	}
	for _, id := range dl.SitesOff {
		fmt.Fprintf(w, "(%s is switched off: its downloads wait)\n", id)
	}
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// ---- subscriptions

func cmdSubs(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fset, asJSON, connect := newLiveFlags("subs", stderr, `
Usage: vitrine subs [flags] [list | add <site> <search> | check <id>... | pause <id>... | resume <id>... | remove <id>... | every <id> <schedule>]

The subscriptions of the running Vitrine (Downloads > Subscriptions): searches
checked again on a schedule for new posts.
  list      every subscription with its group, schedule, last and next check (-site: one site's)
  add       subscribe, e.g. vitrine subs add safebooru blue_sky -every 1d -group Landscapes
  check     check now; pause, resume, remove: by ID (the first column)
  every     a new schedule, e.g. vitrine subs every 7 12h (6h, 1d, 3d, 1w, ...)

Examples:
  vitrine subs -site safebooru
  vitrine subs add safebooru "blue_sky -cloud" -every 3d -first 100
  vitrine subs check 7 8`)
	site := fset.String("site", "", "list: only this site's")
	every := fset.String("every", "1d", "add: how often to check (6h, 1d, 3d, 1w, ...)")
	first := fset.Int("first", 0, "add: the first check gets at most this many files (0: all)")
	group := fset.String("group", "", "add: into this group (made if it doesn't exist)")
	words, err := parseMixed(fset, args)
	if err != nil {
		return err
	}
	action, rest := "list", words
	if len(rest) > 0 {
		action, rest = rest[0], rest[1:]
	}
	l, err := connect(ctx)
	if err != nil {
		return err
	}
	switch action {
	case "list", "ls":
		var dl downloadsInfo
		if err := l.get(ctx, "/api/downloads", &dl); err != nil {
			return err
		}
		subs := slices.DeleteFunc(dl.Subscriptions, func(s download.Subscription) bool { return *site != "" && s.Site != *site })
		if *asJSON {
			return printJSON(stdout, subs)
		}
		groups := map[int64]string{}
		for _, g := range dl.Groups {
			groups[g.ID] = g.Name
		}
		for _, s := range subs {
			what := strconv.Quote(s.Query)
			if s.Label != "" {
				what = s.Label
			}
			state := "every " + short(time.Duration(s.Interval)*time.Second)
			switch {
			case s.Check != nil && s.Check.State == download.StateRunning:
				state = "checking now"
			case s.Paused:
				state = "paused"
			case s.Error != "":
				state = "failing"
			}
			in := ""
			if g := groups[s.GroupID]; g != "" {
				in = " [" + g + "]"
			}
			fmt.Fprintf(stdout, "#%-5d %-12s %-14s %s%s\n", s.ID, s.Site, state, what, in)
			fmt.Fprintf(stdout, "        %d files · checked %s · next %s\n", s.Downloaded, ago(s.LastCheckAt), ago(s.NextCheckAt))
			if s.Error != "" {
				fmt.Fprintf(stdout, "        error: %s\n", s.Error)
			}
		}
		if len(subs) == 0 {
			fmt.Fprintln(stdout, "no subscriptions")
		}
		return nil
	case "add":
		if len(rest) < 2 {
			fset.Usage()
			return flag.ErrHelp
		}
		d, err := parseEvery(*every)
		if err != nil {
			return err
		}
		req := map[string]any{"site": rest[0], "query": strings.Join(rest[1:], " "), "interval": int64(d.Seconds()), "initial_max": *first}
		if *group != "" {
			req["group_name"] = *group
		}
		var s download.Subscription
		if err := l.post(ctx, "/api/subscriptions", req, &s); err != nil {
			return err
		}
		if *asJSON {
			return printJSON(stdout, s)
		}
		fmt.Fprintf(stdout, "subscribed #%d on %s: %s (every %s; the first check starts now)\n", s.ID, s.Site, s.Query, short(d))
		return nil
	case "check", "remove":
		list, err := ids(rest)
		if err != nil {
			return err
		}
		for _, id := range list {
			if err := l.post(ctx, fmt.Sprintf("/api/subscriptions/%d/%s", id, action), nil, nil); err != nil {
				return fmt.Errorf("#%d: %w", id, err)
			}
			fmt.Fprintf(stdout, "#%d: %s\n", id, map[string]string{"check": "checking", "remove": "removed"}[action])
		}
		return nil
	case "pause", "resume":
		list, err := ids(rest)
		if err != nil {
			return err
		}
		for _, id := range list {
			if err := l.post(ctx, fmt.Sprintf("/api/subscriptions/%d", id), map[string]any{"paused": action == "pause"}, nil); err != nil {
				return fmt.Errorf("#%d: %w", id, err)
			}
			fmt.Fprintf(stdout, "#%d: %sd\n", id, action)
		}
		return nil
	case "every":
		if len(rest) != 2 {
			fset.Usage()
			return flag.ErrHelp
		}
		list, err := ids(rest[:1])
		if err != nil {
			return err
		}
		d, err := parseEvery(rest[1])
		if err != nil {
			return err
		}
		if err := l.post(ctx, fmt.Sprintf("/api/subscriptions/%d", list[0]), map[string]any{"interval": int64(d.Seconds())}, nil); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "#%d: every %s\n", list[0], short(d))
		return nil
	}
	fset.Usage()
	return fmt.Errorf("unknown action %q", action)
}

// ---- failed posts

func cmdFailed(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fset, asJSON, connect := newLiveFlags("failed", stderr, `
Usage: vitrine failed [flags] [list | retry <site> <post>... | retry all | dismiss <site> <post>... | dismiss all]

Posts whose file couldn't be downloaded (Downloads > History > Failed files).
  list      the failed posts, newest first: site, post, why
  retry     try them again (in their site's queue); all: every failed post
  dismiss   forget them (they aren't tried again unless a search meets them)

Examples:
  vitrine failed
  vitrine failed retry safebooru 1234567
  vitrine failed retry all`)
	words, err := parseMixed(fset, args)
	if err != nil {
		return err
	}
	action, rest := "list", words
	if len(rest) > 0 {
		action, rest = rest[0], rest[1:]
	}
	l, err := connect(ctx)
	if err != nil {
		return err
	}
	var res struct {
		Posts []download.FailedPost `json:"posts"`
	}
	if err := l.get(ctx, "/api/failed-posts", &res); err != nil {
		return err
	}
	// The posts an action names: "all", or a site and its posts.
	pick := func() ([]download.FailedPost, error) {
		if len(rest) == 1 && rest[0] == "all" {
			return res.Posts, nil
		}
		if len(rest) < 2 {
			return nil, fmt.Errorf("give a site and its post IDs, or all")
		}
		var out []download.FailedPost
		for _, p := range rest[1:] {
			id, err := strconv.ParseInt(p, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("bad post ID %q", p)
			}
			out = append(out, download.FailedPost{Site: rest[0], Post: id})
		}
		return out, nil
	}
	switch action {
	case "list", "ls":
		if *asJSON {
			return printJSON(stdout, res.Posts)
		}
		for _, p := range res.Posts {
			fmt.Fprintf(stdout, "%-12s %-12d %-9s %s\n", p.Site, p.Post, ago(p.At), p.Error)
			if p.URL != "" {
				fmt.Fprintf(stdout, "%26s%s\n", "", p.URL)
			}
		}
		fmt.Fprintf(stdout, "%d failed posts\n", len(res.Posts))
		return nil
	case "retry":
		list, err := pick()
		if err != nil {
			return err
		}
		started := 0
		for _, p := range list {
			if err := l.post(ctx, "/api/failed-posts/retry", map[string]any{"site": p.Site, "post": strconv.FormatInt(p.Post, 10)}, nil); err != nil {
				fmt.Fprintf(stderr, "%s %d: %v\n", p.Site, p.Post, err)
				continue
			}
			started++
		}
		fmt.Fprintf(stdout, "retrying %d posts (vitrine queue shows them)\n", started)
		return nil
	case "dismiss":
		list, err := pick()
		if err != nil {
			return err
		}
		posts := make([]map[string]string, len(list))
		for i, p := range list {
			posts[i] = map[string]string{"site": p.Site, "post": strconv.FormatInt(p.Post, 10)}
		}
		var out struct {
			Dismissed int `json:"dismissed"`
		}
		if err := l.post(ctx, "/api/failed-posts/dismiss", map[string]any{"posts": posts}, &out); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "dismissed %d posts\n", out.Dismissed)
		return nil
	}
	fset.Usage()
	return fmt.Errorf("unknown action %q", action)
}

// ---- notices

func cmdNotices(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fset, asJSON, connect := newLiveFlags("notices", stderr, `
Usage: vitrine notices [flags] [list | read | clear]

The notices of the running Vitrine (the bell in the web app): downloads that
finished or failed, new files from subscriptions, and so on.
  list      newest first (unread ones marked *)
  read      mark them all read (on every device)
  clear     clear them all`)
	n := fset.Int("n", 30, "list: how many")
	words, err := parseMixed(fset, args)
	if err != nil {
		return err
	}
	action := "list"
	if len(words) > 0 {
		action = words[0]
	}
	l, err := connect(ctx)
	if err != nil {
		return err
	}
	var res struct {
		Notices []library.Notice `json:"notices"`
		Read    int64            `json:"read"`
	}
	if err := l.get(ctx, "/api/notices", &res); err != nil {
		return err
	}
	last := int64(0)
	for _, x := range res.Notices {
		last = max(last, x.ID)
	}
	switch action {
	case "list", "ls":
		list := slices.Clone(res.Notices)
		slices.Reverse(list) // (newest first)
		list = list[:min(*n, len(list))]
		if *asJSON {
			return printJSON(stdout, list)
		}
		for _, x := range list {
			mark := " "
			if x.ID > res.Read {
				mark = "*"
			}
			fmt.Fprintf(stdout, "%s %s  %s\n", mark, time.UnixMilli(x.At).Format("Jan 2 15:04"), noticeText(x))
		}
		if len(list) == 0 {
			fmt.Fprintln(stdout, "no notices")
		}
		return nil
	case "read", "clear":
		if err := l.post(ctx, "/api/notices/"+action, map[string]int64{"up_to": last}, nil); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s: all notices\n", map[string]string{"read": "marked read", "clear": "cleared"}[action])
		return nil
	}
	fset.Usage()
	return fmt.Errorf("unknown action %q", action)
}

// noticeText is a notice in a line of words.
func noticeText(x library.Notice) string {
	what := x.Query
	if x.Subscription {
		what = "subscription " + strconv.Quote(x.Query)
	} else if what != "" {
		what = strconv.Quote(what)
	}
	parts := []string{x.Kind}
	if x.Site != "" {
		parts = append(parts, x.Site)
	}
	if what != "" {
		parts = append(parts, what)
	}
	if x.New > 0 {
		parts = append(parts, fmt.Sprintf("%d new", x.New))
	}
	if x.Tag != "" {
		parts = append(parts, "tag "+x.Tag)
	}
	if x.Text != "" {
		parts = append(parts, x.Text)
	}
	if x.Error != "" {
		parts = append(parts, "- "+x.Error)
	}
	return strings.Join(parts, " ")
}

// ---- logs

func cmdLogs(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fset, asJSON, connect := newLiveFlags("logs", stderr, `
Usage: vitrine logs [flags]

The running Vitrine's recent log (kept across restarts), oldest first.

Examples:
  vitrine logs -n 50
  vitrine logs -level warn
  vitrine logs -f`)
	n := fset.Int("n", 100, "how many records")
	level := fset.String("level", "", "only this level and worse: debug, info, warn, error")
	follow := fset.Bool("f", false, "keep printing new records (Ctrl+C stops)")
	if _, err := parseMixed(fset, args); err != nil {
		return err
	}
	l, err := connect(ctx)
	if err != nil {
		return err
	}
	type record struct {
		T     int64             `json:"t"`
		Level string            `json:"level"`
		Msg   string            `json:"msg"`
		Attrs map[string]string `json:"attrs"`
	}
	after := int64(0)
	for {
		q := url.Values{"n": {strconv.Itoa(*n)}, "after": {strconv.FormatInt(after, 10)}}
		if *level != "" {
			q.Set("level", *level)
		}
		var res struct {
			Records []record `json:"records"`
		}
		if err := l.get(ctx, "/api/logs?"+q.Encode(), &res); err != nil {
			return err
		}
		for _, r := range res.Records {
			after = max(after, r.T)
			if *asJSON {
				b, _ := json.Marshal(r)
				fmt.Fprintln(stdout, string(b))
				continue
			}
			line := time.UnixMilli(r.T).Format("2006-01-02 15:04:05") + " " + fmt.Sprintf("%-5s", r.Level) + " " + r.Msg
			for _, k := range slices.Sorted(maps.Keys(r.Attrs)) {
				v := r.Attrs[k]
				if strings.ContainsAny(v, " \"") {
					v = strconv.Quote(v)
				}
				line += " " + k + "=" + v
			}
			fmt.Fprintln(stdout, line)
		}
		if !*follow {
			return nil
		}
		*n = 5000 // (following: everything new)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(2 * time.Second):
		}
	}
}

// ---- any API call

func cmdAPI(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fset, _, connect := newLiveFlags("api", stderr, `
Usage: vitrine api [flags] [GET|POST] <path> [<JSON body>]

Calls the running Vitrine's API (the one the web app uses) and prints the answer:
for anything the other commands don't cover, and for scripts and debugging.
The API is described in docs/architecture/server.md.

Examples:
  vitrine api /api/stats
  vitrine api "/api/search?q=blue_sky&limit=5"
  vitrine api POST /api/favorite '{"ids": [12], "on": true}'`)
	rest, err := parseMixed(fset, args)
	if err != nil {
		return err
	}
	method := "GET"
	if len(rest) > 0 && (strings.EqualFold(rest[0], "GET") || strings.EqualFold(rest[0], "POST")) {
		method, rest = strings.ToUpper(rest[0]), rest[1:]
	}
	if len(rest) == 0 || len(rest) > 2 || !strings.HasPrefix(rest[0], "/") {
		fset.Usage()
		return flag.ErrHelp
	}
	var body any
	if len(rest) == 2 {
		if !json.Valid([]byte(rest[1])) {
			return fmt.Errorf("the body isn't JSON: %s", rest[1])
		}
		body = json.RawMessage(rest[1])
	} else if method == "POST" {
		body = struct{}{}
	}
	l, err := connect(ctx)
	if err != nil {
		return err
	}
	var raw []byte
	if err := l.do(ctx, method, rest[0], body, &raw); err != nil {
		return err
	}
	var v any
	if json.Unmarshal(raw, &v) == nil {
		return printJSON(stdout, v)
	}
	_, err = stdout.Write(raw)
	return err
}
