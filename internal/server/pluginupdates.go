package server

import (
	"context"
	"time"

	"github.com/usevitrine/vitrine/internal/library"
)

// Plugin updates: the plugin list is looked at a few minutes after the start, then once a day (the same setting as
// Vitrine's own update check, updateCheck), and each installed plugin with a newer version there gets a notice for the
// bell - once per version, cleared or not (2026-10-28, the user: "there should be notifications for plugin updates").
// Settings > Sites shows the update with what changed (the index's changelog).

// pluginCheckEvery: how often the plugin list is looked at for updates.
const pluginCheckEvery = 23*time.Hour + 30*time.Minute

// RunPluginChecks looks for plugin updates 5 minutes after the start, then once a day (checked hourly).
func (s *Server) RunPluginChecks(ctx context.Context) {
	if s.dl == nil {
		return
	}
	wait := 5 * time.Minute
	var last time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = time.Hour
		if time.Since(last) > pluginCheckEvery && s.updateCheckOn(ctx) {
			last = time.Now()
			s.checkPluginUpdates(ctx)
		}
	}
}

// checkPluginUpdates tells the bell about each installed plugin the plugin list has a newer version of (once per
// version).
func (s *Server) checkPluginUpdates(ctx context.Context) {
	list, err := s.dl.Plugins(ctx, true)
	if err != nil {
		s.log.Warn("checking for plugin updates", "err", err)
		return
	}
	for _, p := range list {
		if p.Update && !s.notices.hasFor("plugin", p.ID, p.Version) {
			s.notices.put(library.Notice{At: time.Now().UnixMilli(), Kind: "plugin", Site: p.ID, Query: p.Version})
			s.log.Info("a plugin update is available", "plugin", p.ID, "version", p.Version, "installed", p.InstalledVersion)
		}
	}
}
