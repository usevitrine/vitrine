package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/usevitrine/vitrine/internal/library"
)

// Updates: once a day the server asks GitHub for Vitrine's latest release (the setting updateCheck; missing = on).
// A newer one than this program: GET /api/update says so (Settings > Updates) with that version's CHANGELOG.md (the
// repository's at its tag: the page shows what's new in it), and the bell gets a notice, once per version (kind
// "release"). Development builds (a version that isn't vX.Y.Z) never call one newer.
//
// The Update button: with Watchtower set up (watchtower_url + watchtower_token: its HTTP API, the update endpoint on),
// POST /api/update starts the update and answers at once; the page follows it on GET /api/update ("updating": the
// step) in a full-screen updater. The steps: a backup of the library database, then Watchtower pulls the newest image
// and recreates the containers running it - this one among them: the program stops (the page sees it go: restarting)
// and the new one starts (the page sees the new version and reloads into it). The server waits for Watchtower's
// answer, so a failure (no newer image, a failed pull, a refused token) comes back as "update_error" instead of the
// page waiting for a restart that never happens. Watchtower says nothing about the pull's progress.

// ReleasesURL is GitHub's API address for the latest release.
const ReleasesURL = "https://api.github.com/repos/usevitrine/vitrine/releases/latest"

// ChangelogURL is where a release's CHANGELOG.md is (%s: its tag).
const ChangelogURL = "https://raw.githubusercontent.com/usevitrine/vitrine/%s/CHANGELOG.md"

// UpdateImage is the image Watchtower is asked to update (the official one; any tag).
const UpdateImage = "ghcr.io/usevitrine/vitrine"

// UpdateConfig is how the server checks for and installs updates.
type UpdateConfig struct {
	ReleasesURL     string // "" = ReleasesURL; "off" = never ask (tests)
	ChangelogURL    string // "" = ChangelogURL (%s: the tag)
	UserAgent       string
	WatchtowerURL   string // "" = no Update button
	WatchtowerToken string
}

type updates struct {
	cfg    UpdateConfig
	client *http.Client
	mu     sync.Mutex
	latest string // the newest release's tag ("" = not known yet)
	page   string // its page on GitHub
	at     int64  // when it was last asked (Unix ms)
	err    string // why the last check failed
	log    string // the newer release's CHANGELOG.md ("" = none, or it couldn't be read)
	// The update under way: its step ("backup", "download"; "" = none) and why the last one failed.
	step, stepErr string
}

// config is the settings now (SetUpdates can change them).
func (u *updates) config() UpdateConfig {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.cfg
}

// SetUpdates turns on the update check (RunUpdateChecks) and its API.
func (s *Server) SetUpdates(c UpdateConfig) {
	if c.ReleasesURL == "" {
		c.ReleasesURL = ReleasesURL
	}
	if c.ChangelogURL == "" {
		c.ChangelogURL = ChangelogURL
	}
	if s.updates != nil { // (set again: the new settings, the routes are there)
		s.updates.mu.Lock()
		s.updates.cfg = c
		s.updates.mu.Unlock()
		return
	}
	s.updates = &updates{cfg: c, client: &http.Client{Timeout: 30 * time.Second}}
	s.handle("GET /api/update", s.updateState)
	s.handle("POST /api/update/check", s.updateCheckNow)
	s.handle("POST /api/update", s.updateStart)
}

// updateCheckOn is the setting updateCheck (missing = on).
func (s *Server) updateCheckOn(ctx context.Context) bool {
	set, err := s.lib.Settings(ctx)
	return err != nil || string(set["updateCheck"]) != "false"
}

// RunUpdateChecks asks for the latest release a few minutes after the start, then once a day (checked hourly).
func (s *Server) RunUpdateChecks(ctx context.Context) {
	if s.updates == nil {
		return
	}
	wait := 3 * time.Minute
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = time.Hour
		s.updates.mu.Lock()
		due := time.Since(time.UnixMilli(s.updates.at)) > 23*time.Hour+30*time.Minute
		s.updates.mu.Unlock()
		if due && s.updateCheckOn(ctx) {
			s.checkForUpdate(ctx)
		}
	}
}

// checkForUpdate asks GitHub for the latest release and tells the bell about a newer one (once per version).
func (s *Server) checkForUpdate(ctx context.Context) {
	u := s.updates
	if u.config().ReleasesURL == "off" {
		return
	}
	tag, page, err := u.fetchLatest(ctx)
	u.mu.Lock()
	u.at = time.Now().UnixMilli()
	if err != nil {
		u.err = err.Error()
		u.mu.Unlock()
		s.log.Warn("checking for an update", "err", err)
		return
	}
	u.mu.Unlock()
	changelog := ""
	if newerVersion(tag, s.info.Version) {
		if changelog, err = u.fetchChangelog(ctx, tag); err != nil {
			s.log.Warn("reading the new version's changelog", "version", tag, "err", err) // (the update is still offered)
		}
	}
	u.mu.Lock()
	u.latest, u.page, u.err, u.log = tag, page, "", changelog
	u.mu.Unlock()
	if newerVersion(tag, s.info.Version) && !s.notices.has("release", tag) {
		s.notices.put(library.Notice{At: time.Now().UnixMilli(), Kind: "release", Query: tag})
		s.log.Info("an update is available", "version", tag)
	}
}

func (u *updates) fetchLatest(ctx context.Context) (tag, page string, err error) {
	cfg := u.config()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.ReleasesURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", cfg.UserAgent)
	res, err := u.client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("GitHub answered %s", res.Status)
	}
	var rel struct {
		Tag  string `json:"tag_name"`
		Page string `json:"html_url"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&rel); err != nil {
		return "", "", err
	}
	if _, ok := parseVersion(rel.Tag); !ok {
		return "", "", fmt.Errorf("the latest release has no version number (%q)", rel.Tag)
	}
	return rel.Tag, rel.Page, nil
}

// fetchChangelog reads a release's CHANGELOG.md (at most 1 MB).
func (u *updates) fetchChangelog(ctx context.Context, tag string) (string, error) {
	cfg := u.config()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(cfg.ChangelogURL, url.PathEscape(tag)), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", cfg.UserAgent)
	res, err := u.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("answered %s", res.Status)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	return string(b), err
}

// parseVersion reads "v1.2.3" (or "1.2.3"); anything else (dev, test-abc123, a pre-release) isn't one.
func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// newerVersion tells whether latest is a newer release than current (never for a development build).
func newerVersion(latest, current string) bool {
	l, ok1 := parseVersion(latest)
	c, ok2 := parseVersion(current)
	if !ok1 || !ok2 {
		return false
	}
	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func (s *Server) updateStateJSON(ctx context.Context) map[string]any {
	u := s.updates
	check := s.updateCheckOn(ctx)
	u.mu.Lock()
	defer u.mu.Unlock()
	return map[string]any{
		"current":      s.info.Version,
		"latest":       u.latest,
		"page":         u.page,
		"newer":        newerVersion(u.latest, s.info.Version),
		"changelog":    u.log, // (only a newer release's)
		"checked_at":   u.at,
		"error":        u.err,
		"check":        check,
		"can_update":   u.cfg.WatchtowerURL != "",
		"updating":     u.step,
		"update_error": u.stepErr,
	}
}

func (s *Server) updateState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.updateStateJSON(r.Context()))
}

// updateCheckNow is Settings' "Check now" (also when the daily check is off: asked for).
func (s *Server) updateCheckNow(w http.ResponseWriter, r *http.Request) {
	s.checkForUpdate(r.Context())
	writeJSON(w, http.StatusOK, s.updateStateJSON(r.Context()))
}

// updateStart starts the update (see the top of this file) and answers at once.
func (s *Server) updateStart(w http.ResponseWriter, r *http.Request) {
	u := s.updates
	cfg := u.config()
	if cfg.WatchtowerURL == "" {
		writeError(w, http.StatusConflict, errors.New("updating from here needs Watchtower (watchtower_url)"))
		return
	}
	u.mu.Lock()
	if u.step != "" {
		u.mu.Unlock()
		writeError(w, http.StatusConflict, errors.New("an update is under way"))
		return
	}
	u.step, u.stepErr = "backup", ""
	u.mu.Unlock()
	go s.runUpdate(cfg)
	writeJSON(w, http.StatusAccepted, map[string]any{"started": true})
}

// runUpdate backs up the library database, then has Watchtower update; on success this program is stopped before
// Watchtower answers. Whatever ends here (an answer, an error) failed: its step goes back to "" with the reason.
func (s *Server) runUpdate(cfg UpdateConfig) {
	u := s.updates
	fail := func(err error) {
		s.log.Error("update failed", "err", err)
		u.mu.Lock()
		u.step, u.stepErr = "", err.Error()
		u.mu.Unlock()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	backupName := ""
	if s.backups != nil {
		_, keep := s.BackupSettings(ctx)
		b, err := s.backups.m.Create(ctx, keep)
		if err != nil {
			fail(fmt.Errorf("the backup before updating failed, so nothing was updated: %w", err))
			return
		}
		backupName = b.Name
	}
	u.mu.Lock()
	u.step = "download"
	u.mu.Unlock()
	s.log.Info("update started (Watchtower)", "from", s.info.Version, "backup", backupName)

	q := url.Values{"image": {UpdateImage}, "timeout": {"14m"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.WatchtowerURL, "/")+"/v1/update?"+q.Encode(), nil)
	if err != nil {
		fail(err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+cfg.WatchtowerToken)
	res, err := (&http.Client{}).Do(req) // (no timeout of its own: pulling an image takes a while; ctx bounds it)
	if err != nil {
		fail(fmt.Errorf("couldn't reach Watchtower: %w", err))
		return
	}
	defer res.Body.Close()
	var out struct {
		Error   string `json:"error"`
		Summary struct {
			Updated    int  `json:"updated"`
			Failed     int  `json:"failed"`
			ScanFailed bool `json:"scan_failed"`
		} `json:"summary"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&out)
	switch {
	case res.StatusCode == http.StatusUnauthorized:
		fail(errors.New("Watchtower refused the token (watchtower_token)"))
	case res.StatusCode != http.StatusOK:
		fail(fmt.Errorf("Watchtower answered %s %s", res.Status, out.Error))
	case out.Summary.Failed > 0 || out.Summary.ScanFailed:
		fail(errors.New("Watchtower couldn't update it - see Watchtower's log"))
	case out.Summary.Updated == 0:
		fail(errors.New("Watchtower found no newer image (is the new version's image out yet?)"))
	default:
		fail(errors.New("Watchtower updated another container but not this one"))
	}
}
