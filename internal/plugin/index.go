package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// The plugin index: a JSON list of plugins that can be installed (the
// vitrine-plugins repo publishes one). Its source is an https URL or a local
// file or folder; each entry's file is relative to it.
//
//	{"plugins": [{"id": "safebooru", "name": "Safebooru", "site": "safebooru.org", "version": "1.2.0",
//	              "file": "plugins/safebooru.yaml", "description": "...", "kind": "booru", "nsfw": true,
//	              "changelog": [{"version": "1.2.0", "date": "2026-10-28", "changes": ["..."]}]}]}

// IndexEntry is one plugin in the index.
type IndexEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Site        string `json:"site"`
	Version     string `json:"version"`
	File        string `json:"file"`
	Description string `json:"description,omitempty"`
	Kind        string `json:"kind,omitempty"` // what sort of site, for the filters in Add site: booru, gallery or social
	NSFW        bool   `json:"nsfw,omitempty"`
	// MinVitrine: the oldest Vitrine that reads this version of the plugin (a manifest field older ones don't know
	// is an error for them: manifests are read strictly). Older ones aren't offered it (Supported).
	MinVitrine string `json:"min_vitrine,omitempty"`
	// Changelog: what each version changed, newest first - shown before updating (the versions not installed yet) and
	// in the site's Version history (2026-10-28, the user: "a plugin changelog system"). Older Vitrines ignore it.
	Changelog []ChangelogEntry `json:"changelog,omitempty"`
}

// ChangelogEntry is what one version of a plugin changed.
type ChangelogEntry struct {
	Version string   `json:"version"`
	Date    string   `json:"date,omitempty"` // YYYY-MM-DD
	Changes []string `json:"changes"`        // each a line in plain words: what it does for the user
}

// AppVersion is the running Vitrine's version ("dev" or "test-..." for a build of the code: as new as any).
var AppVersion = "dev"

// Supported reports whether this Vitrine reads the entry's plugin (see MinVitrine).
func (e IndexEntry) Supported() bool {
	v := strings.TrimPrefix(AppVersion, "v")
	if e.MinVitrine == "" || v == "" || v[0] < '0' || v[0] > '9' {
		return true
	}
	return !NewerVersion(e.MinVitrine, v)
}

// Index is the plugin list.
type Index struct {
	Plugins []IndexEntry `json:"plugins"`
}

const maxIndexBytes = 1 << 20

var indexClient = &http.Client{Timeout: 20 * time.Second}

// read fetches a file from the index source (a URL or a local path).
func read(ctx context.Context, src string) ([]byte, error) {
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", UserAgent)
		resp, err := indexClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%s answered %s", req.URL.Host, resp.Status)
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, maxIndexBytes+1))
		if err == nil && len(b) > maxIndexBytes {
			err = errors.New("too large")
		}
		return b, err
	}
	f, err := os.Open(src)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxIndexBytes+1))
	if err == nil && len(b) > maxIndexBytes {
		err = errors.New("too large")
	}
	return b, err
}

// indexFile turns a source into the index file's location (a folder means
// its index.json).
func indexFile(src string) string {
	if !strings.Contains(src, "://") {
		if fi, err := os.Stat(src); err == nil && fi.IsDir() {
			return filepath.Join(src, "index.json")
		}
	}
	return src
}

// FetchIndex reads the plugin list.
func FetchIndex(ctx context.Context, src string) (Index, error) {
	var ix Index
	if src == "" {
		return ix, errors.New("no plugin list is set (plugin_index)")
	}
	b, err := read(ctx, indexFile(src))
	if err != nil {
		return ix, fmt.Errorf("reading the plugin list: %w", err)
	}
	if err := json.Unmarshal(b, &ix); err != nil {
		return ix, fmt.Errorf("the plugin list isn't valid: %w", err)
	}
	ok := ix.Plugins[:0]
	for _, e := range ix.Plugins {
		if idPattern.MatchString(e.ID) && e.File != "" && !strings.Contains(e.File, "..") {
			ok = append(ok, e)
		}
	}
	ix.Plugins = ok
	return ix, nil
}

// FetchPlugin downloads an index entry's manifest and checks it.
func FetchPlugin(ctx context.Context, src string, e IndexEntry) (Manifest, []byte, error) {
	at := indexFile(src)
	var where string
	if strings.Contains(at, "://") {
		base, err := url.Parse(at)
		if err != nil {
			return Manifest{}, nil, err
		}
		ref, err := url.Parse(e.File)
		if err != nil || ref.IsAbs() {
			return Manifest{}, nil, fmt.Errorf("bad plugin file %q", e.File)
		}
		where = base.ResolveReference(ref).String()
	} else {
		where = filepath.Join(filepath.Dir(at), filepath.FromSlash(e.File))
	}
	b, err := read(ctx, where)
	if err != nil {
		return Manifest{}, nil, fmt.Errorf("downloading the %s plugin: %w", e.ID, err)
	}
	m, err := Parse(b)
	if err != nil {
		return Manifest{}, nil, err
	}
	if m.ID != e.ID {
		return Manifest{}, nil, fmt.Errorf("the plugin file is for %q, not %q", m.ID, e.ID)
	}
	return m, b, nil
}

// NewerVersion reports whether version a is newer than b ("1.10.0" > "1.9.2";
// missing parts count as 0; anything else compares as text).
func NewerVersion(a, b string) bool {
	pa, pb := strings.Split(strings.TrimPrefix(a, "v"), "."), strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < max(len(pa), len(pb)); i++ {
		x, y := "0", "0"
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		nx, ex := strconv.Atoi(x)
		ny, ey := strconv.Atoi(y)
		switch {
		case ex == nil && ey == nil && nx != ny:
			return nx > ny
		case (ex != nil || ey != nil) && x != y:
			return x > y
		}
	}
	return false
}
