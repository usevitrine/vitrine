package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// cmdProfile records what the running server's own code spends its time on (Go's CPU profile, through the local
// socket) into a file for `go tool pprof` - for load the "load" lines in the log can't explain (2026-10-30).
func cmdProfile(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fset := flag.NewFlagSet("profile", flag.ContinueOnError)
	fset.SetOutput(stderr)
	fset.Usage = func() {
		fmt.Fprint(stderr, `Usage: vitrine profile [flags]

Records what the running Vitrine spends its processor time on (its own code, not ffmpeg's) for a while, into a
file to read with "go tool pprof -top <file>" (on any computer with Go). Best while the load is happening.

Examples:
  vitrine profile                          # 30 seconds into vitrine-cpu.pprof
  vitrine profile -seconds 60 -o /data/cpu.pprof
  vitrine profile -kind heap -o heap.pprof  # memory in use instead

Flags:
`)
		fset.PrintDefaults()
	}
	load := configFlags(fset)
	seconds := fset.Int("seconds", 30, "how long to record (cpu)")
	out := fset.String("o", "", "the file (default vitrine-<kind>.pprof)")
	kind := fset.String("kind", "cpu", "cpu, heap or goroutine")
	if err := fset.Parse(args); err != nil {
		return err
	}
	c, err := load()
	if err != nil {
		return err
	}
	l, err := needLive(ctx, c)
	if err != nil {
		return err
	}
	path := map[string]string{"cpu": fmt.Sprintf("/debug/pprof/profile?seconds=%d", *seconds), "heap": "/debug/pprof/heap",
		"goroutine": "/debug/pprof/goroutine"}[*kind]
	if path == "" {
		return fmt.Errorf("-kind: cpu, heap or goroutine")
	}
	if *out == "" {
		*out = "vitrine-" + *kind + ".pprof"
	}
	if *kind == "cpu" {
		fmt.Fprintf(stderr, "recording for %ds...\n", *seconds)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(*seconds+30)*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://vitrine"+path, nil)
	if err != nil {
		return err
	}
	resp, err := l.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s: %s", resp.Status, b)
	}
	f, err := os.Create(*out)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s (%d KB) - read it with: go tool pprof -top %s\n", *out, n/1024, *out)
	return nil
}
