package plugin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/media"
)

// redditNew is a subreddit's newest posts as the API sends them (raw_json=1), cut to what the engine reads: an image,
// a gallery (an image and an animation), a video, a RedGIFs link, a text post, a crosspost, a removed post.
const redditNew = `{"kind":"Listing","data":{"after":"t3_ab1","children":[
{"kind":"t3","data":{"id":"abc","subreddit":"EarthPorn","author":"Some_Artist","permalink":"/r/EarthPorn/comments/abc/x/","url":"https://i.redd.it/one.jpg","created_utc":1794988800.0,"over_18":false,"link_flair_text":":star: Original Content","preview":{"images":[{"source":{"url":"https://preview.redd.it/one.jpg","width":4000,"height":3000}}]}}},
{"kind":"t3","data":{"id":"abb","subreddit":"EarthPorn","author":"other","permalink":"/r/EarthPorn/comments/abb/x/","url":"https://www.reddit.com/gallery/abb","created_utc":1794988700.0,"over_18":true,"is_gallery":true,
 "gallery_data":{"items":[{"media_id":"m1"},{"media_id":"m2"}]},
 "media_metadata":{"m1":{"status":"valid","e":"Image","m":"image/png","s":{"u":"https://preview.redd.it/m1.png?width=800&s=abc","x":800,"y":600}},"m2":{"status":"valid","e":"AnimatedImage","s":{"gif":"https://i.redd.it/m2.gif","mp4":"https://preview.redd.it/m2.gif?format=mp4","x":400,"y":300}}}}},
{"kind":"t3","data":{"id":"aba","subreddit":"EarthPorn","author":"other","permalink":"/r/EarthPorn/comments/aba/x/","url":"https://v.redd.it/xyz","created_utc":1794988600.0,
 "media":{"reddit_video":{"fallback_url":"https://v.redd.it/xyz/DASH_720.mp4?source=fallback","width":1280,"height":720,"is_gif":false}}}},
{"kind":"t3","data":{"id":"ab9","subreddit":"EarthPorn","author":"other","permalink":"/r/EarthPorn/comments/ab9/x/","url":"https://www.redgifs.com/watch/happyfluffycat","created_utc":1794988500.0,"over_18":true,
 "preview":{"reddit_video_preview":{"fallback_url":"https://v.redd.it/rg/DASH_480.mp4","width":640,"height":480}}}},
{"kind":"t3","data":{"id":"ab8","subreddit":"EarthPorn","author":"other","permalink":"/r/EarthPorn/comments/ab8/x/","url":"https://example.test/article","created_utc":1794988400.0}},
{"kind":"t3","data":{"id":"ab7","subreddit":"pics","author":"reposter","permalink":"/r/pics/comments/ab7/x/","url":"/r/EarthPorn/comments/abc/x/","created_utc":1794988300.0,
 "crosspost_parent_list":[{"id":"abc","subreddit":"EarthPorn","author":"Some_Artist","url":"https://i.redd.it/one.jpg"}]}},
{"kind":"t3","data":{"id":"ab6","subreddit":"EarthPorn","author":"[deleted]","permalink":"/r/EarthPorn/comments/ab6/x/","url":"https://i.redd.it/gone.jpg","created_utc":1794988200.0,"removed_by_category":"moderator"}}
]}}`

func TestReddit(t *testing.T) {
	var mu sync.Mutex
	var asked []string
	tokens := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.Path+"?"+r.URL.RawQuery)
		mu.Unlock()
		switch r.URL.Path {
		case "/api/v1/access_token":
			user, pass, ok := r.BasicAuth()
			r.ParseForm()
			if !ok || user != "APPID" || pass != "SECRET" || r.PostForm.Get("grant_type") != "client_credentials" {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			mu.Lock()
			tokens++
			mu.Unlock()
			io.WriteString(w, `{"access_token":"TOK","token_type":"bearer","expires_in":86400}`)
			return
		case "/v2/auth/temporary":
			io.WriteString(w, `{"token":"RGTOK"}`)
			return
		case "/v2/gifs/happyfluffycat":
			io.WriteString(w, `{"gif":{"id":"happyfluffycat","createDate":1794988000,"type":1,"width":1080,"height":1920,"tags":["Big Smile","Outdoors"],"userName":"Some_Artist","urls":{"hd":"https://media.redgifs.com/HappyFluffyCat.mp4","sd":"https://media.redgifs.com/HappyFluffyCat-mobile.mp4"}}}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer TOK" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/r/EarthPorn/new":
			if r.URL.Query().Get("after") != "" {
				io.WriteString(w, `{"kind":"Listing","data":{"after":null,"children":[]}}`)
				return
			}
			io.WriteString(w, redditNew)
		case "/api/info":
			// (one post of the listing, by its fullname)
			id := strings.TrimPrefix(r.URL.Query().Get("id"), "t3_")
			var l struct {
				Data struct {
					Children []json.RawMessage `json:"children"`
				} `json:"data"`
			}
			json.Unmarshal([]byte(redditNew), &l)
			for _, ch := range l.Data.Children {
				if strings.Contains(string(ch), `"id":"`+id+`"`) {
					io.WriteString(w, `{"kind":"Listing","data":{"children":[`+string(ch)+`]}}`)
					return
				}
			}
			io.WriteString(w, `{"kind":"Listing","data":{"children":[]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	defer func(tok, rg string) { redditTokenURL, redditRedgifsBase = tok, rg }(redditTokenURL, redditRedgifsBase)
	redditTokenURL, redditRedgifsBase = srv.URL+"/api/v1/access_token", srv.URL
	m, err := Parse([]byte(`
id: reddit_test
engine: reddit
base: ` + srv.URL + `
auth: {fields: [client_id, client_secret, username, password], login: reddit, required: true}
limits: {requests_per_sec: 100}
ratings: {sfw: general, nsfw: explicit}
tags:
  namespaces: {artist: creator, subreddit: subreddit}
`))
	if err != nil {
		t.Fatal(err)
	}
	c := testClient(m, srv, map[string]string{"client_id": "APPID", "client_secret": "SECRET"})
	if !c.HasCredentials() {
		t.Fatal("an app's ID and secret should be enough")
	}
	e, err := NewEngine(c)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	page, err := e.Search(ctx, "r/EarthPorn", 0)
	if err != nil {
		t.Fatal(err)
	}
	ids := []int64{}
	for _, p := range page.Posts {
		ids = append(ids, p.ID)
	}
	// abc, abb's two files (last first), aba, ab9, ab7 (the crosspost) - no text post, no removed one.
	want := []int64{1336800, 1336701, 1336700, 1336600, 1336500, 1336300}
	if len(ids) != len(want) {
		t.Fatalf("posts %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("posts %v, want %v", ids, want)
		}
	}
	p := page.Posts[0]
	if p.FileURL != "https://i.redd.it/one.jpg" || strings.Join(p.Tags, " ") != "earthporn some_artist original_content" ||
		p.TagTypes["earthporn"] != "subreddit" || p.TagTypes["some_artist"] != "artist" || p.Rating != media.RatingGeneral ||
		p.Width != 4000 || p.PostURL != "https://www.reddit.com/r/EarthPorn/comments/abc/x/" ||
		!p.Posted.Equal(time.Unix(1794988800, 0)) {
		t.Fatalf("image %+v", p)
	}
	if g := page.Posts[1]; g.FileURL != "https://preview.redd.it/m2.gif?format=mp4" || g.Rating != media.RatingExplicit || g.Width != 400 {
		t.Fatalf("animation %+v", g)
	}
	if g := page.Posts[2]; g.FileURL != "https://i.redd.it/m1.png" || len(g.AltFileURLs) != 1 {
		t.Fatalf("gallery image %+v", g)
	}
	v := page.Posts[3]
	if v.FileURL != "https://v.redd.it/xyz/DASH_720.mp4?source=fallback" || len(v.AudioURLs) == 0 ||
		v.AudioURLs[0] != "https://v.redd.it/xyz/DASH_AUDIO_128.mp4" {
		t.Fatalf("video %+v", v)
	}
	rg := page.Posts[4]
	if !rg.Partial || rg.FileURL != "" {
		t.Fatalf("a RedGIFs clip should wait for Lookup: %+v", rg)
	}
	if x := page.Posts[5]; x.FileURL != "https://i.redd.it/one.jpg" || x.Tags[0] != "pics" || x.Tags[1] != "reposter" {
		t.Fatalf("crosspost %+v", x)
	}
	// The clip, when its file is needed: from RedGIFs, with its tags and its sound.
	got, err := e.Lookup(ctx, 1336500)
	if err != nil || got == nil {
		t.Fatalf("clip: %+v %v", got, err)
	}
	if got.FileURL != "https://media.redgifs.com/HappyFluffyCat.mp4" || got.Partial ||
		strings.Join(got.Tags, " ") != "earthporn other big_smile outdoors some_artist" || got.TagTypes["some_artist"] != "artist" ||
		got.TagTypes["earthporn"] != "subreddit" || got.Sources[0] != "https://www.redgifs.com/watch/happyfluffycat" {
		t.Fatalf("clip %+v", got)
	}
	// A refresh: the post again by its ID.
	if got, err := e.Lookup(ctx, 1336700); err != nil || got == nil || got.FileURL != "https://i.redd.it/m1.png" {
		t.Fatalf("lookup: %+v %v", got, err)
	}
	// Resumed part-way through the gallery: its first file, then the posts after it.
	page, err = e.Search(ctx, "r/EarthPorn", 1336701)
	if err != nil || len(page.Posts) != 1 || page.Posts[0].ID != 1336700 {
		t.Fatalf("resumed: %+v %v", page.Posts, err)
	}
	// ... and below that, the posts after it.
	if page, err = e.Search(ctx, "r/EarthPorn", 1336700); err != nil || len(page.Posts) != 0 {
		t.Fatalf("below the gallery: %+v %v", page.Posts, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if tokens != 1 {
		t.Errorf("%d tokens, want 1 (kept)", tokens)
	}
	if last := asked[len(asked)-1]; !strings.Contains(last, "after=t3_abb") {
		t.Errorf("resumed with %s", last)
	}
}

func TestRedditQueries(t *testing.T) {
	m, err := Parse([]byte("id: r\nengine: reddit\nbase: https://oauth.reddit.com\ndomains: [reddit.com, redd.it]\n" +
		"auth: {fields: [client_id, client_secret, username, password], login: reddit}\n"))
	if err != nil {
		t.Fatal(err)
	}
	e, _ := NewEngine(NewClient(m, nil))
	r := e.(*reddit)
	for _, x := range []struct{ q, path, query, post, err string }{
		{q: "r/EarthPorn", path: "/r/EarthPorn/new"},
		{q: "/r/pics+aww", path: "/r/pics+aww/new"},
		{q: "r/EarthPorn mountain lake", path: "/r/EarthPorn/search", query: "mountain lake"},
		{q: "u/some_artist", path: "/user/some_artist/submitted"},
		{q: "mountain", path: "/search", query: "mountain"},
		{q: "", path: "/r/all/new"},
		{q: "https://www.reddit.com/r/EarthPorn/comments/1abcd2/a_title/", post: "1abcd2"},
		{q: "https://redd.it/1abcd2", post: "1abcd2"},
		{q: "https://old.reddit.com/r/EarthPorn/", path: "/r/EarthPorn/new"},
		{q: "https://www.reddit.com/user/some_artist/", path: "/user/some_artist/submitted"},
		{q: "u/some_artist mountain", err: "can't be searched"},
		{q: "https://example.test/x", err: "isn't a post"},
	} {
		got, err := r.parse(x.q)
		switch {
		case x.err != "":
			if err == nil || !strings.Contains(err.Error(), x.err) {
				t.Errorf("%q: %v, want an error with %q", x.q, err, x.err)
			}
		case err != nil:
			t.Errorf("%q: %v", x.q, err)
		case got.path != x.path || got.post != x.post || got.v.Get("q") != x.query:
			t.Errorf("%q: %+v", x.q, got)
		}
	}
	if f := redditFlair(":snoo_joy: Big  Win :star:"); f != "big_win" {
		t.Errorf("flair %q", f)
	}
	if _, err := Parse([]byte("id: r\nengine: reddit\nbase: https://oauth.reddit.com\nauth: {fields: [client_id], login: reddit}\n")); err == nil {
		t.Error("login: reddit with other fields")
	}
}
