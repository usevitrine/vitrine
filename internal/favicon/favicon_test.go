package favicon

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

var (
	png16 = append([]byte("\x89PNG\r\n\x1a\n"), "small"...)
	png64 = append([]byte("\x89PNG\r\n\x1a\n"), "right size"...)
	ico   = []byte{0, 0, 1, 0, 1, 0, 16, 16}
)

// sites serves made-up sites by name: every host goes to this one server.
func sites(t *testing.T, h http.HandlerFunc) (*Cache, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	c, err := Open(t.TempDir(), "Vitrine-test")
	if err != nil {
		t.Fatal(err)
	}
	addr := srv.Listener.Addr().String()
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", addr)
		},
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	c.client = &http.Client{Transport: tr}
	return c, &hits
}

func TestIconFromPageLinks(t *testing.T) {
	c, _ := sites(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Write([]byte(`<html><head>
				<link rel="mask-icon" href="/mask.svg">
				<link rel="icon" type="image/svg+xml" href="/icon.svg">
				<link rel="icon" sizes="16x16" href="/16.png">
				<LINK REL='shortcut icon' sizes=64x64 href='/64.png?a=1&amp;b=2'>
				<link rel="apple-touch-icon" href="/apple.png">
			</head></html>`))
		case "/64.png":
			if r.URL.RawQuery != "a=1&b=2" {
				http.NotFound(w, r)
				return
			}
			w.Write(png64)
		case "/16.png":
			w.Write(png16)
		default:
			http.NotFound(w, r)
		}
	})
	got, err := c.Get(context.Background(), "Example.com")
	if err != nil || string(got) != string(png64) {
		t.Fatalf("got %q, %v (want the 64 px one)", got, err)
	}
	if _, err := os.Stat(filepath.Join(c.dir, "example.com.icon")); err != nil {
		t.Fatal("not kept:", err)
	}
}

func TestFaviconIcoAndCache(t *testing.T) {
	c, hits := sites(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Write([]byte(`<html>no icon links</html>`))
		case "/favicon.ico":
			w.Write(ico)
		default:
			http.NotFound(w, r)
		}
	})
	for i := 0; i < 2; i++ {
		got, err := c.Get(context.Background(), "example.com")
		if err != nil || string(got) != string(ico) {
			t.Fatalf("got %q, %v", got, err)
		}
	}
	if n := hits.Load(); n != 2 { // the page and the icon, once
		t.Fatalf("%d requests: the second Get should come from the disk", n)
	}
	if ContentType(ico) != "image/x-icon" {
		t.Fatal(ContentType(ico))
	}
}

func TestNoneIsRememberedAndNotAnImageIsRefused(t *testing.T) {
	c, hits := sites(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Write([]byte(`<link rel="icon" href="/page.png">`))
		case "/page.png":
			w.Write([]byte(`<html>a login page, not an image</html>`))
		default:
			http.NotFound(w, r)
		}
	})
	if _, err := c.Get(context.Background(), "example.com"); !errors.Is(err, ErrNone) {
		t.Fatalf("err %v, want none", err)
	}
	n := hits.Load()
	if _, err := c.Get(context.Background(), "example.com"); !errors.Is(err, ErrNone) || hits.Load() != n {
		t.Fatalf("asked again at once (%d -> %d requests), err %v", n, hits.Load(), err)
	}
	// A week later it's asked again.
	old := time.Now().Add(-retryNoneIn - time.Hour)
	os.Chtimes(filepath.Join(c.dir, "example.com.none"), old, old)
	c.Get(context.Background(), "example.com")
	if hits.Load() == n {
		t.Fatal("not asked again after a week")
	}
}

func TestImageHostFallsBackToTheSite(t *testing.T) {
	c, _ := sites(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "example.com" && r.URL.Path == "/favicon.ico" {
			w.Write(ico)
			return
		}
		http.NotFound(w, r)
	})
	got, err := c.Get(context.Background(), "img.example.com")
	if err != nil || string(got) != string(ico) {
		t.Fatalf("got %q, %v (want the site's own icon)", got, err)
	}
}

func TestOldIconKeptWhenTheSiteIsGone(t *testing.T) {
	c, hits := sites(t, func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	path := filepath.Join(c.dir, "example.com.icon")
	os.WriteFile(path, png16, 0o644)
	old := time.Now().Add(-keepFor - time.Hour)
	os.Chtimes(path, old, old)
	got, err := c.Get(context.Background(), "example.com")
	if err != nil || string(got) != string(png16) {
		t.Fatalf("got %q, %v (want the old one)", got, err)
	}
	n := hits.Load()
	c.Get(context.Background(), "example.com")
	if hits.Load() != n {
		t.Fatal("asked again at once (the old one should count as fresh for another month)")
	}
}

func TestOnlyPublicSites(t *testing.T) {
	for _, h := range []string{"127.0.0.1", "localhost", "a_b.example.com", "example.com:8080", "::1", "-a.example.com"} {
		if ValidHost(h) {
			t.Errorf("%q accepted", h)
		}
	}
	if !ValidHost("img.example.com") {
		t.Error("img.example.com refused")
	}
	for _, a := range []string{"127.0.0.1:443", "10.0.0.2:443", "192.168.1.5:443", "100.100.1.1:443", "[::1]:443", "[fd00::1]:443", "169.254.1.1:443", "0.0.0.0:443"} {
		if publicOnly("tcp", a, nil) == nil {
			t.Errorf("%s allowed", a)
		}
	}
	if err := publicOnly("tcp", "1.1.1.1:443", nil); err != nil {
		t.Error(err)
	}
	c, err := Open(t.TempDir(), "Vitrine-test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(context.Background(), "localhost"); !errors.Is(err, ErrNone) {
		t.Fatal(err)
	}
}

func TestParent(t *testing.T) {
	for host, want := range map[string]string{
		"img.example.com":     "example.com",
		"example.com":         "",
		"a.example.co.uk":     "example.co.uk",
		"example.co.uk":       "",
		"cdn.img.example.org": "img.example.org",
	} {
		if got := parent(host); got != want {
			t.Errorf("parent(%q) = %q, want %q", host, got, want)
		}
	}
}

func TestOtherAddressWhenTheMainOneIsBehindABotCheck(t *testing.T) {
	c, _ := sites(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "api.example.com" && r.URL.Path == "/favicon.ico" {
			w.Write(ico)
			return
		}
		w.WriteHeader(http.StatusForbidden) // a CAPTCHA page, everywhere else
		w.Write([]byte("<html>prove you're human</html>"))
	})
	if _, err := c.Get(context.Background(), "other.example.org"); !errors.Is(err, ErrNone) {
		t.Fatalf("err %v", err)
	}
	got, err := c.Get(context.Background(), "example.com", "api.example.com")
	if err != nil || string(got) != string(ico) {
		t.Fatalf("got %q, %v (want the API address's icon)", got, err)
	}
	if _, err := os.Stat(filepath.Join(c.dir, "example.com.icon")); err != nil {
		t.Fatal("kept under the site's own name:", err)
	}
}
