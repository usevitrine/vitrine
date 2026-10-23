package plugin

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	shmMD5a = "0123456789abcdef0123456789abcdef"
	shmMD5b = "fedcba9876543210fedcba9876543210"
)

// A Shimmie2 list page (the parts the engine reads), newest first.
const shmList = `<html><body><section id='image-list'>
<a href='/post/view/202' class='thumb shm-thumb shm-thumb-link ' data-tags='blue_sky some_artist' data-height='720' data-width='1280' data-mime='video/mp4' data-post-id='202'><img id='thumb_202' title='Blue_Sky Some_Artist // 1280x720 // 2.1MB // mp4' alt='' src='/_thumbs/` + shmMD5a + `/thumb.jpg' /></a>
<a href='/post/view/201' class='thumb shm-thumb shm-thumb-link ' data-tags='' data-height='800' data-width='600' data-mime='image/png' data-post-id='201'><img id='thumb_201' title='' alt='' src='/_thumbs/` + shmMD5b + `/thumb.jpg' /></a>
</section><section id='paginator'><a href='/post/list/blue_sky+order%3Did_desc/1'>1</a> <a href='/post/list/blue_sky+order%3Did_desc/7'>Last</a></section></body></html>`

// A Shimmie2 video post page.
const shmVideoPage = `<html><body>
<video id="video_202" playsinline><source src="https://SITE/_images/` + shmMD5a + `/202%20-%20Blue_Sky%20Some_Artist.mp4" type="video/mp4"></video>
<section id='Tagsleft'><table class="tag_list sortable"><tbody>
<tr><td class="tag_name_cell"> <a class="tag_name" href="/post/list/Blue_Sky/1">Blue Sky</a></td></tr>
<tr><td class="tag_name_cell"> <a class="tag_name" href="/post/list/Some_Artist/1">Some Artist</a></td></tr>
</tbody></table></section>
<form action='/_images/` + shmMD5a + `/202%20-%20Blue_Sky%20Some_Artist.mp4' method='GET'><input type='submit' value='Download'></form>
<table class='image_info form'>
<tr><th>Uploader</th><td><a class='username' href='/user/someone'>someone</a>, <time datetime='2026-10-05T22:55:30+00:00'>October 5, 2026; 22:55</time></td></tr>
<tr><th>Source</th><td><div><a href='https://example.test/some_artist/1'>example.test/some_artist/1</a></div></td></tr>
<tr><th>Rating</th><td> Safe </td></tr>
</table></body></html>`

// A Shimmie2 image post page without the Download form (only the main image).
const shmImagePage = `<html><body>
<img alt='main image' class='shm-main-image' id='main_image' src='/_images/` + shmMD5b + `/201%20-%20tagme.png' data-width='600'>
<table class='image_info form'><tr><th>Source</th><td></td></tr><tr><th>Rating</th><td> Unrated </td></tr></table>
</body></html>`

// fakeShimmie is a Shimmie2 site with a login (some_user / right).
func fakeShimmie(t *testing.T) (Manifest, *httptest.Server, *[]string, *atomic.Int32) {
	t.Helper()
	var lists []string
	var logins atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.EscapedPath()
		switch {
		case path == "/user_admin/login" && r.Method == http.MethodPost:
			logins.Add(1)
			r.ParseForm()
			if r.Form.Get("user") == "some_user" && r.Form.Get("pass") == "right" {
				http.SetCookie(w, &http.Cookie{Name: "shm_user", Value: "some_user", Path: "/"})
				http.SetCookie(w, &http.Cookie{Name: "shm_session", Value: "s3ss10n", Path: "/"})
				http.Redirect(w, r, "/user/some_user", http.StatusFound)
				return
			}
			io.WriteString(w, `<html><form action='/user_admin/login' method='POST'>No user with those details</form></html>`)
		case strings.HasPrefix(path, "/user/"):
			io.WriteString(w, `<html><a href='/user_admin/logout'>Log Out</a></html>`)
		case strings.HasPrefix(path, "/post/list/"):
			if strings.Contains(r.URL.RawQuery, "right") || strings.Contains(path, "right") {
				t.Errorf("the password went into a list URL: %s", r.URL)
			}
			ck, _ := r.Cookie("shm_session")
			lists = append(lists, path+" session="+map[bool]string{true: "yes", false: "no"}[ck != nil])
			if strings.Contains(path, "nothing_here") {
				w.WriteHeader(http.StatusNotFound)
				io.WriteString(w, `<html><title>No Images Found</title></html>`)
				return
			}
			io.WriteString(w, shmList)
		case path == "/post/view/202":
			io.WriteString(w, strings.ReplaceAll(shmVideoPage, "https://SITE", "https://"+r.Host))
		case path == "/post/view/201":
			io.WriteString(w, shmImagePage)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	m, err := Parse([]byte(`
id: shimmie_test
engine: shimmie2
base: ` + srv.URL + `
post_url: ` + srv.URL + `/post/view/{id}
auth: {fields: [username, password], login: shimmie2}
limits: {requests_per_sec: 100}
ratings: {safe: general, questionable: questionable, explicit: explicit}
`))
	if err != nil {
		t.Fatal(err)
	}
	return m, srv, &lists, &logins
}

func TestShimmieSearch(t *testing.T) {
	m, srv, lists, _ := fakeShimmie(t)
	e, _ := NewEngine(testClient(m, srv, nil))
	ctx := context.Background()

	page, err := e.Search(ctx, " blue_sky -cloud ", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Posts) != 2 || page.Total != 14 {
		t.Fatalf("posts %d, total %d", len(page.Posts), page.Total)
	}
	p := page.Posts[0]
	if p.ID != 202 || !p.Partial || hex.EncodeToString(p.MD5[:]) != shmMD5a || strings.Join(p.Tags, " ") != "blue_sky some_artist" || p.Width != 1280 {
		t.Fatalf("first post %+v", p)
	}
	// Tags joined with +, newest first always asked for; resuming adds id<N.
	if _, err := e.Search(ctx, "blue_sky", 201); err != nil {
		t.Fatal(err)
	}
	want := []string{"/post/list/blue_sky+-cloud+order=id_desc/1", "/post/list/blue_sky+id%3C201+order=id_desc/1"}
	for i, w := range want {
		if got := strings.Fields((*lists)[i])[0]; got != w {
			t.Errorf("list URL %d = %s, want %s", i, got, w)
		}
	}
	// A search with its own order keeps it; an empty result is a 404 page.
	e.Search(ctx, "blue_sky order=score_desc", 0)
	if got := strings.Fields((*lists)[2])[0]; got != "/post/list/blue_sky+order=score_desc/1" {
		t.Errorf("own order: %s", got)
	}
	if page, err := e.Search(ctx, "nothing_here", 0); err != nil || page.Total != 0 || len(page.Posts) != 0 {
		t.Fatalf("no posts: %+v %v", page, err)
	}
}

func TestShimmieLookup(t *testing.T) {
	m, srv, _, _ := fakeShimmie(t)
	e, _ := NewEngine(testClient(m, srv, nil))
	ctx := context.Background()

	p, err := e.Lookup(ctx, 202)
	if err != nil || p == nil {
		t.Fatalf("lookup: %+v %v", p, err)
	}
	if !strings.HasSuffix(p.FileURL, "/_images/"+shmMD5a+"/202%20-%20Blue_Sky%20Some_Artist.mp4") || hex.EncodeToString(p.MD5[:]) != shmMD5a {
		t.Errorf("file %s md5 %x", p.FileURL, p.MD5)
	}
	if strings.Join(p.Tags, " ") != "blue_sky some_artist" {
		t.Errorf("tags %v", p.Tags)
	}
	if len(p.Sources) != 1 || p.Sources[0] != "https://example.test/some_artist/1" {
		t.Errorf("sources %v", p.Sources)
	}
	if p.Rating != m.Rating("safe") || !p.Posted.Equal(time.Date(2026, 10, 5, 22, 55, 30, 0, time.UTC)) {
		t.Errorf("rating %v posted %v", p.Rating, p.Posted)
	}
	img, err := e.Lookup(ctx, 201)
	if err != nil || img == nil || !strings.HasSuffix(img.FileURL, "/_images/"+shmMD5b+"/201%20-%20tagme.png") || len(img.Sources) != 0 {
		t.Fatalf("image: %+v %v", img, err)
	}
	if gone, err := e.Lookup(ctx, 999); err != nil || gone != nil {
		t.Fatalf("missing post: %+v %v", gone, err)
	}
}

func TestShimmieLogin(t *testing.T) {
	m, srv, lists, logins := fakeShimmie(t)
	ctx := context.Background()

	// Logged in once, the session goes with every request.
	c := testClient(m, srv, map[string]string{"username": "some_user", "password": "right"})
	c.http.Jar = newSessionJar()
	e, _ := NewEngine(c)
	for range 3 {
		if _, err := e.Search(ctx, "blue_sky", 0); err != nil {
			t.Fatal(err)
		}
	}
	if logins.Load() != 1 {
		t.Fatalf("logged in %d times, want 1", logins.Load())
	}
	for _, l := range *lists {
		if !strings.HasSuffix(l, "session=yes") {
			t.Fatalf("a search without the session: %s", l)
		}
	}
	// New credentials: logged in again with them; wrong ones say so.
	c.SetCredentials(map[string]string{"username": "some_user", "password": "wrong"})
	_, err := e.Search(ctx, "blue_sky", 0)
	if !errors.Is(err, ErrLoginRefused) || strings.Contains(err.Error(), "wrong") {
		t.Fatalf("wrong password: %v", err)
	}
	// No credentials: searches as a guest.
	guest, _ := NewEngine(testClient(m, srv, nil))
	if _, err := guest.Search(ctx, "blue_sky", 0); err != nil {
		t.Fatal(err)
	}
}

func TestShimmieManifest(t *testing.T) {
	_, err := Parse([]byte("id: x\nengine: shimmie2\nbase: https://example.test\nauth: {fields: [username], login: shimmie2}\n"))
	if err == nil || !strings.Contains(err.Error(), "two fields") {
		t.Fatalf("one field: %v", err)
	}
	if _, err := Parse([]byte("id: x\nengine: shimmie2\nbase: https://example.test\nauth: {fields: [a, b], login: other}\n")); err == nil {
		t.Fatal("an unknown login passed")
	}
}

func TestDecaret(t *testing.T) {
	for in, want := range map[string]string{
		"lost_records:_bloom_^a_rage": "lost_records:_bloom_&_rage",
		"^^_^^":                       "^_^",
		"a^sb^qc^dd^be":               "a/b?c.d\\e",
		"no_carets":                   "no_carets",
		"odd^x":                       "odd^x",
		"end^":                        "end^",
	} {
		if got := decaret(in); got != want {
			t.Errorf("decaret(%q) = %q, want %q", in, got, want)
		}
	}
}
