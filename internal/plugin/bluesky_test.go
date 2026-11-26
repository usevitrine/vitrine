package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/media"
)

// fakeBluesky serves 150 made-up posts by some_artist.bsky.social (newest first, one a minute): post i has two
// images when i%3 == 0, a video when i%3 == 1, one image otherwise; the even ones have the hashtag #BlueSky; post 0
// is labeled porn. Every fifth item of the profile's feed is also a repost (left out). The profile's feed pages by
// time, the search by its own cursors and only for a signed-in account.
func fakeBluesky(t *testing.T) (Manifest, *httptest.Server, *atomic.Int32, *[]string) {
	t.Helper()
	var sessions atomic.Int32
	var paths []string
	base := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	const did = "did:plc:someartist"
	at := func(i int) time.Time { return base.Add(-time.Duration(i) * time.Minute) }
	post := func(i int) string {
		var embed string
		switch i % 3 {
		case 0:
			embed = `{"$type":"app.bsky.embed.images","images":[{"image":{"ref":{"$link":"img` + fmt.Sprint(i) + `a"},"mimeType":"image/png"},"aspectRatio":{"width":800,"height":600}},` +
				`{"image":{"ref":{"$link":"img` + fmt.Sprint(i) + `b"},"mimeType":"image/jpeg"},"aspectRatio":{"width":10,"height":20}}]}`
		case 1:
			embed = `{"$type":"app.bsky.embed.video","video":{"ref":{"$link":"vid` + fmt.Sprint(i) + `"},"mimeType":"video/mp4"},"aspectRatio":{"width":1080,"height":1920}}`
		default:
			embed = `{"$type":"app.bsky.embed.recordWithMedia","media":{"$type":"app.bsky.embed.images","images":[{"image":{"ref":{"$link":"img` + fmt.Sprint(i) + `a"}}}]}}`
		}
		facets, labels := `[]`, `[]`
		if i%2 == 0 {
			facets = `[{"features":[{"$type":"app.bsky.richtext.facet#tag","tag":"BlueSky"}]},{"features":[{"$type":"app.bsky.richtext.facet#link","uri":"https://x"}]}]`
		}
		if i == 0 {
			labels = `[{"val":"porn"}]`
		}
		return fmt.Sprintf(`{"uri":"at://%s/app.bsky.feed.post/rk%d","author":{"did":%q,"handle":"Some_Artist.bsky.social"},"indexedAt":%q,"labels":%s,`+
			`"record":{"createdAt":%q,"embed":%s,"facets":%s,"tags":["Painting"]}}`,
			did, i, did, at(i).Add(2*time.Second).Format(time.RFC3339Nano), labels, at(i).Format(time.RFC3339Nano), embed, facets)
	}
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		q := r.URL.Query()
		auth := r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/xrpc/com.atproto.server.createSession":
			var b struct{ Identifier, Password string }
			json.NewDecoder(r.Body).Decode(&b)
			if b.Identifier != "me.bsky.social" || b.Password != "abcd-efgh-ijkl-mnop" {
				w.WriteHeader(401)
				io.WriteString(w, `{"error":"AuthenticationRequired"}`)
				return
			}
			fmt.Fprintf(w, `{"accessJwt":"tok%d","refreshJwt":"x","did":"did:plc:me"}`, sessions.Add(1))
		case "/" + did:
			fmt.Fprintf(w, `{"id":%q,"service":[{"id":"#atproto_pds","type":"AtprotoPersonalDataServer","serviceEndpoint":%q}]}`, did, srv.URL)
		case "/xrpc/app.bsky.feed.getAuthorFeed":
			if strings.TrimPrefix(q.Get("actor"), "@") != "some_artist.bsky.social" || q.Get("filter") != "posts_with_media" {
				w.WriteHeader(400)
				io.WriteString(w, `{"error":"InvalidRequest","message":"Profile not found"}`)
				return
			}
			until := base.Add(time.Hour)
			if c := q.Get("cursor"); c != "" {
				until, _ = time.Parse(time.RFC3339Nano, c)
			}
			var items []string
			last := ""
			for i := 0; i < 150 && len(items) < 40; i++ {
				if !at(i).Before(until) {
					continue
				}
				items = append(items, `{"post":`+post(i)+`}`)
				if i%5 == 4 {
					items = append(items, `{"post":`+post(i)+`,"reason":{"$type":"app.bsky.feed.defs#reasonRepost"}}`)
				}
				last = at(i).Format("2006-01-02T15:04:05.000Z")
			}
			cur := ""
			if len(items) > 0 {
				cur = `,"cursor":"` + last + `"`
			}
			fmt.Fprintf(w, `{"feed":[%s]%s}`, strings.Join(items, ","), cur)
		case "/xrpc/app.bsky.feed.searchPosts":
			if auth == "" {
				w.WriteHeader(403) // (a guest: as the real one, a web page)
				io.WriteString(w, `<html><title>403 Forbidden</title></html>`)
				return
			}
			if auth != fmt.Sprintf("Bearer tok%d", sessions.Load()) {
				w.WriteHeader(401)
				io.WriteString(w, `{"error":"InvalidToken"}`)
				return
			}
			if q.Get("q") != "#bluesky" || q.Get("sort") != "latest" || q.Get("tag") != "bluesky" {
				io.WriteString(w, `{"posts":[]}`)
				return
			}
			from := 0
			if c := q.Get("cursor"); c != "" {
				fmt.Sscanf(c, "c%d", &from)
			}
			if u := q.Get("until"); u != "" {
				ut, _ := time.Parse(time.RFC3339Nano, u)
				for from < 150 && !at(from).Before(ut) {
					from++
				}
			}
			var list []string
			i := from
			for ; i < 150 && len(list) < 25; i++ {
				if i%2 == 0 {
					list = append(list, post(i))
				}
			}
			fmt.Fprintf(w, `{"posts":[%s],"cursor":"c%d"}`, strings.Join(list, ","), i)
		case "/xrpc/app.bsky.feed.getPosts":
			if q.Get("uris") == "at://"+did+"/app.bsky.feed.post/rk3" {
				fmt.Fprintf(w, `{"posts":[%s]}`, post(3))
				return
			}
			if q.Get("uris") == "at://"+did+"/app.bsky.feed.post/rk6" {
				fmt.Fprintf(w, `{"posts":[%s]}`, post(6))
				return
			}
			io.WriteString(w, `{"posts":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	old := [3]string{blueskyPublic, blueskyCDN, blueskyPLC}
	blueskyPublic, blueskyCDN, blueskyPLC = srv.URL, srv.URL+"/img/", srv.URL+"/"
	t.Cleanup(func() { blueskyPublic, blueskyCDN, blueskyPLC = old[0], old[1], old[2] })
	m, err := Parse([]byte(`
id: bsky_test
engine: bluesky
base: ` + srv.URL + `
site: bsky.app
auth: {fields: [handle, app_password], login: bluesky}
limits: {requests_per_sec: 1000}
ratings: {porn: explicit, sexual: questionable, nudity: questionable, safe: general}
tags: {types: page, namespaces: {artist: creator}}
`))
	if err != nil {
		t.Fatal(err)
	}
	return m, srv, &sessions, &paths
}

func TestBlueskyProfile(t *testing.T) {
	m, srv, sessions, _ := fakeBluesky(t)
	e, _ := NewEngine(testClient(m, srv, nil))
	ctx := context.Background()
	var all []Post
	before := int64(0)
	for pages := 0; ; pages++ {
		page, err := e.Search(ctx, "@some_artist.bsky.social", before)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Posts) == 0 {
			break
		}
		if pages > 20 {
			t.Fatal("no end")
		}
		all = append(all, page.Posts...)
		before = page.Posts[len(page.Posts)-1].ID
	}
	// 150 posts: 50 with two images, 100 with one file - and no repost twice.
	if len(all) != 200 {
		t.Fatalf("%d files, want 200", len(all))
	}
	for i := 1; i < len(all); i++ {
		if all[i].ID >= all[i-1].ID {
			t.Fatalf("file %d's ID isn't below the one before", i)
		}
	}
	// Post 0: its second image first, then its first; originals from the author's server, the CDN's copy after.
	p, q := all[1], all[0]
	did := "did:plc:someartist"
	if p.FileURL != srv.URL+"/xrpc/com.atproto.sync.getBlob?cid=img0a&did="+url.QueryEscape(did) || len(p.AltFileURLs) != 1 ||
		p.AltFileURLs[0] != srv.URL+"/img/"+did+"/img0a@jpeg" || p.Width != 800 || p.Height != 600 || q.ID != p.ID+1 {
		t.Fatalf("post 0's first image: %+v (then %+v)", p, q)
	}
	if p.PostURL != "https://bsky.app/profile/"+did+"/post/rk0" || p.Rating != media.RatingExplicit ||
		!p.Posted.Equal(time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("post 0: %+v", p)
	}
	if strings.Join(p.Tags, " ") != "bluesky painting some_artist.bsky.social" || p.TagTypes["some_artist.bsky.social"] != "artist" {
		t.Fatalf("tags %v, types %v", p.Tags, p.TagTypes)
	}
	// Post 1: a video (no CDN copy), rated safe; post 2: the quote's own image.
	if v := all[2]; !strings.Contains(v.FileURL, "cid=vid1") || len(v.AltFileURLs) != 0 || v.Rating != media.RatingGeneral ||
		strings.Join(v.Tags, " ") != "painting some_artist.bsky.social" {
		t.Fatalf("post 1: %+v", v)
	}
	if im := all[3]; !strings.Contains(im.FileURL, "cid=img2a") {
		t.Fatalf("post 2: %+v", im)
	}
	if sessions.Load() != 0 {
		t.Fatal("a guest signed in")
	}

	// Resumed after a restart (a new engine: no cursor kept) below post 1's video: post 2 on.
	e2, _ := NewEngine(testClient(m, srv, nil))
	page, err := e2.Search(ctx, "https://bsky.app/profile/some_artist.bsky.social", all[2].ID)
	if err != nil || len(page.Posts) == 0 || page.Posts[0].ID != all[3].ID {
		t.Fatalf("resumed: %v, %d posts", err, len(page.Posts))
	}

	// A profile's hashtag as a guest: its feed, filtered here.
	page, err = e2.Search(ctx, "@some_artist.bsky.social #BlueSky -#nope", 0)
	if err != nil || len(page.Posts) == 0 {
		t.Fatal(err)
	}
	for _, p := range page.Posts {
		if p.Tags[0] != "bluesky" {
			t.Fatalf("a post without the hashtag: %+v", p)
		}
	}
	if _, err := e2.Search(ctx, "@nobody.bsky.social", 0); err == nil {
		t.Fatal("no error for a missing profile")
	}
}

func TestBlueskySearch(t *testing.T) {
	m, srv, sessions, paths := fakeBluesky(t)
	ctx := context.Background()
	guest, _ := NewEngine(testClient(m, srv, nil))
	if _, err := guest.Search(ctx, "#BlueSky", 0); err == nil || !strings.Contains(err.Error(), "needs an account") {
		t.Fatalf("a guest's hashtag search: %v", err)
	}
	if _, err := guest.Search(ctx, "#a ~#b", 0); err == nil || !strings.Contains(err.Error(), "either") {
		t.Fatalf("either: %v", err)
	}
	bad, _ := NewEngine(testClient(m, srv, map[string]string{"handle": "me.bsky.social", "app_password": "wrong"}))
	if _, err := bad.Search(ctx, "#BlueSky", 0); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("a wrong password: %v", err)
	}

	e, _ := NewEngine(testClient(m, srv, map[string]string{"handle": "@me.bsky.social", "app_password": "abcd-efgh-ijkl-mnop"}))
	var all []Post
	before := int64(0)
	for pages := 0; pages < 20; pages++ {
		page, err := e.Search(ctx, "BlueSky", before)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Posts) == 0 {
			break
		}
		all = append(all, page.Posts...)
		before = page.Posts[len(page.Posts)-1].ID
	}
	// The 75 even posts: 25 with two images (i%6 == 0), 50 with one file.
	if len(all) != 100 {
		t.Fatalf("%d files, want 100", len(all))
	}
	if n := sessions.Load(); n != 1 { // (the wrong password's try made none; this engine one)
		t.Fatalf("%d sign-ins", n)
	}
	// Resumed after a restart: from the time below the ID.
	e2, _ := NewEngine(testClient(m, srv, map[string]string{"handle": "me.bsky.social", "app_password": "abcd-efgh-ijkl-mnop"}))
	page, err := e2.Search(ctx, "#bluesky", all[50].ID)
	if err != nil || len(page.Posts) == 0 || page.Posts[0].ID != all[51].ID {
		t.Fatalf("resumed: %v, %+v", err, page.Posts)
	}

	// A token Bluesky refuses: a new session, once.
	sessions.Add(1)
	n := len(*paths)
	if page, err := e2.Search(ctx, "#bluesky", 0); err != nil || len(page.Posts) == 0 {
		t.Fatalf("after a refused token: %v", err)
	}
	if signins := strings.Count(strings.Join((*paths)[n:], " "), "createSession"); signins != 1 {
		t.Fatalf("%d sign-ins after a refused token", signins)
	}
}

func TestBlueskyPostLink(t *testing.T) {
	m, srv, _, _ := fakeBluesky(t)
	e, _ := NewEngine(testClient(m, srv, nil))
	ctx := context.Background()
	link := "https://bsky.app/profile/did:plc:someartist/post/rk6"
	page, err := e.Search(ctx, link, 0)
	if err != nil || len(page.Posts) != 2 || page.Total != 2 {
		t.Fatalf("a post's link: %v, %+v", err, page)
	}
	if next, err := e.Search(ctx, link, page.Posts[1].ID); err != nil || len(next.Posts) != 0 {
		t.Fatalf("after it: %v, %+v", err, next)
	}
	// A refresh: by the link, the file asked for.
	for _, want := range page.Posts {
		got, err := e.(URLEngine).LookupURL(ctx, link, want.ID)
		if err != nil || got == nil || got.ID != want.ID || got.FileURL != want.FileURL {
			t.Fatalf("refresh of %d: %v, %+v", want.ID, err, got)
		}
	}
	if got, err := e.(URLEngine).LookupURL(ctx, "https://bsky.app/profile/did:plc:someartist/post/gone", 1); err != nil || got != nil {
		t.Fatalf("a gone post: %v, %+v", err, got)
	}
	if _, ok, err := SinglePost(m, "id:5"); ok || err == nil {
		t.Fatal("id:N should be refused")
	}
	if _, err := e.Search(ctx, "fav:me", 0); err == nil {
		t.Fatal("likes should be refused for now")
	}
}

// TestBlueskyLive asks the real Bluesky (VITRINE_LIVE=1): a profile's media posts as a guest, the next page, a
// post's link and its original file's server.
func TestBlueskyLive(t *testing.T) {
	if os.Getenv("VITRINE_LIVE") == "" {
		t.Skip("set VITRINE_LIVE=1")
	}
	m, err := Parse([]byte(`
id: bluesky
engine: bluesky
base: https://bsky.social
site: bsky.app
domains: [bsky.app, bsky.social, bsky.network, plc.directory]
auth: {fields: [handle, app_password], login: bluesky}
ratings: {porn: explicit, sexual: questionable, nudity: questionable, safe: general}
tags: {types: page, namespaces: {artist: creator}}
`))
	if err != nil {
		t.Fatal(err)
	}
	e, _ := NewEngine(NewClient(m, nil))
	ctx := context.Background()
	page, err := e.Search(ctx, "@bsky.app", 0)
	if err != nil || len(page.Posts) == 0 {
		t.Fatalf("profile: %v", err)
	}
	p := page.Posts[0]
	t.Logf("first: %d %s %s %v", p.ID, p.FileURL, p.PostURL, p.Tags)
	if !strings.Contains(p.FileURL, ".host.bsky.network/xrpc/com.atproto.sync.getBlob?") {
		t.Fatalf("not the original: %s", p.FileURL)
	}
	next, err := e.Search(ctx, "@bsky.app", page.Posts[len(page.Posts)-1].ID)
	if err != nil || len(next.Posts) == 0 || next.Posts[0].ID >= page.Posts[len(page.Posts)-1].ID {
		t.Fatalf("next page: %v, %d", err, len(next.Posts))
	}
	got, err := e.(URLEngine).LookupURL(ctx, p.PostURL, p.ID)
	if err != nil || got == nil || got.FileURL != p.FileURL {
		t.Fatalf("by link: %v, %+v", err, got)
	}
	body, _, err := NewClient(m, nil).File(ctx, p.FileURL)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(body)
	body.Close()
	t.Logf("file: %d bytes", len(b))
}
