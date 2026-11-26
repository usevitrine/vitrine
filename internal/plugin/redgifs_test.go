package plugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/usevitrine/vitrine/internal/media"
)

// fakeRedgifs serves 200 made-up clips (newest first, one a minute; clip i is "clip<letters>") to a user
// "some_artist" and the tag "Blue Sky" (in any case, as on the site), 80 a page, behind a temporary token.
func fakeRedgifs(t *testing.T) (Manifest, *httptest.Server, *[]*http.Request, *atomic.Int32) {
	t.Helper()
	var reqs []*http.Request
	var tokens atomic.Int32
	name := func(i int) string { // (letters only, like the site's IDs)
		s := ""
		for n := i; ; n /= 26 {
			s = string(rune('a'+n%26)) + s
			if n < 26 {
				break
			}
		}
		return "clip" + s
	}
	clip := func(srv *httptest.Server, i int) string {
		rare := "" // (every third clip, from the second: the site sends them for "blue sky,rare" with the others, loosely)
		if i%3 == 1 {
			rare = `,"Rare"`
		}
		return fmt.Sprintf(`{"id":%q,"createDate":%d,"type":1,"width":1080,"height":1920,"tags":["Blue Sky","Big  Smile","blue sky"%s],`+
			`"userName":"Some_Artist","urls":{"hd":"%s/%s.mp4","sd":"%s/%s-mobile.mp4"}}`, name(i), 1794900000-60*i, rare, srv.URL, name(i), srv.URL, name(i))
	}
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs = append(reqs, r)
		if r.URL.Path == "/v2/auth/temporary" {
			n := tokens.Add(1)
			fmt.Fprintf(w, `{"token":"tok%d","addr":"x","agent":"x"}`, n)
			return
		}
		if r.Header.Get("Authorization") != "Bearer tok"+strconv.Itoa(int(tokens.Load())) {
			w.WriteHeader(401)
			io.WriteString(w, `{"error":{"code":"BadTokenFormat","status":401}}`)
			return
		}
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/v2/gifs/"+name(3):
			io.WriteString(w, `{"gif":`+clip(srv, 3)+`,"user":{}}`)
		case strings.HasPrefix(r.URL.Path, "/v2/gifs/") && r.URL.Path != "/v2/gifs/search":
			w.WriteHeader(404)
			io.WriteString(w, `{"error":{"code":"GifNotFound","description":"Gif not found."}}`)
		case r.URL.Path == "/v2/users/nobody/search":
			w.WriteHeader(404)
			io.WriteString(w, `{"error":{"code":"UserNotFound","message":"User not found.","status":404}}`)
		case r.URL.Path == "/v2/users/some_artist/search" || (r.URL.Path == "/v2/gifs/search" && strings.HasPrefix(strings.ToLower(q.Get("tags")), "blue sky")):
			n, _ := strconv.Atoi(q.Get("page"))
			if n > 3 {
				w.WriteHeader(500)
				io.WriteString(w, `{"error":{"code":"Search","status":500}}`)
				return
			}
			var list []string
			for i := (n - 1) * 80; i < min(n*80, 200); i++ {
				list = append(list, clip(srv, i))
			}
			fmt.Fprintf(w, `{"gifs":[%s],"page":%d,"pages":3,"total":200}`, strings.Join(list, ","), n)
		default:
			io.WriteString(w, `{"gifs":[],"page":1,"pages":0,"total":0}`)
		}
	}))
	t.Cleanup(srv.Close)
	m, err := Parse([]byte(`
id: rgtest
engine: redgifs
base: ` + srv.URL + `
limits: {requests_per_sec: 1000}
ratings: {"": explicit}
tags: {types: post, namespaces: {artist: creator}}
`))
	if err != nil {
		t.Fatal(err)
	}
	return m, srv, &reqs, &tokens
}

func TestRedgifsSearch(t *testing.T) {
	m, srv, reqs, tokens := fakeRedgifs(t)
	e, _ := NewEngine(testClient(m, srv, nil))
	ctx := context.Background()
	page, err := e.Search(ctx, "@Some_Artist", 0)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 200 || len(page.Posts) != 80 {
		t.Fatalf("first page: %d posts of %d", len(page.Posts), page.Total)
	}
	p := page.Posts[0]
	if p.ID != redgifsID("clipa", 1794900000) || p.FileURL != srv.URL+"/clipa.mp4" || len(p.AltFileURLs) != 1 ||
		p.PostURL != "https://www.redgifs.com/watch/clipa" || p.Rating != media.RatingExplicit || p.Posted.Unix() != 1794900000 {
		t.Fatalf("post: %+v", p)
	}
	if strings.Join(p.Tags, " ") != "blue_sky big_smile some_artist" || p.TagTypes["some_artist"] != "artist" || p.TagTypes["big_smile"] != "general" {
		t.Fatalf("tags %v, types %v", p.Tags, p.TagTypes)
	}
	for i := 1; i < len(page.Posts); i++ {
		if page.Posts[i].ID >= page.Posts[i-1].ID {
			t.Fatalf("post %d's ID isn't below the one before", i)
		}
	}
	last := func(r *http.Request) http.Header { return r.Header }
	if h := last((*reqs)[len(*reqs)-1]); h.Get("Authorization") != "Bearer tok1" {
		t.Fatalf("no token sent: %v", h)
	}

	// The next page: remembered (one request), then the rest; the end is an empty page.
	n := len(*reqs)
	next, err := e.Search(ctx, "@Some_Artist", page.Posts[79].ID)
	if err != nil || len(next.Posts) != 80 || next.Posts[0].ID >= page.Posts[79].ID || len(*reqs) != n+1 {
		t.Fatalf("second page: %d posts, %v, %d requests", len(next.Posts), err, len(*reqs)-n)
	}
	third, _ := e.Search(ctx, "@Some_Artist", next.Posts[79].ID)
	if len(third.Posts) != 40 {
		t.Fatalf("third page: %d", len(third.Posts))
	}
	if end, err := e.Search(ctx, "@Some_Artist", third.Posts[39].ID); err != nil || len(end.Posts) != 0 {
		t.Fatalf("past the end: %+v %v", end, err)
	}

	// After a restart (nothing remembered): the page is found by halving, mid-page too.
	fresh, _ := NewEngine(testClient(m, srv, nil))
	mid, err := fresh.Search(ctx, "@Some_Artist", page.Posts[79].ID-1) // (just below clip 79: clip 80 on, page 2)
	if err != nil || len(mid.Posts) != 80 || mid.Posts[0].ID != next.Posts[0].ID {
		t.Fatalf("found page: %d posts, %v", len(mid.Posts), err)
	}
	inside, _ := fresh.Search(ctx, "@Some_Artist", next.Posts[10].ID) // (inside page 2: its last 69)
	if len(inside.Posts) != 69 || inside.Posts[0].ID != next.Posts[11].ID {
		t.Fatalf("inside a page: %d posts", len(inside.Posts))
	}

	// Tags: all of them (the site's tags=a,b), videos unless type:images.
	if page, err = e.Search(ctx, "blue_sky", 0); err != nil || len(page.Posts) != 80 {
		t.Fatalf("tag search: %d %v", len(page.Posts), err)
	}
	if q := (*reqs)[len(*reqs)-1].URL.Query(); q.Get("tags") != "blue sky" || q.Get("type") != "g" || q.Get("order") != "latest" {
		t.Fatalf("tag query: %v", q)
	}
	e.Search(ctx, "blue_sky big_smile type:images", 0)
	if q := (*reqs)[len(*reqs)-1].URL.Query(); q.Get("tags") != "blue sky,big smile" || q.Get("type") != "i" {
		t.Fatalf("two tags, images: %v", q)
	}
	// Past a tag search's last page the site answers 500: the end.
	if end, err := e.Search(ctx, "blue_sky", third.Posts[39].ID); err != nil || len(end.Posts) != 0 {
		t.Fatalf("past the last tag page: %+v %v", end, err)
	}

	// The site's search is loose (it sends every clip for "blue sky,rare", and for a tag it doesn't know): only clips
	// with every tag are kept, the site's count isn't, and a tag no clip has is said.
	rare, err := e.Search(ctx, "blue_sky rare", 0)
	if err != nil || len(rare.Posts) != 27 || rare.Total != -1 {
		t.Fatalf("loose results: %d posts of %d, %v", len(rare.Posts), rare.Total, err)
	}
	for _, p := range rare.Posts {
		if !slices.Contains(p.Tags, "rare") {
			t.Fatalf("a clip without the tag: %v", p.Tags)
		}
	}
	if more, _ := e.Search(ctx, "blue_sky rare", rare.Posts[26].ID); len(more.Posts) != 26 { // (clips 82 to 157)
		t.Fatalf("loose results, the next page: %d", len(more.Posts))
	}
	if _, err := e.Search(ctx, "blue_sky made_up", 0); err == nil || !strings.Contains(err.Error(), "no clips with all of blue sky, made up") {
		t.Fatalf("a made-up tag: %v", err)
	}

	// A clip's link: that clip.
	if page, err = e.Search(ctx, "https://www.redgifs.com/watch/clipd#x", 0); err != nil || len(page.Posts) != 1 || page.Posts[0].PostURL != "https://www.redgifs.com/watch/clipd" {
		t.Fatalf("a clip's link: %+v %v", page, err)
	}
	if page, err = e.Search(ctx, "https://www.redgifs.com/watch/goneclip", 0); err != nil || len(page.Posts) != 0 {
		t.Fatalf("a missing clip: %+v %v", page, err)
	}
	// A user's link is that user's clips.
	if page, _ = e.Search(ctx, "https://www.redgifs.com/users/some_artist", 0); len(page.Posts) != 80 {
		t.Fatalf("a user's link: %d", len(page.Posts))
	}

	// Refused: the site's own words.
	for q, want := range map[string]string{
		"@nobody":            "no user",
		"blue_sky -cloud":    "can't leave a tag out",
		"~a ~b":              "can't leave a tag out",
		"@some_artist cloud": "@user alone",
		"fav:me":             "sign-in",
	} {
		if _, err := e.Search(ctx, q, 0); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", q, err, want)
		}
	}

	// Nothing: the newest of all (the site test's search) - no tags asked.
	e.Search(ctx, "", 0)
	if q := (*reqs)[len(*reqs)-1].URL.Query(); q.Has("tags") || q.Get("type") != "g" || (*reqs)[len(*reqs)-1].URL.Path != "/v2/gifs/search" {
		t.Fatalf("an empty search: %v", (*reqs)[len(*reqs)-1].URL)
	}

	// A refused token is fetched again once.
	tokens.Add(1) // (the server now wants a newer one)
	if page, err = e.Search(ctx, "@some_artist", 0); err != nil || len(page.Posts) != 80 {
		t.Fatalf("after the token went stale: %d %v", len(page.Posts), err)
	}
	if _, err := e.Lookup(ctx, p.ID); !errors.Is(err, ErrNoLookup) {
		t.Fatalf("lookup: %v", err)
	}
	// By its link (a refresh): the clip.
	if got, err := e.(URLEngine).LookupURL(ctx, "https://www.redgifs.com/watch/clipd", 0); err != nil || got == nil || got.PostURL != "https://www.redgifs.com/watch/clipd" {
		t.Fatalf("lookup by link: %+v %v", got, err)
	}
	if got, err := e.(URLEngine).LookupURL(ctx, "https://www.redgifs.com/watch/goneclip", 0); err != nil || got != nil {
		t.Fatalf("a gone clip by link: %+v %v", got, err)
	}
}

func TestRedgifsManifest(t *testing.T) {
	m, _, _, _ := fakeRedgifs(t)
	if m.CanSinglePost() || m.TagQueries()["creator"] != "@{tag}" {
		t.Fatalf("single %v, tag queries %v", m.CanSinglePost(), m.TagQueries())
	}
	if tmpl, _ := m.OrQuery(); tmpl != "" {
		t.Fatalf("OR: %q", tmpl)
	}
}

// Against the real site: only with VITRINE_LIVE=1 - the other tests never leave the machine. (Adult content: it
// only reads the replies, nothing is saved.)
func TestRedgifsLive(t *testing.T) {
	if os.Getenv("VITRINE_LIVE") != "1" {
		t.Skip("set VITRINE_LIVE=1 to ask the real site")
	}
	m, err := Parse([]byte(`
id: redgifs
engine: redgifs
base: https://api.redgifs.com
domains: [redgifs.com]
limits: {requests_per_sec: 1}
ratings: {"": explicit}
tags: {types: post, namespaces: {artist: creator}}
`))
	if err != nil {
		t.Fatal(err)
	}
	e, _ := NewEngine(NewClient(m, nil))
	ctx := context.Background()
	page, err := e.Search(ctx, "cosplay", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Posts) == 0 || page.Total <= 0 {
		t.Fatalf("page: %d posts of %d", len(page.Posts), page.Total)
	}
	p := page.Posts[0]
	t.Logf("total %d; newest %s, %d tags, posted %v", page.Total, p.PostURL, len(p.Tags), p.Posted)
	last := page.Posts[len(page.Posts)-1].ID
	fresh, _ := NewEngine(NewClient(m, nil)) // (nothing remembered: the page is found by halving)
	next, err := fresh.Search(ctx, "cosplay", last)
	if err != nil || len(next.Posts) == 0 || next.Posts[0].ID >= last {
		t.Fatalf("next page: %d %v", len(next.Posts), err)
	}
	one, err := e.Search(ctx, p.PostURL, 0)
	if err != nil || len(one.Posts) != 1 || one.Posts[0].ID != p.ID {
		t.Fatalf("the clip's link: %+v %v", one, err)
	}
}
