package download

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/usevitrine/vitrine/internal/plugin"
)

func TestCredentialsAndTest(t *testing.T) {
	e := newEnvWith(t, 5, "auth: {fields: [api_key, user_id], required: true, help: Get them from your account.}\n")
	ctx := context.Background()
	e.dl.SecretsPath = filepath.Join(t.TempDir(), "secrets.toml")

	info := e.dl.SiteInfos()
	if len(info) != 1 || info[0].ID != "fake" || info[0].Set["api_key"] || !info[0].Required || info[0].Help == "" || info[0].BuiltIn {
		t.Fatalf("site info: %+v", info)
	}
	if r, _ := e.dl.TestSite(ctx, "fake", nil); r.OK || r.Message != "Add the credentials first." {
		t.Fatalf("test without credentials: %+v", r)
	}
	// Typed but not saved (a login dialog's Test): half of them isn't enough; both connect, and nothing is saved.
	if r, _ := e.dl.TestSite(ctx, "fake", map[string]string{"api_key": "typed"}); r.OK || r.Message != "Add the credentials first." {
		t.Fatalf("test with half the typed credentials: %+v", r)
	}
	if r, err := e.dl.TestSite(ctx, "fake", map[string]string{"api_key": "typed", "user_id": "1"}); err != nil || !r.OK {
		t.Fatalf("test with typed credentials: %+v %v", r, err)
	}
	if e.dl.SiteInfos()[0].Set["api_key"] {
		t.Fatal("a test saved the typed credentials")
	}
	if r, _ := e.dl.TestSite(ctx, "fake", nil); r.OK {
		t.Fatalf("the site in use got the typed credentials: %+v", r)
	}
	if _, err := e.dl.TestSite(ctx, "fake", map[string]string{"password": "x"}); !errors.Is(err, ErrBadField) {
		t.Fatalf("test with an unknown field: %v", err)
	}

	if err := e.dl.SetCredentials("fake", map[string]string{"api_key": " test-key ", "user_id": "42"}); err != nil {
		t.Fatal(err)
	}
	if set := e.dl.SiteInfos()[0].Set; !set["api_key"] || !set["user_id"] {
		t.Fatalf("set: %v", set)
	}
	saved, err := plugin.LoadSecrets(e.dl.SecretsPath)
	if err != nil || saved["fake"]["api_key"] != "test-key" || saved["fake"]["user_id"] != "42" {
		t.Fatalf("file: %v %v", saved, err)
	}
	if r, err := e.dl.TestSite(ctx, "fake", nil); err != nil || !r.OK || r.Posts == 0 {
		t.Fatalf("test: %+v %v", r, err)
	}

	// One field changed, one left alone; then removed.
	e.dl.SetCredentials("fake", map[string]string{"user_id": "7"})
	saved, _ = plugin.LoadSecrets(e.dl.SecretsPath)
	if saved["fake"]["api_key"] != "test-key" || saved["fake"]["user_id"] != "7" {
		t.Fatalf("partial update: %v", saved)
	}
	e.dl.SetCredentials("fake", map[string]string{"api_key": "", "user_id": ""})
	saved, _ = plugin.LoadSecrets(e.dl.SecretsPath)
	if _, ok := saved["fake"]; ok || e.dl.SiteInfos()[0].Set["api_key"] {
		t.Fatalf("removed: %v", saved)
	}
	if err := e.dl.SetCredentials("fake", map[string]string{"password": "x"}); !errors.Is(err, ErrBadField) {
		t.Fatalf("unknown field: %v", err)
	}
	if err := e.dl.SetCredentials("nope", nil); !errors.Is(err, ErrUnknownSite) {
		t.Fatalf("unknown site: %v", err)
	}
}
