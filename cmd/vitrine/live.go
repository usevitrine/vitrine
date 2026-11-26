package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/usevitrine/vitrine/internal/config"
)

// The running server, reached through its local socket (config Socket, by default <data_dir>/vitrine.sock): the
// same API the web app uses, acting as the account without a login. Commands that change the library go through it
// when Vitrine is running, so the server sees the change at once (its search index, the open web app).

// errNotRunning: no Vitrine answers on the socket (not running, or the socket is off).
var errNotRunning = errors.New("Vitrine isn't running (or its local socket is off: config socket)")

type live struct {
	hc *http.Client
}

// dialLive connects to the running server; errNotRunning when there is none.
func dialLive(ctx context.Context, c config.Config) (*live, error) {
	if c.Socket == "" || c.Socket == "off" {
		return nil, errNotRunning
	}
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", c.Socket)
	}
	conn, err := dial(ctx, "", "")
	if err != nil {
		return nil, errNotRunning
	}
	conn.Close()
	return &live{hc: &http.Client{Transport: &http.Transport{DialContext: dial, DisableCompression: false}}}, nil
}

// needLive is dialLive for the commands that only work with a running server.
func needLive(ctx context.Context, c config.Config) (*live, error) {
	l, err := dialLive(ctx, c)
	if errors.Is(err, errNotRunning) {
		return nil, fmt.Errorf("%w - start it with vitrine serve (in Docker: docker exec <container> vitrine ...)", err)
	}
	return l, err
}

// do sends a request (a body: JSON) and decodes the JSON answer into out (nil: ignored). The server's
// {"error": "..."} becomes the error.
func (l *live) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://vitrine"+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := l.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			return errors.New(e.Error)
		}
		return fmt.Errorf("%s %s: %s", method, path, resp.Status)
	}
	if out == nil {
		return nil
	}
	if raw, ok := out.(*[]byte); ok {
		*raw = data
		return nil
	}
	return json.Unmarshal(data, out)
}

func (l *live) get(ctx context.Context, path string, out any) error {
	return l.do(ctx, http.MethodGet, path, nil, out)
}

func (l *live) post(ctx context.Context, path string, body, out any) error {
	if body == nil {
		body = struct{}{}
	}
	return l.do(ctx, http.MethodPost, path, body, out)
}

// esc escapes a path part (a site ID, a name).
func esc(s string) string { return url.PathEscape(s) }

// ago writes a past moment shortly ("3m ago", "2h ago", "5d ago").
func ago(ms int64) string {
	if ms <= 0 {
		return "never"
	}
	d := time.Since(time.UnixMilli(ms))
	switch {
	case d < 0:
		return "in " + short(-d)
	case d < time.Minute:
		return "just now"
	}
	return short(d) + " ago"
}

// short writes a duration in its largest unit ("40s", "12m", "3h", "2d").
func short(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// parseEvery reads a schedule: Go durations (90m, 12h) and days or weeks (1d, 3d, 1w).
func parseEvery(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	for suffix, unit := range map[string]time.Duration{"d": 24 * time.Hour, "w": 7 * 24 * time.Hour} {
		if n, ok := strings.CutSuffix(s, suffix); ok {
			var k float64
			if _, err := fmt.Sscanf(n, "%g", &k); err == nil && k > 0 {
				return time.Duration(k * float64(unit)), nil
			}
		}
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("schedule %q: e.g. 6h, 1d, 3d, 1w", s)
	}
	return d, nil
}

// parseMixed parses a command's flags wherever they are among its words ("queue add site query -max 5"), leaving
// every other word - a search's "-cloud" too - in order as an argument. "--" ends the flags.
func parseMixed(fset *flag.FlagSet, args []string) ([]string, error) {
	var flags, words []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			words = append(words, args[i+1:]...)
			break
		}
		name, _, hasValue := strings.Cut(strings.TrimLeft(a, "-"), "=")
		f := fset.Lookup(name)
		if !strings.HasPrefix(a, "-") || a == "-" || f == nil {
			words = append(words, a)
			continue
		}
		flags = append(flags, a)
		if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); !hasValue && !(ok && bf.IsBoolFlag()) && i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	if err := fset.Parse(flags); err != nil {
		return nil, err
	}
	return words, nil
}
