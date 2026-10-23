// Package logring keeps the server's recent log records for the problem report
// (see internal/server/report.go), with their attributes still apart (key and
// value) so private values can be hidden by key. They're kept in memory and,
// when a file is given, also in a small log file of JSON lines (rotated: the
// current file and one older one), so they survive restarts and crashes.
package logring

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
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

	path string // "" = memory only
	max  int64  // the file is rotated past this size
	f    *os.File
	size int64
}

// New returns a ring for n records in memory.
func New(n int) *Ring { return &Ring{buf: make([]Record, n)} }

// Open returns a ring that also writes to the log file at path (rotated to
// path.1 when it grows past max bytes).
func Open(path string, n int, max int64) (*Ring, error) {
	r := New(n)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return r, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return r, err
	}
	r.path, r.max, r.f, r.size = path, max, f, fi.Size()
	return r, nil
}

// Close closes the log file.
func (r *Ring) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}

func (r *Ring) add(rec Record) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf[r.next] = rec
	r.next = (r.next + 1) % len(r.buf)
	if r.next == 0 {
		r.full = true
	}
	if r.f == nil {
		return
	}
	line, err := json.Marshal(toLine(rec))
	if err != nil {
		return
	}
	line = append(line, '\n')
	if r.size+int64(len(line)) > r.max {
		r.rotate()
		if r.f == nil {
			return
		}
	}
	if n, err := r.f.Write(line); err == nil {
		r.size += int64(n)
	}
}

// rotate: the current file becomes path.1 (the older one goes); a new one starts.
func (r *Ring) rotate() {
	r.f.Close()
	r.f = nil
	if err := os.Rename(r.path, r.path+".1"); err != nil {
		return
	}
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	r.f, r.size = f, 0
}

// Records returns up to n of the newest records, oldest first: from the log
// file (this run and earlier ones) if there is one, else from memory.
func (r *Ring) Records(n int) []Record {
	r.mu.Lock()
	path := r.path
	mem := r.memory()
	r.mu.Unlock()
	if path == "" {
		if len(mem) > n {
			mem = mem[len(mem)-n:]
		}
		return mem
	}
	var out []Record
	for _, p := range []string{path + ".1", path} {
		out = append(out, readFile(p)...)
		if len(out) > n {
			out = out[len(out)-n:]
		}
	}
	return out
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

func readFile(path string) []Record {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
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
