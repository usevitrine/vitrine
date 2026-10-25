package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/backup"
)

func TestNewerVersion(t *testing.T) {
	for _, c := range []struct {
		latest, current string
		want            bool
	}{
		{"v1.0.2", "v1.0.1", true},
		{"v1.1.0", "v1.0.9", true},
		{"v2.0.0", "v1.9.9", true},
		{"v1.0.1", "v1.0.1", false},
		{"v1.0.0", "v1.0.1", false},
		{"v1.0.10", "v1.0.9", true}, // (numbers, not text)
		{"1.0.2", "v1.0.1", true},
		{"v1.0.2", "dev", false},          // a development build is never behind
		{"v1.0.2", "test-86eb879", false}, // the dev server's builds
		{"v1.0.2-rc1", "v1.0.1", false},   // a pre-release isn't one
		{"", "v1.0.1", false},
	} {
		if got := newerVersion(c.latest, c.current); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}

// The check: a newer release is reported and gets one notice (not again on the next check); the setting turns the
// daily check off.
func TestUpdateCheck(t *testing.T) {
	e := newEnv(t)
	e.loginAs("me", "correct horse")
	tag := "v1.0.2"
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "Vitrine/test" {
			t.Errorf("User-Agent %q", r.Header.Get("User-Agent"))
		}
		if r.URL.Path == "/changelog/v1.0.2" {
			io.WriteString(w, "# Changelog\n\n## 1.0.2 - 2026-11-01\n\n- **Something new**\n")
			return
		}
		writeJSON(w, 200, map[string]string{"tag_name": tag, "html_url": "https://github.com/usevitrine/vitrine/releases/tag/" + tag})
	}))
	defer gh.Close()
	e.h.SetInfo(Info{Version: "v1.0.1"})
	e.h.SetUpdates(UpdateConfig{ReleasesURL: gh.URL, ChangelogURL: gh.URL + "/changelog/%s", UserAgent: "Vitrine/test"})

	var st struct {
		Current, Latest, Page, Changelog string
		Newer, Check                     bool
		CanUpdate                        bool `json:"can_update"`
	}
	if code := e.call("POST", "/api/update/check", map[string]any{}, &st); code != 200 || st.Latest != "v1.0.2" || !st.Newer || !st.Check || st.CanUpdate || !strings.Contains(st.Changelog, "## 1.0.2") {
		t.Fatalf("check: %d %+v", code, st)
	}
	e.h.checkForUpdate(context.Background())
	n := 0
	for _, x := range e.h.notices.after(0) {
		if x.Kind == "release" && x.Query == "v1.0.2" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("release notices: %d, want 1", n)
	}
	// Up to date: not newer.
	tag = "v1.0.1"
	e.call("POST", "/api/update/check", map[string]any{}, &st)
	if st.Newer || st.Changelog != "" {
		t.Fatalf("same version called newer: %+v", st)
	}
	if code := e.call("POST", "/api/settings", map[string]any{"updateCheck": false}, nil); code != 200 {
		t.Fatalf("setting: %d", code)
	}
	if e.h.updateCheckOn(context.Background()) {
		t.Fatal("updateCheck false still on")
	}
}

// The Update button: without Watchtower refused; with it, the update runs in the background (a backup, then
// Watchtower asked with the token for Vitrine's image) and a failure comes back as update_error.
func TestUpdateStart(t *testing.T) {
	e := newEnv(t)
	e.loginAs("me", "correct horse")
	e.h.SetInfo(Info{Version: "v1.0.1"})
	e.h.SetUpdates(UpdateConfig{ReleasesURL: "off"})
	if code := e.call("POST", "/api/update", map[string]any{}, nil); code != http.StatusConflict {
		t.Fatalf("without Watchtower: %d", code)
	}

	var asked atomic.Int32
	updated := 0
	wt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/update" || r.URL.Query().Get("image") != UpdateImage {
			t.Errorf("Watchtower asked %s %s", r.Method, r.URL)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		asked.Add(1)
		writeJSON(w, 200, map[string]any{"summary": map[string]any{"scanned": 1, "updated": updated}})
	}))
	defer wt.Close()
	e.h.SetBackups(backup.New(filepath.Join(e.dir, "meta.db"), filepath.Join(e.dir, "backups")), func() {})
	var st struct {
		Updating    string
		UpdateError string `json:"update_error"`
		CanUpdate   bool   `json:"can_update"`
	}
	run := func() {
		t.Helper()
		if code := e.call("POST", "/api/update", map[string]any{}, nil); code != http.StatusAccepted {
			t.Fatalf("start: %d", code)
		}
		for i := 0; i < 100; i++ {
			e.call("GET", "/api/update", nil, &st)
			if st.Updating == "" {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("still updating: %+v", st)
	}
	e.h.SetUpdates(UpdateConfig{ReleasesURL: "off", WatchtowerURL: wt.URL + "/", WatchtowerToken: "wrong"})
	run()
	if !st.CanUpdate || !strings.Contains(st.UpdateError, "token") {
		t.Fatalf("wrong token: %+v", st)
	}
	e.h.SetUpdates(UpdateConfig{ReleasesURL: "off", WatchtowerURL: wt.URL + "/", WatchtowerToken: "secret"})
	run()
	if asked.Load() != 1 || !strings.Contains(st.UpdateError, "no newer image") {
		t.Fatalf("nothing newer: asked %d, %+v", asked.Load(), st)
	}
	var list struct{ Backups []backup.Info }
	if e.call("GET", "/api/backups", nil, &list); len(list.Backups) < 2 {
		t.Fatalf("a backup before each update: %d", len(list.Backups))
	}
}

// Installed with the Windows installer: Update downloads the release's installer, checks its SHA-256 against
// GitHub's, and hands it to RunSetup - a mismatch never gets that far.
func TestUpdateWithInstaller(t *testing.T) {
	e := newEnv(t)
	e.loginAs("me", "correct horse")
	setup := []byte("MZ pretend installer")
	sum := sha256.Sum256(setup)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	var gh *httptest.Server
	gh = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/download/Vitrine-Setup-1.0.2-x64.exe" {
			w.Write(setup)
			return
		}
		writeJSON(w, 200, map[string]any{"tag_name": "v1.0.2", "assets": []map[string]string{
			{"name": "Vitrine-Setup-1.0.2-x64.exe", "browser_download_url": gh.URL + "/download/Vitrine-Setup-1.0.2-x64.exe", "digest": digest},
		}})
	}))
	defer gh.Close()
	e.h.SetInfo(Info{Version: "v1.0.1"})
	var ran atomic.Value
	cfg := UpdateConfig{ReleasesURL: gh.URL, ChangelogURL: gh.URL + "/changelog/%s", SetupName: "Vitrine-Setup-%s-x64.exe",
		RunSetup: func(path string) error {
			b, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(b, setup) {
				t.Errorf("RunSetup got %q (%v)", b, err)
			}
			ran.Store(path)
			return nil
		}}
	e.h.SetUpdates(cfg)
	var st struct {
		Updating    string
		UpdateError string `json:"update_error"`
		CanUpdate   bool   `json:"can_update"`
	}
	if e.call("POST", "/api/update/check", map[string]any{}, &st); !st.CanUpdate {
		t.Fatalf("no Update button with the installer: %+v", st)
	}
	wait := func(done func() bool) {
		t.Helper()
		for i := 0; i < 100 && !done(); i++ {
			time.Sleep(50 * time.Millisecond)
			e.call("GET", "/api/update", nil, &st)
		}
	}

	digest = "sha256:" + strings.Repeat("0", 64) // (a file that isn't the release's)
	e.call("POST", "/api/update/check", map[string]any{}, &st)
	if code := e.call("POST", "/api/update", map[string]any{}, nil); code != http.StatusAccepted {
		t.Fatalf("start: %d", code)
	}
	wait(func() bool { return st.UpdateError != "" })
	if !strings.Contains(st.UpdateError, "checksum") || ran.Load() != nil {
		t.Fatalf("a wrong checksum: %+v", st)
	}

	digest = "sha256:" + hex.EncodeToString(sum[:])
	e.call("POST", "/api/update/check", map[string]any{}, &st)
	e.call("POST", "/api/update", map[string]any{}, nil)
	wait(func() bool { return ran.Load() != nil })
	if ran.Load() == nil || st.UpdateError != "" {
		t.Fatalf("installer not run: %+v", st)
	}
	os.RemoveAll(filepath.Dir(ran.Load().(string)))
}
