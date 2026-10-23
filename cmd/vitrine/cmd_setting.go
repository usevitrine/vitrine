package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"slices"
)

// cmdSetting lists, shows or changes the settings shared by every device (the web app's Settings).
func cmdSetting(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fset := flag.NewFlagSet("setting", flag.ContinueOnError)
	fset.SetOutput(stderr)
	fset.Usage = func() {
		fmt.Fprintln(stderr, `Usage: vitrine setting [flags] [<name> [<value>]]

Without a name: every setting that is set, one per line. With a name: its
value. With a value too: sets it (a JSON value - a number, true/false, or a
string in quotes). Only known names and sensible values are accepted.
With Vitrine running, the change goes through it (it applies at once).

Examples:
  vitrine setting
  vitrine setting pauseAfter 10
  vitrine setting defaultSearch '"system:imported<5d"'`)
		fset.PrintDefaults()
	}
	load := configFlags(fset)
	if err := fset.Parse(args); err != nil {
		return err
	}
	rest := fset.Args()
	if len(rest) > 2 {
		fset.Usage()
		return flag.ErrHelp
	}
	c, err := load()
	if err != nil {
		return err
	}
	if ran, err := withLive(ctx, c, func(l *live) error { return liveSetting(ctx, l, rest, stdout) }); ran {
		return err
	}
	lib, closeLib, err := openLibrary(ctx, c)
	if err != nil {
		return err
	}
	defer closeLib()

	if len(rest) == 2 {
		v := json.RawMessage(rest[1])
		if !json.Valid(v) {
			v, _ = json.Marshal(rest[1]) // (a bare word: a string)
		}
		if err := lib.SetSettings(ctx, map[string]json.RawMessage{rest[0]: v}); err != nil {
			return err
		}
	}
	set, err := lib.Settings(ctx)
	if err != nil {
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
	names := make([]string, 0, len(set))
	for k := range set {
		names = append(names, k)
	}
	slices.Sort(names)
	for _, k := range names {
		fmt.Fprintf(stdout, "%s = %s\n", k, set[k])
	}
	return nil
}
