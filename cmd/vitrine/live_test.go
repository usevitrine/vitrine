package main

import (
	"context"
	"flag"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// The commands that talk to a running Vitrine: a real server (serve) in the background, the commands through its
// local socket - and, once it's stopped, the library commands on the files again.
func TestLiveCommands(t *testing.T) {
	t.Setenv("VITRINE_CONFIG", "")
	data, src := t.TempDir(), t.TempDir()
	writeImage(t, filepath.Join(src, "a.png"), 10, true)
	if out, err := runCmd(t, "import", "-data-dir", data, src); err != nil {
		t.Fatalf("import: %v\n%s", err, out)
	}
	sock := filepath.Join(data, "vitrine.sock")

	// Not running yet: the live-only commands say so; the library ones work on the files.
	if out, err := runCmd(t, "status", "-data-dir", data); err == nil || !strings.Contains(err.Error(), "isn't running") {
		t.Fatalf("status without a server: %v\n%s", err, out)
	}
	if out, err := runCmd(t, "search", "-data-dir", data); err != nil || !strings.Contains(out, "1 files") {
		t.Fatalf("offline search: %v\n%s", err, out)
	}

	ctx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"serve", "-data-dir", data, "-listen", "127.0.0.1:0"}, os.Stdout, os.Stderr)
	}()
	defer func() {
		stop()
		if err := <-done; err != nil {
			t.Errorf("serve: %v", err)
		}
		if _, err := os.Stat(sock); !os.IsNotExist(err) {
			t.Errorf("the socket is left behind: %v", err)
		}
	}()
	// (no account yet: a command through the socket is told to make one)
	deadline := time.Now().Add(20 * time.Second)
	for {
		out, err := runCmd(t, "status", "-data-dir", data)
		if err != nil && strings.Contains(err.Error(), "no account yet") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the server never answered on its socket: %v\n%s", err, out)
		}
		time.Sleep(100 * time.Millisecond)
	}
	setPassword(t, data)

	out, err := runCmd(t, "status", "-data-dir", data)
	if err != nil || !strings.Contains(out, "Library    1 files") || !strings.Contains(out, "Queue      0 running") {
		t.Fatalf("status: %v\n%s", err, out)
	}
	// Through the server: tagged in its index at once (a search finds it), settings, the blacklist.
	if out, err := runCmd(t, "tag", "-data-dir", data, "-add", "live_tag", "1"); err != nil || !strings.Contains(out, "updated 1 files") {
		t.Fatalf("tag: %v\n%s", err, out)
	}
	if out, err := runCmd(t, "search", "-data-dir", data, "live_tag"); err != nil || !strings.Contains(out, "1 files") || !strings.Contains(out, "#1 ") {
		t.Fatalf("search after tagging: %v\n%s", err, out)
	}
	if out, err := runCmd(t, "setting", "-data-dir", data, "pauseAfter", "7"); err != nil || strings.TrimSpace(out) != "7" {
		t.Fatalf("setting: %v\n%s", err, out)
	}
	if out, err := runCmd(t, "blacklist", "-data-dir", data, "add", "never_this"); err != nil || !strings.Contains(out, "never_this") {
		t.Fatalf("blacklist: %v\n%s", err, out)
	}
	if out, err := runCmd(t, "api", "-data-dir", data, "/api/blacklist"); err != nil || !strings.Contains(out, `"never_this"`) {
		t.Fatalf("api: %v\n%s", err, out)
	}
	// The queue, subscriptions, notices, failed posts, sites, logs: answered (nothing running here).
	for _, c := range [][]string{{"queue"}, {"subs"}, {"notices"}, {"failed"}, {"sites"}, {"logs", "-n", "5"}} {
		out, err := runCmd(t, append(c, "-data-dir", data)...)
		if err != nil {
			t.Fatalf("%s: %v\n%s", c[0], err, out)
		}
	}
	if out, _ := runCmd(t, "logs", "-data-dir", data, "-n", "50"); !strings.Contains(out, "local socket") {
		t.Fatalf("logs without the start-up records:\n%s", out)
	}
	// A wrong action, and an unknown site: the server's own error.
	if _, err := runCmd(t, "queue", "-data-dir", data, "frobnicate"); err == nil {
		t.Fatal("an unknown action passed")
	}
	if _, err := runCmd(t, "queue", "-data-dir", data, "add", "no_such_site", "blue_sky"); err == nil {
		t.Fatal("an unknown site passed")
	}
}

// setPassword makes the account (as vitrine passwd would, the password on standard input).
func setPassword(t *testing.T, data string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.WriteString("a-long-password\n")
	w.Close()
	var out strings.Builder
	if err := cmdPasswd(context.Background(), []string{"-data-dir", data, "tester"}, &out, &out, r); err != nil {
		t.Fatalf("passwd: %v\n%s", err, out.String())
	}
}

// Flags anywhere among a command's words; words that only look like flags (a search's -cloud) stay words.
func TestParseMixed(t *testing.T) {
	fset := flag.NewFlagSet("x", flag.ContinueOnError)
	fset.SetOutput(io.Discard)
	n := fset.Int("max", 0, "")
	w := fset.Bool("w", false, "")
	words, err := parseMixed(fset, []string{"add", "safebooru", "blue_sky", "-cloud", "-max", "5", "-w", "--", "-max"})
	if err != nil {
		t.Fatal(err)
	}
	if *n != 5 || !*w || !slices.Equal(words, []string{"add", "safebooru", "blue_sky", "-cloud", "-max"}) {
		t.Fatalf("max %d, w %v, words %q", *n, *w, words)
	}
	if _, err := parseMixed(fset, []string{"-max", "lots"}); err == nil {
		t.Fatal("a bad value passed")
	}
}

func TestParseEvery(t *testing.T) {
	for in, want := range map[string]time.Duration{"6h": 6 * time.Hour, "1d": 24 * time.Hour, "3D": 72 * time.Hour, "1w": 168 * time.Hour, "90m": 90 * time.Minute} {
		if got, err := parseEvery(in); err != nil || got != want {
			t.Errorf("%s: %v %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "soon", "-1d", "0h"} {
		if _, err := parseEvery(bad); err == nil {
			t.Errorf("%q passed", bad)
		}
	}
}
