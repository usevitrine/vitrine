package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeEH is a gallery site like e-hentai.org: a list (newest first, next=
// cursor), the gdata API, galleries with pages of thumbnails, picture pages,
// and a forum login (some_user / right).
type fakeEH struct {
	mu       sync.Mutex
	srv      *httptest.Server
	limit    bool     // picture pages serve the 509 picture
	banned   bool     // picture pages answer with the ban for loading too many pages
	cookies  []string // the ipb_member_id each list request carried
	apiCalls int      // gdata (and other API) requests
	mpv      bool     // the multi-page viewer is open to the account (else "eeenope")
	mpvCalls int
}

func newFakeEH(t *testing.T) (*fakeEH, Manifest) {
	t.Helper()
	f := &fakeEH{}
	gal := map[int64]string{300: "aaaaaaaaaa", 299: "bbbbbbbbbb", 250: "cccccccccc"}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "https://" + r.Host
		switch {
		case r.URL.Path == "/forums/index.php" && r.Method == http.MethodPost:
			r.ParseForm()
			if r.Form.Get("UserName") == "some_user" && r.Form.Get("PassWord") == "right" {
				http.SetCookie(w, &http.Cookie{Name: "ipb_member_id", Value: "42", Path: "/"})
				http.SetCookie(w, &http.Cookie{Name: "ipb_pass_hash", Value: "h4sh", Path: "/"})
			}
			io.WriteString(w, `<html>Thanks</html>`)
		case r.URL.Path == "/":
			ck, _ := r.Cookie("ipb_member_id")
			f.mu.Lock()
			if ck != nil {
				f.cookies = append(f.cookies, ck.Value)
			} else {
				f.cookies = append(f.cookies, "")
			}
			f.mu.Unlock()
			if r.URL.Query().Get("f_search") == "blocked" {
				w.WriteHeader(http.StatusUnavailableForLegalReasons)
				return
			}
			if r.URL.Query().Get("next") == "299" {
				fmt.Fprintf(w, `<html><p>Found about 1,234 results.</p><a href="%s/g/250/cccccccccc/">x</a></html>`, base)
				return
			}
			fmt.Fprintf(w, `<html><p>Found about 1,234 results.</p><a href="%[1]s/g/300/aaaaaaaaaa/">a</a><a href="%[1]s/g/300/aaaaaaaaaa/">a</a>
				<a href="%[1]s/g/299/bbbbbbbbbb/">b</a><a id="dnext" href="%[1]s/?next=299">next</a></html>`, base)
		case r.URL.Path == "/api.php":
			f.mu.Lock()
			f.apiCalls++
			f.mu.Unlock()
			var req struct {
				Gidlist [][]any `json:"gidlist"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			var out []map[string]any
			for _, g := range req.Gidlist {
				gid := int64(g[0].(float64))
				out = append(out, map[string]any{
					"gid": gid, "token": gal[gid], "title": fmt.Sprintf("Gallery %d &amp; more", gid), "title_jpn": "ギャラリー",
					"category": "Manga", "posted": "1700000000", "filecount": "3", "first_gid": "250",
					"tags": []string{"artist:some artist", "female:glasses", "language:english", "plain"},
				})
			}
			json.NewEncoder(w).Encode(map[string]any{"gmetadata": out})
		case strings.HasPrefix(r.URL.Path, "/mpv/"):
			f.mu.Lock()
			f.mpvCalls++
			open := f.mpv
			f.mu.Unlock()
			if !open || r.URL.Path != "/mpv/300/aaaaaaaaaa/" {
				io.WriteString(w, "eeenope")
				return
			}
			io.WriteString(w, `<html><script>var pagecount = 3;
var imagelist = [{"n":"01.jpg","k":"1111111111","t":"x"},{"n":"02.jpg","k":"2222222222","t":"x"},{"n":"03.jpg","k":"3333333333","t":"x"}];
</script></html>`)
		case strings.HasPrefix(r.URL.Path, "/g/"):
			if r.URL.Path == "/g/250/cccccccccc/" {
				fmt.Fprintf(w, `<html><div id="gnd">There are newer versions:<br /><a href="%[1]s/g/299/bbbbbbbbbb/">v2</a><br /><a href="%[1]s/g/300/aaaaaaaaaa/">v3</a></div></html>`, base)
				return
			}
			// Two thumbnails a page: pages 1-2, then 3.
			if r.URL.Query().Get("p") == "1" {
				fmt.Fprintf(w, `<html><a href="%s/s/3333333333/300-3"><img></a></html>`, base)
				return
			}
			// (the uploader's comment links pages too: a chapter index - never pages of their own)
			fmt.Fprintf(w, `<html><div id="gdt"><a href="%[1]s/s/1111111111/300-1"><img></a><a href="%[1]s/s/2222222222/300-2"><img></a></div>`+
				`<div id="cdiv">Chapters: <a href="%[1]s/s/3333333333/300-3">003</a> <a href="%[1]s/s/9999999999/300-99">099</a></div></html>`, base)
		case strings.HasPrefix(r.URL.Path, "/s/"):
			f.mu.Lock()
			limit, banned := f.limit, f.banned
			f.mu.Unlock()
			if banned {
				io.WriteString(w, "Your IP address has been temporarily banned for excessive pageloads which indicates that you are using automated mirroring/harvesting software. The ban expires in 59 minutes and 48 seconds")
				return
			}
			if limit {
				io.WriteString(w, `<html><img id="img" src="https://ehgt.org/g/509.gif" /></html>`)
				return
			}
			n := r.URL.Path[strings.LastIndex(r.URL.Path, "-")+1:]
			srv := "one"
			if r.URL.Query().Get("nl") != "" {
				srv = "two"
			}
			fmt.Fprintf(w, `<html><img id="img" src="%s/img/%s/%s.png" /><a href="#" onclick="return nl('r%s')">reload</a></html>`, base, srv, n, n)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	m, err := Parse([]byte(`
id: eh_test
engine: ehentai
base: ` + f.srv.URL + `
post_url: ` + f.srv.URL + `/g/{id}/
auth: {fields: [username, password, ipb_member_id, ipb_pass_hash], login: ehentai}
limits: {requests_per_sec: 100}
tags: {namespaces: {artist: creator, parody: series, character: character}}
`))
	if err != nil {
		t.Fatal(err)
	}
	return f, m
}

func TestEHentaiSearch(t *testing.T) {
	f, m := newFakeEH(t)
	e, _ := NewEngine(testClient(m, f.srv, nil))
	ctx := context.Background()
	page, err := e.Search(ctx, "some artist", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Posts) != 2 || page.Total != 1234 || page.Posts[0].ID != 300 || page.Posts[1].ID != 299 {
		t.Fatalf("page: %d posts, total %d: %+v", len(page.Posts), page.Total, page.Posts)
	}
	p := page.Posts[0]
	g := p.Gallery
	if g == nil || g.Title != "Gallery 300 & more" || g.TitleAlt != "ギャラリー" || g.Pages != 3 || g.Key != "250" || g.Category != "Manga" || g.Token != "aaaaaaaaaa" {
		t.Fatalf("gallery: %+v", g)
	}
	if strings.Join(p.Tags, " ") != "some_artist glasses english plain" || p.TagTypes["some_artist"] != "artist" || p.TagTypes["glasses"] != "female" {
		t.Fatalf("tags %v types %v", p.Tags, p.TagTypes)
	}
	if len(g.SiteTags) != 4 || g.SiteTags[1] != (SiteTag{"female", "glasses"}) || g.SiteTags[3] != (SiteTag{"", "plain"}) {
		t.Fatalf("site tags %+v", g.SiteTags)
	}
	// The same name in two namespaces: both kept with their own (TagTypes, by name, can't).
	two := (&ehentai{c: e.(*ehentai).c}).toPost(ehMeta{GID: 1, Token: "aaaaaaaaaa", Files: "1", Tags: []string{"female:glasses", "male:glasses"}})
	if len(two.Gallery.SiteTags) != 2 || two.Gallery.SiteTags[0].NS != "female" || two.Gallery.SiteTags[1].NS != "male" {
		t.Fatalf("two namespaces: %+v", two.Gallery.SiteTags)
	}
	if p.Posted.Unix() != 1700000000 || !strings.HasSuffix(p.PostURL, "/g/300/aaaaaaaaaa/") {
		t.Fatalf("posted %v, url %s", p.Posted, p.PostURL)
	}
	// The next page: older galleries.
	older, err := e.Search(ctx, "some artist", 299)
	if err != nil || len(older.Posts) != 1 || older.Posts[0].ID != 250 {
		t.Fatalf("older: %+v %v", older, err)
	}
	// One gallery by its link: its newest version.
	one, err := e.Search(ctx, f.srv.URL+"/g/250/cccccccccc/", 0)
	if err != nil || len(one.Posts) != 1 || one.Posts[0].ID != 300 {
		t.Fatalf("by link: %+v %v", one, err)
	}
	if more, _ := e.Search(ctx, f.srv.URL+"/g/250/cccccccccc/", 300); len(more.Posts) != 0 {
		t.Fatal("a gallery link has a second page")
	}
	// A blocked country is explained.
	if _, err := e.Search(ctx, "blocked", 0); !errors.Is(err, ErrRegionBlocked) {
		t.Fatalf("blocked: %v", err)
	}
}

func TestEHentaiPages(t *testing.T) {
	f, m := newFakeEH(t)
	e, _ := NewEngine(testClient(m, f.srv, nil))
	ge := e.(GalleryEngine)
	ctx := context.Background()
	listed := func(from int) ([]string, []int) {
		t.Helper()
		var pages []string
		var at []int
		err := ge.GalleryPages(ctx, 300, &Gallery{Token: "aaaaaaaaaa", Pages: 3}, from, func(i int, u, key string) error {
			if !strings.Contains(u, "/s/"+key+"/") {
				t.Fatalf("page %d: key %q, link %s", i, key, u)
			}
			pages, at = append(pages, u), append(at, i)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return pages, at
	}
	pages, at := listed(0)
	if len(pages) != 3 || !strings.HasSuffix(pages[2], "/s/3333333333/300-3") || at[2] != 2 {
		t.Fatalf("pages %v %v", pages, at)
	}
	// From page 3 on (the second page of thumbnails): only it.
	if later, at := listed(2); len(later) != 1 || at[0] != 2 || later[0] != pages[2] {
		t.Fatalf("from page 3: %v %v", later, at)
	}
	img, reload, err := ge.PageImage(ctx, pages[1], "")
	if err != nil || !strings.HasSuffix(img, "/img/one/2.png") || reload != "r2" {
		t.Fatalf("picture %s %s %v", img, reload, err)
	}
	// Another server when one fails.
	if img, _, _ := ge.PageImage(ctx, pages[1], reload); !strings.HasSuffix(img, "/img/two/2.png") {
		t.Fatalf("reloaded picture %s", img)
	}
	// The image limit.
	f.mu.Lock()
	f.limit = true
	f.mu.Unlock()
	if _, _, err := ge.PageImage(ctx, pages[0], ""); !errors.Is(err, ErrImageLimit) {
		t.Fatalf("limit: %v", err)
	}
	// A ban for loading pages too fast (a plain-text answer): said so, with how long.
	f.mu.Lock()
	f.limit, f.banned = false, true
	f.mu.Unlock()
	if _, _, err := ge.PageImage(ctx, pages[0], ""); !errors.Is(err, ErrBanned) || !strings.Contains(err.Error(), "59 minutes") {
		t.Fatalf("ban: %v", err)
	}
}

// The multi-page viewer lists every page in one request (an account with its Hath Perk); refused, the thumbnail
// pages do it - and it isn't asked again for a while.
func TestEHentaiMPV(t *testing.T) {
	f, m := newFakeEH(t)
	ctx := context.Background()
	list := func(ge GalleryEngine, from int) []string {
		t.Helper()
		var keys []string
		err := ge.GalleryPages(ctx, 300, &Gallery{Token: "aaaaaaaaaa", Pages: 3}, from, func(i int, u, key string) error {
			if !strings.HasSuffix(u, fmt.Sprintf("/s/%s/300-%d", key, i+1)) {
				t.Fatalf("page %d: %s (key %s)", i, u, key)
			}
			keys = append(keys, key)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return keys
	}
	creds := map[string]string{"ipb_member_id": "77", "ipb_pass_hash": "x"}
	f.mu.Lock()
	f.mpv = true
	f.mu.Unlock()
	e, _ := NewEngine(testClient(m, f.srv, creds))
	if keys := list(e.(GalleryEngine), 1); strings.Join(keys, ",") != "2222222222,3333333333" {
		t.Fatalf("from the viewer: %v", keys)
	}
	// Refused: the thumbnail pages, and the viewer isn't asked again.
	f.mu.Lock()
	f.mpv, f.mpvCalls = false, 0
	f.mu.Unlock()
	e2, _ := NewEngine(testClient(m, f.srv, creds))
	for range 2 {
		if keys := list(e2.(GalleryEngine), 0); strings.Join(keys, ",") != "1111111111,2222222222,3333333333" {
			t.Fatalf("from the thumbnails: %v", keys)
		}
	}
	f.mu.Lock()
	calls := f.mpvCalls
	f.mu.Unlock()
	if calls != 1 {
		t.Fatalf("the viewer asked %d times, want once", calls)
	}
	// Without an account it isn't tried.
	e3, _ := NewEngine(testClient(m, f.srv, nil))
	list(e3.(GalleryEngine), 0)
	if f.mpvCalls != 1 {
		t.Fatal("the viewer was asked without an account")
	}
}

func TestEHentaiLogin(t *testing.T) {
	f, m := newFakeEH(t)
	ctx := context.Background()
	// The forum login: its cookies go with the gallery pages.
	e, _ := NewEngine(testClient(m, f.srv, map[string]string{"username": "some_user", "password": "right"}))
	if _, err := e.Search(ctx, "x", 0); err != nil {
		t.Fatal(err)
	}
	// Pasted cookies work without a login.
	e2, _ := NewEngine(testClient(m, f.srv, map[string]string{"ipb_member_id": "77", "ipb_pass_hash": "x"}))
	if _, err := e2.Search(ctx, "x", 0); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	got := strings.Join(f.cookies, ",")
	f.mu.Unlock()
	if got != "42,77" {
		t.Fatalf("cookies sent: %q, want 42,77", got)
	}
	// A wrong password says so.
	e3, _ := NewEngine(testClient(m, f.srv, map[string]string{"username": "some_user", "password": "wrong"}))
	if _, err := e3.Search(ctx, "x", 0); !errors.Is(err, ErrLoginRefused) {
		t.Fatalf("wrong password: %v", err)
	}
}

// TestEHentaiLive asks e-hentai.org itself (VITRINE_LIVE=1): one gallery by its link, its pages, one picture's address.
func TestEHentaiLive(t *testing.T) {
	if os.Getenv("VITRINE_LIVE") == "" {
		t.Skip("VITRINE_LIVE not set")
	}
	m, err := Parse([]byte("id: e-hentai\nengine: ehentai\nbase: https://e-hentai.org\npost_url: https://e-hentai.org/g/{id}/\n" +
		"domains: [e-hentai.org, hath.network, ehgt.org]\nlimits: {requests_per_sec: 1, connections: 1}\n" +
		"auth: {fields: [username, password, ipb_member_id, ipb_pass_hash], login: ehentai}\n"))
	if err != nil {
		t.Fatal(err)
	}
	e, _ := NewEngine(NewClient(m, nil))
	ctx := context.Background()
	page, err := e.Search(ctx, os.Getenv("VITRINE_LIVE_GALLERY"), 0)
	if err != nil || len(page.Posts) != 1 {
		t.Fatalf("gallery: %+v %v", page, err)
	}
	p := page.Posts[0]
	t.Logf("gallery %d: %q, %d pages, key %s, %d tags (%d with a namespace)", p.ID, p.Gallery.Title, p.Gallery.Pages, p.Gallery.Key, len(p.Tags), len(p.TagTypes))
	var pages []string
	err = e.(GalleryEngine).GalleryPages(ctx, p.ID, p.Gallery, 0, func(_ int, u, _ string) error {
		pages = append(pages, u)
		return nil
	})
	if err != nil || len(pages) != p.Gallery.Pages {
		t.Fatalf("pages: %d of %d: %v", len(pages), p.Gallery.Pages, err)
	}
	img, reload, err := e.(GalleryEngine).PageImage(ctx, pages[0], "")
	if err != nil || img == "" || reload == "" {
		t.Fatalf("picture: %q %q %v", img, reload, err)
	}
	t.Logf("first picture from %s", strings.SplitN(strings.TrimPrefix(img, "https://"), "/", 2)[0])
}

// A gallery page's thumbnails as the site shows them (2026-10-22): parts of a sprite, each linking its picture page.
func TestGalleryThumbs(t *testing.T) {
	body := []byte(`<html><table class="ptt"><tr><td><a href="https://site.test/g/300/abcdef0123/" onclick="return false">1</a></td>` +
		`<td><a href="https://site.test/g/300/abcdef0123/?p=1" onclick="return false">2</a></td><td><a href="https://site.test/g/300/abcdef0123/?p=2" onclick="return false">3</a></td></tr></table>` +
		`<div id="gdt" class="gt200"><a href="https://site.test/s/40805a266a/300-1"><div title="Page 1: a.jpg" style="width:200px;height:282px;background:transparent url(https://img.site.test/c2/x/300-0.webp) -0px 0 no-repeat"></div></a>` +
		`<a href="https://site.test/s/3d47ca5878/300-2"><div title="Page 2: b.jpg" style="width:200px;height:294px;background:transparent url(https://img.site.test/c2/x/300-0.webp) -200px 0 no-repeat"></div></a></div>` +
		`<div id="cdiv"><a href="https://site.test/s/9999999999/300-7">a comment's link</a></div></html>`)
	thumbs, pages, err := parseThumbs("site.test", body)
	if err != nil {
		t.Fatal(err)
	}
	want := []GalleryThumb{
		{Page: 0, URL: "https://img.site.test/c2/x/300-0.webp", X: 0, Y: 0, W: 200, H: 282},
		{Page: 1, URL: "https://img.site.test/c2/x/300-0.webp", X: 200, Y: 0, W: 200, H: 294},
	}
	if pages != 3 || len(thumbs) != 2 || thumbs[0] != want[0] || thumbs[1] != want[1] {
		t.Fatalf("thumbs %+v, pages %d", thumbs, pages)
	}
	// Logged in: each box inside another, with the page number under it.
	in := []byte(`<html><div id="gdt" class="gt200"><a href="https://site.test/s/40805a266a/300-1"><div><div title="Page 1: a.jpg" style="width:200px;height:240px;background:transparent url(https://img.site.test/x.webp) -0px 0 no-repeat"></div><div>1</div></div></a>` +
		`<a href="https://site.test/s/3d47ca5878/300-2"><div><div title="Page 2: b.jpg" style="width:200px;height:240px;background:transparent url(https://img.site.test/x.webp) -200px 0 no-repeat"></div><div>2</div></div></a></div></html>`)
	if thumbs, _, err := parseThumbs("site.test", in); err != nil || len(thumbs) != 2 || thumbs[1].X != 200 || thumbs[1].Page != 1 {
		t.Fatalf("logged in: %+v %v", thumbs, err)
	}
	if _, _, err := parseThumbs("site.test", []byte(`<html><div id="gdt"></div></html>`)); err == nil {
		t.Fatal("no thumbnails: no error")
	}
}

// The API's tag suggestions: names starting with the text first, then the shorter.
func TestTagSuggest(t *testing.T) {
	body := []byte(`{"tags":{"1":{"ns":"female","tn":"light blue skin"},"2":{"ns":"female","tn":"blue skin"},"3":{"ns":"male","tn":"blue eyes"}}}`)
	got, err := parseTagSuggest("blue", body)
	if err != nil || len(got) != 3 || got[0] != (SiteTag{"female", "blue_skin"}) || got[1] != (SiteTag{"male", "blue_eyes"}) || got[2].Name != "light_blue_skin" {
		t.Fatalf("%+v %v", got, err)
	}
	if got, err := parseTagSuggest("zzz", []byte(`{"tags":[]}`)); err != nil || len(got) != 0 {
		t.Fatalf("none: %+v %v", got, err)
	}
}

// category:<name> terms become the site's category filter (the ones left out), the rest its search.
func TestEHentaiCategorySearch(t *testing.T) {
	v := ehSearch(`category:manga female:"blue sky$" category:artist_cg`)
	if v.Get("f_cats") != strconv.Itoa(1023-4-8) || v.Get("f_search") != `female:"blue sky$"` {
		t.Fatalf("%v", v)
	}
	if v := ehSearch(`female:"blue sky$"`); v.Has("f_cats") || v.Get("f_search") != `female:"blue sky$"` {
		t.Fatalf("no category: %v", v)
	}
	// The other filters: the advanced search's fields.
	v = ehSearch(`rating:4 pages:20-100 filter:expunged filter:torrent landscape`)
	if v.Get("f_srdd") != "4" || v.Get("f_spf") != "20" || v.Get("f_spt") != "100" || v.Get("f_sh") != "on" || v.Get("f_sto") != "on" ||
		v.Get("advsearch") != "1" || v.Get("f_search") != "landscape" {
		t.Fatalf("filters: %v", v)
	}
	if v := ehSearch(`pages:-50 rating:9`); v.Has("f_spf") || v.Get("f_spt") != "50" || v.Has("f_srdd") || v.Has("f_search") {
		t.Fatalf("open range, bad rating: %v", v)
	}
}

// Browse (a foreground request) needs no API call for galleries whose details it asked for lately; a download (in
// the background) always asks.
func TestEHentaiDetailsKept(t *testing.T) {
	f, m := newFakeEH(t)
	e, _ := NewEngine(testClient(m, f.srv, nil))
	calls := func() int {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.apiCalls
	}
	fg := Foreground(context.Background())
	if _, err := e.Search(fg, "some artist", 0); err != nil {
		t.Fatal(err)
	}
	first := calls()
	page, err := e.Search(fg, "some artist", 0)
	if err != nil || len(page.Posts) != 2 || page.Posts[0].ID != 300 {
		t.Fatalf("again: %+v %v", page.Posts, err)
	}
	if calls() != first {
		t.Fatalf("the same galleries asked for again: %d API calls, then %d", first, calls())
	}
	if _, err := e.Search(context.Background(), "some artist", 0); err != nil {
		t.Fatal(err)
	}
	if calls() == first {
		t.Fatal("a background search used kept details")
	}
}
