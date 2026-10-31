package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/usevitrine/vitrine/internal/download"
	"github.com/usevitrine/vitrine/internal/logring"
)

// listLogs: GET /api/logs?n=&after=&level= -> {records: [{t, level, msg, attrs}]}: the newest n log records (200 by
// default, at most 5000), oldest first; after (Unix ms): only later ones (to follow along); level: that and worse
// (debug, info, warn, error). For the vitrine logs command - unlike the problem report, not made anonymous.
func (s *Server) listLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	n, _ := strconv.Atoi(q.Get("n"))
	if n <= 0 {
		n = 200
	}
	n = min(n, 5000)
	after, _ := strconv.ParseInt(q.Get("after"), 10, 64)
	var floor slog.Level
	if v := q.Get("level"); v != "" {
		if err := floor.UnmarshalText([]byte(v)); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("level %q: debug, info, warn or error", v))
			return
		}
	} else {
		floor = slog.LevelDebug
	}
	type record struct {
		T     int64             `json:"t"`
		Level string            `json:"level"`
		Msg   string            `json:"msg"`
		Attrs map[string]string `json:"attrs,omitempty"`
	}
	out := []record{}
	if s.logs != nil {
		for _, rec := range s.logs.RecordsAt(5000, floor) { // (the newest 5000 at that level, out of the whole log)
			if rec.Time.UnixMilli() <= after {
				continue
			}
			x := record{T: rec.Time.UnixMilli(), Level: rec.Level.String(), Msg: rec.Msg}
			for _, a := range rec.Attrs {
				if x.Attrs == nil {
					x.Attrs = map[string]string{}
				}
				x.Attrs[a.Key] = a.Value.String()
			}
			out = append(out, x)
		}
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	writeJSON(w, http.StatusOK, map[string]any{"records": out})
}

// SetLogs gives the server the recent log records (for the problem report).
func (s *Server) SetLogs(r *logring.Ring) { s.logs = r }

// ---- The problem report (Settings > Stats > Problem report): a text file to
// attach to a bug report - the setup, the counts and the recent log, with
// nothing private in it. Tags, searches, links, names and paths are replaced
// by placeholders (the same value always gets the same one, so the log still
// reads); site names become site-1, site-2...; free text (errors) loses links,
// quoted text, e-mail addresses and anything that looks like a key.

// Attribute keys whose values are always private (their kind names the placeholder).
var privateKeys = map[string]string{
	"query": "search", "q": "search", "search": "search",
	"tag": "tag", "tags": "tag", "parent": "tag", "child": "tag", "from": "tag", "to": "tag",
	"ns": "namespace", "namespace": "namespace",
	"name": "name", "user": "name", "title": "name",
	"url": "url", "host": "host", "path": "path", "dir": "path", "config": "path",
	"site": "site", "plugin": "site", "sites": "site",
}

var (
	reURL    = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^\s"'<>]+`)
	reQuoted = regexp.MustCompile(`"[^"]*"|“[^”]*”`)
	reEmail  = regexp.MustCompile(`[^\s@"'<>]+@[^\s@"'<>]+\.[a-zA-Z]{2,}`)
	reSecret = regexp.MustCompile(`(?i)\b(api_?key|key|token|pass(word)?|secret|user_id|auth)=[^\s&"']+`)
	reLong   = regexp.MustCompile(`\b[A-Za-z0-9_\-]{28,}\b`)
)

type redactor struct {
	aliases map[string]map[string]string // kind -> value -> placeholder
	sites   []string                     // known site IDs and addresses, longest first (hidden inside text)
}

func newRedactor(siteNames []string) *redactor {
	r := &redactor{aliases: map[string]map[string]string{}}
	for _, n := range siteNames {
		if n != "" {
			r.sites = append(r.sites, n)
		}
	}
	sort.Slice(r.sites, func(i, j int) bool { return len(r.sites[i]) > len(r.sites[j]) })
	return r
}

// alias is the placeholder for a private value ("tag-3"); site addresses
// share their site's placeholder (see siteAlias).
func (r *redactor) alias(kind, v string) string {
	if v == "" {
		return ""
	}
	m := r.aliases[kind]
	if m == nil {
		m = map[string]string{}
		r.aliases[kind] = m
	}
	if a, ok := m[v]; ok {
		return a
	}
	a := fmt.Sprintf("%s-%d", kind, len(m)+1)
	m[v] = a
	return a
}

// text cleans free text: links, quoted text, addresses, keys and known site names go.
func (r *redactor) text(s string) string {
	s = reURL.ReplaceAllString(s, "<link>")
	s = reEmail.ReplaceAllString(s, "<email>")
	s = reSecret.ReplaceAllString(s, "$1=<hidden>")
	s = reQuoted.ReplaceAllString(s, `"…"`)
	s = reLong.ReplaceAllString(s, "<hidden>")
	for _, n := range r.sites {
		if strings.Contains(s, n) {
			s = strings.ReplaceAll(s, n, r.alias("site", n))
		}
	}
	return s
}

func (r *redactor) attr(a slog.Attr) string {
	key := a.Key
	if i := strings.LastIndex(key, "."); i >= 0 {
		key = key[i+1:]
	}
	if a.Value.Kind() != slog.KindString {
		return a.Key + "=" + a.Value.String() // numbers, times, durations, booleans
	}
	v := a.Value.String()
	// A request's path (never its query): readable - file names are hashes and
	// site IDs, which text() hides.
	if key == "version" || key == "address" {
		return a.Key + "=" + fmt.Sprintf("%q", v) // never private (and a version looks like a key)
	}
	if key == "path" && strings.HasPrefix(v, "/") {
		return a.Key + "=" + r.text(v)
	}
	if kind, ok := privateKeys[key]; ok {
		parts := strings.Split(v, ", ")
		for i, p := range parts {
			parts[i] = r.alias(kind, p)
		}
		return a.Key + "=" + strings.Join(parts, ",")
	}
	v = r.text(v)
	if strings.ContainsAny(v, " =") {
		v = fmt.Sprintf("%q", v)
	}
	return a.Key + "=" + v
}

// report: GET /api/report (?download=1: as a file).
func (s *Server) report(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var names []string
	engines := map[string]string{}
	var infos []download.SiteInfo
	if s.dl != nil {
		infos = s.dl.SiteInfos()
		for _, si := range infos {
			names = append(names, si.ID, si.Site, si.Host, si.Name)
			engines[si.ID] = si.Engine
		}
	}
	if m, err := s.lib.SiteNames(ctx); err == nil {
		for _, n := range m {
			if n != "user" {
				names = append(names, n)
			}
		}
	}
	red := newRedactor(names)
	st, err := s.lib.Stats(ctx, 0, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	sv := s.serverInfo(ctx)
	storage := s.storage(st)

	var b strings.Builder
	p := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	size := func(n int64) string { return humanSize(n) }
	p("Vitrine problem report")
	p("Made %s. Tags, searches, links, names and site names are replaced by placeholders.", time.Now().Format("2006-01-02 15:04 -07:00"))
	p("")
	p("Server")
	p("  version %s, %s, Go %s, %d CPUs, docker=%v", sv.Version, sv.OS, strings.TrimPrefix(sv.Go, "go"), sv.CPUs, sv.InDocker)
	p("  running for %s, memory %s (heap %s)", (time.Duration(sv.Uptime) * time.Second).String(), size(int64(sv.Memory)), size(int64(sv.Heap)))
	p("  ffmpeg %s, libvips %s", orNone(sv.FFmpeg), orNone(sv.Vips))
	p("")
	p("Library")
	p("  %d files, %s", st.Files, size(st.Bytes))
	for _, k := range st.Kinds {
		p("  %s: %d files, %s", k.Kind, k.Files, size(k.Bytes))
	}
	p("  %d favorites", st.Favorites)
	p("  %d tags in use, %d tag links (%d by the user), %d parents, %d siblings, %d untagged files", st.Tags, st.TagLinks, st.UserLinks, st.Parents, st.Siblings, st.Untagged)
	p("  deleted: %d in the undo window (%s), %d for good", st.Trash.Files, size(st.Trash.Bytes), st.Removed)
	p("")
	p("Storage")
	p("  originals %s, database %s, thumbnails %s, previews %s, backups %s", size(storage.Originals), size(storage.Database), size(storage.Thumbs), size(storage.Previews), size(storage.Backups))
	for _, d := range storage.Disks {
		p("  disk (%s): %s free of %s", d.For, size(int64(d.Free)), size(int64(d.Total)))
	}
	p("")
	p("Sites")
	for _, si := range infos {
		var notes []string
		if si.BuiltIn {
			notes = append(notes, "built in")
		}
		if si.Version != "" {
			notes = append(notes, "version "+si.Version)
		}
		if len(si.Fields) > 0 {
			set := 0
			for _, f := range si.Fields {
				if si.Set[f] {
					set++
				}
			}
			login := fmt.Sprintf("login %d of %d fields set", set, len(si.Fields))
			if si.Required {
				login += " (required)"
			}
			notes = append(notes, login)
		}
		if si.Off {
			notes = append(notes, "switched off")
		}
		notes = append(notes, fmt.Sprintf("%d subscriptions", si.Subscriptions))
		p("  %s: %s engine, %s", red.alias("site", si.ID), si.Engine, strings.Join(notes, ", "))
	}
	for _, si := range st.Sites {
		eng := engines[si.Site]
		if eng == "" {
			eng = "plugin not installed"
		}
		p("  %s (%s): %d files, %d posts seen", red.alias("site", si.Site), eng, si.Files, si.Posts)
	}
	p("")
	p("Downloads")
	p("  subscriptions: %d (%d paused, %d failing)", st.Subs.Total, st.Subs.Paused, st.Subs.Failing)
	if s.dl != nil {
		if list, err := s.dl.List(ctx); err == nil {
			states := map[string]int{}
			for _, q := range list {
				states[q.State]++
			}
			var parts []string
			for _, k := range []string{"running", "queued", "paused", "failed", "done"} {
				if states[k] > 0 {
					parts = append(parts, fmt.Sprintf("%d %s", states[k], k))
				}
			}
			p("  searches: %s", orNone(strings.Join(parts, ", ")))
			shown := 0
			for i := len(list) - 1; i >= 0 && shown < 15; i-- {
				q := list[i]
				if q.State != "failed" && q.Error == "" {
					continue
				}
				shown++
				p("  failed: %s %s after %d posts (%d attempts): %s", red.alias("site", q.Site), red.alias("search", q.Query), q.Handled, q.Attempts, red.text(q.Error))
			}
		}
		if subs, err := s.dl.Subscriptions(ctx); err == nil {
			for _, sub := range subs {
				if sub.Error == "" {
					continue
				}
				last := "never"
				if sub.LastCheckAt > 0 {
					last = time.Unix(sub.LastCheckAt, 0).Format("2006-01-02 15:04")
				}
				p("  subscription failing: %s %s, every %s, last check %s: %s", red.alias("site", sub.Site), red.alias("search", sub.Query),
					time.Duration(sub.Interval*int64(time.Second)).String(), last, red.text(sub.Error))
			}
		}
	}
	p("")
	if set, err := s.lib.Settings(ctx); err == nil {
		keys := make([]string, 0, len(set))
		for k := range set {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			v := string(set[k])
			if strings.HasPrefix(v, "\"") || strings.HasPrefix(v, "[") || strings.HasPrefix(v, "{") {
				if v == "\"\"" || v == "[]" || v == "{}" {
					v = "empty"
				} else {
					v = "set" // text, lists: may be private
				}
			}
			parts = append(parts, k+"="+v)
		}
		p("Settings")
		for i := 0; i < len(parts); i += 6 {
			p("  %s", strings.Join(parts[i:min(i+6, len(parts))], " "))
		}
		p("")
	}
	write := func(recs []logring.Record) {
		for _, rec := range recs {
			line := rec.Time.Format("2006-01-02 15:04:05") + " " + rec.Level.String() + " " + red.text(rec.Msg)
			for _, a := range rec.Attrs {
				line += " " + red.attr(a)
			}
			b.WriteString(line + "\n")
		}
	}
	// Warnings and errors first, from their own log (it reaches back weeks; the whole log, hours on a busy
	// downloader).
	var warns, recs []logring.Record
	if s.logs != nil {
		warns = s.logs.RecordsAt(300, slog.LevelWarn)
		recs = s.logs.Records(5000)
	}
	if len(warns) > 0 {
		p("Warnings and errors (the last %d, since %s)", len(warns), warns[0].Time.Format("2006-01-02 15:04"))
		write(warns)
		p("")
	}
	if len(recs) > 0 {
		p("Log (the last %d records, since %s; earlier runs too)", len(recs), recs[0].Time.Format("2006-01-02 15:04"))
	} else {
		p("Log (empty)")
	}
	write(recs)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Query().Get("download") != "" {
		w.Header().Set("Content-Disposition", `attachment; filename="vitrine-report-`+time.Now().Format("2006-01-02")+`.txt"`)
	}
	w.Write([]byte(b.String()))
}

func orNone(s string) string {
	if s == "" {
		return "not found"
	}
	return s
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 4; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTP"[exp])
}

// ---- Errors in the web app (window errors, failed promises) go to the
// server's log, so the report has them too. A few per minute at most.

var clientErrors struct {
	sync.Mutex
	window time.Time
	n      int
}

// clientLog: POST /api/log {message, stack, where}.
func (s *Server) clientLog(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Message string `json:"message"`
		Stack   string `json:"stack"`
		Where   string `json:"where"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	clientErrors.Lock()
	if time.Since(clientErrors.window) > time.Minute {
		clientErrors.window, clientErrors.n = time.Now(), 0
	}
	clientErrors.n++
	over := clientErrors.n > 20
	clientErrors.Unlock()
	if !over {
		cut := func(s string, n int) string {
			if len(s) > n {
				return s[:n] + "…"
			}
			return s
		}
		s.log.Warn("web app error", "message", cut(req.Message, 500), "where", cut(req.Where, 100), "stack", cut(req.Stack, 1500))
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
