package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// mastodon reads a Mastodon server's API (https://docs.joinmastodon.org):
// hashtag timelines and profiles' media posts. Each file of a post is one
// Vitrine post (ID = status ID * 10 + the file's place, 0-3), with the post's
// hashtags and its author as the creator (tag type "artist").
//
// Searches: "#tag" (the hashtag's timeline), "#tag #other" (both), "#tag
// ~#other" (either), "#tag -#other" (without), "@name" (a profile's own media
// posts, no boosts), "@name #tag" (theirs with that hashtag); a pasted
// /tags/<tag> or /@name address works too. A word without # is a hashtag too. Without an access token, hashtag timelines are
// the server's own users' posts (most servers only show those to guests).
type mastodon struct {
	c *Client

	mu       sync.Mutex
	accounts map[string]string // acct -> account ID
	fav      favLists
}

type mastoStatus struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Sensitive bool      `json:"sensitive"`
	URL       string    `json:"url"`
	Account   struct {
		Acct string `json:"acct"`
	} `json:"account"`
	Tags []struct {
		Name string `json:"name"`
	} `json:"tags"`
	Media []struct {
		Type string `json:"type"` // image, gifv, video, audio, unknown
		URL  string `json:"url"`
		Meta struct {
			Original struct {
				Width  int `json:"width"`
				Height int `json:"height"`
			} `json:"original"`
		} `json:"meta"`
	} `json:"media_attachments"`
	Reblog *json.RawMessage `json:"reblog"`
}

// mastoQuery is a parsed search.
type mastoQuery struct {
	account         string
	tags, any, none []string
}

func parseMastoQuery(q string) (mastoQuery, error) {
	var mq mastoQuery
	for _, w := range strings.Fields(q) {
		if u, err := url.Parse(w); err == nil && u.Scheme != "" && u.Host != "" {
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			switch {
			case len(parts) >= 2 && parts[0] == "tags":
				mq.tags = append(mq.tags, parts[1])
			case len(parts) >= 1 && strings.HasPrefix(parts[0], "@"):
				mq.account = strings.TrimPrefix(parts[0], "@")
			}
			continue
		}
		switch {
		case strings.HasPrefix(w, "@"):
			mq.account = strings.TrimPrefix(w, "@")
		case strings.HasPrefix(w, "~"):
			mq.any = append(mq.any, strings.TrimPrefix(strings.TrimPrefix(w, "~"), "#"))
		case strings.HasPrefix(w, "-"):
			mq.none = append(mq.none, strings.TrimPrefix(strings.TrimPrefix(w, "-"), "#"))
		default:
			mq.tags = append(mq.tags, strings.TrimPrefix(w, "#"))
		}
	}
	for _, l := range [][]string{mq.tags, mq.any, mq.none} {
		for i, t := range l {
			l[i] = strings.ToLower(t)
		}
	}
	if mq.account == "" && len(mq.tags) == 0 {
		if len(mq.any) == 0 {
			return mq, errors.New("type a #hashtag or a @profile")
		}
		mq.tags, mq.any = mq.any[:1], mq.any[1:] // "~a ~b": a or b
		if len(mq.any) == 0 {
			mq.any = nil
		}
	}
	return mq, nil
}

// keeps reports whether a status matches the parts of the search the API
// couldn't do itself (a profile's extra hashtags).
func (mq mastoQuery) keeps(st mastoStatus) bool {
	has := map[string]bool{}
	for _, t := range st.Tags {
		has[strings.ToLower(t.Name)] = true
	}
	for _, t := range mq.tags {
		if !has[t] {
			return false
		}
	}
	for _, t := range mq.none {
		if has[t] {
			return false
		}
	}
	if len(mq.any) == 0 {
		return true
	}
	for _, t := range mq.any {
		if has[t] {
			return true
		}
	}
	return false
}

func (e *mastodon) api(ctx context.Context, path string, q url.Values) ([]byte, error) {
	u, err := url.Parse(strings.TrimRight(e.c.m.Base, "/") + path)
	if err != nil {
		return nil, err
	}
	u.RawQuery = q.Encode()
	return e.c.API(ctx, u)
}

// accountID finds a profile's account ID by name.
func (e *mastodon) accountID(ctx context.Context, acct string) (string, error) {
	e.mu.Lock()
	id, ok := e.accounts[strings.ToLower(acct)]
	e.mu.Unlock()
	if ok {
		return id, nil
	}
	body, err := e.api(ctx, "/api/v1/accounts/lookup", url.Values{"acct": {acct}})
	var se *StatusError
	if errors.As(err, &se) && se.Code == 404 {
		return "", fmt.Errorf("%s: no profile @%s", e.c.m.Site, acct)
	}
	if err != nil {
		return "", err
	}
	var a struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(body, &a) != nil || a.ID == "" {
		return "", fmt.Errorf("%s: unexpected reply: %s", e.c.m.Site, snippet(body))
	}
	e.mu.Lock()
	if e.accounts == nil {
		e.accounts = map[string]string{}
	}
	e.accounts[strings.ToLower(acct)] = a.ID
	e.mu.Unlock()
	return a.ID, nil
}

func (e *mastodon) Search(ctx context.Context, query string, before int64) (Page, error) {
	if user, ok := Favorites(query); ok {
		return e.favorites(ctx, user, before)
	}
	mq, err := parseMastoQuery(query)
	if err != nil {
		return Page{}, fmt.Errorf("%s: %w", e.c.m.Site, err)
	}
	path := ""
	q := url.Values{"only_media": {"true"}, "limit": {"40"}}
	filter := mastoQuery{} // what's checked here (the API can't)
	if mq.account != "" {
		id, err := e.accountID(ctx, mq.account)
		if err != nil {
			return Page{}, err
		}
		path = "/api/v1/accounts/" + id + "/statuses"
		q.Set("exclude_reblogs", "true")
		if len(mq.tags) > 0 {
			q.Set("tagged", mq.tags[0])
		}
		filter = mastoQuery{tags: mq.tags, any: mq.any, none: mq.none}
	} else {
		path = "/api/v1/timelines/tag/" + url.PathEscape(mq.tags[0])
		for _, t := range mq.tags[1:] {
			q.Add("all[]", t)
		}
		for _, t := range mq.any {
			q.Add("any[]", t)
		}
		for _, t := range mq.none {
			q.Add("none[]", t)
		}
		if !e.c.HasCredentials() || len(e.c.m.Auth.Fields) == 0 {
			q.Set("local", "true") // guests see the server's own users' posts
		}
	}
	// Below a post ID: a file of a status (status*10 + its number) - below it in that status are its files with
	// smaller numbers, so the status itself comes again when that's one of its later files (a search resumed
	// part-way through a status; the downloader skips the files at and above the point).
	maxID := ""
	if before > 0 {
		sid := before / 10
		if before%10 != 0 {
			sid++
		}
		maxID = strconv.FormatInt(sid, 10)
	}
	// Pages whose posts are all filtered out (or have no files we take) are
	// passed over, so an empty page means the end.
	page := Page{Total: -1}
	types := map[string]string{}
	for tries := 0; tries < 10; tries++ {
		if maxID != "" {
			q.Set("max_id", maxID)
		}
		body, err := e.api(ctx, path, q)
		if err != nil {
			return Page{}, err
		}
		var sts []mastoStatus
		if err := json.Unmarshal(body, &sts); err != nil {
			return Page{}, fmt.Errorf("%s: unexpected reply: %s", e.c.m.Site, snippet(body))
		}
		if len(sts) == 0 {
			break
		}
		for _, st := range sts {
			if st.Reblog != nil && string(*st.Reblog) != "null" || !filter.keeps(st) {
				continue
			}
			page.Posts = append(page.Posts, e.posts(st, types)...)
		}
		if len(page.Posts) > 0 {
			break
		}
		maxID = sts[len(sts)-1].ID
	}
	for i := range page.Posts {
		page.Posts[i].TagTypes = types
	}
	if len(page.Posts) == 0 {
		page.Total = 0
	}
	return page, nil
}

// posts makes one post per file of a status (images, animations, videos).
func (e *mastodon) posts(st mastoStatus, types map[string]string) []Post {
	sid, err := strconv.ParseInt(st.ID, 10, 64)
	if err != nil || sid <= 0 || sid > (1<<63-1)/10 {
		return nil
	}
	creator := strings.ToLower(st.Account.Acct)
	tags := []string{}
	seen := map[string]bool{}
	for _, t := range st.Tags {
		n := strings.ToLower(t.Name)
		if n != "" && !seen[n] {
			seen[n] = true
			tags = append(tags, n)
		}
	}
	if creator != "" && !seen[creator] {
		tags = append(tags, creator)
		types[creator] = "artist"
	}
	rating := "safe"
	if st.Sensitive {
		rating = "sensitive"
	}
	// Newest first like everything else: a status's files from its last to its first (their IDs going down, as
	// the downloader's resume point needs - in the other order, a search resumed part-way through a status
	// skipped its last files for good).
	var out []Post
	for i := len(st.Media) - 1; i >= 0; i-- {
		m := st.Media[i]
		if i > 9 || m.URL == "" || (m.Type != "image" && m.Type != "gifv" && m.Type != "video") {
			continue
		}
		out = append(out, Post{
			ID: sid*10 + int64(i), FileURL: m.URL, Tags: tags, PostURL: st.URL, Posted: st.CreatedAt,
			Rating: e.c.m.Rating(rating), Width: m.Meta.Original.Width, Height: m.Meta.Original.Height,
		})
	}
	return out
}

// Lookup fetches one file of a status (nil if the post or the file is gone).
func (e *mastodon) Lookup(ctx context.Context, id int64) (*Post, error) {
	body, err := e.api(ctx, "/api/v1/statuses/"+strconv.FormatInt(id/10, 10), nil)
	var se *StatusError
	if errors.As(err, &se) && (se.Code == 404 || se.Code == 410) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var st mastoStatus
	if err := json.Unmarshal(body, &st); err != nil {
		return nil, fmt.Errorf("%s: unexpected reply: %s", e.c.m.Site, snippet(body))
	}
	types := map[string]string{}
	for _, p := range e.posts(st, types) {
		if p.ID == id {
			p.TagTypes = types
			return &p, nil
		}
	}
	return nil, nil
}
