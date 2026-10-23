package plugin

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// shimmie reads a Shimmie2 site (https://github.com/shish/shimmie2) through
// its web pages - many Shimmie sites have no API switched on. A list page
// (/post/list/<tags>/1) gives each post's ID, tags and md5 (the thumbnail's
// folder), so posts the library has or deleted are skipped without opening
// them; a post's page (/post/view/<id>) gives the file, source, rating and
// upload time.
//
// Searches: tags joined with "+"; resuming adds id<N; order=id_desc is always
// added (without it some sites mix older posts in) unless the search has its
// own order=. Login (auth.login: shimmie2): the site's login form, then its
// session cookie.
type shimmie struct {
	c       *Client
	loginMu sync.Mutex // one login at a time
}

var (
	// <a href='/post/view/1' class='thumb shm-thumb ...' data-tags='...' ...><img ... src='/_thumbs/<md5>/thumb.jpg'
	shmThumb    = regexp.MustCompile(`(?s)<a\s([^>]*\bshm-thumb\b[^>]*)>\s*<img\s([^>]*)>`)
	shmThumbMD5 = regexp.MustCompile(`/_thumbs/([0-9a-f]{32})/`)
	shmListPage = regexp.MustCompile(`/post/list/(?:[^"'/]+/)?(\d+)["']`)
	shmFileForm = regexp.MustCompile(`<form action=['"]([^'"]*/_images/[0-9a-f]{32}/[^'"]+)['"]`)
	shmVideo    = regexp.MustCompile(`<source src=['"]([^'"]*/_images/[^'"]+)['"]`)
	shmMainImg  = regexp.MustCompile(`<img[^>]*\bid=['"]main_image['"][^>]*\bsrc=['"]([^'"]+)['"]|<img[^>]*\bsrc=['"]([^'"]+)['"][^>]*\bid=['"]main_image['"]`)
	shmFileMD5  = regexp.MustCompile(`/_images/([0-9a-f]{32})/`)
	shmTagName  = regexp.MustCompile(`<a class=["']tag_name["'] href=["']/post/list/([^"'/]+)/1["']`)
	shmPosted   = regexp.MustCompile(`(?s)<th>Uploader</th>.*?<time datetime=['"]([^'"]+)['"]`)
	shmSource   = regexp.MustCompile(`(?s)<th>Source</th>\s*<td>(.*?)</td>`)
	shmHref     = regexp.MustCompile(`href=['"]([^'"]+)['"]`)
	shmRating   = regexp.MustCompile(`(?s)<th>Rating</th>\s*<td>\s*([^<]*?)\s*</td>`)
	shmLoggedIn = regexp.MustCompile(`/user_admin/logout|/user/logout|>Log Out<`)
)

// attr reads an HTML attribute (” or "" quoted) from a tag's attributes.
func attr(attrs, name string) string {
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `=(?:'([^']*)'|"([^"]*)")`)
	m := re.FindStringSubmatch(attrs)
	if m == nil {
		return ""
	}
	return html.UnescapeString(m[1] + m[2])
}

// decaret undoes Shimmie's caret codes in a tag link: it writes ^ / \ ? & .
// as ^^ ^s ^b ^q ^a ^d ("bloom_^a_rage" is "bloom_&_rage"). An unknown code
// stays as it is.
func decaret(s string) string {
	if !strings.Contains(s, "^") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '^' && i+1 < len(s) {
			if c, ok := shmCarets[s[i+1]]; ok {
				b.WriteByte(c)
				i++
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

var shmCarets = map[byte]byte{'^': '^', 's': '/', 'b': '\\', 'q': '?', 'a': '&', 'd': '.'}

// listURL is the first page of a search.
func (s *shimmie) listURL(query string, before int64) (*url.URL, error) {
	tags := strings.Fields(query)
	ordered := false
	for _, t := range tags {
		if strings.HasPrefix(strings.ToLower(t), "order=") || strings.HasPrefix(strings.ToLower(t), "order:") {
			ordered = true
		}
	}
	if before > 0 {
		tags = append(tags, "id<"+strconv.FormatInt(before, 10))
	}
	if !ordered {
		tags = append(tags, "order=id_desc")
	}
	parts := make([]string, len(tags))
	for i, t := range tags {
		parts[i] = strings.ReplaceAll(url.PathEscape(t), "+", "%2B")
	}
	return url.Parse(strings.TrimRight(s.c.m.Base, "/") + "/post/list/" + strings.Join(parts, "+") + "/1")
}

func (s *shimmie) Search(ctx context.Context, query string, before int64) (Page, error) {
	if user, ok := Favorites(query); ok {
		q, err := s.favQuery(user)
		if err != nil {
			return Page{}, err
		}
		query = q
	}
	m := s.c.m
	if err := s.login(ctx); err != nil {
		return Page{}, err
	}
	u, err := s.listURL(query, before)
	if err != nil {
		return Page{}, err
	}
	body, err := s.c.API(ctx, u)
	var se *StatusError
	if errors.As(err, &se) && se.Code == 404 {
		return Page{Total: 0}, nil // Shimmie answers a search with no posts with a 404 page
	}
	if err != nil {
		return Page{}, err
	}
	text := string(body)
	if !strings.Contains(text, "<html") && !strings.Contains(text, "<HTML") {
		return Page{}, fmt.Errorf("%s: unexpected reply: %s", m.Site, snippet(body))
	}
	page := Page{Total: -1}
	seen := map[int64]bool{}
	for _, mm := range shmThumb.FindAllStringSubmatch(text, -1) {
		a, img := mm[1], mm[2]
		id, _ := strconv.ParseInt(attr(a, "data-post-id"), 10, 64)
		if id <= 0 {
			if v := strings.TrimPrefix(attr(a, "href"), "/post/view/"); v != "" {
				id, _ = strconv.ParseInt(v, 10, 64)
			}
		}
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		p := Post{ID: id, PostURL: m.Post(id), Partial: true, Rating: m.Rating("")}
		p.Tags = strings.Fields(strings.ToLower(attr(a, "data-tags")))
		p.Width, _ = strconv.Atoi(attr(a, "data-width"))
		p.Height, _ = strconv.Atoi(attr(a, "data-height"))
		if md := shmThumbMD5.FindStringSubmatch(attr(img, "src")); md != nil {
			if b, err := hex.DecodeString(md[1]); err == nil {
				copy(p.MD5[:], b)
			}
		}
		page.Posts = append(page.Posts, p)
	}
	// The total, roughly: the last page number (pagination links) times a page.
	last := 1
	for _, mm := range shmListPage.FindAllStringSubmatch(text, -1) {
		last = max(last, atoiSafe(mm[1]))
	}
	if len(page.Posts) == 0 {
		page.Total = 0
	} else {
		page.Total = last * len(page.Posts)
	}
	return page, nil
}

// Lookup reads a post's page; nil if the site doesn't have it.
func (s *shimmie) Lookup(ctx context.Context, id int64) (*Post, error) {
	if err := s.login(ctx); err != nil {
		return nil, err
	}
	u, err := url.Parse(strings.TrimRight(s.c.m.Base, "/") + "/post/view/" + strconv.FormatInt(id, 10))
	if err != nil {
		return nil, err
	}
	body, err := s.c.API(ctx, u)
	var se *StatusError
	if errors.As(err, &se) && se.Code == 404 {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return parseShimmiePost(s.c.m, id, string(body)), nil
}

// parseShimmiePost reads a Shimmie2 post page (nil: no file on it).
func parseShimmiePost(m Manifest, id int64, text string) *Post {
	p := &Post{ID: id, PostURL: m.Post(id), Rating: m.Rating("")}
	// The file: the Download form's link (images and videos alike), else the
	// video's source or the main image.
	switch {
	case shmFileForm.MatchString(text):
		p.FileURL = absURL(m.Base, shmFileForm.FindStringSubmatch(text)[1])
	case shmVideo.MatchString(text):
		p.FileURL = absURL(m.Base, shmVideo.FindStringSubmatch(text)[1])
	default:
		if mm := shmMainImg.FindStringSubmatch(text); mm != nil {
			p.FileURL = absURL(m.Base, mm[1]+mm[2])
		}
	}
	if p.FileURL == "" {
		return nil
	}
	if mm := shmFileMD5.FindStringSubmatch(p.FileURL); mm != nil {
		if b, err := hex.DecodeString(mm[1]); err == nil {
			copy(p.MD5[:], b)
		}
	}
	seen := map[string]bool{}
	for _, mm := range shmTagName.FindAllStringSubmatch(text, -1) {
		name, err := url.PathUnescape(html.UnescapeString(mm[1]))
		name = strings.ToLower(decaret(name))
		if err != nil || name == "" || seen[name] {
			continue
		}
		seen[name] = true
		p.Tags = append(p.Tags, name)
	}
	if mm := shmSource.FindStringSubmatch(text); mm != nil {
		var hrefs []string
		for _, h := range shmHref.FindAllStringSubmatch(mm[1], -1) {
			hrefs = append(hrefs, html.UnescapeString(h[1]))
		}
		p.Sources = SourceLinks(strings.Join(hrefs, " "))
	}
	if mm := shmRating.FindStringSubmatch(text); mm != nil {
		p.Rating = m.Rating(strings.ToLower(strings.TrimSpace(mm[1])))
	}
	if mm := shmPosted.FindStringSubmatch(text); mm != nil {
		if t, err := time.Parse(time.RFC3339, mm[1]); err == nil {
			p.Posted = t
		}
	}
	return p
}

// login signs in through the site's login form when the plugin uses it and
// credentials are set, and again whenever the session has run out.
func (s *shimmie) login(ctx context.Context) error {
	c := s.c
	if c.m.Auth.Login != "shimmie2" || !c.HasCredentials() {
		return nil
	}
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	if c.hasSession() {
		return nil
	}
	c.credMu.RLock()
	user, pass := c.creds[c.m.Auth.Fields[0]], c.creds[c.m.Auth.Fields[1]]
	c.credMu.RUnlock()
	u, err := url.Parse(strings.TrimRight(c.m.Base, "/") + "/user_admin/login")
	if err != nil {
		return err
	}
	body, err := c.PostForm(ctx, u, url.Values{"user": {user}, "pass": {pass}, "gobu": {"Log In"}})
	if err != nil {
		return fmt.Errorf("%s: logging in: %w", c.m.Site, err)
	}
	if !c.hasSession() && !shmLoggedIn.Match(body) {
		return fmt.Errorf("%s: %w", c.m.Site, ErrLoginRefused)
	}
	return nil
}
