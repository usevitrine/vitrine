package download

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/plugin"
)

func TestInstallPlugin(t *testing.T) {
	e := newEnv(t, 3)
	ctx := context.Background()
	if _, err := e.dl.InstallPlugin(ctx, []byte("id: other\n")); !errors.Is(err, ErrNoPluginsDir) && !errors.Is(err, ErrBadPlugin) {
		t.Fatalf("no plugins folder: %v", err)
	}
	e.dl.PluginsDir = filepath.Join(t.TempDir(), "plugins")

	// A new site: saved, listed, searchable by its address.
	newSite := "id: other\nversion: 1.0.0\nengine: gelbooru-v02\nbase: https://api.other.test\npost_url: https://other.test/post/{id}\n" +
		"tags: {namespaces: {artist: creator}}\n"
	res, err := e.dl.InstallPlugin(ctx, []byte(newSite))
	if err != nil {
		t.Fatal(err)
	}
	if !res.New || res.ID != "other" || res.Site != "other.test" || res.Version != "1.0.0" {
		t.Fatalf("install: %+v", res)
	}
	if b, err := os.ReadFile(filepath.Join(e.dl.PluginsDir, "other.yaml")); err != nil || string(b) != newSite {
		t.Fatalf("saved file: %q %v", b, err)
	}
	if e.dl.Site("other") == nil || len(e.dl.Sites()) != 2 {
		t.Fatalf("sites: %v", e.dl.Sites())
	}
	if _, err := e.dl.Add(ctx, "other", "x", 0); err != nil {
		t.Fatalf("a search on the new site: %v", err)
	}

	// An update of an installed site keeps its credentials file entry and ID.
	before := e.dl.Site("other").ID
	res, err = e.dl.InstallPlugin(ctx, []byte(newSite[:len("id: other\nversion: ")]+"1.1.0"+newSite[len("id: other\nversion: 1.0.0"):]))
	if err != nil {
		t.Fatal(err)
	}
	if res.New || res.OldVersion != "1.0.0" || res.Version != "1.1.0" || e.dl.Site("other").ID != before ||
		e.dl.Site("other").Client.Manifest().Version != "1.1.0" {
		t.Fatalf("update: %+v", res)
	}

	// A bad file is refused and changes nothing.
	if _, err := e.dl.InstallPlugin(ctx, []byte("id: other\nengine: nope\n")); !errors.Is(err, ErrBadPlugin) {
		t.Fatalf("bad plugin: %v", err)
	}
	if e.dl.Site("other").Client.Manifest().Version != "1.1.0" {
		t.Fatal("a refused file replaced the plugin")
	}
}

func TestPluginIndex(t *testing.T) {
	e := newEnv(t, 3)
	ctx := context.Background()
	e.dl.PluginsDir = filepath.Join(t.TempDir(), "plugins")
	repo := t.TempDir()
	os.MkdirAll(filepath.Join(repo, "plugins"), 0o755)
	os.WriteFile(filepath.Join(repo, "plugins", "other.yaml"), []byte("id: other\nversion: 2.0.0\nengine: gelbooru-v02\nbase: https://api.other.test\npost_url: https://other.test/post/{id}\n"), 0o644)
	os.WriteFile(filepath.Join(repo, "index.json"), []byte(`{"plugins": [
		{"id": "other", "name": "Other", "site": "other.test", "version": "2.0.0", "file": "plugins/other.yaml"},
		{"id": "fake", "name": "Fake", "site": "fake.example.test", "version": "9.0.0", "file": "plugins/fake.yaml"},
		{"id": "../evil", "version": "1", "file": "x.yaml"}]}`), 0o644)
	e.dl.PluginIndex = repo // a folder with index.json

	list, err := e.dl.Plugins(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != "other" || list[0].Installed || !list[1].Installed {
		t.Fatalf("list: %+v", list)
	}
	if !list[1].Update { // the test site's manifest has no version: 9.0.0 is newer
		t.Fatalf("fake should have an update: %+v", list[1])
	}
	res, err := e.dl.InstallFromIndex(ctx, "other")
	if err != nil || !res.New || res.Version != "2.0.0" {
		t.Fatalf("install from the list: %+v %v", res, err)
	}
	if list, _ = e.dl.Plugins(ctx, false); !list[0].Installed || list[0].InstalledVersion != "2.0.0" || list[0].Update {
		t.Fatalf("after installing: %+v", list[0])
	}
	if _, err := e.dl.InstallFromIndex(ctx, "fake"); err == nil {
		t.Fatal("installed a plugin whose file is missing")
	}
	e.dl.PluginIndex = filepath.Join(repo, "missing")
	if _, err := e.dl.Plugins(ctx, true); err == nil {
		t.Fatal("no error for a missing list")
	}
}

func TestRemovePlugin(t *testing.T) {
	e := newEnv(t, 3)
	ctx := context.Background()
	e.dl.PluginsDir = filepath.Join(t.TempDir(), "plugins")
	other := "id: other\nversion: 1.0.0\nengine: gelbooru-v02\nbase: https://api.other.test\npost_url: https://other.test/post/{id}\n"
	if _, err := e.dl.InstallPlugin(ctx, []byte(other)); err != nil {
		t.Fatal(err)
	}
	id, _ := e.dl.Add(ctx, "other", "kept", 0)
	if _, err := e.dl.Subscribe(ctx, "other", "kept_sub", 24*time.Hour, 0); err != nil {
		t.Fatal(err)
	}
	var info SiteInfo
	for _, s := range e.dl.SiteInfos() {
		if s.ID == "other" {
			info = s
		}
	}
	if !info.Removable || info.Overrides || info.Subscriptions != 1 || info.Downloads < 1 {
		t.Fatalf("site info: %+v", info)
	}

	res, err := e.dl.RemovePlugin(ctx, "other")
	if err != nil || res.Restored || res.Site != "other.test" {
		t.Fatalf("remove: %+v %v", res, err)
	}
	if e.dl.Site("other") != nil {
		t.Fatal("the site is still there")
	}
	if _, err := os.Stat(filepath.Join(e.dl.PluginsDir, "other.yaml")); !os.IsNotExist(err) {
		t.Fatalf("the file is still there: %v", err)
	}
	if s, err := e.dl.Get(ctx, id); err != nil || s.Query != "kept" {
		t.Fatalf("the search wasn't kept: %+v %v", s, err)
	}
	if _, err := e.dl.RemovePlugin(ctx, "other"); !errors.Is(err, ErrUnknownSite) {
		t.Fatalf("removing twice: %v", err)
	}
	// Installed again: back to work with the same searches.
	if res, err := e.dl.InstallPlugin(ctx, []byte(other)); err != nil || !res.New {
		t.Fatalf("reinstall: %+v %v", res, err)
	}

	// A file replacing a built-in plugin goes back to the built-in one.
	b, ok := plugin.Builtin("safebooru")
	if !ok {
		t.Fatal("no built-in safebooru")
	}
	if _, err := e.dl.RemovePlugin(ctx, "fake"); err == nil {
		t.Fatal("removed a plugin that has no file")
	}
	over := "id: safebooru\nversion: 99.0.0\nengine: gelbooru-v02\nbase: https://safebooru.org\npost_url: https://safebooru.org/index.php?page=post&s=view&id={id}\n"
	if _, err := e.dl.InstallPlugin(ctx, []byte(over)); err != nil {
		t.Fatal(err)
	}
	for _, s := range e.dl.SiteInfos() {
		if s.ID == "safebooru" && !s.Overrides {
			t.Fatalf("override not marked: %+v", s)
		}
	}
	if res, err := e.dl.RemovePlugin(ctx, "safebooru"); err != nil || !res.Restored {
		t.Fatalf("back to built-in: %+v %v", res, err)
	}
	if v := e.dl.Site("safebooru").Client.Manifest(); !v.BuiltIn || v.Version != b.Version {
		t.Fatalf("after going back: %+v", v)
	}
	// A built-in plugin can go too: remembered (not back after a restart) until it is installed again.
	if _, err := e.dl.RemovePlugin(ctx, "safebooru"); err != nil || e.dl.Site("safebooru") != nil {
		t.Fatalf("removing a built-in: %v", err)
	}
	restarted, err := New(ctx, e.lib, e.db, map[string]plugin.Manifest{"safebooru": b}, nil, e.dl.log)
	if err != nil || restarted.Site("safebooru") != nil {
		t.Fatalf("back after a restart: %v", err)
	}
	if _, err := e.dl.InstallPlugin(ctx, []byte(over)); err != nil || e.dl.Site("safebooru") == nil {
		t.Fatalf("installed again: %v", err)
	}
	if again, _ := New(ctx, e.lib, e.db, map[string]plugin.Manifest{"safebooru": b}, nil, e.dl.log); again.Site("safebooru") == nil {
		t.Fatal("still remembered as removed after installing it again")
	}
}
