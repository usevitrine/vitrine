package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/usevitrine/vitrine/internal/search"
	"github.com/usevitrine/vitrine/internal/tag"
)

func cmdSearch(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fset := flag.NewFlagSet("search", flag.ContinueOnError)
	fset.SetOutput(stderr)
	fset.Usage = func() {
		fmt.Fprintln(stderr, `Usage: vitrine search [flags] <search>

Examples:
  vitrine search blue_sky -cloud
  vitrine search "( some_artist ~ other_artist )" system:has_audio sort:duration
  vitrine search system:imported<1d system:type=video`)
		fset.PrintDefaults()
	}
	load := configFlags(fset)
	n := fset.Int("n", 20, "how many results to list")
	showTags := fset.Bool("tags", false, "list each file's tags")
	if err := fset.Parse(args); err != nil {
		return err
	}
	q, err := search.Parse(strings.Join(fset.Args(), " "))
	if err != nil {
		return err
	}
	c, err := load()
	if err != nil {
		return err
	}
	if ran, err := withLive(ctx, c, func(l *live) error { return liveSearch(ctx, l, strings.Join(fset.Args(), " "), *n, *showTags, stdout) }); ran {
		return err
	}
	lib, closeLib, err := openLibrary(ctx, c)
	if err != nil {
		return err
	}
	defer closeLib()

	start := time.Now()
	res := lib.Index().Search(q, time.Now())
	took := time.Since(start)
	for _, u := range res.Unknown {
		fmt.Fprintf(stdout, "note: no tag %q in the library\n", u)
	}
	tookText := took.Round(time.Microsecond).String()
	if took < time.Millisecond {
		tookText = "under 1 ms" // also covers Windows' coarse clock reading 0
	}
	fmt.Fprintf(stdout, "%d files (search took %s)\n", res.Total, tookText)

	files, err := lib.Files(ctx, res.IDs[:min(*n, len(res.IDs))])
	if err != nil {
		return err
	}
	for _, f := range files {
		desc := fmt.Sprintf("%dx%d", f.Width, f.Height)
		if f.DurationMS > 0 {
			desc += fmt.Sprintf(" %.1fs", float64(f.DurationMS)/1000)
		}
		if f.HasAudio {
			desc += " audio"
		}
		fmt.Fprintf(stdout, "#%-7d %-5s %-22s %8s  %s\n", f.ID, f.Type, desc, size(f.Size), f.Path)
		if *showTags {
			ft, err := lib.FileTags(ctx, f.ID)
			if err != nil {
				return err
			}
			names := make([]string, len(ft))
			for i, t := range ft {
				names[i] = t.Tag
			}
			fmt.Fprintf(stdout, "         tags: %s\n", strings.Join(names, " "))
		}
	}
	return nil
}

func size(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func cmdTag(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fset := flag.NewFlagSet("tag", flag.ContinueOnError)
	fset.SetOutput(stderr)
	fset.Usage = func() {
		fmt.Fprintln(stderr, `Usage: vitrine tag [flags] <file ID>...

Example: vitrine tag -add blue_sky -add creator:someone -remove cloud 12 13`)
		fset.PrintDefaults()
	}
	load := configFlags(fset)
	var add, remove listFlag
	fset.Var(&add, "add", "tag to add (repeatable)")
	fset.Var(&remove, "remove", "tag to remove (repeatable)")
	if err := fset.Parse(args); err != nil {
		return err
	}
	var ids []uint32
	for _, a := range fset.Args() {
		id, err := strconv.ParseUint(strings.TrimPrefix(a, "#"), 10, 32)
		if err != nil {
			return fmt.Errorf("bad file ID %q", a)
		}
		ids = append(ids, uint32(id))
	}
	if len(ids) == 0 || len(add)+len(remove) == 0 {
		fset.Usage()
		return flag.ErrHelp
	}
	c, err := load()
	if err != nil {
		return err
	}
	if ran, err := withLive(ctx, c, func(l *live) error { return liveTag(ctx, l, ids, add, remove, stdout) }); ran {
		return err
	}
	lib, closeLib, err := openLibrary(ctx, c)
	if err != nil {
		return err
	}
	defer closeLib()

	parse := func(texts []string) []tag.Tag {
		out := make([]tag.Tag, len(texts))
		for i, t := range texts {
			out[i] = lib.ParseTag(t)
		}
		return out
	}
	if len(add) > 0 {
		if err := lib.AddTags(ctx, ids, parse(add)); err != nil {
			return err
		}
	}
	if len(remove) > 0 {
		if err := lib.RemoveTags(ctx, ids, parse(remove)); err != nil {
			return err
		}
	}
	fmt.Fprintf(stdout, "updated %d files\n", len(ids))
	return nil
}

// listFlag collects a repeatable string flag.
type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(s string) error { *l = append(*l, s); return nil }
