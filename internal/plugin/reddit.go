package plugin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// reddit reads Reddit's API (oauth.reddit.com) with an OAuth token for the user's own Reddit app (2026-11-18: without
// one, every API address answers 403 "Blocked" from a VPN's address). The token: app-only (client_credentials) for
// a script app, the account's (password grant) when a username and password are set too, or an installed app's (no
// secret: installed_client with a device ID) - kept until shortly before it runs out, fetched again on a 401.
//
// Searches: "r/name" (a subreddit's newest; r/a+b for several), "r/name words" (its search, newest first), "u/name"
// (a user's posts), words alone (all of Reddit's search), a post's link (that post), a subreddit's or user's link.
// Reddit's lists end after about 1,000 posts.
//
// Posts: each file of a post (a gallery's images, a video, an image, a RedGIFs clip it links) is one Vitrine post:
// ID = the post's number (base 36 on the site) * 100 + its place. Reddit has no tags: the subreddit (tag type
// subreddit), the poster (artist: a creator), the post's flair; a RedGIFs clip brings RedGIFs' tags too. A Reddit video
// keeps its sound in a file of its own (AudioURLs: the downloader joins them).
type reddit struct {
	c  *Client
	rg *redgifs // RedGIFs clips that posts link (their own client: api.redgifs.com, its own pace)

	mu       sync.Mutex
	tokenEnd time.Time
	device   string
	// clips: posts that are a RedGIFs clip (Partial until Lookup asks RedGIFs, only for files the library needs) -
	// the Reddit post's part and the clip's ID, by post ID; at most redditClipsMax, the oldest dropped.
	clips map[int64]redditClip
	order []int64
}

type redditClip struct {
	post     Post
	clip     string
	fallback string // Reddit's own copy of the clip (no sound), if the clip is gone from RedGIFs
}

const redditClipsMax = 5000

// redditTokenURL is where tokens come from (www.reddit.com, not the API's host).
var redditTokenURL = "https://www.reddit.com/api/v1/access_token"

// redditRedgifsBase is RedGIFs' API, for the clips posts link (tests: a fake).
var redditRedgifsBase = "https://api.redgifs.com"

func newReddit(c *Client) (*reddit, error) {
	m, err := Parse([]byte("id: " + c.m.ID + "_redgifs\nengine: redgifs\nbase: " + redditRedgifsBase +
		"\ndomains: [redgifs.com]\nlimits: {requests_per_sec: 2}\n"))
	if err != nil {
		return nil, err
	}
	m.Site = "redgifs.com"
	rc := NewClient(m, nil)
	rc.SetHTTPClient(c.http)
	return &reddit{c: c, rg: &redgifs{c: rc}, clips: map[int64]redditClip{}}, nil
}

type redditListing struct {
	Data struct {
		After    string `json:"after"`
		Children []struct {
			Kind string     `json:"kind"`
			Data redditPost `json:"data"`
		} `json:"children"`
	} `json:"data"`
}

type redditMedia struct {
	Status string `json:"status"`
	E      string `json:"e"` // Image, AnimatedImage
	M      string `json:"m"` // image/jpg, image/png, ...
	S      struct {
		U   string `json:"u"`
		GIF string `json:"gif"`
		MP4 string `json:"mp4"`
		X   int    `json:"x"`
		Y   int    `json:"y"`
	} `json:"s"`
}

type redditVideo struct {
	FallbackURL string `json:"fallback_url"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	IsGIF       bool   `json:"is_gif"`
	HasAudio    *bool  `json:"has_audio"`
}

type redditPost struct {
	ID            string                 `json:"id"`
	Subreddit     string                 `json:"subreddit"`
	Author        string                 `json:"author"`
	Permalink     string                 `json:"permalink"`
	URL           string                 `json:"url"`
	CreatedUTC    float64                `json:"created_utc"`
	Over18        bool                   `json:"over_18"`
	Flair         string                 `json:"link_flair_text"`
	Removed       string                 `json:"removed_by_category"`
	IsGallery     bool                   `json:"is_gallery"`
	MediaMetadata map[string]redditMedia `json:"media_metadata"`
	GalleryData   *struct {
		Items []struct {
			MediaID string `json:"media_id"`
		} `json:"items"`
	} `json:"gallery_data"`
	Media *struct {
		RedditVideo *redditVideo `json:"reddit_video"`
	} `json:"media"`
	Preview *struct {
		Images []struct {
			Source struct {
				URL    string `json:"url"`
				Width  int    `json:"width"`
				Height int    `json:"height"`
			} `json:"source"`
		} `json:"images"`
		VideoPreview *redditVideo `json:"reddit_video_preview"`
	} `json:"preview"`
	Crossposts []redditPost `json:"crosspost_parent_list"`
}

type redditQuery struct {
	path string
	v    url.Values
	post string // one post (its base-36 ID)
}

var (
	redditPostPath = regexp.MustCompile(`^/(?:r/[^/]+/)?comments/([a-z0-9]+)`)
	redditSubPath  = regexp.MustCompile(`^/r/([A-Za-z0-9_+]+)/?$`)
	redditUserPath = regexp.MustCompile(`^/(?:u|user)/([A-Za-z0-9_-]+)`)
	redditName     = regexp.MustCompile(`^[A-Za-z0-9_+-]+$`)
	redditEmoji    = regexp.MustCompile(`:[A-Za-z0-9_-]+:`)
)

func (r *reddit) parse(q string) (redditQuery, error) {
	q = strings.TrimSpace(q)
	site := r.c.m.Site
	if lq := strings.ToLower(q); strings.HasPrefix(lq, "https://") || strings.HasPrefix(lq, "http://") {
		u, err := url.Parse(q)
		if err == nil {
			p := strings.TrimSuffix(u.Path, "/")
			switch host := strings.ToLower(u.Hostname()); {
			case host == "redd.it" && redditName.MatchString(strings.TrimPrefix(p, "/")):
				return redditQuery{post: strings.ToLower(strings.TrimPrefix(p, "/"))}, nil
			case host == "reddit.com" || strings.HasSuffix(host, ".reddit.com"):
				if m := redditPostPath.FindStringSubmatch(p); m != nil {
					return redditQuery{post: m[1]}, nil
				}
				if m := redditSubPath.FindStringSubmatch(p + "/"); m != nil {
					return r.parse("r/" + m[1])
				}
				if m := redditUserPath.FindStringSubmatch(p); m != nil {
					return r.parse("u/" + m[1])
				}
			}
		}
		return redditQuery{}, fmt.Errorf("%s: that link isn't a post, a subreddit or a user: open one and copy its address", site)
	}
	if _, ok := Favorites(q); ok {
		return redditQuery{}, fmt.Errorf("%s: saved posts aren't supported", site)
	}
	var sub, user string
	var words []string
	for _, w := range strings.Fields(q) {
		tw := strings.TrimPrefix(w, "/")
		lw, name := strings.ToLower(tw), tw[strings.Index(tw, "/")+1:]
		switch {
		case strings.HasPrefix(lw, "r/") && sub == "" && user == "" && redditName.MatchString(name):
			sub = name
		case (strings.HasPrefix(lw, "u/") || strings.HasPrefix(lw, "user/")) && sub == "" && user == "" && redditName.MatchString(name):
			user = name
		default:
			words = append(words, w)
		}
	}
	v := url.Values{"limit": {"100"}, "raw_json": {"1"}}
	switch {
	case user != "" && len(words) > 0:
		return redditQuery{}, fmt.Errorf("%s: a user's posts can't be searched: u/name alone", site)
	case user != "":
		v.Set("sort", "new")
		return redditQuery{path: "/user/" + url.PathEscape(user) + "/submitted", v: v}, nil
	case sub != "" && len(words) == 0:
		return redditQuery{path: "/r/" + sub + "/new", v: v}, nil
	case len(words) == 0:
		return redditQuery{path: "/r/all/new", v: v}, nil // (an empty search: the newest of all - the site test)
	}
	v.Set("q", strings.Join(words, " "))
	v.Set("sort", "new")
	v.Set("include_over_18", "on")
	if sub != "" {
		v.Set("restrict_sr", "1")
		return redditQuery{path: "/r/" + sub + "/search", v: v}, nil
	}
	return redditQuery{path: "/search", v: v}, nil
}

// token gets an access token when there's none, it's about to run out, or the site refused it (again).
func (r *reddit) token(ctx context.Context, again bool) error {
	r.mu.Lock()
	fresh := !again && time.Now().Before(r.tokenEnd)
	r.mu.Unlock()
	if fresh {
		return nil
	}
	id, secret := r.c.credential("client_id"), r.c.credential("client_secret")
	if id == "" {
		return ErrNoCredentials
	}
	form := url.Values{"grant_type": {"client_credentials"}}
	switch user, pass := r.c.credential("username"), r.c.credential("password"); {
	case user != "" && pass != "":
		form = url.Values{"grant_type": {"password"}, "username": {user}, "password": {pass}}
	case secret == "":
		r.mu.Lock()
		if r.device == "" {
			b := make([]byte, 12)
			rand.Read(b)
			r.device = hex.EncodeToString(b)
		}
		form = url.Values{"grant_type": {"https://oauth.reddit.com/grants/installed_client"}, "device_id": {r.device}}
		r.mu.Unlock()
	}
	u, err := url.Parse(redditTokenURL)
	if err != nil {
		return err
	}
	r.c.SetToken("")
	body, err := r.c.PostFormBasic(ctx, u, id, secret, form)
	var se *StatusError
	if errors.As(err, &se) && (se.Code == 401 || se.Code == 400) {
		return fmt.Errorf("%s: the app's ID and secret (or the account's name and password) were refused", r.c.m.Site)
	}
	if err != nil {
		return fmt.Errorf("%s: no access token: %w", r.c.m.Site, err)
	}
	var t struct {
		Token   string `json:"access_token"`
		Expires int    `json:"expires_in"`
		Error   string `json:"error"`
	}
	if json.Unmarshal(body, &t) != nil || t.Token == "" {
		if t.Error != "" {
			return fmt.Errorf("%s: no access token: %s (check the app's ID and secret, and the account's name and password)", r.c.m.Site, t.Error)
		}
		return fmt.Errorf("%s: no access token (%s)", r.c.m.Site, snippet(body))
	}
	r.c.SetToken(t.Token)
	r.mu.Lock()
	r.tokenEnd = time.Now().Add(time.Duration(max(t.Expires-300, 60)) * time.Second)
	r.mu.Unlock()
	return nil
}

// get asks the API (a fresh token after a 401).
func (r *reddit) get(ctx context.Context, p string, v url.Values) ([]byte, error) {
	u, err := url.Parse(strings.TrimRight(r.c.m.Base, "/") + p)
	if err != nil {
		return nil, err
	}
	u.RawQuery = v.Encode()
	for try := 0; ; try++ {
		if err := r.token(ctx, try > 0); err != nil {
			return nil, err
		}
		body, err := r.c.API(ctx, u)
		var se *StatusError
		if errors.As(err, &se) && se.Code == 401 && try == 0 {
			continue
		}
		if errors.As(err, &se) && (se.Code == 403 || se.Code == 404) {
			return nil, fmt.Errorf("%s: not found, private or banned (%d)", r.c.m.Site, se.Code)
		}
		return body, err
	}
}

func (r *reddit) listing(ctx context.Context, p string, v url.Values) (redditListing, error) {
	var l redditListing
	body, err := r.get(ctx, p, v)
	if err != nil {
		return l, err
	}
	if err := json.Unmarshal(body, &l); err != nil {
		return l, fmt.Errorf("%s: unexpected reply: %s", r.c.m.Site, snippet(body))
	}
	return l, nil
}

// info fetches one post by its base-36 ID (nil: none).
func (r *reddit) info(ctx context.Context, id36 string) (*redditPost, error) {
	l, err := r.listing(ctx, "/api/info", url.Values{"id": {"t3_" + id36}, "raw_json": {"1"}})
	if err != nil {
		return nil, err
	}
	for _, ch := range l.Data.Children {
		if ch.Kind == "t3" && ch.Data.ID == id36 {
			return &ch.Data, nil
		}
	}
	return nil, nil
}

func (r *reddit) Search(ctx context.Context, query string, before int64) (Page, error) {
	q, err := r.parse(query)
	if err != nil {
		return Page{}, err
	}
	types := map[string]string{}
	page := Page{Total: -1}
	if q.post != "" {
		if before > 0 {
			return Page{Total: -1}, nil // (one post: all of it on the first page)
		}
		p, err := r.info(ctx, q.post)
		if err != nil || p == nil {
			return Page{Total: 0}, err
		}
		page.Posts = r.posts(*p, types)
	} else {
		after := ""
		if before > 0 {
			id := before / 100
			after = "t3_" + strconv.FormatInt(id, 36)
			if before%100 != 0 { // (resumed part-way through a post: its files below that one first)
				p, err := r.info(ctx, strconv.FormatInt(id, 36))
				if err != nil {
					return Page{}, err
				}
				if p != nil {
					for _, post := range r.posts(*p, types) {
						if post.ID < before {
							page.Posts = append(page.Posts, post)
						}
					}
				}
			}
		}
		// Pages with no files we take (links to articles, text posts) are passed over: an empty page is the end.
		for tries := 0; tries < 10 && len(page.Posts) == 0; tries++ {
			v := url.Values{}
			for k, vs := range q.v {
				v[k] = vs
			}
			if after != "" {
				v.Set("after", after)
			}
			l, err := r.listing(ctx, q.path, v)
			if err != nil {
				return Page{}, err
			}
			for _, ch := range l.Data.Children {
				if ch.Kind == "t3" {
					page.Posts = append(page.Posts, r.posts(ch.Data, types)...)
				}
			}
			if after = l.Data.After; after == "" {
				break
			}
		}
	}
	for i := range page.Posts {
		page.Posts[i].TagTypes = types
	}
	return page, nil
}

// Lookup fetches one file's post again (a refresh, or a RedGIFs clip's details when its file is needed).
func (r *reddit) Lookup(ctx context.Context, id int64) (*Post, error) {
	r.mu.Lock()
	pc, ok := r.clips[id]
	r.mu.Unlock()
	if !ok {
		p, err := r.info(ctx, strconv.FormatInt(id/100, 36))
		if err != nil || p == nil {
			return nil, err
		}
		types := map[string]string{}
		for _, post := range r.posts(*p, types) {
			if post.ID == id {
				post.TagTypes = types
				if !post.Partial {
					return &post, nil
				}
				r.mu.Lock()
				pc = r.clips[id]
				r.mu.Unlock()
				ok = true
				break
			}
		}
		if !ok {
			return nil, nil
		}
	}
	return r.clip(ctx, pc)
}

// clip fills a post that links a RedGIFs clip from RedGIFs: its file (with sound) and tags, the clip's user a creator
// too. Gone from RedGIFs: Reddit's own copy, if it has one.
func (r *reddit) clip(ctx context.Context, pc redditClip) (*Post, error) {
	post := pc.post
	post.Partial = false
	types := map[string]string{}
	for k, v := range post.TagTypes {
		types[k] = v
	}
	g, err := r.rg.clip(ctx, pc.clip)
	if err != nil && pc.fallback == "" {
		return nil, err
	}
	switch {
	case g != nil:
		post.FileURL, post.AltFileURLs, post.Width, post.Height = g.FileURL, g.AltFileURLs, g.Width, g.Height
		post.Tags = append([]string(nil), post.Tags...)
		for _, t := range g.Tags {
			if !slices.Contains(post.Tags, t) {
				post.Tags = append(post.Tags, t)
			}
			if typ := g.TagTypes[t]; typ != "" && types[t] == "" {
				types[t] = typ
			}
		}
	case pc.fallback != "":
		post.FileURL = pc.fallback
	default:
		return nil, nil // (gone from RedGIFs and no copy on Reddit)
	}
	post.TagTypes = types
	return &post, nil
}

// posts makes one post per file of a Reddit post (nothing for a text post, a link to an article, a removed post).
func (r *reddit) posts(d redditPost, types map[string]string) []Post {
	id, err := strconv.ParseInt(d.ID, 36, 64)
	if err != nil || id <= 0 || d.Removed != "" {
		return nil
	}
	src := d
	if len(d.Crossposts) > 0 {
		src = d.Crossposts[0] // (a crosspost: the files are the original post's)
	}
	var tags []string
	add := func(t, typ string) {
		if t == "" || types[t] != "" && types[t] != typ && typ != "" {
			return
		}
		for _, have := range tags {
			if have == t {
				return
			}
		}
		tags = append(tags, t)
		types[t] = typ
	}
	add(strings.ToLower(d.Subreddit), "subreddit")
	if a := strings.ToLower(d.Author); a != "" && a != "[deleted]" {
		add(a, "artist")
	}
	if f := redditFlair(d.Flair); f != "" {
		add(f, "general")
	}
	rating := "sfw"
	if d.Over18 || src.Over18 {
		rating = "nsfw"
	}
	base := Post{Tags: tags, Rating: r.c.m.Rating(rating), PostURL: "https://www.reddit.com" + d.Permalink}
	if d.CreatedUTC > 0 {
		base.Posted = time.Unix(int64(d.CreatedUTC), 0).UTC()
	}
	var out []Post
	file := func(u string, w, h int) *Post {
		p := base
		p.Tags = append([]string(nil), tags...)
		p.ID, p.FileURL, p.Width, p.Height = id*100+int64(len(out)), u, w, h
		out = append(out, p)
		return &out[len(out)-1]
	}
	host := ""
	if u, err := url.Parse(src.URL); err == nil {
		host = strings.ToLower(u.Hostname())
	}
	switch {
	case src.IsGallery && src.GalleryData != nil:
		for _, it := range src.GalleryData.Items {
			m, ok := src.MediaMetadata[it.MediaID]
			if !ok || m.Status != "valid" {
				continue
			}
			u := m.S.U
			if m.E == "AnimatedImage" {
				u = m.S.MP4
				if u == "" {
					u = m.S.GIF
				}
			}
			if u == "" || len(out) >= 100 {
				continue
			}
			// An image as it was posted (i.redd.it/<its ID>.<type>); the preview server's copy if that fails.
			if ext, ok := strings.CutPrefix(m.M, "image/"); ok && m.E == "Image" && redditName.MatchString(it.MediaID) && redditName.MatchString(ext) {
				p := file("https://i.redd.it/"+it.MediaID+"."+strings.Replace(ext, "jpeg", "jpg", 1), m.S.X, m.S.Y)
				p.AltFileURLs = []string{u}
				continue
			}
			file(u, m.S.X, m.S.Y)
		}
	case src.Media != nil && src.Media.RedditVideo != nil && src.Media.RedditVideo.FallbackURL != "":
		v := src.Media.RedditVideo
		p := file(v.FallbackURL, v.Width, v.Height)
		if !v.IsGIF && (v.HasAudio == nil || *v.HasAudio) {
			p.AudioURLs = redditAudio(v.FallbackURL)
		}
	case host == "redgifs.com" || strings.HasSuffix(host, ".redgifs.com"):
		m := redgifsClipPath.FindStringSubmatch(mustPath(src.URL))
		if m == nil {
			break
		}
		fallback := ""
		if src.Preview != nil && src.Preview.VideoPreview != nil {
			fallback = src.Preview.VideoPreview.FallbackURL
		}
		p := file("", 0, 0)
		p.Partial = true
		p.Sources = []string{src.URL}
		p.TagTypes = types // (the Reddit tags types, for Lookup: the page sets it again for the others)
		r.remember(redditClip{post: *p, clip: strings.ToLower(m[1]), fallback: fallback})
	case host == "i.redd.it":
		w, h := 0, 0
		if src.Preview != nil && len(src.Preview.Images) > 0 {
			w, h = src.Preview.Images[0].Source.Width, src.Preview.Images[0].Source.Height
		}
		file(src.URL, w, h)
	case src.Preview != nil && src.Preview.VideoPreview != nil && src.Preview.VideoPreview.FallbackURL != "":
		// (an animation on another site Reddit keeps a copy of: that copy, without sound)
		v := src.Preview.VideoPreview
		p := file(v.FallbackURL, v.Width, v.Height)
		p.Sources = []string{src.URL}
	}
	// Newest first like everything else: a gallery from its last file to its first (IDs going down, as the
	// downloader's resume point needs - see the Mastodon engine).
	slices.Reverse(out)
	return out
}

// remember keeps a RedGIFs post for Lookup.
func (r *reddit) remember(pc redditClip) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.clips[pc.post.ID]; !ok {
		r.order = append(r.order, pc.post.ID)
		if len(r.order) > redditClipsMax {
			delete(r.clips, r.order[0])
			r.order = r.order[1:]
		}
	}
	r.clips[pc.post.ID] = pc
}

// redditAudio is where a Reddit video's sound may be: next to the video, under one of the names Reddit has used.
func redditAudio(video string) []string {
	u, err := url.Parse(video)
	if err != nil {
		return nil
	}
	u.RawQuery = ""
	dir := path.Dir(u.Path)
	var out []string
	for _, name := range []string{"DASH_AUDIO_128.mp4", "DASH_AUDIO_64.mp4", "DASH_audio.mp4", "audio"} {
		a := *u
		a.Path = dir + "/" + name
		out = append(out, a.String())
	}
	return out
}

// redditFlair turns a post's flair into a tag ("" for none, or one too long to be a tag).
func redditFlair(f string) string {
	f = strings.TrimSpace(redditEmoji.ReplaceAllString(f, " "))
	if f == "" || len(f) > 40 {
		return ""
	}
	return strings.Join(strings.Fields(strings.ToLower(f)), "_")
}

func mustPath(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Path
}
