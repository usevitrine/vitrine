package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// cfSite is a site behind Cloudflare: every request without the clearance
// (cookie and the browser's User-Agent together) gets the challenge. strict:
// it refuses even cleared requests (it checks more than cookies).
func cfSite(t *testing.T, strict bool) (Manifest, *httptest.Server) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ck, _ := r.Cookie("cf_clearance")
		if !strict && ck != nil && ck.Value == "ok" && r.Header.Get("User-Agent") == "BrowserUA" {
			io.WriteString(w, "<html>the real page</html>")
			return
		}
		w.Header().Set("Server", "cloudflare")
		w.Header().Set("cf-mitigated", "challenge")
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, "<title>Just a moment...</title>")
	}))
	t.Cleanup(srv.Close)
	m, err := Parse([]byte(`
id: test
engine: gelbooru-v02-html
base: ` + srv.URL + `
post_url: ` + srv.URL + `/post/{id}
auth: {fields: [api_key]}
limits: {requests_per_sec: 100}
`))
	if err != nil {
		t.Fatal(err)
	}
	return m, srv
}

// fakeFlare is a FlareSolverr that hands out the clearance for host.
func fakeFlare(t *testing.T, host string) (*httptest.Server, *atomic.Int32, *[]string) {
	t.Helper()
	var calls atomic.Int32
	var mu sync.Mutex
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			io.WriteString(w, `{"msg":"FlareSolverr is ready!","version":"3.3.21"}`)
			return
		}
		var req struct{ Cmd, URL string }
		json.NewDecoder(r.Body).Decode(&req)
		calls.Add(1)
		mu.Lock()
		asked = append(asked, req.URL)
		mu.Unlock()
		time.Sleep(50 * time.Millisecond) // a browser visit takes a while
		json.NewEncoder(w).Encode(map[string]any{"status": "ok", "solution": map[string]any{
			"status": 200, "response": "<html>the page from the browser</html>", "userAgent": "BrowserUA",
			"cookies": []map[string]any{{"name": "cf_clearance", "value": "ok", "domain": "." + host, "expires": time.Now().Add(time.Hour).Unix()}},
		}})
	}))
	t.Cleanup(srv.Close)
	return srv, &calls, &asked
}

func pageURL(srv *httptest.Server, q string) *url.URL {
	u, _ := url.Parse(srv.URL + "/index.php?page=post" + q)
	return u
}

func TestCloudflareWithoutFlareSolverr(t *testing.T) {
	m, srv := cfSite(t, false)
	c := testClient(m, srv, nil)
	c.SetSolver(NewSolver()) // no address: off
	_, err := c.API(context.Background(), pageURL(srv, ""))
	if !errors.Is(err, ErrCloudflare) || !strings.Contains(err.Error(), "Settings > Sites") {
		t.Fatalf("err = %v", err)
	}
}

// A site that is down behind Cloudflare (503, Cloudflare's challenge-platform script in its own page) isn't a
// challenge: it's an error saying what the page says, and FlareSolverr isn't asked.
func TestSiteDownBehindCloudflare(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "cloudflare")
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, `<html><head><title>Temporarily  down</title></head><body>We are down.<script src="/cdn-cgi/challenge-platform/scripts/jsd/main.js"></script></body></html>`)
	}))
	t.Cleanup(srv.Close)
	m, err := Parse([]byte("id: test\nengine: gelbooru-v02-html\nbase: " + srv.URL + "\npost_url: " + srv.URL + "/post/{id}\nlimits: {requests_per_sec: 100}\n"))
	if err != nil {
		t.Fatal(err)
	}
	flare, calls, _ := fakeFlare(t, "example.invalid")
	s := NewSolver()
	s.SetEndpoint(flare.URL)
	c := testClient(m, srv, nil)
	c.SetSolver(s)
	_, err = c.API(context.Background(), pageURL(srv, ""))
	var se *StatusError
	if errors.Is(err, ErrCloudflare) || !errors.As(err, &se) || se.Code != 503 || !strings.Contains(err.Error(), "its page says: Temporarily down") {
		t.Fatalf("err = %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("FlareSolverr asked %d times", calls.Load())
	}
}

func TestCloudflarePassed(t *testing.T) {
	m, srv := cfSite(t, false)
	host, _ := url.Parse(srv.URL)
	flare, calls, asked := fakeFlare(t, host.Hostname())
	s := NewSolver()
	s.SetEndpoint(flare.URL)
	c := testClient(m, srv, map[string]string{"api_key": "SECRET"})
	c.SetSolver(s)

	// Five at once: one browser visit, all get the real page.
	var wg sync.WaitGroup
	errs := make(chan error, 5)
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, err := c.API(context.Background(), pageURL(srv, ""))
			if err == nil && string(body) != "<html>the real page</html>" {
				err = errors.New("body " + string(body))
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("FlareSolverr asked %d times, want 1", calls.Load())
	}
	// The clearance is reused: no more visits.
	if _, err := c.API(context.Background(), pageURL(srv, "&s=list")); err != nil || calls.Load() != 1 {
		t.Fatalf("second request: %v, %d visits", err, calls.Load())
	}
	// Credentials never went to FlareSolverr.
	for _, a := range *asked {
		if strings.Contains(a, "SECRET") {
			t.Fatalf("FlareSolverr was given the credentials: %s", a)
		}
	}
}

func TestCloudflareStrictSite(t *testing.T) {
	m, srv := cfSite(t, true)
	host, _ := url.Parse(srv.URL)
	flare, _, _ := fakeFlare(t, host.Hostname())
	s := NewSolver()
	s.SetEndpoint(flare.URL)

	// A page without credentials: the browser's page is used.
	c := testClient(m, srv, nil)
	c.SetSolver(s)
	body, err := c.API(context.Background(), pageURL(srv, ""))
	if err != nil || string(body) != "<html>the page from the browser</html>" {
		t.Fatalf("page: %q, %v", body, err)
	}
	// With credentials the browser's page was a different URL: an error that says why.
	c2 := testClient(m, srv, map[string]string{"api_key": "SECRET"})
	c2.SetSolver(s)
	_, err = c2.API(context.Background(), pageURL(srv, ""))
	if !errors.Is(err, ErrCloudflare) || !strings.Contains(err.Error(), "still refused") {
		t.Fatalf("err = %v", err)
	}
}

func TestFlareSolverrPing(t *testing.T) {
	flare, _, _ := fakeFlare(t, "example.test")
	s := NewSolver()
	if v, err := s.Ping(context.Background(), flare.URL); err != nil || v != "3.3.21" {
		t.Fatalf("ping: %q %v", v, err)
	}
	if _, err := s.Ping(context.Background(), "ftp://nope"); err == nil {
		t.Fatal("a bad address passed")
	}
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "hello") }))
	defer other.Close()
	if _, err := s.Ping(context.Background(), other.URL); err == nil || !strings.Contains(err.Error(), "not as FlareSolverr") {
		t.Fatalf("not FlareSolverr: %v", err)
	}
}
