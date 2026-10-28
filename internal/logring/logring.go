// Package logring keeps the server's recent log records for the problem report
// (see internal/server/report.go), with their attributes still apart (key and
// value) so private values can be hidden by key. They're kept in memory and,
// when a file is given, also in a small log file of JSON lines (rotated: the
// current file and one older one), so they survive restarts and crashes.
// Warnings and errors also go to a log of their own beside it: the main one is
// mostly debug lines (one per post) and reaches back only hours on a busy
// downloader; the warnings' reaches back weeks.
package logring

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// Record is one log record: its attributes flattened ("group.key").
type Record struct {
	Time  time.Time
	Level slog.Level
	Msg   string
	Attrs []slog.Attr
}

// Ring holds the newest records: in memory, and in the log file if there is one.
type Ring struct {
	mu   sync.Mutex
	buf  []Record
	next int
	full bool

	main *logFile // every record (nil: memory only)
	warn *logFile // warnings and errors (nil: none)
}

// New returns a ring for n records in memory.
func New(n int) *Ring { return &Ring{buf: make([]Record, n)} }

// Open returns a ring that also writes to the log file at path (rotated to
// path.1 when it grows past max bytes), and its warnings and errors to
// WarningsPath(path) (rotated at a quarter of that).
func Open(path string, n int, max int64) (*Ring, error) {
	r := New(n)
	main, err := openLogFile(path, max)
	if err != nil {
		return r, err
	}
	r.main = main
	if r.warn, err = openLogFile(WarningsPath(path), max/4); err != nil {
		r.warn = nil // (the main log still works)
	}
	return r, nil
}

// WarningsPath is the warnings' log beside the log at path (vitrine.log -> vitrine-warnings.log).
func WarningsPath(path string) string {
	ext := filepath.Ext(path)
	return strings.TrimSuffix(path, ext) + "-warnings" + ext
}

// Close closes the log files.
func (r *Ring) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return errors.Join(r.main.close(), r.warn.close())
}

// logFile is a log file of JSON lines, rotated to path.1 past max bytes.
type logFile struct {
	path string
	max  int64
	f    *os.File
	size int64
}

func openLogFile(path string, max int64) (*logFile, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	return &logFile{path: path, max: max, f: f, size: fi.Size()}, nil
}

func (lf *logFile) close() error {
	if lf == nil || lf.f == nil {
		return nil
	}
	err := lf.f.Close()
	lf.f = nil
	return err
}

func (lf *logFile) write(line []byte) {
	if lf.f != nil && lf.size+int64(len(line)) > lf.max {
		lf.rotate()
	}
	if lf.f == nil {
		return
	}
	if n, err := lf.f.Write(line); err == nil {
		lf.size += int64(n)
	}
}

// rotate: the current file becomes path.1 (the older one goes); a new one starts.
func (lf *logFile) rotate() {
	lf.f.Close()
	lf.f = nil
	if err := os.Rename(lf.path, lf.path+".1"); err != nil {
		return
	}
	f, err := os.OpenFile(lf.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	lf.f, lf.size = f, 0
}

// records reads up to n of the newest records at or above floor, oldest first (the older file, then the current).
func (lf *logFile) records(n int, floor slog.Level) []Record {
	var out []Record
	for _, p := range []string{lf.path + ".1", lf.path} {
		out = append(out, readFile(p, floor)...)
		if len(out) > n {
			out = out[len(out)-n:]
		}
	}
	return out
}

// drop rewrites both files without the lines gone says yes to (the current one reopened for appending).
func (lf *logFile) drop(gone func(string) bool) {
	for _, p := range []string{lf.path + ".1", lf.path} {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var out []byte
		n := 0
		for _, ln := range bytes.SplitAfter(data, []byte("\n")) {
			if len(ln) > 0 && gone(string(ln)) {
				n++
				continue
			}
			out = append(out, ln...)
		}
		if n == 0 {
			continue
		}
		if p == lf.path && lf.f != nil {
			lf.f.Close()
			lf.f = nil
		}
		if os.WriteFile(p+".tmp", out, 0o600) == nil {
			os.Rename(p+".tmp", p)
		}
		if p == lf.path {
			if f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
				lf.f, lf.size = f, int64(len(out))
			}
		}
	}
}

func (r *Ring) add(rec Record) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf[r.next] = rec
	r.next = (r.next + 1) % len(r.buf)
	if r.next == 0 {
		r.full = true
	}
	if r.main == nil {
		return
	}
	line, err := json.Marshal(toLine(rec))
	if err != nil {
		return
	}
	line = append(line, '\n')
	r.main.write(line)
	if r.warn != nil && rec.Level >= slog.LevelWarn {
		r.warn.write(line)
	}
}

// Records returns up to n of the newest records, oldest first: from the log
// file (this run and earlier ones) if there is one, else from memory.
func (r *Ring) Records(n int) []Record { return r.RecordsAt(n, slog.Level(math.MinInt)) }

// RecordsAt is Records of only the records at or above floor - picked out of
// the whole log, not out of the newest n (warnings and errors from the
// warnings' log, which reaches back further).
func (r *Ring) RecordsAt(n int, floor slog.Level) []Record {
	r.mu.Lock()
	main, warn := r.main, r.warn
	mem := r.memory()
	r.mu.Unlock()
	switch {
	case main == nil:
		mem = slices.DeleteFunc(mem, func(rec Record) bool { return rec.Level < floor })
		if len(mem) > n {
			mem = mem[len(mem)-n:]
		}
		return mem
	case warn != nil && floor >= slog.LevelWarn:
		return warn.records(n, floor)
	default:
		return main.records(n, floor)
	}
}

func (r *Ring) memory() []Record {
	if !r.full {
		return append([]Record(nil), r.buf[:r.next]...)
	}
	return append(append([]Record(nil), r.buf[r.next:]...), r.buf[:r.next]...)
}

// ---- The file: one JSON object per record, its attributes as [key, value]
// pairs in order (numbers and booleans stay numbers and booleans).

type line struct {
	T time.Time `json:"t"`
	L string    `json:"l"`
	M string    `json:"m"`
	A [][2]any  `json:"a,omitempty"`
}

func toLine(rec Record) line {
	l := line{T: rec.Time, L: rec.Level.String(), M: rec.Msg}
	for _, a := range rec.Attrs {
		var v any
		switch a.Value.Kind() {
		case slog.KindInt64:
			v = a.Value.Int64()
		case slog.KindUint64:
			v = a.Value.Uint64()
		case slog.KindFloat64:
			v = a.Value.Float64()
		case slog.KindBool:
			v = a.Value.Bool()
		case slog.KindString:
			v = a.Value.String()
		default: // durations, times: as text, but not strings (never private)
			v = map[string]string{"v": a.Value.String()}
		}
		l.A = append(l.A, [2]any{a.Key, v})
	}
	return l
}

// levelMark is where a line's level is (toLine writes the time, then the level).
var levelMark = []byte(`"l":"`)

// below reports whether a line's level is under floor - read without decoding the line (most of a busy log are
// debug lines, skipped when only warnings are asked for).
func below(ln []byte, floor slog.Level) bool {
	i := bytes.Index(ln, levelMark)
	if i < 0 {
		return false
	}
	rest := ln[i+len(levelMark):]
	j := bytes.IndexByte(rest, '"')
	if j < 0 {
		return false
	}
	var lv slog.Level
	return lv.UnmarshalText(rest[:j]) == nil && lv < floor
}

func readFile(path string, floor slog.Level) []Record {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		if below(sc.Bytes(), floor) {
			continue
		}
		var l line
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue // a line cut short by a crash
		}
		var lv slog.Level
		lv.UnmarshalText([]byte(l.L))
		rec := Record{Time: l.T, Level: lv, Msg: l.M}
		for _, kv := range l.A {
			k, _ := kv[0].(string)
			var v slog.Value
			switch x := kv[1].(type) {
			case string:
				v = slog.StringValue(x)
			case float64:
				if x == float64(int64(x)) {
					v = slog.Int64Value(int64(x))
				} else {
					v = slog.Float64Value(x)
				}
			case bool:
				v = slog.BoolValue(x)
			case map[string]any:
				s, _ := x["v"].(string)
				v = slog.AnyValue(text(s))
			default:
				v = slog.StringValue(fmt.Sprint(x))
			}
			rec.Attrs = append(rec.Attrs, slog.Attr{Key: k, Value: v})
		}
		out = append(out, rec)
	}
	return out
}

// text is a value shown as it is (a duration or time read back from the file):
// not a string, so the report never takes it for a private one.
type text string

func (t text) String() string { return string(t) }

// Tee returns a handler that passes records to next and keeps a copy in ring.
func Tee(next slog.Handler, ring *Ring) slog.Handler {
	return &handler{next: next, ring: ring}
}

type handler struct {
	next   slog.Handler
	ring   *Ring
	attrs  []slog.Attr // from WithAttrs, keys already prefixed
	prefix string      // from WithGroup: "group."
}

// Enabled: the log file takes everything from debug up (the details of what
// happened); the next handler (the terminal) only what its level allows.
func (h *handler) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= slog.LevelDebug || h.next.Enabled(ctx, l)
}

func (h *handler) Handle(ctx context.Context, rec slog.Record) error {
	attrs := append([]slog.Attr(nil), h.attrs...)
	rec.Attrs(func(a slog.Attr) bool {
		attrs = flatten(attrs, h.prefix, a)
		return true
	})
	h.ring.add(Record{Time: rec.Time, Level: rec.Level, Msg: rec.Message, Attrs: attrs})
	if !h.next.Enabled(ctx, rec.Level) {
		return nil
	}
	return h.next.Handle(ctx, rec)
}

func (h *handler) WithAttrs(as []slog.Attr) slog.Handler {
	c := *h
	c.attrs = append([]slog.Attr(nil), h.attrs...)
	for _, a := range as {
		c.attrs = flatten(c.attrs, h.prefix, a)
	}
	c.next = h.next.WithAttrs(as)
	return &c
}

func (h *handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	c := *h
	c.prefix = h.prefix + name + "."
	c.next = h.next.WithGroup(name)
	return &c
}

// flatten adds a (resolved; groups become "group.key" attributes).
func flatten(out []slog.Attr, prefix string, a slog.Attr) []slog.Attr {
	a.Value = a.Value.Resolve()
	if a.Value.Kind() == slog.KindGroup {
		p := prefix
		if a.Key != "" {
			p += a.Key + "."
		}
		for _, g := range a.Value.Group() {
			out = flatten(out, p, g)
		}
		return out
	}
	if a.Key == "" {
		return out
	}
	// Errors and other values become their text now (they may change later).
	if a.Value.Kind() == slog.KindAny {
		a.Value = slog.StringValue(a.Value.String())
	}
	return append(out, slog.Attr{Key: prefix + a.Key, Value: a.Value})
}

// Forget drops every record that mentions any of words (case-insensitively, in its message or values) from memory
// and the log files - a site wiped from the server leaves no line naming it. It returns how many went.
func (r *Ring) Forget(words ...string) int {
	for i, w := range words {
		words[i] = strings.ToLower(w)
	}
	mentions := func(s string) bool {
		s = strings.ToLower(s)
		for _, w := range words {
			if w != "" && strings.Contains(s, w) {
				return true
			}
		}
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	var keep []Record
	for _, rec := range r.memory() {
		text := rec.Msg
		for _, a := range rec.Attrs {
			text += " " + a.Key + " " + a.Value.String()
		}
		if mentions(text) {
			n++
		} else {
			keep = append(keep, rec)
		}
	}
	clear(r.buf)
	r.next, r.full = copy(r.buf, keep[max(0, len(keep)-len(r.buf)):]), false
	if r.next == len(r.buf) {
		r.next, r.full = 0, true
	}
	// The files: rewritten without those lines.
	for _, lf := range []*logFile{r.main, r.warn} {
		if lf != nil {
			lf.drop(mentions)
		}
	}
	return n
}
