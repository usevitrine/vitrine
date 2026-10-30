package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/usevitrine/vitrine/internal/auth"
	"github.com/usevitrine/vitrine/internal/backup"
	"github.com/usevitrine/vitrine/internal/download"
	"github.com/usevitrine/vitrine/internal/favicon"
	"github.com/usevitrine/vitrine/internal/i18n"
	"github.com/usevitrine/vitrine/internal/library"
	"github.com/usevitrine/vitrine/internal/logring"
	"github.com/usevitrine/vitrine/internal/media"
	"github.com/usevitrine/vitrine/internal/push"
	"github.com/usevitrine/vitrine/internal/renditions"
	"github.com/usevitrine/vitrine/internal/server"
)

func cmdServe(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fset := flag.NewFlagSet("serve", flag.ContinueOnError)
	fset.SetOutput(stderr)
	load := configFlags(fset)
	listen := fset.String("listen", "", "address to listen on, e.g. :8080 (overrides config)")
	if err := fset.Parse(args); err != nil {
		return err
	}
	c, err := load()
	if err != nil {
		return err
	}
	if *listen != "" {
		c.Listen = *listen
	}
	level := slog.LevelInfo
	if strings.EqualFold(os.Getenv("VITRINE_LOG_LEVEL"), "debug") {
		level = slog.LevelDebug // logs every request
	}
	// The records are also kept for the problem report (Settings > Stats): in a
	// log file in the data folder (two files of 16 MB at most; debug detail too), so they
	// survive restarts and crashes.
	logDir := filepath.Join(c.DataDir, "logs")
	logs, logErr := logring.New(3000), os.MkdirAll(logDir, 0o700)
	if logErr == nil {
		logs, logErr = logring.Open(filepath.Join(logDir, "vitrine.log"), 3000, 16<<20)
	}
	defer logs.Close()
	log := slog.New(logring.Tee(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level}), logs))
	if logErr != nil {
		log.Warn("no log file (the problem report has this run only)", "err", logErr)
	}

	// A restore staged in Settings > Backups (or with "vitrine restore") goes in
	// place before anything opens the database.
	if did, err := backup.ApplyPending(filepath.Join(c.DataDir, "meta.db")); err != nil {
		return err
	} else if did {
		log.Warn("restored the library from a backup (the replaced database is meta.db.before-restore)")
	}
	ctx, stopServe := context.WithCancel(ctx) // stopServe: the web app's Restart
	defer stopServe()

	start := time.Now()
	lib, d, closeLib, err := openApp(ctx, c)
	if err != nil {
		return err
	}
	defer closeLib()
	st := lib.Index().Stats()
	log.Info("library loaded", "files", st.Files, "tags", st.Tags, "took", time.Since(start).Round(time.Millisecond))

	// The GPU that decodes videos (media.HWAccel): looked for now, so the log says which one.
	media.HWLog = func(msg string) { log.Warn(msg) }
	if name, tried := lib.Tools().HW.Detect(ctx); name != "" {
		log.Info("videos decoded by the graphics hardware", "with", name)
		log.Debug("graphics hardware tried", "found", strings.Join(tried, "; "))
	} else if c.HWAccel != "off" {
		log.Info("videos decoded by the processor (no graphics hardware found - hwaccel in the config)", "tried", strings.Join(tried, "; "))
	}
	go logLoad(ctx, log)

	a := auth.New(d)
	ln, err := net.Listen("tcp", c.Listen)
	if err != nil {
		return err
	}
	if err := i18n.SetDefault(c.Language); err != nil {
		log.Error("language setting ignored", "err", err)
	}
	handler := server.New(lib, a, log)
	handler.SetLogs(logs)
	handler.SetInfo(server.Info{Version: version, DataDir: c.DataDir, FilesDir: c.FilesDir, BackupDir: c.BackupDir})
	if rend, err := renditions.Open(filepath.Join(c.DataDir, "renditions"), int64(c.RenditionsMaxGB*(1<<30))); err != nil {
		log.Error("resized copies of huge images disabled", "err", err)
	} else {
		handler.SetRenditions(rend)
	}
	if icons, err := favicon.Open(filepath.Join(c.DataDir, "favicons"), userAgent()); err != nil {
		log.Error("site icons disabled", "err", err)
	} else {
		handler.SetFavicons(icons)
	}
	handler.SetBackups(backup.New(filepath.Join(c.DataDir, "meta.db"), c.BackupDir), stopServe)
	go handler.RunDailyBackups(ctx)
	handler.SetUpdates(server.UpdateConfig{ReleasesURL: c.ReleasesURL, ChangelogURL: c.ChangelogURL, UserAgent: userAgent(), WatchtowerURL: c.WatchtowerURL, WatchtowerToken: c.WatchtowerToken,
		SetupName: setupName, RunSetup: runSetup})
	go handler.RunUpdateChecks(ctx)
	go handler.RunPluginChecks(ctx)
	go handler.RunRenditionsAhead(ctx)
	pusher, err := push.Open(ctx, d, log)
	if err != nil {
		log.Error("push notifications disabled", "err", err)
	} else {
		handler.SetPush(pusher)
	}
	dl, err := newDownloader(ctx, c, lib, d, log)
	if err != nil {
		log.Error("downloader not started", "err", err)
	} else {
		var notifier *push.Notifier
		if pusher != nil {
			n := push.NewNotifier(pusher, func(ctx context.Context, kind string) bool {
				set, err := lib.Settings(ctx)
				if err != nil {
					return true
				}
				key := map[string]string{"new": "notifyNew", "failed": "notifyFailed", "paused": "notifyFailed", "back": "notifyFailed", "done": "notifyDone", "blacklisted": "notifyBlacklisted"}[kind]
				return string(set[key]) != "false" // on unless turned off
			})
			defer n.Flush() // stopping: what's waiting (the hour's new files) goes now
			notifier = n
		}
		// The web client's bell hears of every event; push notifications as the user set them.
		dl.OnEvent = func(e download.Event) {
			handler.Notice(e)
			if notifier != nil {
				notifier.Add(push.Event{Kind: e.Kind, Site: e.Site, Query: e.Query, Subscription: e.Subscription, New: e.New, Error: e.Error, Tag: e.Tag, First: e.First, Pages: e.Pages})
			}
		}
		// FlareSolverr for Cloudflare challenges, if one is set up (Settings > Sites); when a site that keeps
		// failing to answer pauses.
		if set, err := lib.Settings(ctx); err == nil {
			var n int
			if json.Unmarshal(set["pauseAfter"], &n) == nil {
				dl.SetPauseAfter(n)
			}
			var first bool
			if json.Unmarshal(set["checksFirst"], &first) == nil {
				dl.SetChecksFirst(first)
			}
			var u string
			if json.Unmarshal(set["flaresolverrURL"], &u) == nil && u != "" {
				dl.Solver.SetEndpoint(u)
				log.Info("Cloudflare challenges go to FlareSolverr")
			}
		}
		handler.SetDownloader(dl)
		go dl.Run(ctx)
		log.Info("downloader running", "sites", strings.Join(dl.Sites(), ", "))
	}
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn), // (handler panics, TLS errors: into the log and the report)
		// Requests end when the server stops (live event streams would otherwise hold it up).
		BaseContext: func(net.Listener) context.Context { return ctx },
		IdleTimeout: 2 * time.Minute,
		// No write timeout: videos stream for as long as they play.
	}
	log.Info("listening", "address", ln.Addr().String(), "version", versionString())
	if need, _ := a.SetupNeeded(ctx); need {
		log.Warn("no account yet: open the web page to create one (do this before exposing Vitrine to the internet)")
	}

	// Thumbnails (and probing) as files come in - downloads don't wait for
	// them - and any left from before (newest first).
	go lib.RunMedia(ctx, max(1, runtime.NumCPU()/2),
		func(id uint32, err error) { log.Warn("thumbnail failed", "file", id, "err", err) },
		func(files, failed int) { log.Debug("thumbnails made", "files", files, "failed", failed) })

	// The trash empties itself of files deleted more than trashDays ago (the setting; 0 / missing: never).
	go func() {
		for ctx.Err() == nil {
			if set, err := lib.Settings(ctx); err == nil {
				var days float64
				if json.Unmarshal(set["trashDays"], &days) == nil && days > 0 {
					if n, err := lib.Purge(ctx, time.Now().Add(-time.Duration(days*24)*time.Hour)); err != nil {
						log.Error("emptying the trash", "err", err)
					} else if n > 0 {
						log.Info("trash: removed for good (deleted long enough ago)", "files", n, "days", days)
					}
				}
			}
			select {
			case <-time.After(time.Hour):
			case <-ctx.Done():
			}
		}
	}()

	// The library's health check (library.HealthCheck): looked at hourly, from 15 minutes after the start. The
	// database and every original weekly; a 28th of the originals (at least 10 GB) read back daily at 40 MB/s - a
	// whole pass about every four weeks. What's found: each file in the log (warnings: kept for weeks), a notice.
	go func() {
		wait := 15 * time.Minute
		for {
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return
			}
			wait = time.Hour
			var total int64
			for _, n := range lib.Index().Census(0, 0, 0).Types {
				total += n[1]
			}
			missing, damaged := 0, 0
			before, _ := lib.Health(ctx)
			h, err := lib.HealthCheck(ctx, max(total/28, 10<<30), 40<<20, func(f library.HealthFinding) {
				if f.Missing {
					missing++
					log.Warn("health check: an original is missing", "file", f.ID, "path", f.Path)
				} else {
					damaged++
					log.Warn("health check: an original is damaged (no longer matches its checksum)", "file", f.ID, "path", f.Path)
				}
			})
			if err != nil && ctx.Err() == nil {
				log.Error("health check", "err", err)
			}
			tr := i18n.For("")
			if missing > 0 {
				handler.AppNotice(tr.T("health.missing", "n", missing), "error", "errors")
			}
			if damaged > 0 {
				handler.AppNotice(tr.T("health.damaged", "n", damaged), "error", "errors")
			}
			if h.DBCheckedAt != before.DBCheckedAt && h.DBProblem != "" {
				log.Error("health check: the database has a problem", "problem", h.DBProblem)
				handler.AppNotice(tr.T("health.database", "problem", h.DBProblem), "error", "errors")
			}
			if h.LastPassAt != before.LastPassAt {
				log.Info("health check: every original read back", "files", h.Verified, "damaged", h.DamagedCount)
			}
		}
	}()

	// Near-duplicates: every file's thumbnail hashed and compared with the
	// library (new downloads within a minute; the existing library in batches).
	go func() {
		checked, found := 0, 0
		for ctx.Err() == nil {
			ids, err := lib.PendingPHash(ctx, 500)
			if err != nil {
				log.Error("duplicates", "err", err)
			}
			if len(ids) > 0 {
				n, err := lib.HashFiles(ctx, ids)
				if err != nil {
					if ctx.Err() == nil {
						log.Error("duplicates", "err", err)
					}
					ids = nil // try again in a minute
				}
				checked += len(ids)
				found += n
			}
			if len(ids) < 500 {
				if checked > 0 {
					log.Info("looked for duplicates", "files", checked, "pairs", found)
					checked, found = 0, 0
				}
				select {
				case <-time.After(time.Minute):
				case <-ctx.Done():
				}
			}
		}
	}()

	// Preview frames for the scrub bar: every video without them, one at a time
	// (new downloads within a minute; the existing library little by little).
	go func() {
		// Made by an older recipe: made again (the old ones serve meanwhile).
		if n, err := lib.RequeueOldSprites(ctx); err != nil {
			log.Error("preview frames", "err", err)
		} else if n > 0 {
			log.Info("preview frames to make again (a better recipe)", "videos", n)
		}
		made, failed := 0, 0
		for ctx.Err() == nil {
			ids, err := lib.PendingSprites(ctx, 50)
			if err != nil {
				log.Error("preview frames", "err", err)
			}
			for _, id := range ids {
				took, err := lib.MakeSprite(ctx, id)
				switch {
				case errors.Is(err, library.ErrNoMediaTools) || ctx.Err() != nil:
					return
				case err != nil:
					failed++
					log.Warn("preview frames failed", "err", err)
				default:
					made++
					log.Debug("preview frames made", "file", id, "took", took.Wall.Round(time.Millisecond),
						"cpu", took.CPU.Round(time.Millisecond), "gpu", took.GPU)
				}
				if (made+failed)%200 == 0 {
					log.Info("making preview frames", "made", made, "failed", failed)
				}
			}
			if len(ids) == 0 {
				if made+failed > 0 {
					log.Info("preview frames done", "made", made, "failed", failed)
					made, failed = 0, 0
				}
				select {
				case <-time.After(time.Minute):
				case <-ctx.Done():
				}
			}
		}
	}()

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	// The local socket: the vitrine command talks to this server through it (no login).
	var localSrv *http.Server
	if lln, err := listenLocal(c.Socket); err != nil {
		log.Warn("local socket off (the vitrine command can't reach this server)", "socket", c.Socket, "err", err)
	} else if lln != nil {
		// Go's profiles (vitrine profile) on the local socket only - never on the network port.
		local := http.NewServeMux()
		local.Handle("/", handler.Local())
		local.HandleFunc("/debug/pprof/profile", pprof.Profile)
		local.Handle("/debug/pprof/heap", pprof.Handler("heap"))
		local.Handle("/debug/pprof/goroutine", pprof.Handler("goroutine"))
		localSrv = &http.Server{Handler: local, ReadHeaderTimeout: 10 * time.Second, BaseContext: srv.BaseContext,
			ErrorLog: srv.ErrorLog, IdleTimeout: 2 * time.Minute}
		go localSrv.Serve(lln)
		log.Info("local socket", "path", c.Socket)
	}
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if localSrv != nil {
		localSrv.Shutdown(shutdownCtx)
	}
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

// listenLocal opens the server's local socket (nil: turned off). One left by a server that didn't stop cleanly is
// replaced; one a running server answers on is not (two servers on one library).
func listenLocal(path string) (net.Listener, error) {
	if path == "" || path == "off" {
		return nil, nil
	}
	if conn, err := net.DialTimeout("unix", path, time.Second); err == nil {
		conn.Close()
		return nil, errors.New("another Vitrine is running with this socket")
	}
	os.Remove(path) // (a stale one)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	// Owner only (Linux, macOS): whoever can open it acts as the account.
	if err := os.Chmod(path, 0o600); err != nil && runtime.GOOS != "windows" {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

func cmdPasswd(ctx context.Context, args []string, stdout, stderr io.Writer, stdin io.Reader) error {
	fset := flag.NewFlagSet("passwd", flag.ContinueOnError)
	fset.SetOutput(stderr)
	fset.Usage = func() {
		fmt.Fprintln(stderr, "Usage: vitrine passwd [flags] <name>\n\nSets the password (creating the account if needed) and logs out every session.\nThe password is read from the terminal, or from standard input if it isn't one.")
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

	var password string
	if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprint(stdout, "New password: ")
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(stdout)
		if err != nil {
			return err
		}
		fmt.Fprint(stdout, "Again: ")
		b2, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(stdout)
		if err != nil {
			return err
		}
		if string(b) != string(b2) {
			return errors.New("the passwords don't match")
		}
		password = string(b)
	} else {
		line, err := bufio.NewReader(stdin).ReadString('\n')
		if err != nil && line == "" {
			return fmt.Errorf("reading the password: %w", err)
		}
		password = strings.TrimRight(line, "\r\n")
	}

	_, d, closeLib, err := openApp(ctx, c)
	if err != nil {
		return err
	}
	defer closeLib()
	if err := auth.New(d).SetPassword(ctx, fset.Arg(0), password); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "password set for %s\n", fset.Arg(0))
	return nil
}

// cmdHealthcheck asks the local server whether it is up (for Docker's
// HEALTHCHECK): exit 0 if /api/health answers, 1 otherwise.
func cmdHealthcheck(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fset := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	fset.SetOutput(stderr)
	load := configFlags(fset)
	if err := fset.Parse(args); err != nil {
		return err
	}
	c, err := load()
	if err != nil {
		return err
	}
	host, port, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return fmt.Errorf("listen address %q: %w", c.Listen, err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+net.JoinHostPort(host, port)+"/api/health", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check: %s", resp.Status)
	}
	fmt.Fprintln(stdout, "ok")
	return nil
}
