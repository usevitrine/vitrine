package download

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/usevitrine/vitrine/internal/plugin"
)

// Installing and updating site plugins while Vitrine runs: the manifest is
// saved in the plugins folder and the site is put to work at once (a running
// search finishes with the old version; the next one uses the new).

// prepareSite makes a site from a manifest: its ID in meta.db, client,
// engine, the site's address as a system:site alias and its namespaces.
func (d *Downloader) prepareSite(ctx context.Context, m plugin.Manifest, creds map[string]string) (*Site, error) {
	siteID, err := d.lib.Site(ctx, m.ID)
	if err != nil {
		return nil, err
	}
	c := plugin.NewClient(m, creds)
	c.SetSolver(d.Solver)
	c.SetLogger(d.log)
	c.OnPause(func(paused bool, p plugin.Pause) { d.sitePaused(m, paused, p) })
	c.SetClock(func() time.Time { return d.now() })
	// (paused before a restart, or before the plugin was updated: still paused, its next try as it was)
	if kept, err := d.lib.SitePauses(ctx); err == nil {
		if p, ok := kept[m.ID]; ok {
			c.RestorePause(plugin.Pause{Since: time.UnixMilli(p.Since), Until: time.UnixMilli(p.Until), Reason: p.Reason, Tries: p.Tries})
		}
	}
	d.mu.Lock()
	if d.pauseAfter >= 0 {
		c.SetPauseAfter(d.pauseAfter)
	}
	d.mu.Unlock()
	e, err := plugin.NewEngine(c)
	if err != nil {
		return nil, err
	}
	d.lib.Index().AddSiteAlias(m.Site, siteID) // system:site=safebooru.org as well as =safebooru
	// The plugin's namespaces exist from the start (searches and the web
	// client know them before the first tag uses one).
	var names []string
	for _, ns := range m.Tags.Namespaces {
		names = append(names, ns)
	}
	slices.Sort(names)
	if err := d.lib.EnsureNamespaces(ctx, slices.Compact(names)); err != nil {
		return nil, err
	}
	return &Site{ID: siteID, Client: c, Engine: e}, nil
}

// ErrBadPlugin wraps why a plugin file was refused.
var ErrBadPlugin = errors.New("not a valid plugin")

// ErrNoPluginsDir means the downloader wasn't told where plugins live.
var ErrNoPluginsDir = errors.New("plugins can't be installed here")

// Installed reports what InstallPlugin did.
type Installed struct {
	ID         string `json:"id"`
	Site       string `json:"site"`
	Version    string `json:"version"`
	OldVersion string `json:"old_version,omitempty"` // set when it replaced an installed plugin
	New        bool   `json:"new"`                   // a site that wasn't there before
}

// InstallPlugin checks a plugin manifest, saves it as <id>.yaml in the
// plugins folder and starts using it.
func (d *Downloader) InstallPlugin(ctx context.Context, raw []byte) (Installed, error) {
	m, err := plugin.Parse(raw)
	if err != nil {
		return Installed{}, fmt.Errorf("%w: %v", ErrBadPlugin, err)
	}
	if d.PluginsDir == "" {
		return Installed{}, ErrNoPluginsDir
	}
	d.installMu.Lock()
	defer d.installMu.Unlock()
	var creds map[string]string
	if d.SecretsPath != "" {
		if s, err := plugin.LoadSecrets(d.SecretsPath); err == nil {
			creds = s[m.ID]
		}
	}
	st, err := d.prepareSite(ctx, m, creds)
	if err != nil {
		return Installed{}, err
	}
	// Saved first (a temporary file renamed over the old one), so a crash
	// never leaves half a plugin.
	if err := os.MkdirAll(d.PluginsDir, 0o755); err != nil {
		return Installed{}, err
	}
	tmp, err := os.CreateTemp(d.PluginsDir, ".install-*.tmp")
	if err != nil {
		return Installed{}, err
	}
	_, werr := tmp.Write(raw)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		os.Remove(tmp.Name())
		return Installed{}, err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(d.PluginsDir, m.ID+".yaml")); err != nil {
		os.Remove(tmp.Name())
		return Installed{}, err
	}
	// A built-in site removed before is back once installed.
	if removed := d.idList(ctx, "removedSites"); slices.Contains(removed, m.ID) {
		if err := d.setIDList(ctx, "removedSites", slices.DeleteFunc(removed, func(s string) bool { return s == m.ID })); err != nil {
			return Installed{}, err
		}
	}
	res := Installed{ID: m.ID, Site: m.Site, Version: m.Version}
	if old, ok := d.all()[m.ID]; ok {
		res.OldVersion = old.Client.Manifest().Version
	} else {
		res.New = true
	}
	d.putSite(m.ID, st)
	if res.New {
		d.mu.Lock()
		runCtx := d.runCtx
		d.mu.Unlock()
		if runCtx != nil && !d.IsOff(m.ID) {
			d.startWorker(runCtx, m.ID)
		}
	}
	d.wake(m.ID)
	d.log.Info("plugin installed", "plugin", m.ID, "version", m.Version, "replaced", res.OldVersion)
	return res, nil
}

// PluginInfo is a plugin in the index, with what's installed.
type PluginInfo struct {
	plugin.IndexEntry
	Installed        bool   `json:"installed"`                   // a plugin with this ID is installed
	InstalledVersion string `json:"installed_version,omitempty"` // its version (may be empty)
	Update           bool   `json:"update"`                      // the index has a newer version
}

// pluginIndex caches the index for a few minutes.
type pluginIndex struct {
	mu  sync.Mutex
	ix  plugin.Index
	err error
	at  time.Time
}

const indexCacheFor = 10 * time.Minute

// Plugins lists the plugins in the index (PluginIndex), marking which are
// installed and which have an update. refresh skips the cache.
func (d *Downloader) Plugins(ctx context.Context, refresh bool) ([]PluginInfo, error) {
	d.index.mu.Lock()
	if refresh || d.index.at.IsZero() || time.Since(d.index.at) > indexCacheFor {
		d.index.ix, d.index.err = plugin.FetchIndex(ctx, d.PluginIndex)
		d.index.at = time.Now()
	}
	ix, err := d.index.ix, d.index.err
	d.index.mu.Unlock()
	if err != nil {
		return nil, err
	}
	sites := d.all()
	out := make([]PluginInfo, 0, len(ix.Plugins))
	for _, e := range ix.Plugins {
		p := PluginInfo{IndexEntry: e}
		st, ok := sites[e.ID]
		if ok {
			p.Installed = true
			p.InstalledVersion = st.Client.Manifest().Version
			p.Update = plugin.NewerVersion(e.Version, p.InstalledVersion) && e.Supported()
		} else if !e.Supported() {
			continue // (needs a newer Vitrine: not offered)
		}
		out = append(out, p)
	}
	return out, nil
}

// InstallFromIndex installs (or updates) a plugin from the index.
func (d *Downloader) InstallFromIndex(ctx context.Context, id string) (Installed, error) {
	list, err := d.Plugins(ctx, false)
	if err != nil {
		return Installed{}, err
	}
	for _, p := range list {
		if p.ID == id {
			if !p.Supported() {
				return Installed{}, fmt.Errorf("%s %s needs Vitrine %s or newer - update Vitrine first", p.Name, p.Version, p.MinVitrine)
			}
			_, raw, err := plugin.FetchPlugin(ctx, d.PluginIndex, p.IndexEntry)
			if err != nil {
				return Installed{}, err
			}
			// An update whose file isn't newer than what's installed (the
			// list's version out of step with the file) would report
			// "Updated" and offer the same update again.
			if p.Update {
				if m, err := plugin.Parse(raw); err == nil && !plugin.NewerVersion(m.Version, p.InstalledVersion) {
					return Installed{}, fmt.Errorf("the plugin list offers %s %s, but its file is still %s - nothing to update", p.Name, p.Version, m.Version)
				}
			}
			return d.InstallPlugin(ctx, raw)
		}
	}
	return Installed{}, fmt.Errorf("%w: %q is not in the plugin list", ErrUnknownSite, id)
}

// Removed reports what RemovePlugin did.
type Removed struct {
	ID       string `json:"id"`
	Site     string `json:"site"`
	Name     string `json:"name"`     // the site's own name
	Restored bool   `json:"restored"` // the built-in plugin with this ID is used again
}

// RemovePlugin deletes an installed plugin. Files, tags and history stay; the
// site's searches and subscriptions wait until it is installed again (its
// worker stops - a running search resumes then), and its credentials are
// kept. A file that replaced a built-in plugin goes back to the built-in one;
// a built-in plugin is remembered as removed (installing it again brings it back).
func (d *Downloader) RemovePlugin(ctx context.Context, id string) (Removed, error) {
	d.installMu.Lock()
	defer d.installMu.Unlock()
	st, ok := d.all()[id]
	if !ok {
		return Removed{}, fmt.Errorf("%w: %q", ErrUnknownSite, id)
	}
	m := st.Client.Manifest()
	if m.BuiltIn {
		if err := d.setIDList(ctx, "removedSites", append(d.idList(ctx, "removedSites"), id)); err != nil {
			return Removed{}, err
		}
		d.dropSite(id)
		d.Changed()
		d.log.Info("built-in plugin removed", "plugin", id)
		return Removed{ID: id, Site: m.Site, Name: m.Name}, nil
	}
	if d.PluginsDir == "" {
		return Removed{}, ErrNoPluginsDir
	}
	gone := false
	for _, p := range []string{filepath.Join(d.PluginsDir, id+".yaml"), filepath.Join(d.PluginsDir, id, "plugin.yaml")} {
		if err := os.Remove(p); err == nil {
			gone = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return Removed{}, err
		}
	}
	if !gone {
		return Removed{}, fmt.Errorf("the %s plugin file isn't in the plugins folder", id)
	}
	res := Removed{ID: id, Site: m.Site, Name: m.Name}
	if b, ok := plugin.Builtin(id); ok {
		var creds map[string]string
		if d.SecretsPath != "" {
			if s, err := plugin.LoadSecrets(d.SecretsPath); err == nil {
				creds = s[id]
			}
		}
		nst, err := d.prepareSite(ctx, b, creds)
		if err != nil {
			return Removed{}, err
		}
		d.putSite(id, nst)
		res.Restored = true
	} else {
		d.dropSite(id)
	}
	d.Changed()
	d.log.Info("plugin removed", "plugin", id, "back to built-in", res.Restored)
	return res, nil
}

// dropSite takes a site out and stops its worker.
func (d *Downloader) dropSite(id string) {
	d.mu.Lock()
	next := maps.Clone(*d.sites.Load())
	delete(next, id)
	d.sites.Store(&next)
	cancel := d.cancels[id]
	delete(d.cancels, id)
	d.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// siteUsage counts, per site, its subscriptions and its downloads not done yet.
func (d *Downloader) siteUsage() map[string][2]int {
	out := map[string][2]int{}
	rows, err := d.db.R.Query(`SELECT si.name,
		(SELECT count(*) FROM subscriptions WHERE site_id = si.id),
		(SELECT count(*) FROM searches WHERE site_id = si.id AND state != 'done' AND subscription_id IS NULL)
		FROM sites si`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var subs, dls int
		if rows.Scan(&name, &subs, &dls) == nil {
			out[name] = [2]int{subs, dls}
		}
	}
	return out
}

// ForgetCredentials removes a removed site's credentials from the credentials file (a site wiped from the server).
func (d *Downloader) ForgetCredentials(id string) error {
	if d.SecretsPath == "" {
		return nil
	}
	d.credMu.Lock()
	defer d.credMu.Unlock()
	secrets, err := plugin.LoadSecrets(d.SecretsPath)
	if err != nil {
		return fmt.Errorf("reading the credentials file: %w", err)
	}
	if _, ok := secrets[id]; !ok {
		return nil
	}
	delete(secrets, id)
	return secrets.Save(d.SecretsPath)
}
