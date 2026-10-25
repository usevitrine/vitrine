package plugin

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/media"
)

const sampleXML = `<?xml version="1.0" encoding="UTF-8"?><posts count="2" offset="0">
<post height="600" file_url="https://img.example.test/images/1/a.jpg" rating="e" tags=" blue_sky 9:16 tag_(artist) "
 id="200" width="800" md5="5de2a60b4403d6ebab0e86a2110e86b4" source="https://x.com/a/status/1 https://pixiv.example/2"
 created_at="Wed Oct 14 13:00:17 +0200 2026"/>
<post height="10" file_url="//img.example.test/images/1/b.png" rating="s" tags="red" id="150" width="10" md5="bad"/>
</posts>`

// fakeSite serves the dapi from an httptest TLS server and records the
// queries it got.
func fakeSite(t *testing.T) (Manifest, *httptest.Server, *[]url.Values) {
	t.Helper()
	var got []url.Values
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.Query())
		if r.Header.Get("User-Agent") != UserAgent {
			t.Errorf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		io.WriteString(w, sampleXML)
	}))
	t.Cleanup(srv.Close)
	m, err := Parse([]byte(`
id: test
engine: gelbooru-v02
base: ` + srv.URL + `
post_url: https://site.example.test/post/{id}
domains: [img.example.test]
auth: {fields: [api_key, user_id], required: true}
limits: {requests_per_sec: 50}
ratings: {e: explicit, s: general}
`))
	if err != nil {
		t.Fatal(err)
	}
	return m, srv, &got
}

func testClient(m Manifest, srv *httptest.Server, creds map[string]string) *Client {
	c := NewClient(m, creds)
	h := *srv.Client() // (a copy: each client keeps its own cookie jar, if it has one)
	h.Jar = c.http.Jar
	c.http = &h
	return c
}

func TestGelbooruSearch(t *testing.T) {
	m, srv, got := fakeSite(t)
	c := testClient(m, srv, map[string]string{"api_key": "SECRET", "user_id": "42"})
	e, err := NewEngine(c)
	if err != nil {
		t.Fatal(err)
	}
	page, err := e.Search(context.Background(), " blue_sky -red ", 0)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Posts) != 2 {
		t.Fatalf("page: %+v", page)
	}
	p := page.Posts[0]
	if p.ID != 200 || p.Rating != media.RatingExplicit || len(p.Tags) != 3 || p.Tags[1] != "9:16" ||
		len(p.Sources) != 2 || p.PostURL != "https://site.example.test/post/200" || p.MD5[0] != 0x5d {
		t.Fatalf("post: %+v", p)
	}
	if want := time.Date(2026, 10, 14, 11, 0, 17, 0, time.UTC); !p.Posted.Equal(want) || !page.Posts[1].Posted.IsZero() {
		t.Fatalf("posted times: %v, %v", p.Posted, page.Posts[1].Posted)
	}
	if page.Posts[1].FileURL != "https://img.example.test/images/1/b.png" || page.Posts[1].MD5 != [16]byte{} {
		t.Fatalf("second post: %+v", page.Posts[1])
	}
	q := (*got)[0]
	if q.Get("tags") != "blue_sky -red" || q.Get("api_key") != "SECRET" || q.Get("limit") != "100" {
		t.Fatalf("query sent: %v", q)
	}

	// Resuming below a post ID.
	e.Search(context.Background(), "blue_sky", 150)
	if tags := (*got)[1].Get("tags"); tags != "blue_sky id:<150" {
		t.Fatalf("resume query: %q", tags)
	}
}

func TestCredentialsRequired(t *testing.T) {
	m, srv, _ := fakeSite(t)
	e, _ := NewEngine(testClient(m, srv, nil))
	if _, err := e.Search(context.Background(), "x", 0); err != ErrNoCredentials {
		t.Fatalf("got %v, want ErrNoCredentials", err)
	}
}

func TestErrorsHideURLs(t *testing.T) {
	m, srv, _ := fakeSite(t)
	srv.Close() // connection refused
	c := testClient(m, srv, map[string]string{"api_key": "SECRET", "user_id": "42"})
	e, _ := NewEngine(c)
	_, err := e.Search(context.Background(), "x", 0)
	if err == nil || strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "api_key") {
		t.Fatalf("error leaks the request: %v", err)
	}
}

func TestDomainsEnforced(t *testing.T) {
	m, srv, _ := fakeSite(t)
	c := testClient(m, srv, nil)
	if _, _, err := c.File(context.Background(), "https://evil.example.test/x.jpg"); err == nil ||
		!strings.Contains(err.Error(), "not one of the plugin's domains") {
		t.Fatalf("got %v", err)
	}
	if _, _, err := c.File(context.Background(), "file:///etc/passwd"); err == nil {
		t.Fatal("file: URL accepted")
	}
}

func TestRateLimit(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		io.WriteString(w, `<posts count="0"/>`)
	}))
	defer srv.Close()
	m, _ := Parse([]byte("id: t\nengine: gelbooru-v02\nbase: " + srv.URL + "\nlimits: {requests_per_sec: 10}\n"))
	c := NewClient(m, nil)
	c.http = &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	e, _ := NewEngine(c)
	start := time.Now()
	for range 4 {
		if _, err := e.Search(context.Background(), "x", 0); err != nil {
			t.Fatal(err)
		}
	}
	if d := time.Since(start); d < 280*time.Millisecond {
		t.Fatalf("4 requests at 10/s took %v, want >= 300 ms", d)
	}
}

func TestManifests(t *testing.T) {
	all, err := Load("")
	if err != nil || all["safebooru"].Engine != "gelbooru-v02" {
		t.Fatalf("built-ins: %v %v", all, err)
	}
	if !all["safebooru"].allows("safebooru.org") || all["safebooru"].allows("evil.org") {
		t.Fatal("domain check")
	}

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "mine.yaml"), []byte("id: mine\nengine: gelbooru-v02\nbase: https://mine.example\n"), 0o644)
	all, err = Load(dir)
	if err != nil || all["mine"].Search.PerPage != 100 || all["mine"].Limits.Connections != 1 {
		t.Fatalf("user plugin: %+v %v", all["mine"], err)
	}

	for _, bad := range []string{
		"id: Bad\nengine: gelbooru-v02\nbase: https://x.example\n",
		"id: x\nengine: nope\nbase: https://x.example\n",
		"id: x\nengine: gelbooru-v02\nbase: http://x.example\n",
		"id: x\nengine: gelbooru-v02\nbase: https://x.example\nratings: {e: spicy}\n",
		"id: x\nengine: gelbooru-v02\nbase: https://x.example\nunknown_key: 1\n",
		"id: user\nengine: gelbooru-v02\nbase: https://x.example\n",
	} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("accepted:\n%s", bad)
		}
	}
}

func TestSecrets(t *testing.T) {
	p := filepath.Join(t.TempDir(), "secrets.toml")
	s, err := LoadSecrets(p)
	if err != nil || len(s) != 0 {
		t.Fatalf("missing file: %v %v", s, err)
	}
	s["safebooru"] = map[string]string{"api_key": "k", "user_id": "1"}
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	s2, err := LoadSecrets(p)
	if err != nil || s2["safebooru"]["api_key"] != "k" {
		t.Fatalf("round trip: %v %v", s2, err)
	}
}

func TestGelbooruTagTypes(t *testing.T) {
	var calls []url.Values
	jsonOK := true
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Query())
		if r.URL.Query().Get("json") == "1" {
			if !jsonOK {
				http.Error(w, "nope", http.StatusInternalServerError)
				return
			}
			io.WriteString(w, `[{"id":200,"tag_info":[{"tag":"blue_sky","type":"tag","count":5},
				{"tag":"tag_(artist)","type":"artist","count":1},{"tag":"9:16","type":"metadata","count":9}]},
				{"id":150,"tag_info":[{"tag":"red","type":"character","count":2}]}]`)
			return
		}
		io.WriteString(w, sampleXML)
	}))
	defer srv.Close()
	m, err := Parse([]byte("id: test\nengine: gelbooru-v02\nbase: " + srv.URL + "\ndomains: [img.example.test]\n" +
		"limits: {requests_per_sec: 50}\ntags: {types: tag_info, namespaces: {artist: creator, character: character, metadata: meta}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Namespace("artist") != "creator" || m.Namespace("tag") != "" {
		t.Fatalf("namespaces: %v", m.Tags.Namespaces)
	}
	e, _ := NewEngine(testClient(m, srv, nil))
	page, err := e.Search(context.Background(), "blue_sky", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[1].Get("fields") != "tag_info" || calls[1].Get("tags") != "blue_sky" {
		t.Fatalf("requests: %v", calls)
	}
	tt := page.Posts[0].TagTypes
	if tt["tag_(artist)"] != "artist" || tt["blue_sky"] != "tag" || page.Posts[1].TagTypes["red"] != "character" {
		t.Fatalf("tag types: %v", tt)
	}

	// The JSON request failing doesn't fail the page.
	jsonOK = false
	page, err = e.Search(context.Background(), "blue_sky", 0)
	if err != nil || len(page.Posts) != 2 || page.Posts[0].TagTypes != nil {
		t.Fatalf("without tag types: %v %+v", err, page)
	}

	// Bad manifests.
	for _, bad := range []string{"tags: {types: html}", "tags: {namespaces: {artist: Creator}}"} {
		if _, err := Parse([]byte("id: x\nengine: gelbooru-v02\nbase: https://a.test\n" + bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestTooManyRequests(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		io.WriteString(w, `<posts count="0"/>`)
	}))
	defer srv.Close()
	m, _ := Parse([]byte("id: t\nengine: gelbooru-v02\nbase: " + srv.URL + "\nlimits: {requests_per_sec: 100}\n"))
	c := testClient(m, srv, nil)
	c.tooMany = 200 * time.Millisecond
	e, _ := NewEngine(c)
	start := time.Now()
	if _, err := e.Search(context.Background(), "x", 0); err != nil {
		t.Fatalf("a 429 then OK: %v", err)
	}
	if d := time.Since(start); hits.Load() != 2 || d < 190*time.Millisecond {
		t.Fatalf("%d requests in %v; want a retry after the back-off", hits.Load(), d)
	}
}

func TestSiteAddress(t *testing.T) {
	for yaml, want := range map[string]string{
		"post_url: https://safebooru.org/index.php?page=post&s=view&id={id}\nbase: https://api.safebooru.org": "safebooru.org",
		"post_url: https://www.example.org/p/{id}\nbase: https://api.example.org":                             "example.org",
		"base: https://api.site.test":                "site.test",
		"site: my.site\nbase: https://api.site.test": "my.site",
	} {
		m, err := Parse([]byte("id: x\nengine: gelbooru-v02\n" + yaml))
		if err != nil {
			t.Fatal(err)
		}
		if m.Site != want {
			t.Errorf("%q: site %q, want %q", yaml, m.Site, want)
		}
	}
}

func TestFileRefererPagesAndRedirects(t *testing.T) {
	var referer string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/file.jpg":
			referer = r.Header.Get("Referer")
			if referer == "" {
				http.Redirect(w, r, "/hotlink", http.StatusFound)
				return
			}
			w.Header().Set("Content-Type", "image/jpeg")
			io.WriteString(w, "jpeg")
		case "/hotlink":
			w.Header().Set("Content-Type", "text/html; charset=UTF-8")
			io.WriteString(w, "<html>no hotlinking</html>")
		case "/away":
			http.Redirect(w, r, "https://elsewhere.invalid/x.jpg", http.StatusFound)
		}
	}))
	defer srv.Close()
	m, _ := Parse([]byte("id: t\nengine: gelbooru-v02\nbase: " + srv.URL + "\n"))
	c := testClient(m, srv, nil)
	c.http.CheckRedirect = NewClient(m, nil).http.CheckRedirect // the test client keeps the real redirect rule
	// No referer: the site answers with a page - refused, not imported.
	if _, _, err := c.File(context.Background(), srv.URL+"/file.jpg"); err == nil || !strings.Contains(err.Error(), "web page") {
		t.Fatalf("hotlink page: %v", err)
	}
	// A redirect off the plugin's domains is refused.
	if _, _, err := c.File(context.Background(), srv.URL+"/away"); err == nil || !strings.Contains(err.Error(), "not one of the plugin's domains") {
		t.Fatalf("redirect away: %v", err)
	}
	// With the plugin's referer: the file.
	m.Referer = srv.URL + "/"
	c = testClient(m, srv, nil)
	body, _, err := c.File(context.Background(), srv.URL+"/file.jpg")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(body)
	body.Close()
	if string(b) != "jpeg" || referer != srv.URL+"/" {
		t.Fatalf("file %q, referer %q", b, referer)
	}
}

func TestNewerVersion(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"1.10.0", "1.9.2", true}, {"1.2", "1.2.0", false}, {"1.2.1", "1.2", true}, {"2.0.0", "", true}, {"1.0.0", "1.0.0", false}, {"v1.3", "1.2.9", true}} {
		if got := NewerVersion(c.a, c.b); got != c.want {
			t.Errorf("NewerVersion(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func TestTagQuery(t *testing.T) {
	booru, err := Parse([]byte("id: b\nengine: gelbooru-v02\nbase: https://api.example.com\n"))
	if err != nil {
		t.Fatal(err)
	}
	if q := booru.TagQuery("creator", "some_artist"); q != "some_artist" {
		t.Fatalf("booru creator: %q (boorus search the plain tag)", q)
	}
	masto, err := Parse([]byte("id: m\nengine: mastodon\nbase: https://social.example.com\n"))
	if err != nil {
		t.Fatal(err)
	}
	if q := masto.TagQuery("creator", "some_artist"); q != "@some_artist" {
		t.Fatalf("mastodon creator: %q", q)
	}
	if q := masto.TagQuery("", "blue_sky"); q != "#blue_sky" {
		t.Fatalf("mastodon tag: %q", q)
	}
	if q := masto.TagQuery("series", "blue_sky"); q != "#blue_sky" {
		t.Fatalf("mastodon other namespace: %q", q)
	}
	// A plugin's own rules go over the engine's.
	own, err := Parse([]byte("id: o\nengine: gelbooru-v02\nbase: https://api.example.com\nsearch: {tag_query: {character: \"char:{tag}\"}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if q := own.TagQuery("character", "x"); q != "char:x" {
		t.Fatal(q)
	}
	if q := own.TagQuery("", "x"); q != "x" {
		t.Fatal(q)
	}
	if _, err := Parse([]byte("id: bad\nengine: gelbooru-v02\nbase: https://api.example.com\nsearch: {tag_query: {\"*\": \"#tag\"}}\n")); err == nil {
		t.Fatal("a template without {tag} accepted")
	}
}

// A file download that stops getting data - no answer at all, or a stall
// mid-file - is given up after the idle limit, not held forever.
func TestFileStall(t *testing.T) {
	stop := make(chan struct{})
	defer close(stop)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/silent.png" {
			select { // never answers (until the client gives up)
			case <-stop:
			case <-r.Context().Done():
			}
			// (dropped without an answer: returning sent an empty 200, and on CI's slow race run the client got that -
			// "no answer: <nil>")
			panic(http.ErrAbortHandler)
		}
		w.Header().Set("Content-Length", "1000")
		w.Write(make([]byte, 400))
		w.(http.Flusher).Flush()
		select { // the rest never comes
		case <-stop:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	m, _ := Parse([]byte("id: t\nengine: gelbooru-v02\nbase: " + srv.URL + "\n"))
	c := NewClient(m, nil)
	c.http = &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	c.SetFileIdle(200 * time.Millisecond)

	start := time.Now()
	if _, _, err := c.File(context.Background(), srv.URL+"/silent.png"); !errors.Is(err, ErrStalled) {
		t.Fatalf("no answer: %v", err)
	}
	body, _, err := c.File(context.Background(), srv.URL+"/half.png")
	if err != nil {
		t.Fatal(err)
	}
	n, err := io.Copy(io.Discard, body)
	body.Close()
	if !errors.Is(err, ErrStalled) || n != 400 {
		t.Fatalf("stalled mid-file: %d bytes, %v", n, err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("took %v", d)
	}
	// The slot was given back: another download can start.
	if _, _, err := c.File(context.Background(), srv.URL+"/silent.png"); !errors.Is(err, ErrStalled) {
		t.Fatalf("after: %v", err)
	}
}

// A file that only trickles in (a throttled connection: a little data now and then, never quite none) is given
// up too - less than the minimum in each idle limit - while a steady one gets through.
func TestFileTrickle(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk, every := 50, 20*time.Millisecond // trickle: 2.5 KB/s
		if r.URL.Path == "/steady.png" {
			chunk, every = 5000, 5*time.Millisecond // 1 MB/s
		}
		w.Header().Set("Content-Length", "100000")
		for sent := 0; sent < 100000; sent += chunk {
			if _, err := w.Write(make([]byte, chunk)); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			select {
			case <-time.After(every):
			case <-r.Context().Done():
				return
			}
		}
	}))
	defer srv.Close()
	m, _ := Parse([]byte("id: t\nengine: gelbooru-v02\nbase: " + srv.URL + "\n"))
	c := NewClient(m, nil)
	c.http = &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	c.SetFileIdle(200 * time.Millisecond)
	c.SetFileMin(10000) // 50 KB/s at least

	body, _, err := c.File(context.Background(), srv.URL+"/trickle.png")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = io.Copy(io.Discard, body)
	body.Close()
	if !errors.Is(err, ErrStalled) || time.Since(start) > 2*time.Second {
		t.Fatalf("trickle: %v after %v", err, time.Since(start))
	}
	body, _, err = c.File(context.Background(), srv.URL+"/steady.png")
	if err != nil {
		t.Fatal(err)
	}
	n, err := io.Copy(io.Discard, body)
	body.Close()
	if err != nil || n != 100000 {
		t.Fatalf("steady: %d bytes, %v", n, err)
	}
}

// A file that breaks off (a stall, or a cut connection) carries on where it stopped with a Range request, when
// the site takes them - the reader gets the whole file, unchanged. A site that ignores Range: given up as before
// (never a file pieced together from the wrong bytes).
func TestFileResumes(t *testing.T) {
	file := make([]byte, 30000)
	for i := range file {
		file[i] = byte(i * 7)
	}
	var mu sync.Mutex
	asked := map[string][]string{} // path -> Range headers asked for
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked[r.URL.Path] = append(asked[r.URL.Path], r.Header.Get("Range"))
		tries := len(asked[r.URL.Path])
		mu.Unlock()
		from := 0
		if rg := r.Header.Get("Range"); rg != "" && r.URL.Path != "/norange.bin" {
			fmt.Sscanf(rg, "bytes=%d-", &from)
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", from, len(file)-1, len(file)))
			w.Header().Set("Content-Length", strconv.Itoa(len(file)-from))
			w.WriteHeader(http.StatusPartialContent)
		} else {
			w.Header().Set("Accept-Ranges", "bytes") // (norange.bin says so too, then ignores it)
			w.Header().Set("Content-Length", strconv.Itoa(len(file)))
		}
		// The first two answers break off after 10000 bytes more: one stalls, the other is cut.
		upTo := len(file)
		if tries <= 2 {
			upTo = min(len(file), from+10000)
		}
		w.Write(file[from:upTo])
		w.(http.Flusher).Flush()
		if upTo < len(file) {
			if tries == 1 {
				<-r.Context().Done() // stalls
			} else {
				panic(http.ErrAbortHandler) // cut
			}
		}
	}))
	defer srv.Close()
	m, _ := Parse([]byte("id: t\nengine: gelbooru-v02\nbase: " + srv.URL + "\n"))
	c := NewClient(m, nil)
	c.http = &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	c.SetFileIdle(200 * time.Millisecond)
	c.SetFileMin(1)

	body, size, err := c.File(context.Background(), srv.URL+"/file.bin")
	if err != nil || size != int64(len(file)) {
		t.Fatalf("%d %v", size, err)
	}
	got, err := io.ReadAll(body)
	body.Close()
	if err != nil || !bytes.Equal(got, file) {
		t.Fatalf("read %d of %d bytes, %v (asked %q)", len(got), len(file), err, asked["/file.bin"])
	}
	if r := asked["/file.bin"]; len(r) != 3 || r[1] != "bytes=10000-" || r[2] != "bytes=20000-" {
		t.Fatalf("asked %q", r)
	}

	body, _, err = c.File(context.Background(), srv.URL+"/norange.bin")
	if err != nil {
		t.Fatal(err)
	}
	got, err = io.ReadAll(body)
	body.Close()
	if !errors.Is(err, ErrStalled) || len(got) != 10000 {
		t.Fatalf("a site ignoring Range: %d bytes, %v", len(got), err)
	}
}

// A site whose copy of a file stops at the same byte every time (carrying on from there brings nothing): given up
// as cut short - a clear reason, not just another stall.
func TestFileCutShort(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		from := 0
		if rg := r.Header.Get("Range"); rg != "" {
			fmt.Sscanf(rg, "bytes=%d-", &from)
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-29999/30000", from))
			w.Header().Set("Content-Length", strconv.Itoa(30000-from))
			w.WriteHeader(http.StatusPartialContent)
			w.(http.Flusher).Flush() // (the answer comes; the bytes don't)
		} else {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", "30000")
		}
		if from < 10000 {
			w.Write(make([]byte, 10000-from))
			w.(http.Flusher).Flush()
		}
		<-r.Context().Done() // never more than 10000 bytes
	}))
	defer srv.Close()
	m, _ := Parse([]byte("id: t\nengine: gelbooru-v02\nbase: " + srv.URL + "\n"))
	c := NewClient(m, nil)
	c.http = &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	c.SetFileIdle(150 * time.Millisecond)
	c.SetFileMin(1)
	body, _, err := c.File(context.Background(), srv.URL+"/broken.bin")
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(body)
	body.Close()
	if !errors.Is(err, ErrCutShort) || len(got) != 10000 {
		t.Fatalf("%d bytes, %v", len(got), err)
	}
}

// A download someone waits for (a Retry) doesn't queue behind the files taking the site's connections: it gets
// one more (only one).
func TestUrgentFile(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow.bin" {
			w.Write([]byte("x"))
			w.(http.Flusher).Flush()
			select {
			case <-release:
			case <-r.Context().Done():
			}
			return
		}
		w.Write([]byte("quick"))
	}))
	defer srv.Close()
	defer close(release)
	m, _ := Parse([]byte("id: t\nengine: gelbooru-v02\nbase: " + srv.URL + "\nlimits: {connections: 1}\n"))
	c := NewClient(m, nil)
	c.http = &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	busy, _, err := c.File(context.Background(), srv.URL+"/slow.bin") // takes the one slot
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, _, err := c.File(ctx, srv.URL+"/quick.bin"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a normal download didn't wait: %v", err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	body, _, err := c.File(Urgent(ctx2), srv.URL+"/quick.bin")
	if err != nil {
		t.Fatalf("an urgent one waited: %v", err)
	}
	b, _ := io.ReadAll(body)
	body.Close()
	if string(b) != "quick" {
		t.Fatalf("%q", b)
	}
}

// Files get a connection each (no HTTP/2: one throttled connection slowed every file from a host) - also from a
// server that offers HTTP/2 (the handshake once agreed on it, and its answers couldn't be read).
func TestFilesOwnConnections(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, r.Proto)
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()
	http.Get(srv.URL) // (the default transport used once - as in the app - sets itself up to offer HTTP/2; copies took that)
	tr := newTransport()
	tr.TLSClientConfig.InsecureSkipVerify = true
	resp, err := (&http.Client{Transport: tr}).Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "HTTP/1.1" {
		t.Fatalf("spoke %q", body)
	}
	if c := NewClient(Manifest{}, nil); c.http.Transport == nil {
		t.Fatal("the client doesn't use it")
	}
}

// A site's OR search: a template with {tags} (the separator defaults to " ~ ").
func TestOrSearch(t *testing.T) {
	m, err := Parse([]byte("id: t\nengine: gelbooru-v02\nbase: https://site.test\nsearch: {or: \"( {tags} )\"}\n"))
	if err != nil || m.Search.Or != "( {tags} )" || m.Search.OrSep != " ~ " {
		t.Fatalf("%+v %v", m.Search, err)
	}
	if _, err := Parse([]byte("id: t\nengine: gelbooru-v02\nbase: https://site.test\nsearch: {or: \"( tags )\"}\n")); err == nil {
		t.Fatal("a template without {tags} was accepted")
	}
	if m, _ := Parse([]byte("id: t\nengine: gelbooru-v02\nbase: https://site.test\n")); m.Search.Or != "" {
		t.Fatal("no OR search by default")
	}
	if tmpl, _ := m.OrQuery(); tmpl != "( {tags} )" {
		t.Fatalf("the plugin's: %q", tmpl)
	}
	// The Mastodon engine's "either" hashtags, without the plugin saying so.
	mm, err := Parse([]byte("id: m\nengine: mastodon\nbase: https://masto.test\n"))
	if err != nil {
		t.Fatal(err)
	}
	if tmpl, sep := mm.OrQuery(); tmpl != "~{tags}" || sep != " ~" {
		t.Fatalf("mastodon: %q %q", tmpl, sep)
	}
}

// A file is fetched the way the site's pages link it (file_query: rule34's US cache had broken copies of some
// files under the bare URL only).
func TestFileFetchURL(t *testing.T) {
	m := Manifest{FileQuery: "{id}"}
	if u := m.FileFetchURL("https://img.example.test/images/1/abc.png", 42); u != "https://img.example.test/images/1/abc.png?42" {
		t.Fatal(u)
	}
	if u := m.FileFetchURL("https://img.example.test/abc.png?x=1", 42); u != "https://img.example.test/abc.png?x=1" {
		t.Fatalf("a query of its own stays: %s", u)
	}
	if u := (Manifest{}).FileFetchURL("https://img.example.test/abc.png", 42); u != "https://img.example.test/abc.png" {
		t.Fatalf("without file_query: %s", u)
	}
}

// A file's Last-Modified comes with its body (realbooru keeps it as the upload time).
func TestFileLastModified(t *testing.T) {
	at := time.Date(2026, 10, 21, 20, 4, 25, 0, time.UTC)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		if r.URL.Path == "/dated.jpg" {
			w.Header().Set("Last-Modified", at.Format(http.TimeFormat))
		}
		io.WriteString(w, "jpeg")
	}))
	defer srv.Close()
	m, _ := Parse([]byte("id: t\nengine: gelbooru-v02\nbase: " + srv.URL + "\n"))
	c := testClient(m, srv, nil)
	for path, want := range map[string]time.Time{"/dated.jpg": at, "/plain.jpg": {}} {
		body, _, err := c.File(context.Background(), srv.URL+path)
		if err != nil {
			t.Fatal(err)
		}
		got := body.(interface{ Modified() time.Time }).Modified()
		body.Close()
		if !got.Equal(want) {
			t.Fatalf("%s: modified %v, want %v", path, got, want)
		}
	}
}

// A request called off before its turn gives the turn back (a newer search doesn't wait behind one that never went).
func TestWaitTurnGivenBack(t *testing.T) {
	m := Manifest{Site: "site.test"}
	m.Limits.RequestsPerSec = 2
	c := NewClient(m, nil)
	ctx := context.Background()
	if err := c.waitTurn(ctx); err != nil { // (at once: the next turn in half a second)
		t.Fatal(err)
	}
	gone, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if err := c.waitTurn(gone); err == nil {
		t.Fatal("a called-off request still went")
	}
	start := time.Now()
	if err := c.waitTurn(ctx); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 600*time.Millisecond {
		t.Fatalf("waited %v: the called-off request's turn wasn't given back", d)
	}
}
