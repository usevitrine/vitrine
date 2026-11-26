package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// Sites can be switched off (kept, but nothing runs: no searches, checks or
// tag refreshes; the queue waits) and built-in ones removed. Both are kept in
// the settings table ("sitesOff", "removedSites": lists of plugin IDs).

// ErrSiteOff means the site is switched off.
var ErrSiteOff = errors.New("the site is switched off")

func (d *Downloader) idList(ctx context.Context, key string) []string {
	set, err := d.lib.Settings(ctx)
	if err != nil {
		return nil
	}
	var ids []string
	json.Unmarshal(set[key], &ids)
	return ids
}

func (d *Downloader) setIDList(ctx context.Context, key string, ids []string) error {
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if ids == nil {
		ids = []string{}
	}
	raw, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return d.lib.SetSettings(ctx, map[string]json.RawMessage{key: raw})
}

// IsOff says whether a site is switched off.
func (d *Downloader) IsOff(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.off[id]
}

// SetOff switches a site off or back on. Off: its worker stops (a running
// search carries on from where it was when the site is back on). On: the
// worker starts and due subscription checks are started.
func (d *Downloader) SetOff(ctx context.Context, id string, off bool) error {
	if _, ok := d.all()[id]; !ok {
		return fmt.Errorf("%w: %q", ErrUnknownSite, id)
	}
	ids := slices.DeleteFunc(d.idList(ctx, "sitesOff"), func(s string) bool { return s == id })
	if off {
		ids = append(ids, id)
	}
	if err := d.setIDList(ctx, "sitesOff", ids); err != nil {
		return err
	}
	d.mu.Lock()
	was := d.off[id]
	d.off[id] = off
	cancel := d.cancels[id]
	runCtx := d.runCtx
	if off {
		delete(d.cancels, id)
	}
	d.mu.Unlock()
	switch {
	case off && !was && cancel != nil:
		cancel()
	case !off && was && runCtx != nil:
		d.startWorker(runCtx, id)
		d.kick()
	}
	d.Changed()
	d.log.Info("site switched", "site", id, "off", off)
	return nil
}
