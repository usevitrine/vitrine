package server

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/usevitrine/vitrine/internal/logring"
)

// The problem report keeps the log readable but private: no tags, searches,
// links, keys, addresses or site names - placeholders instead (the same value,
// the same placeholder).
func TestReportHidesPrivateValues(t *testing.T) {
	e := newEnv(t)
	e.loginAs("me", "correct horse")
	ring := logring.New(100)
	log := slog.New(logring.Tee(slog.NewTextHandler(io.Discard, nil), ring))
	e.h.SetLogs(ring)
	if _, err := e.lib.Site(t.Context(), "examplebooru"); err != nil {
		t.Fatal(err)
	}
	if err := e.lib.SetSettings(t.Context(), map[string]json.RawMessage{"defaultSearch": json.RawMessage(`"blue_sky some_artist"`), "loop": json.RawMessage(`true`)}); err != nil {
		t.Fatal(err)
	}
	log.Warn("search failed", "site", "examplebooru", "query", "blue_sky -red", "files", 3,
		"err", errors.New(`GET https://examplebooru.org/index.php?tags=blue_sky: tag "some_artist" not found; api_key=abcdef0123456789abcdef0123456789; mail me@example.com`))
	log.Info("checked", "site", "examplebooru", "took", 1500)
	log.Info("listening", "address", "127.0.0.1:8080", "version", "v0.0.0-20261019205542-b3860a619863+dirty")
	log.Info("examplebooru answered slowly")
	log.Warn("request", "path", "/api/sites/examplebooru/credentials", "status", 500)
	log.Warn("request", "path", "/thumbs/0123456789abcdef0123456789abcdef0123456789abcdef.webp", "status", 404)

	resp, err := e.c.Get(e.srv.URL + "/api/report")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	body := string(b)
	for _, private := range []string{"examplebooru", "blue_sky", "abcdef0123", "some_artist", "me@example.com", "https://", "me "} {
		if strings.Contains(body, private) {
			t.Errorf("the report shows %q:\n%s", private, body)
		}
	}
	for _, want := range []string{"site=site-1 query=search-1 files=3", "site=site-1 took=1500", "version=\"v0.0.0-20261019205542-b3860a619863+dirty\"", "site-1 answered slowly", "path=/api/sites/site-1/credentials status=500", "path=/thumbs/<hidden>.webp", "Vitrine problem report", "Library", "defaultSearch=set", "loop=true"} {
		if !strings.Contains(body, want) {
			t.Errorf("the report lacks %q:\n%s", want, body)
		}
	}
}
