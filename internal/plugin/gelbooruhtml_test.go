package plugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/media"
)

// Made-up pages shaped like a Gelbooru 0.2 site's (list and post pages).
const htmlList = `<html><body><div class="content">
<div class="col thumb" id="s300"><a id="p300" href="https://site.test/index.php?page=post&amp;s=view&amp;id=300"><img src="https://site.test/thumbnails/aa/bb/thumbnail_00112233445566778899aabbccddeeff.jpg" title="x" alt="x"/></a></div>
<div class="col thumb" id="s299"><a id="p299" href="https://site.test/index.php?page=post&amp;s=view&amp;id=299"><img src="https://site.test/thumbnails/cc/dd/thumbnail_ffeeddccbbaa99887766554433221100.jpg" title="x" alt="x"/></a></div>
</div>
<div id="paginator"><a href="?page=post&amp;s=list&amp;tags=blue_sky&amp;pid=42">2</a><a href="?page=post&amp;s=list&amp;tags=blue_sky&amp;pid=84">&gt;&gt;</a></div>
</body></html>`

const htmlVideoPost = `<html><body>
<div id="tagLink"><span>Posted at Oct, 14 2026 by <a href="index.php?page=account&s=profile&id=1">someone</a></span>
<a class="artist" href="index.php?page=post&amp;s=list&amp;tags=some_artist">some artist</a><a class="copyright" href="index.php?page=post&amp;s=list&amp;tags=a_series">a series</a><a class="model" href="index.php?page=post&amp;s=list&amp;tags=jane_doe">jane doe</a><a class="metadata" href="index.php?page=post&amp;s=list&amp;tags=video">video</a><a class="tag-type-general" href="index.php?page=post&amp;s=list&amp;tags=blue_sky">blue sky</a><a class="tag-type-general" href="index.php?page=post&amp;s=list&amp;tags=ice%27cream">ice'cream</a>
</div>
<video style="width: 100%;" controls loop id="gelcomVideoPlayer">
<source src="https://site.test//images/aa/bb/00112233445566778899aabbccddeeff.mp4" type="video/mp4" />
<source src="https://site.test//images/aa/bb/00112233445566778899aabbccddeeff.webm" type="video/webm" />
</video>
<a style="color: #fff;" href="https://site.test//images/aa/bb/00112233445566778899aabbccddeeff.webm">Original</a>
<form><textarea id="tags" name="tags">blue_sky</textarea>
<input type="text" name="source" size="40" id="source" style="width: 100%;" value="https://example.test/post/1 https://example.test/post/2" /></form>
</body></html>`

const htmlImagePost = `<html><body>
<div id="tagLink"><a class="tag-type-general" href="index.php?page=post&amp;s=list&amp;tags=red">red</a></div>
<img style="max-width: 100%;" alt="x" src="https://site.test//samples/aa/bb/sample_ffeeddccbbaa99887766554433221100.jpg" id="image" onclick="javascript:; return false;"/>
<a href="https://site.test//images/cc/dd/ffeeddccbbaa99887766554433221100.png">Original</a>
<input type="text" name="source" id="source" value="" />
</body></html>`

func TestGelbooruHTML(t *testing.T) {
	var got []url.Values
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		got = append(got, q)
		switch {
		case q.Get("s") == "list":
			io.WriteString(w, htmlList)
		case q.Get("id") == "300":
			io.WriteString(w, htmlVideoPost)
		case q.Get("id") == "299":
			io.WriteString(w, htmlImagePost)
		default:
			io.WriteString(w, "<html><body>This post was deleted.</body></html>")
		}
	}))
	defer srv.Close()
	m, err := Parse([]byte(fmt.Sprintf(`
id: htmlsite
engine: gelbooru-v02-html
base: %s
post_url: https://site.test/index.php?page=post&s=view&id={id}
domains: [site.test]
limits: {requests_per_sec: 100}
ratings: {"": explicit}
tags: {types: page, namespaces: {artist: creator, copyright: series, character: character, model: model, metadata: meta}}
`, srv.URL)))
	if err != nil {
		t.Fatal(err)
	}
	if m.Site != "site.test" {
		t.Fatalf("site: %q", m.Site)
	}
	e, _ := NewEngine(testClient(m, srv, nil))

	page, err := e.Search(context.Background(), "blue_sky", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Posts) != 2 || page.Total != 86 {
		t.Fatalf("page: %d posts, total %d", len(page.Posts), page.Total)
	}
	p := page.Posts[0]
	if p.ID != 300 || !p.Partial || p.MD5[0] != 0x00 || p.MD5[15] != 0xff || p.Rating != media.RatingExplicit ||
		p.PostURL != "https://site.test/index.php?page=post&s=view&id=300" {
		t.Fatalf("list post: %+v", p)
	}
	e.Search(context.Background(), "blue_sky", 300)
	if q := got[1]; q.Get("tags") != "blue_sky id:<300" || q.Get("pid") != "0" {
		t.Fatalf("resume query: %v", q)
	}
	e.Search(context.Background(), "", 0)
	if q := got[2]; q.Get("tags") != "all" {
		t.Fatalf("empty search: %v", q)
	}

	// A video post: mp4 first, webm as the fallback; md5 from the original; tag types; sources; date.
	v, err := e.Lookup(context.Background(), 300)
	if err != nil || v == nil {
		t.Fatalf("lookup: %v %v", v, err)
	}
	if v.FileURL != "https://site.test/images/aa/bb/00112233445566778899aabbccddeeff.mp4" ||
		len(v.AltFileURLs) != 1 || !strings.HasSuffix(v.AltFileURLs[0], ".webm") || v.MD5[15] != 0xff {
		t.Fatalf("video files: %q %q", v.FileURL, v.AltFileURLs)
	}
	want := []string{"some_artist", "a_series", "jane_doe", "video", "blue_sky", "ice'cream"}
	if strings.Join(v.Tags, " ") != strings.Join(want, " ") {
		t.Fatalf("tags: %q", v.Tags)
	}
	if v.TagTypes["some_artist"] != "artist" || v.TagTypes["jane_doe"] != "model" || v.TagTypes["blue_sky"] != "general" {
		t.Fatalf("types: %v", v.TagTypes)
	}
	if m.Namespace(v.TagTypes["jane_doe"]) != "model" || m.Namespace(v.TagTypes["video"]) != "meta" {
		t.Fatalf("namespaces")
	}
	if len(v.Sources) != 2 || !v.Posted.Equal(time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("sources %q, posted %v", v.Sources, v.Posted)
	}

	// An image post: the original first; the image shown on the page as the fallback (the Original
	// link sometimes answers 404).
	i, _ := e.Lookup(context.Background(), 299)
	if i == nil || i.FileURL != "https://site.test/images/cc/dd/ffeeddccbbaa99887766554433221100.png" || len(i.Sources) != 0 ||
		len(i.AltFileURLs) != 1 || i.AltFileURLs[0] != "https://site.test/samples/aa/bb/sample_ffeeddccbbaa99887766554433221100.jpg" {
		t.Fatalf("image post: %+v", i)
	}
	// A removed post.
	if gone, err := e.Lookup(context.Background(), 1); gone != nil || err != nil {
		t.Fatalf("removed post: %+v %v", gone, err)
	}
}

// Signed in through a Gelbooru 0.2 site's login form (login: gelbooru): the session cookies are kept, fav:me is that
// user's favorites; a refused user name and password says so.
func TestGelbooruHTMLLogin(t *testing.T) {
	logins := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case q.Get("page") == "account" && q.Get("s") == "login":
			logins++
			r.ParseForm()
			if r.PostForm.Get("user") == "someone" && r.PostForm.Get("pass") == "secret" {
				http.SetCookie(w, &http.Cookie{Name: "user_id", Value: "77", Path: "/"})
				http.SetCookie(w, &http.Cookie{Name: "pass_hash", Value: "h", Path: "/"})
			}
			io.WriteString(w, "<html>ok</html>")
		case q.Get("page") == "favorites":
			if q.Get("id") != "77" {
				http.Error(w, "wrong user", http.StatusBadRequest)
				return
			}
			io.WriteString(w, `<html><a href="index.php?page=post&amp;s=view&amp;id=5" id="p5"><img src="/thumbnails/1/thumbnail_0123456789abcdef0123456789abcdef.jpg?5"/></a></html>`)
		default:
			io.WriteString(w, "<html></html>")
		}
	}))
	defer srv.Close()
	m, err := Parse([]byte("id: t\nengine: gelbooru-v02-html\nbase: " + srv.URL + "\npost_url: " + srv.URL + "/p/{id}\nauth: {fields: [username, password], login: gelbooru}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if by, me := FavoritesBy(m, map[string]bool{"username": true, "password": true}); by != "id" || !me {
		t.Fatalf("favorites by %q, fav:me %v", by, me)
	}
	e, _ := NewEngine(testClient(m, srv, map[string]string{"username": "someone", "password": "secret"}))
	page, err := e.Search(context.Background(), "fav:me", 0)
	if err != nil || len(page.Posts) != 1 || page.Posts[0].ID != 5 {
		t.Fatalf("fav:me: %+v, %v", page.Posts, err)
	}
	e.Search(context.Background(), "blue_sky", 0)
	if logins != 1 {
		t.Fatalf("signed in %d times, want once (the session is kept)", logins)
	}
	bad, _ := NewEngine(testClient(m, srv, map[string]string{"username": "someone", "password": "wrong"}))
	if _, err := bad.Search(context.Background(), "blue_sky", 0); !errors.Is(err, ErrLoginRefused) {
		t.Fatalf("a wrong password: %v", err)
	}
}
