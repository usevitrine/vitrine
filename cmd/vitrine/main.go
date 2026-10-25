// Command vitrine is the Vitrine server and its maintenance commands.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/usevitrine/vitrine/internal/config"
	"github.com/usevitrine/vitrine/internal/media"
)

// version is set at build time with -ldflags "-X main.version=v1.2.3".
var version = "dev"

// gui is set ("1", -ldflags "-X main.gui=1 -H windowsgui") in the Windows installer's Vitrine.exe: a program without
// a console that runs the server from the notification area (tray_windows.go).
var gui string

// setupName and runSetup are the installer update (set by the notification-area program): the release's installer
// for this computer (%s: the version) and how a downloaded one is run - see server.UpdateConfig.
var (
	setupName string
	runSetup  func(path string) error
)

const usage = `Vitrine - a fast, self-hosted media library.

Usage:
  vitrine <command> [flags]

Commands:
  serve        Run the web server (the web app and its API)
  passwd       Set the login password (creates the account if needed)

With Vitrine running (they talk to it through its local socket, no login):
  status       What it's doing: library, queue, subscriptions, sites, notices
  queue        The download queue: list (-w follows), add, pause, resume, remove, clear
  subs         Subscriptions: list, add, check, pause, resume, remove, every
  failed       Posts whose file failed: list, retry, dismiss
  notices      The bell's notices: list, read, clear
  logs         Its recent log (-f follows, -level warn)
  api          Call any API endpoint, e.g. vitrine api /api/stats

The library (through the running Vitrine if there is one, else on its files):
  sites        Site plugins (and with Vitrine running: on, off, try)
  credentials  Set a site's credentials (API key etc.)
  download     Run a search on a site and download it, without the server
  import       Add files or folders to the library (originals are copied, not moved)
  import-comic Add a comic (a folder of pictures or a zip/cbz) to the Comics page
  search       Search the library, e.g. vitrine search blue_sky -cloud system:imported<5d
  tag          Add or remove tags on files
  blacklist    Tags never downloaded (list, add, remove, matches)
  setting      List, show or change a setting (e.g. vitrine setting pauseAfter 10)
  process      Make missing thumbnails (and read video length, size, audio)
  backup       Back up the library database (safe while Vitrine runs)
  restore      Restore a backup (in place the next time Vitrine starts)
  config       Show the settings in effect
  version      Show the version

Run "vitrine <command> -h" for a command's flags.
`

func main() {
	if gui != "" {
		os.Exit(runTray(os.Args[1:]))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, "vitrine:", err)
		}
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		// Portable (a config.toml beside the program): started without a command - a double-click - it serves.
		if config.PortableFile() == "" {
			fmt.Fprint(stderr, usage)
			return flag.ErrHelp
		}
		args = []string{"serve"}
	}
	cmd, args := args[0], args[1:]
	switch cmd {
	case "status":
		return cmdStatus(ctx, args, stdout, stderr)
	case "queue":
		return cmdQueue(ctx, args, stdout, stderr)
	case "subs":
		return cmdSubs(ctx, args, stdout, stderr)
	case "failed":
		return cmdFailed(ctx, args, stdout, stderr)
	case "notices":
		return cmdNotices(ctx, args, stdout, stderr)
	case "logs":
		return cmdLogs(ctx, args, stdout, stderr)
	case "api":
		return cmdAPI(ctx, args, stdout, stderr)
	case "download":
		return cmdDownload(ctx, args, stdout, stderr)
	case "sites":
		return cmdSites(ctx, args, stdout, stderr)
	case "credentials":
		return cmdCredentials(ctx, args, stdout, stderr, os.Stdin)
	case "serve":
		return cmdServe(ctx, args, stdout, stderr)
	case "healthcheck":
		return cmdHealthcheck(ctx, args, stdout, stderr)
	case "passwd":
		return cmdPasswd(ctx, args, stdout, stderr, os.Stdin)
	case "import":
		return cmdImport(ctx, args, stdout, stderr)
	case "import-comic":
		return cmdImportComic(ctx, args, stdout, stderr)
	case "search":
		return cmdSearch(ctx, args, stdout, stderr)
	case "tag":
		return cmdTag(ctx, args, stdout, stderr)
	case "blacklist":
		return cmdBlacklist(ctx, args, stdout, stderr)
	case "setting":
		return cmdSetting(ctx, args, stdout, stderr)
	case "backup":
		return cmdBackup(ctx, args, stdout, stderr)
	case "restore":
		return cmdRestore(ctx, args, stdout, stderr)
	case "process":
		return cmdProcess(ctx, args, stdout, stderr)
	case "config":
		return cmdConfig(args, stdout, stderr)
	case "version", "-version", "--version":
		fmt.Fprintln(stdout, "vitrine", versionString())
		return nil
	case "help", "-h", "-help", "--help":
		fmt.Fprint(stdout, usage)
		return nil
	}
	fmt.Fprint(stderr, usage)
	return fmt.Errorf("unknown command %q", cmd)
}

// configFlags adds the flags every command shares and returns a loader that
// applies them over the config file and environment.
func configFlags(fset *flag.FlagSet) func() (config.Config, error) {
	file := fset.String("config", "", "config `file` (default: $VITRINE_CONFIG or "+config.DefaultFile()+")")
	dataDir := fset.String("data-dir", "", "data `dir` for databases and thumbnails (overrides config)")
	filesDir := fset.String("files-dir", "", "`dir` for original files (overrides config)")
	return func() (config.Config, error) {
		return config.Load(*file, os.Getenv, config.Config{DataDir: *dataDir, FilesDir: *filesDir})
	}
}

func cmdConfig(args []string, stdout, stderr io.Writer) error {
	fset := flag.NewFlagSet("config", flag.ContinueOnError)
	fset.SetOutput(stderr)
	load := configFlags(fset)
	if err := fset.Parse(args); err != nil {
		return err
	}
	c, err := load()
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "data_dir      = %q\nfiles_dir     = %q\nlisten        = %q\n", c.DataDir, c.FilesDir, c.Listen)
	tools := media.FindTools(c.FFmpeg, c.FFprobe, c.VipsThumbnail)
	for _, t := range []struct{ name, path string }{
		{"ffmpeg", tools.FFmpeg}, {"ffprobe", tools.FFprobe}, {"vipsthumbnail", tools.VipsThumbnail},
	} {
		if t.path == "" {
			t.path = "(not found)"
		}
		fmt.Fprintf(stdout, "%-13s = %q\n", t.name, t.path)
	}
	return nil
}

func versionString() string {
	v := version
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return v
	}
	if v == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		v = info.Main.Version
	}
	var rev, dirty string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				dirty = "-dirty"
			}
		}
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if rev != "" {
		v += " (" + rev + dirty + ")"
	}
	return strings.TrimSpace(v + " " + info.GoVersion)
}
