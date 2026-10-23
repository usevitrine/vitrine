package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/usevitrine/vitrine/internal/library"
)

func cmdBlacklist(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fset := flag.NewFlagSet("blacklist", flag.ContinueOnError)
	fset.SetOutput(stderr)
	fset.Usage = func() {
		fmt.Fprintln(stderr, `Usage: vitrine blacklist [flags] [list | add <tag>... | remove <tag>... | matches]

Posts with a blacklisted tag are never downloaded. A tag may use '*' (ai_*).
Tags may also be given comma-separated. "matches" counts library files that
have blacklisted tags (delete them in the web app: Tags > Blacklist).

Example: vitrine blacklist add ai_generated furry "ai_*"`)
		fset.PrintDefaults()
	}
	load := configFlags(fset)
	if err := fset.Parse(args); err != nil {
		return err
	}
	action, rest := "list", fset.Args()
	if len(rest) > 0 {
		action, rest = rest[0], rest[1:]
	}
	var tags []string
	for _, a := range rest {
		for _, t := range strings.Split(a, ",") {
			if t = library.NormalizePattern(t); t != "" {
				tags = append(tags, t)
			}
		}
	}
	if (action == "add" || action == "remove") && len(tags) == 0 {
		fset.Usage()
		return flag.ErrHelp
	}
	c, err := load()
	if err != nil {
		return err
	}
	if !slices.Contains([]string{"list", "add", "remove", "matches"}, action) {
		fset.Usage()
		return fmt.Errorf("unknown action %q", action)
	}
	if ran, err := withLive(ctx, c, func(l *live) error { return liveBlacklist(ctx, l, action, tags, stdout) }); ran {
		return err
	}
	lib, closeLib, err := openLibrary(ctx, c)
	if err != nil {
		return err
	}
	defer closeLib()

	b, err := lib.Blacklist(ctx)
	if err != nil {
		return err
	}
	cur := b.Patterns()
	switch action {
	case "list":
	case "add":
		cur = append(cur, tags...)
	case "remove":
		cur = slices.DeleteFunc(cur, func(p string) bool { return slices.Contains(tags, p) })
	case "matches":
		m, err := lib.BlacklistedFiles(ctx, nil)
		if err != nil {
			return err
		}
		for _, pc := range m.Counts {
			if pc.Files > 0 {
				fmt.Fprintf(stdout, "%-30s %d\n", pc.Pattern, pc.Files)
			}
		}
		fmt.Fprintf(stdout, "%d files with blacklisted tags (%.1f MB)\n", m.Total, float64(m.Size)/1e6)
		return nil
	default:
		fset.Usage()
		return fmt.Errorf("unknown action %q", action)
	}
	if action != "list" {
		if cur, err = lib.SetBlacklist(ctx, cur); err != nil {
			return err
		}
	}
	for _, p := range cur {
		fmt.Fprintln(stdout, p)
	}
	fmt.Fprintf(stdout, "%d blacklisted tags\n", len(cur))
	return nil
}
