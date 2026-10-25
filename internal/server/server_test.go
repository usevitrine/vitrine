package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/image/webp"

	"github.com/usevitrine/vitrine/internal/auth"
	"github.com/usevitrine/vitrine/internal/backup"
	"github.com/usevitrine/vitrine/internal/db"
	"github.com/usevitrine/vitrine/internal/download"
	"github.com/usevitrine/vitrine/internal/favicon"
	"github.com/usevitrine/vitrine/internal/library"
	"github.com/usevitrine/vitrine/internal/media"
	"github.com/usevitrine/vitrine/internal/renditions"
	"github.com/usevitrine/vitrine/internal/store"
	"github.com/usevitrine/vitrine/internal/tag"
	"github.com/usevitrine/vitrine/internal/thumbs"
)

type testEnv struct {
	t   *testing.T
	lib *library.Library
	srv *httptest.Server
	c   *http.Client
	h   *Server
	dir string
	db  *db.DB
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "meta.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	s, _ := store.Open(filepath.Join(dir, "files"))
	th, err := thumbs.Open(ctx, filepath.Join(dir, "thumbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { th.Close() })
	lib, err := library.Open(ctx, library.Deps{DB: d, Store: s, Thumbs: th, Tools: media.FindTools("", "", "")})
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(lib, auth.New(d), log)
	rend, err := renditions.Open(filepath.Join(dir, "renditions"), 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	h.SetRenditions(rend)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	return &testEnv{t: t, lib: lib, srv: srv, c: &http.Client{Jar: jar}, h: h, dir: dir, db: d}
}

// call sends a request and decodes a JSON reply into out (if not nil).
func (e *testEnv) call(method, path string, body any, out any) int {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := e.c.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			e.t.Fatalf("%s %s: decoding reply: %v", method, path, err)
		}
	}
	return resp.StatusCode
}

func (e *testEnv) loginAs(name, password string) {
	e.t.Helper()
	if code := e.call("POST", "/api/setup", map[string]string{"name": name, "password": password}, nil); code != 200 {
		e.t.Fatalf("setup: %d", code)
	}
}

func pngBytes(t *testing.T, w, h int, seed byte) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = byte(i) ^ seed
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

func TestLoginFlow(t *testing.T) {
	e := newEnv(t)
	var sess map[string]any
	e.call("GET", "/api/session", nil, &sess)
	if sess["setup_needed"] != true || sess["logged_in"] != false {
		t.Fatalf("fresh session: %v", sess)
	}
	if code := e.call("GET", "/api/stats", nil, nil); code != 401 {
		t.Fatalf("stats without login: %d", code)
	}
	if code := e.call("GET", "/api/health", nil, nil); code != 200 {
		t.Fatalf("health: %d", code)
	}

	e.loginAs("me", "correct horse")
	e.call("GET", "/api/session", nil, &sess)
	if sess["logged_in"] != true || sess["user"] != "me" {
		t.Fatalf("after setup: %v", sess)
	}
	if code := e.call("POST", "/api/setup", map[string]string{"name": "x", "password": "yyyyyyyy"}, nil); code != 409 {
		t.Fatalf("second setup: %d", code)
	}
	if code := e.call("GET", "/api/stats", nil, nil); code != 200 {
		t.Fatalf("stats when logged in: %d", code)
	}

	e.call("POST", "/api/logout", struct{}{}, nil)
	if code := e.call("GET", "/api/stats", nil, nil); code != 401 {
		t.Fatalf("stats after logout: %d", code)
	}
	if code := e.call("POST", "/api/login", map[string]string{"name": "me", "password": "nope nope"}, nil); code != 401 {
		t.Fatalf("bad login: %d", code)
	}
	if code := e.call("POST", "/api/login", map[string]string{"name": "me", "password": "correct horse"}, nil); code != 200 {
		t.Fatalf("login: %d", code)
	}
	if code := e.call("GET", "/api/nope", nil, nil); code != 404 {
		t.Fatalf("unknown endpoint: %d", code)
	}
}

func TestCrossSiteRefused(t *testing.T) {
	e := newEnv(t)
	e.loginAs("me", "correct horse")
	// A form post (not JSON) is refused.
	resp, _ := e.c.Post(e.srv.URL+"/api/delete", "application/x-www-form-urlencoded", strings.NewReader("ids=1"))
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("form post: %d", resp.StatusCode)
	}
	// JSON from another origin is refused.
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/delete", strings.NewReader(`{"ids":[1]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	resp, _ = e.c.Do(req)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin post: %d", resp.StatusCode)
	}
}

// find=<ids>: where those files are in the search's list (the page keeps the open file and what's on screen by them
// when files arrive - in an order other than newest first, new ones land anywhere).
func TestSearchFind(t *testing.T) {
	e := newEnv(t)
	e.loginAs("me", "correct horse")
	ctx := context.Background()
	var ids []uint32
	for i := 0; i < 4; i++ {
		f, err := e.lib.Import(ctx, bytes.NewReader(pngBytes(t, 64+i, 48, byte(i))))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, f.ID)
	}
	var res struct {
		IDs   []uint32       `json:"ids"`
		Found map[string]int `json:"found"`
	}
	path := fmt.Sprintf("/api/search?q=sort:imported_asc&find=%d,%d,999999", ids[2], ids[0])
	if code := e.call("GET", path, nil, &res); code != 200 {
		t.Fatalf("search: %d", code)
	}
	if len(res.Found) != 2 || res.Found[fmt.Sprint(ids[2])] != 2 || res.Found[fmt.Sprint(ids[0])] != 0 {
		t.Fatalf("found %v in %v (want %d at 2, %d at 0; one not in it: left out)", res.Found, res.IDs, ids[2], ids[0])
	}
}

func TestSearchFilesTags(t *testing.T) {
	e := newEnv(t)
	e.loginAs("me", "correct horse")
	ctx := context.Background()
	var ids []uint32
	for i := 0; i < 5; i++ {
		f, err := e.lib.Import(ctx, bytes.NewReader(pngBytes(t, 64+i, 48, byte(i))))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, f.ID)
	}

	var edit map[string]int
	if code := e.call("POST", "/api/tags", map[string]any{"ids": ids[:3], "add": []string{"Blue_Sky", "red"}}, &edit); code != 200 {
		t.Fatalf("tag edit: %d", code)
	}
	e.call("POST", "/api/tags", map[string]any{"ids": ids[2:3], "remove": []string{"red"}}, nil)

	var res struct {
		Result  string   `json:"result"`
		Total   int      `json:"total"`
		IDs     []uint32 `json:"ids"`
		Unknown []string `json:"unknown"`
	}
	e.call("GET", "/api/search?q=blue_sky+red&limit=1", nil, &res)
	if res.Total != 2 || len(res.IDs) != 1 || res.IDs[0] != ids[1] {
		t.Fatalf("search: %+v", res)
	}
	var slice struct {
		IDs []uint32 `json:"ids"`
	}
	e.call("GET", "/api/results/"+res.Result+"?offset=1&limit=10", nil, &slice)
	if len(slice.IDs) != 1 || slice.IDs[0] != ids[0] {
		t.Fatalf("slice: %+v", slice)
	}
	if code := e.call("GET", "/api/results/nope", nil, nil); code != 404 {
		t.Fatalf("unknown result: %d", code)
	}
	// Dates for the fast scroller: sampled at evenly spaced positions.
	var dates struct {
		Total int     `json:"total"`
		Dates []int64 `json:"dates"`
	}
	// The masonry grid's shapes: every file's width/height x 1024, in result
	// order (known once probed; square before).
	probed := true
	for _, id := range ids {
		probed = probed && e.lib.ProcessMedia(ctx, id) == nil
	}
	var shaped struct {
		IDs    []uint32 `json:"ids"`
		Shapes []byte   `json:"shapes"` // (base64 in the JSON)
	}
	e.call("GET", "/api/search?q=sort:imported_asc&limit=10&shapes=1", nil, &shaped)
	if len(shaped.Shapes) != 2*5 {
		t.Fatalf("shapes: %+v", shaped)
	}
	for i := range 5 {
		got := int(shaped.Shapes[2*i]) | int(shaped.Shapes[2*i+1])<<8
		want := 1024
		if probed {
			want = (64 + i) * 1024 / 48
		}
		if got < want-1 || got > want+1 || shaped.IDs[i] != ids[i] {
			t.Fatalf("shape %d: %d, want %d", i, got, want)
		}
	}
	e.call("GET", "/api/search?q=&limit=0", nil, &res)
	if code := e.call("GET", "/api/results/"+res.Result+"/dates?n=3", nil, &dates); code != 200 ||
		dates.Total != 5 || len(dates.Dates) != 3 || dates.Dates[0] < dates.Dates[2] || dates.Dates[2] == 0 {
		t.Fatalf("dates: %d %+v", code, dates)
	}

	// New files since a result was taken (the grid's "N new files" pill).
	var snap struct {
		Now int64 `json:"now"`
	}
	e.call("GET", "/api/search?q=&limit=0", nil, &snap)
	var fresh struct {
		New int `json:"new"`
	}
	if e.call("GET", "/api/new?q=&since="+strconv.FormatInt(snap.Now, 10), nil, &fresh); fresh.New != 0 || snap.Now == 0 {
		t.Fatalf("new before importing: %+v %+v", snap, fresh)
	}
	time.Sleep(2 * time.Millisecond)
	extra, _ := e.lib.Import(ctx, bytes.NewReader(pngBytes(t, 99, 40, 9)))
	e.call("GET", "/api/new?q=&since="+strconv.FormatInt(snap.Now, 10), nil, &fresh)
	if fresh.New != 1 {
		t.Fatalf("new after importing: %+v", fresh)
	}
	e.call("GET", "/api/new?q=blue_sky&since="+strconv.FormatInt(snap.Now, 10), nil, &fresh)
	if fresh.New != 0 {
		t.Fatalf("new matching another search: %+v", fresh)
	}
	e.call("POST", "/api/delete", map[string]any{"ids": []uint32{extra.ID}}, nil)

	// Saved searches.
	var saved struct {
		ID    int64 `json:"id"`
		Saved []struct {
			Name  string `json:"name"`
			Count int    `json:"count"`
		} `json:"saved"`
	}
	if code := e.call("POST", "/api/saved", map[string]string{"name": "Sky", "query": "blue_sky"}, &saved); code != 200 ||
		len(saved.Saved) != 1 || saved.Saved[0].Count != 3 {
		t.Fatalf("save: %d %+v", code, saved)
	}
	if code := e.call("POST", "/api/saved", map[string]string{"query": "( broken"}, nil); code != 400 {
		t.Fatalf("saving a bad search: %d", code)
	}
	if code := e.call("POST", "/api/saved/"+strconv.FormatInt(saved.ID, 10)+"/remove", map[string]any{}, &saved); code != 200 || len(saved.Saved) != 0 {
		t.Fatalf("remove: %d %+v", code, saved)
	}
	if code := e.call("POST", "/api/saved/999/remove", map[string]any{}, nil); code != 404 {
		t.Fatalf("remove missing: %d", code)
	}
	if code := e.call("GET", "/api/search?q=(+broken", nil, nil); code != 400 {
		t.Fatalf("bad query: %d", code)
	}

	var ac struct {
		Suggestions []struct {
			Tag   string `json:"tag"`
			Count int    `json:"count"`
		} `json:"suggestions"`
	}
	e.call("GET", "/api/autocomplete?q=bl", nil, &ac)
	if len(ac.Suggestions) != 1 || ac.Suggestions[0].Count != 3 {
		t.Fatalf("autocomplete: %+v", ac)
	}

	var tags struct {
		Tags []struct {
			Tag     string   `json:"tag"`
			Origins []string `json:"origins"`
		} `json:"tags"`
	}
	e.call("GET", "/api/files/"+itoa(ids[0])+"/tags", nil, &tags)
	if len(tags.Tags) != 2 || tags.Tags[0].Tag != "blue_sky" || tags.Tags[0].Origins[0] != "user" {
		t.Fatalf("file tags: %+v", tags)
	}

	// Relations through the API, including a refused loop.
	var rel struct {
		Parents []library.Relation `json:"parents"`
	}
	if code := e.call("POST", "/api/relations", map[string]string{"kind": "parent", "action": "add", "from": "red", "to": "color"}, &rel); code != 200 || len(rel.Parents) != 1 {
		t.Fatalf("add parent: %d %+v", code, rel)
	}
	if code := e.call("POST", "/api/relations", map[string]string{"kind": "parent", "action": "add", "from": "color", "to": "red"}, nil); code != 400 {
		t.Fatalf("loop: %d", code)
	}
	e.call("GET", "/api/search?q=color", nil, &res)
	if res.Total != 2 {
		t.Fatalf("parent search via API: %+v", res)
	}

	// Delete and undo.
	e.call("POST", "/api/delete", map[string]any{"ids": ids[:1]}, nil)
	e.call("GET", "/api/search?q=", nil, &res)
	if res.Total != 4 {
		t.Fatalf("after delete: %d", res.Total)
	}
	e.call("POST", "/api/undelete", map[string]any{"ids": ids[:1]}, nil)
	e.call("GET", "/api/search?q=", nil, &res)
	if res.Total != 5 {
		t.Fatalf("after undelete: %d", res.Total)
	}
}

func TestServeFilesAndThumbs(t *testing.T) {
	e := newEnv(t)
	e.loginAs("me", "correct horse")
	ctx := context.Background()
	data := pngBytes(t, 300, 200, 9)
	f, err := e.lib.Import(ctx, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	hasTools := e.lib.ProcessMedia(ctx, f.ID) == nil

	var files struct {
		Files []fileJSON `json:"files"`
	}
	e.call("GET", "/api/files?ids="+itoa(f.ID)+",999", nil, &files)
	if len(files.Files) != 1 {
		t.Fatalf("files: %+v", files)
	}
	fj := files.Files[0]
	if fj.MIME != "image/png" || !strings.HasPrefix(fj.URL, "/files/") {
		t.Fatalf("file JSON: %+v", fj)
	}

	// The original, whole and as a range.
	resp, err := e.c.Get(e.srv.URL + fj.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !bytes.Equal(body, data) || resp.Header.Get("Content-Type") != "image/png" ||
		!strings.Contains(resp.Header.Get("Cache-Control"), "immutable") {
		t.Fatalf("original: %d, %d bytes, %v", resp.StatusCode, len(body), resp.Header)
	}
	req, _ := http.NewRequest("GET", e.srv.URL+fj.URL, nil)
	req.Header.Set("Range", "bytes=10-19")
	resp, _ = e.c.Do(req)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || !bytes.Equal(body, data[10:20]) {
		t.Fatalf("range: %d %q", resp.StatusCode, body)
	}

	// Without the session cookie: the signed link works (video loaders may
	// not send cookies), an unsigned or wrongly signed one doesn't.
	if !strings.Contains(fj.URL, "?k=") {
		t.Fatalf("file URL isn't signed: %s", fj.URL)
	}
	unsigned, _, _ := strings.Cut(fj.URL, "?")
	for path, want := range map[string]int{
		fj.URL:                       200,
		unsigned:                     401,
		unsigned + "?k=AAAAAAAAAAAA": 401,
	} {
		resp, _ = http.Get(e.srv.URL + path)
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("no cookie, %s: %d, want %d", path, resp.StatusCode, want)
		}
	}
	// Another file's signature doesn't open this one.
	other, _ := e.lib.Import(ctx, bytes.NewReader(pngBytes(t, 20, 20, 77)))
	e.call("GET", "/api/files?ids="+itoa(other.ID), nil, &files)
	_, otherSig, _ := strings.Cut(files.Files[0].URL, "?")
	resp, _ = http.Get(e.srv.URL + unsigned + "?" + otherSig)
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("another file's signature accepted: %d", resp.StatusCode)
	}

	if hasTools {
		if fj.Thumb == "" || fj.Width != 300 || len(fj.Placeholder) != 48 {
			t.Fatalf("thumb info: %+v", fj)
		}
		resp, _ = e.c.Get(e.srv.URL + fj.Thumb)
		body, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/webp" || len(body) == 0 ||
			!strings.Contains(resp.Header.Get("Cache-Control"), "immutable") {
			t.Fatalf("thumbnail: %d %v", resp.StatusCode, resp.Header)
		}
		// The URL names the file's fingerprint: one for another file (say, a
		// cached URL from an older library with the same IDs) isn't served.
		if !strings.Contains(fj.Thumb, "&f="+fj.SHA256[:16]) {
			t.Fatalf("thumb URL lacks the fingerprint: %s", fj.Thumb)
		}
		stale := fmt.Sprintf("/thumbs/%d.webp?v=1&f=0123456789abcdef", f.ID)
		resp, _ = e.c.Get(e.srv.URL + stale)
		resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Fatalf("thumbnail with another file's fingerprint: %d", resp.StatusCode)
		}
	}

	// A deleted file is still served (it's in the trash: viewable, restorable) until it's removed for good.
	e.lib.Delete(ctx, []uint32{f.ID}, "")
	resp, _ = e.c.Get(e.srv.URL + fj.URL)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("deleted file (in the trash): %d", resp.StatusCode)
	}
	if _, err := e.lib.PurgeFiles(ctx, []uint32{f.ID}); err != nil {
		t.Fatal(err)
	}
	resp, _ = e.c.Get(e.srv.URL + fj.URL)
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("purged file: %d", resp.StatusCode)
	}
}

func TestWebClientServed(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/", "/some/client/route"} {
		resp, err := http.Get(e.srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || !strings.Contains(string(body), "<title>") ||
			resp.Header.Get("Content-Security-Policy") == "" {
			t.Fatalf("%s: %d %q", p, resp.StatusCode, body)
		}
	}
}

// The app's code comes compressed (once, kept), and unpacks to exactly the file.
func TestAssetsGzipped(t *testing.T) {
	e := newEnv(t)
	resp, err := http.Get(e.srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	m := regexp.MustCompile(`src="(/assets/[^"]+\.js)"`).FindSubmatch(page)
	if m == nil {
		t.Skip("no built web app in this checkout")
	}
	plain := func() []byte {
		req, _ := http.NewRequest(http.MethodGet, e.srv.URL+string(m[1]), nil)
		req.Header.Set("Accept-Encoding", "identity")
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return b
	}()
	for range 2 { // (the second from the kept copy)
		req, _ := http.NewRequest(http.MethodGet, e.srv.URL+string(m[1]), nil)
		req.Header.Set("Accept-Encoding", "gzip")
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Header.Get("Content-Encoding") != "gzip" || !strings.Contains(resp.Header.Get("Content-Type"), "javascript") {
			t.Fatalf("headers: %v", resp.Header)
		}
		zr, err := gzip.NewReader(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(zr)
		resp.Body.Close()
		if !bytes.Equal(got, plain) || len(plain) == 0 {
			t.Fatalf("unpacked %d bytes, the file is %d", len(got), len(plain))
		}
	}
}

// The security headers: a strict content policy (no plugins, forms only to Vitrine itself), and HSTS only when the
// request came over HTTPS (directly or through a proxy that says so).
func TestSecurityHeaders(t *testing.T) {
	e := newEnv(t)
	get := func(proto string) http.Header {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, e.srv.URL+"/", nil)
		if proto != "" {
			req.Header.Set("X-Forwarded-Proto", proto)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.Header
	}
	h := get("")
	csp := h.Get("Content-Security-Policy")
	for _, want := range []string{"object-src 'none'", "form-action 'self'", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("policy %q lacks %q", csp, want)
		}
	}
	if h.Get("X-Frame-Options") != "DENY" || h.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("headers: %v", h)
	}
	if v := h.Get("Strict-Transport-Security"); v != "" {
		t.Errorf("HSTS over plain HTTP: %q", v)
	}
	if v := get("https").Get("Strict-Transport-Security"); !strings.HasPrefix(v, "max-age=") {
		t.Errorf("HSTS over HTTPS: %q", v)
	}
}

func itoa(n uint32) string { return strconv.FormatUint(uint64(n), 10) }

func TestRenditions(t *testing.T) {
	e := newEnv(t)
	e.loginAs("me", "correct horse")
	ctx := context.Background()
	big, err := e.lib.Import(ctx, bytes.NewReader(pngBytes(t, 3000, 1000, 1)))
	if err != nil {
		t.Fatal(err)
	}
	small, _ := e.lib.Import(ctx, bytes.NewReader(pngBytes(t, 300, 200, 2)))
	if err := e.lib.ProcessMedia(ctx, big.ID); err != nil {
		t.Skip("media tools needed:", err)
	}
	e.lib.ProcessMedia(ctx, small.ID)

	get := func(id uint32) (*http.Response, []byte) {
		var files struct {
			Files []fileJSON `json:"files"`
		}
		e.call("GET", "/api/files?ids="+itoa(id), nil, &files)
		resp, err := e.c.Get(e.srv.URL + files.Files[0].URL + "&size=2048") // the URL already has ?k=
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp, body
	}

	resp, body := get(big.ID)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/webp" {
		t.Fatalf("rendition: %d %v", resp.StatusCode, resp.Header)
	}
	cfg, err := webp.DecodeConfig(bytes.NewReader(body))
	if err != nil || cfg.Width != 2048 || cfg.Height != 683 {
		t.Fatalf("rendition is %dx%d (%v), want 2048x683", cfg.Width, cfg.Height, err)
	}

	// A small image comes back as the original.
	resp, _ = get(small.ID)
	if resp.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("small image: %v", resp.Header.Get("Content-Type"))
	}
}

// Copies ahead: only at sizes asked for to fill a screen, only for images
// bigger than that; zooming in (no for=screen) doesn't count.
func TestRenditionsAhead(t *testing.T) {
	e := newEnv(t)
	e.loginAs("me", "correct horse")
	ctx := context.Background()
	big, _ := e.lib.Import(ctx, bytes.NewReader(pngBytes(t, 3000, 1000, 1)))
	other, _ := e.lib.Import(ctx, bytes.NewReader(pngBytes(t, 2800, 900, 3)))
	mid, _ := e.lib.Import(ctx, bytes.NewReader(pngBytes(t, 2000, 900, 4)))
	if err := e.lib.ProcessMedia(ctx, big.ID); err != nil {
		t.Skip("media tools needed:", err)
	}
	e.lib.ProcessMedia(ctx, other.ID)
	e.lib.ProcessMedia(ctx, mid.ID)
	if n := e.h.makeRenditionsAhead(ctx); n != 0 {
		t.Fatalf("made %d with no screen sizes known", n)
	}

	var files struct {
		Files []fileJSON `json:"files"`
	}
	e.call("GET", "/api/files?ids="+itoa(big.ID), nil, &files)
	for _, q := range []string{"&size=2560", "&size=1600&for=screen"} { // a zoom, then a screen
		resp, err := e.c.Get(e.srv.URL + files.Files[0].URL + q)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	if got := e.h.rend.ScreenSizes(time.Hour); len(got) != 1 || got[0] != 1600 {
		t.Fatalf("screen sizes %v, want [1600]", got)
	}
	if n := e.h.makeRenditionsAhead(ctx); n != 2 { // other and mid (big has it already)
		t.Fatalf("made %d ahead, want 2", n)
	}
	for _, f := range []library.File{big, other, mid} {
		row, _ := e.lib.Files(ctx, []uint32{f.ID})
		if !e.h.rend.Has(row[0].SHA256.String(), 1600) {
			t.Errorf("file %d has no 1600 copy", f.ID)
		}
	}
	if n := e.h.makeRenditionsAhead(ctx); n != 0 {
		t.Fatalf("made %d on a second pass", n)
	}
}

// Backups: made, listed and downloaded; a raw upload needs our header (a
// cross-site page can't send one), a non-backup is refused, a backup is staged.
func TestBackupsAPI(t *testing.T) {
	e := newEnv(t)
	e.loginAs("me", "correct horse")
	restarted := make(chan struct{}, 1)
	e.h.SetBackups(backup.New(filepath.Join(e.dir, "meta.db"), filepath.Join(e.dir, "backups")), func() { restarted <- struct{}{} })

	var made struct{ Backup backup.Info }
	if code := e.call("POST", "/api/backups", map[string]any{}, &made); code != 200 || made.Backup.Name == "" {
		t.Fatalf("create: %d %+v", code, made)
	}
	var list struct {
		Backups []backup.Info
		Pending bool
	}
	e.call("GET", "/api/backups", nil, &list)
	if len(list.Backups) != 1 || list.Pending {
		t.Fatalf("list: %+v", list)
	}
	resp, err := e.c.Get(e.srv.URL + "/api/backups/" + made.Backup.Name + "/download")
	if err != nil {
		t.Fatal(err)
	}
	gz, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || len(gz) < 2 || gz[0] != 0x1f || gz[1] != 0x8b {
		t.Fatalf("download: %d, %d bytes", resp.StatusCode, len(gz))
	}
	if code := e.call("GET", "/api/backups/..%2Fmeta.db/download", nil, nil); code != 404 {
		t.Fatalf("a path got through: %d", code)
	}

	upload := func(body []byte, header bool) int {
		req, _ := http.NewRequest("POST", e.srv.URL+"/api/backups/upload", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/octet-stream")
		if header {
			req.Header.Set("X-Vitrine-Upload", "1")
		}
		resp, err := e.c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := upload(gz, false); code != http.StatusUnsupportedMediaType {
		t.Fatalf("upload without our header: %d", code)
	}
	if code := upload([]byte("not a backup"), true); code != http.StatusBadRequest {
		t.Fatalf("garbage: %d", code)
	}
	if code := upload(gz, true); code != 200 {
		t.Fatalf("upload: %d", code)
	}
	e.call("GET", "/api/backups", nil, &list)
	if !list.Pending {
		t.Fatal("not staged")
	}
	e.call("POST", "/api/backups/cancel-restore", map[string]any{}, nil)
	if code := e.call("POST", "/api/restart", map[string]any{}, nil); code != 200 {
		t.Fatalf("restart: %d", code)
	}
	select {
	case <-restarted:
	case <-time.After(2 * time.Second):
		t.Fatal("restart not called")
	}
}

// Text answers are gzipped when asked for; files and the event stream never are.
func TestGzip(t *testing.T) {
	e := newEnv(t)
	e.loginAs("me", "correct horse")
	get := func(path string) *http.Response {
		req, _ := http.NewRequest("GET", e.srv.URL+path, nil)
		req.Header.Set("Accept-Encoding", "gzip")
		tr := &http.Transport{DisableCompression: true} // see the raw answer
		c := &http.Client{Transport: tr, Jar: e.c.Jar}
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	resp := get("/api/search?q=")
	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("JSON not gzipped: %v", resp.Header)
	}
	zr, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.NewDecoder(zr).Decode(&out); err != nil {
		t.Fatalf("gzipped JSON: %v", err)
	}
	resp.Body.Close()
	if _, ok := out["result"]; !ok {
		t.Fatalf("reply: %v", out)
	}
	resp = get("/")
	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("the page not gzipped: %v", resp.Header)
	}
	resp.Body.Close()
	if resp := get("/files/nope"); resp.Header.Get("Content-Encoding") != "" {
		t.Fatalf("a file was gzipped")
	}
}

// FlareSolverr's address: saved, used at once, checked (saved even if it doesn't answer).
func TestFlareSolverrSetting(t *testing.T) {
	e := newEnv(t)
	e.loginAs("me", "correct horse")
	ctx := context.Background()
	dl, err := download.New(ctx, e.lib, e.db, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	e.h.SetDownloader(dl)
	flare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"msg":"FlareSolverr is ready!","version":"3.3.21"}`)
	}))
	defer flare.Close()

	var out map[string]string
	if code := e.call("POST", "/api/flaresolverr", map[string]string{"url": flare.URL + "/"}, &out); code != 200 || out["version"] != "3.3.21" || out["url"] != flare.URL {
		t.Fatalf("set: %d %v", code, out)
	}
	if dl.Solver.Endpoint() != flare.URL {
		t.Fatalf("solver endpoint %q", dl.Solver.Endpoint())
	}
	set, _ := e.lib.Settings(ctx)
	if string(set["flaresolverrURL"]) != `"`+flare.URL+`"` {
		t.Fatalf("stored %s", set["flaresolverrURL"])
	}
	if code := e.call("POST", "/api/flaresolverr", map[string]string{"url": "http://127.0.0.1:1"}, &out); code != 200 || out["error"] == "" {
		t.Fatalf("unreachable: %d %v", code, out)
	}
	if code := e.call("POST", "/api/flaresolverr", map[string]string{"url": "ftp://x"}, nil); code != 400 {
		t.Fatalf("bad address: %d", code)
	}
	if code := e.call("POST", "/api/flaresolverr", map[string]string{"url": ""}, &out); code != 200 || dl.Solver.Endpoint() != "" {
		t.Fatalf("off: %d %v", code, out)
	}
}

func TestFavicons(t *testing.T) {
	e := newEnv(t)
	icons, err := favicon.Open(filepath.Join(e.dir, "favicons"), "Vitrine-test")
	if err != nil {
		t.Fatal(err)
	}
	e.h.SetFavicons(icons)
	icon := pngBytes(t, 16, 16, 1)
	os.WriteFile(filepath.Join(e.dir, "favicons", "example.com.icon"), icon, 0o644) // kept from before: no fetch
	get := func(host string) *http.Response {
		resp, err := e.c.Get(e.srv.URL + "/api/favicon?host=" + host)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		return resp
	}
	if r := get("example.com"); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("logged out: %d", r.StatusCode)
	}
	e.loginAs("me", "correct horse")
	r := get("example.com")
	body, _ := io.ReadAll(r.Body)
	if r.StatusCode != 200 || r.Header.Get("Content-Type") != "image/png" || r.Header.Get("X-Content-Type-Options") != "nosniff" || !bytes.Equal(body, icon) {
		t.Fatalf("icon: %d %v", r.StatusCode, r.Header)
	}
	for _, h := range []string{"localhost", "127.0.0.1", "example.com:8080", ""} {
		if r := get(url.QueryEscape(h)); r.StatusCode != http.StatusNotFound || r.Header.Get("Cache-Control") == "" {
			t.Fatalf("%q: %d %v", h, r.StatusCode, r.Header)
		}
	}
}

func TestTagSites(t *testing.T) {
	e := newEnv(t)
	e.loginAs("me", "correct horse")
	ctx := context.Background()
	a, _ := e.lib.Site(ctx, "site-a")
	b, _ := e.lib.Site(ctx, "site-b")
	var ids []uint32
	for i := range 3 {
		f, err := e.lib.Import(ctx, bytes.NewReader(pngBytes(t, 64+i, 48, byte(i))))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, f.ID)
	}
	sky := []tag.Tag{{Name: "blue_sky"}}
	for _, st := range []struct{ file, site uint32 }{{ids[0], a}, {ids[1], b}, {ids[2], b}} {
		if err := e.lib.SetSiteTags(ctx, st.file, st.site, sky); err != nil {
			t.Fatal(err)
		}
	}
	e.call("POST", "/api/tags", map[string]any{"ids": ids[:1], "add": []string{"red"}}, nil) // the user's: no site

	var res struct {
		Tags map[string][]struct {
			Site  string `json:"site"`
			Count int    `json:"count"`
		} `json:"tags"`
	}
	if code := e.call("GET", "/api/tags/sites?t=blue_sky&t=red&t=nope", nil, &res); code != 200 {
		t.Fatalf("tag sites: %d", code)
	}
	got := res.Tags["blue_sky"]
	if len(got) != 2 || got[0].Site != "site-b" || got[0].Count != 2 || got[1].Site != "site-a" || got[1].Count != 1 {
		t.Fatalf("blue_sky: %+v", got)
	}
	if len(res.Tags["red"]) != 0 || res.Tags["nope"] == nil {
		t.Fatalf("red, nope: %+v", res.Tags)
	}
	if code := e.call("GET", "/api/tags/sites", nil, nil); code != 400 {
		t.Fatalf("no tags: %d", code)
	}
}

// Anonymous sign-in: signed out, every page is a plain sign-in form saying nothing about the software, and the app's
// other files answer 404; signing in through it works, and signed in the app is served as always.
func TestAnonymousSignIn(t *testing.T) {
	e := newEnv(t)
	e.loginAs("tester", "a-long-password")
	if code := e.call("POST", "/api/settings", map[string]any{"anonymousLogin": true}, nil); code != 200 {
		t.Fatalf("setting: %d", code)
	}
	fetch := func(c *http.Client, path string) (int, string, http.Header) {
		resp, err := c.Get(e.srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b), resp.Header
	}
	stranger := &http.Client{}
	for _, p := range []string{"/", "/signin", "/downloads", "/settings/viewing"} {
		code, body, h := fetch(stranger, p)
		if code != 200 || !strings.Contains(body, "<title>Sign in</title>") || !strings.Contains(body, "/_s/signin.js") {
			t.Fatalf("%s: %d\n%s", p, code, body)
		}
		if strings.Contains(strings.ToLower(body), "vitrine") || strings.Contains(body, "manifest") || strings.Contains(body, "/assets/") {
			t.Fatalf("%s gives the software away:\n%s", p, body)
		}
		if !strings.Contains(h.Get("Cache-Control"), "no-store") {
			t.Fatalf("%s may be kept: %q", p, h.Get("Cache-Control"))
		}
	}
	for _, p := range []string{"/_s/signin.js", "/_s/signin.css"} {
		if code, body, _ := fetch(stranger, p); code != 200 || strings.Contains(strings.ToLower(body), "vitrine") {
			t.Fatalf("%s: %d\n%s", p, code, body)
		}
	}
	for _, p := range []string{"/manifest.webmanifest", "/sw.js", "/favicon.ico", "/assets/index.js", "/startup.png?w=390&h=844&r=3"} {
		if code, _, _ := fetch(stranger, p); code != 404 {
			t.Fatalf("%s: %d, want 404", p, code)
		}
	}
	// What the app asks first says only that it's anonymous (it then goes to /signin).
	var sess map[string]any
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/session", nil)
	resp, err := stranger.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	json.NewDecoder(resp.Body).Decode(&sess)
	resp.Body.Close()
	if sess["anonymous"] != true || sess["logged_in"] != false {
		t.Fatalf("session: %v", sess)
	}
	// Signed in (the test's client has the cookie): the app as always.
	if code, body, _ := fetch(e.c, "/"); code != 200 || strings.Contains(body, "/_s/signin.js") {
		t.Fatalf("signed in: %d\n%s", code, body)
	}
	// Off again: the app's own page for everyone.
	e.call("POST", "/api/settings", map[string]any{"anonymousLogin": false}, nil)
	if _, body, _ := fetch(stranger, "/"); strings.Contains(body, "/_s/signin.js") {
		t.Fatal("still the plain page with the setting off")
	}
}

// The trash: deleted files searched by their tags in their own index (scope=trash), still viewable, restored one by
// one, removed for good one by one or all at once (their originals too); the library's search never sees them.
func TestTrash(t *testing.T) {
	e := newEnv(t)
	e.loginAs("me", "correct horse")
	ctx := context.Background()
	var ids []uint32
	var shas []string
	for i := 0; i < 4; i++ {
		f, err := e.lib.Import(ctx, bytes.NewReader(pngBytes(t, 30+i, 30, byte(40+i))))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, f.ID)
		shas = append(shas, hex.EncodeToString(f.SHA256[:]))
	}
	e.call("POST", "/api/tags", map[string]any{"ids": ids[:3], "add": []string{"trash_me"}}, nil)
	e.call("POST", "/api/delete", map[string]any{"ids": ids[:3]}, nil)
	type res struct {
		Total int      `json:"total"`
		IDs   []uint32 `json:"ids"`
	}
	var live, trash res
	e.call("GET", "/api/search?q=trash_me&limit=10", nil, &live)
	e.call("GET", "/api/search?q=trash_me&limit=10&scope=trash", nil, &trash)
	if live.Total != 0 || trash.Total != 3 {
		t.Fatalf("library %d, trash %d (want 0, 3)", live.Total, trash.Total)
	}
	var info struct{ Files, Days int }
	if e.call("GET", "/api/trash", nil, &info); info.Files != 3 || info.Days != 0 {
		t.Fatalf("trash info: %+v", info)
	}
	// A deleted file's original still opens (the Trash page's viewer).
	resp, err := e.c.Get(e.srv.URL + "/files/" + shas[0] + ".png")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("a deleted file's original: %d", resp.StatusCode)
	}
	// Restore one; remove one for good; then empty the trash.
	e.call("POST", "/api/undelete", map[string]any{"ids": ids[:1]}, nil)
	var p map[string]int
	if e.call("POST", "/api/trash/purge", map[string]any{"ids": []uint32{ids[1], ids[3]}}, &p); p["purged"] != 1 { // (ids[3] isn't deleted: left alone)
		t.Fatalf("purge: %v", p)
	}
	e.call("GET", "/api/search?q=trash_me&limit=10&scope=trash", nil, &trash)
	e.call("GET", "/api/search?q=trash_me&limit=10", nil, &live)
	if trash.Total != 1 || trash.IDs[0] != ids[2] || live.Total != 1 || live.IDs[0] != ids[0] {
		t.Fatalf("after restore + purge: trash %+v, library %+v", trash, live)
	}
	if e.call("POST", "/api/trash/empty", map[string]any{}, &p); p["purged"] != 1 {
		t.Fatalf("empty: %v", p)
	}
	if e.call("GET", "/api/trash", nil, &info); info.Files != 0 {
		t.Fatalf("emptied, still %d", info.Files)
	}
	// Gone for good: the original too, and no undo.
	resp, _ = e.c.Get(e.srv.URL + "/files/" + shas[2] + ".png")
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("a purged original: %d", resp.StatusCode)
	}
	if code := e.call("POST", "/api/undelete", map[string]any{"ids": ids[2:3]}, nil); code == 200 {
		t.Fatal("undid a purge")
	}
	// The setting: after how many days it empties itself.
	if code := e.call("POST", "/api/settings", map[string]any{"trashDays": 30}, nil); code != 200 {
		t.Fatalf("trashDays: %d", code)
	}
	if e.call("GET", "/api/trash", nil, &info); info.Days != 30 {
		t.Fatalf("days: %+v", info)
	}
}

// The sections: Media and Comics can't both be off; switching Media off with delete removes its files for good (in
// the background), then a notice says so.
func TestSectionOff(t *testing.T) {
	e := newEnv(t)
	e.loginAs("me", "correct horse")
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := e.lib.Import(ctx, bytes.NewReader(pngBytes(t, 20+i, 20, byte(60+i)))); err != nil {
			t.Fatal(err)
		}
	}
	var info map[string]struct{ Items int }
	if e.call("GET", "/api/sections", nil, &info); info["media"].Items != 3 {
		t.Fatalf("sections: %+v", info)
	}
	if code := e.call("POST", "/api/settings", map[string]any{"comicsOff": true}, nil); code != 200 {
		t.Fatalf("comics off: %d", code)
	}
	if code := e.call("POST", "/api/sections/media/off", map[string]any{"delete": true}, nil); code != 400 {
		t.Fatalf("both off: %d, want 400", code)
	}
	e.call("POST", "/api/settings", map[string]any{"comicsOff": false}, nil)
	var res map[string]any
	if code := e.call("POST", "/api/sections/media/off", map[string]any{"delete": true}, &res); code != 200 || res["deleting"] != true {
		t.Fatalf("media off: %d %v", code, res)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		e.call("GET", "/api/sections", nil, &info)
		var st struct{ Trash struct{ Files int } }
		e.call("GET", "/api/stats", nil, &st)
		if info["media"].Items == 0 && st.Trash.Files == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not deleted: %+v, trash %d", info, st.Trash.Files)
		}
		time.Sleep(50 * time.Millisecond)
	}
	var set map[string]any
	if e.call("GET", "/api/settings", nil, &set); set["mediaOff"] != true {
		t.Fatalf("settings: %v", set)
	}
	var nt struct {
		Notices []struct{ Text, Tone string }
	}
	deadline = time.Now().Add(5 * time.Second)
	for e.call("GET", "/api/notices", nil, &nt); len(nt.Notices) == 0 && time.Now().Before(deadline); e.call("GET", "/api/notices", nil, &nt) {
		time.Sleep(50 * time.Millisecond)
	}
	if len(nt.Notices) == 0 || nt.Notices[len(nt.Notices)-1].Tone != "removed" {
		t.Fatalf("no notice: %+v", nt.Notices)
	}
}

// /licenses: open to anyone, Vitrine's license and where its source is (the AGPL), and the notices of what's built in.
func TestLicenses(t *testing.T) {
	e := newEnv(t)
	resp, err := http.Get(e.srv.URL + "/licenses")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	text := string(body)
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") {
		t.Fatalf("%d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	for _, want := range []string{"GNU Affero General Public License", "Source code: " + SourceURL, "golang.org/x/crypto", "modernc.org/sqlite"} {
		// (the web app's licenses - Lucide, with its Feather notice; the font - come with the built web app, not here)
		if !strings.Contains(text, want) {
			t.Errorf("/licenses lacks %q", want)
		}
	}
}

// The notices name every module go.mod requires directly, at its version: a new or updated dependency needs
// `npm run notices` (web/scripts/go-notices.mjs) - this fails until it's run.
func TestLicenseNoticesCurrent(t *testing.T) {
	mod, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	inRequire := false
	for _, line := range strings.Split(string(mod), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "require (":
			inRequire = true
			continue
		case line == ")":
			inRequire = false
			continue
		}
		if !inRequire || strings.Contains(line, "// indirect") || line == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		if !strings.Contains(goNotices, f[0]+" "+f[1]) {
			t.Errorf("internal/server/licenses/go.txt lacks %s %s: run npm run notices in web/", f[0], f[1])
		}
	}
}

// GET /api/downloads?active=1: only what's going on now and the paused sites (the top bar's menu reads it on every
// change) - not the whole page.
func TestDownloadsActiveOnly(t *testing.T) {
	e := newEnv(t)
	e.loginAs("me", "correct horse")
	dl, err := download.New(context.Background(), e.lib, e.db, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	e.h.SetDownloader(dl)
	var all, active map[string]json.RawMessage
	if code := e.call("GET", "/api/downloads", nil, &all); code != 200 || all["subscriptions"] == nil || all["searches"] == nil {
		t.Fatalf("all: %d %v", code, all)
	}
	if code := e.call("GET", "/api/downloads?active=1", nil, &active); code != 200 || len(active) != 2 || string(active["searches"]) != "[]" || active["sites_paused"] == nil {
		t.Fatalf("active: %d %v", code, active)
	}
}
