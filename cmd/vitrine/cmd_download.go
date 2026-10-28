package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/term"

	"github.com/usevitrine/vitrine/internal/config"
	"github.com/usevitrine/vitrine/internal/db"
	"github.com/usevitrine/vitrine/internal/download"
	"github.com/usevitrine/vitrine/internal/library"
	"github.com/usevitrine/vitrine/internal/plugin"
)

func pluginsDir(c config.Config) string  { return filepath.Join(c.DataDir, "plugins") }
func secretsPath(c config.Config) string { return filepath.Join(c.DataDir, "secrets.toml") }

// userAgent identifies Vitrine to sites (downloads, site icons), with the project's page and contact address.
func userAgent() string {
	return "Vitrine/" + version + " (+https://github.com/usevitrine/vitrine; hello@vitrine.software)"
}

// newDownloader loads the plugins and credentials and prepares a downloader.
func newDownloader(ctx context.Context, c config.Config, lib *library.Library, d *db.DB, log *slog.Logger) (*download.Downloader, error) {
	plugin.UserAgent = userAgent()
	plugin.AppVersion = version
	plugins, err := plugin.Load(pluginsDir(c))
	if err != nil {
		return nil, err
	}
	secrets, err := plugin.LoadSecrets(secretsPath(c))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", secretsPath(c), err)
	}
	dl, err := download.New(ctx, lib, d, plugins, secrets, log)
	if dl != nil {
		dl.SecretsPath = secretsPath(c)
		dl.PluginsDir = pluginsDir(c)
		dl.PluginIndex = c.PluginIndex
	}
	return dl, err
}

func cmdDownload(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fset := flag.NewFlagSet("download", flag.ContinueOnError)
	fset.SetOutput(stderr)
	fset.Usage = func() {
		fmt.Fprintln(stderr, `Usage: vitrine download [flags] <site> <search>

Runs a search on a site and downloads its posts into the library (newest
first). Stop it with Ctrl+C at any time: running it again carries on exactly
where it stopped. "vitrine sites" lists the sites.

While "vitrine serve" is running, add downloads in the web page instead: the
server runs them, and its search sees the new files right away.

Example: vitrine download -max 50 safebooru blue_sky clouds`)
		fset.PrintDefaults()
	}
	load := configFlags(fset)
	maxPosts := fset.Int("max", 0, "stop after this many posts (0 = all)")
	if err := fset.Parse(args); err != nil {
		return err
	}
	if fset.NArg() < 2 {
		fset.Usage()
		return flag.ErrHelp
	}
	site, query := fset.Arg(0), strings.Join(fset.Args()[1:], " ")
	c, err := load()
	if err != nil {
		return err
	}
	lib, d, closeLib, err := openApp(ctx, c)
	if err != nil {
		return err
	}
	defer closeLib()
	log := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	dl, err := newDownloader(ctx, c, lib, d, log)
	if err != nil {
		return err
	}
	st := dl.Site(site)
	if st == nil {
		return fmt.Errorf("no plugin %q (have: %s)", site, strings.Join(dl.Sites(), ", "))
	}
	if st.Client.Manifest().Auth.Required && !st.Client.HasCredentials() {
		return fmt.Errorf("%s needs an account: run \"vitrine credentials %s\" first", st.Client.Manifest().Name, site)
	}

	// Carry on an unfinished identical search instead of starting over.
	var id int64
	all, err := dl.List(ctx)
	if err != nil {
		return err
	}
	for _, s := range all {
		if s.Site == site && s.Query == query && s.MaxPosts == *maxPosts &&
			(s.State == download.StateRunning || s.State == download.StateQueued) {
			id = s.ID
			fmt.Fprintf(stdout, "carrying on search #%d (%d posts handled so far)\n", s.ID, s.Handled)
		}
	}
	if id == 0 {
		if id, err = dl.Add(ctx, site, query, *maxPosts); err != nil {
			return err
		}
	}

	outcomes := map[int]string{download.Downloaded: "downloaded", download.Already: "have", download.Deleted: "deleted before",
		download.Failed: "FAILED", download.Skipped: "skipped"}
	dl.OnPost = func(s download.Search, p plugin.Post, outcome int, err error) {
		total := ""
		if s.SiteTotal > 0 {
			total = fmt.Sprintf(" of ~%d", s.SiteTotal)
		}
		line := fmt.Sprintf("[%d%s] post %d: %s", s.Handled, total, p.ID, outcomes[outcome])
		if err != nil {
			line += " (" + err.Error() + ")"
		}
		fmt.Fprintln(stdout, line)
	}
	if err := dl.RunQueue(ctx, site); err != nil && ctx.Err() == nil {
		return err
	}
	s, err := dl.Get(context.Background(), id)
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		fmt.Fprintf(stdout, "stopped after %d posts; run the same command again to carry on\n", s.Handled)
		return nil
	}
	fmt.Fprintf(stdout, "search #%d %s: %d downloaded, %d already in the library, %d deleted before, %d skipped, %d failed\n",
		s.ID, s.State, s.Downloaded, s.Already, s.Deleted, s.Skipped, s.Failed)
	if s.Error != "" {
		return fmt.Errorf("search failed: %s", s.Error)
	}
	return nil
}

func cmdSites(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fset := flag.NewFlagSet("sites", flag.ContinueOnError)
	fset.SetOutput(stderr)
	fset.Usage = func() {
		fmt.Fprintln(stderr, `Usage: vitrine sites [flags] [on <site> | off <site> | try <site>]

The site plugins: with Vitrine running, as it has them (switched off, paused
after failing to answer, credentials, subscriptions and downloads); else the
installed plugins. With a running Vitrine:
  on, off   switch a site on or off (off: nothing runs, its queue waits)
  try       a paused site: try it now`)
		fset.PrintDefaults()
	}
	load := configFlags(fset)
	asJSON := fset.Bool("json", false, "print the server's answer as JSON (Vitrine running)")
	if err := fset.Parse(args); err != nil {
		return err
	}
	c, err := load()
	if err != nil {
		return err
	}
	if ran, err := withLive(ctx, c, func(l *live) error { return liveSites(ctx, l, fset.Args(), *asJSON, stdout) }); ran {
		return err
	}
	if fset.NArg() > 0 {
		return fmt.Errorf("%w - switching sites needs it running", errNotRunning)
	}
	plugins, err := plugin.Load(pluginsDir(c))
	if err != nil {
		return err
	}
	secrets, err := plugin.LoadSecrets(secretsPath(c))
	if err != nil {
		return err
	}
	for _, id := range slices.Sorted(maps.Keys(plugins)) {
		m := plugins[id]
		creds := "no account needed"
		if len(m.Auth.Fields) > 0 {
			creds = "credentials missing (vitrine credentials " + id + ")"
			if plugin.NewClient(m, secrets[id]).HasCredentials() {
				creds = "credentials set"
			}
		}
		fmt.Fprintf(stdout, "%-12s %-20s %s %s - %s\n", id, m.Name, m.Engine, m.Version, creds)
	}
	fmt.Fprintf(stdout, "\nMore plugins: put their .yaml files in %s\n", pluginsDir(c))
	return nil
}

func cmdCredentials(ctx context.Context, args []string, stdout, stderr io.Writer, stdin io.Reader) error {
	fset := flag.NewFlagSet("credentials", flag.ContinueOnError)
	fset.SetOutput(stderr)
	fset.Usage = func() {
		fmt.Fprintln(stderr, "Usage: vitrine credentials [flags] <site>\n\nAsks for the site's credentials (e.g. API key and user ID) and saves them in\nsecrets.toml in the data directory. Values typed in a terminal aren't shown.")
		fset.PrintDefaults()
	}
	load := configFlags(fset)
	if err := fset.Parse(args); err != nil {
		return err
	}
	if fset.NArg() != 1 {
		fset.Usage()
		return flag.ErrHelp
	}
	c, err := load()
	if err != nil {
		return err
	}
	plugins, err := plugin.Load(pluginsDir(c))
	if err != nil {
		return err
	}
	m, ok := plugins[fset.Arg(0)]
	if !ok {
		return fmt.Errorf("no plugin %q", fset.Arg(0))
	}
	if len(m.Auth.Fields) == 0 {
		fmt.Fprintf(stdout, "%s needs no credentials\n", m.Name)
		return nil
	}
	if m.Auth.Help != "" {
		fmt.Fprintln(stdout, m.Auth.Help)
	}
	secrets, err := plugin.LoadSecrets(secretsPath(c))
	if err != nil {
		return err
	}
	values := map[string]string{}
	f, isFile := stdin.(*os.File)
	tty := isFile && term.IsTerminal(int(f.Fd()))
	rd := bufio.NewReader(stdin)
	for _, field := range m.Auth.Fields {
		fmt.Fprintf(stdout, "%s: ", field)
		var v string
		if tty {
			b, err := term.ReadPassword(int(f.Fd()))
			fmt.Fprintln(stdout)
			if err != nil {
				return err
			}
			v = string(b)
		} else {
			line, err := rd.ReadString('\n')
			if err != nil && line == "" {
				return fmt.Errorf("reading %s: %w", field, err)
			}
			v = line
		}
		if v = strings.TrimSpace(v); v == "" {
			return fmt.Errorf("%s can't be empty", field)
		}
		values[field] = v
	}
	// Vitrine running: through it (it saves them and uses them at once).
	if ran, err := withLive(ctx, c, func(l *live) error {
		return l.post(ctx, "/api/sites/"+esc(m.ID)+"/credentials", map[string]any{"values": values}, nil)
	}); ran {
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "saved %s credentials (the running Vitrine uses them now)\n", m.Name)
		return nil
	}
	if err := os.MkdirAll(c.DataDir, 0o755); err != nil {
		return err
	}
	secrets[m.ID] = values
	if err := secrets.Save(secretsPath(c)); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "saved %s credentials in %s\n", m.Name, secretsPath(c))
	return nil
}
