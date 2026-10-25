package plugin

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// UserAgent identifies Vitrine to sites, with a contact URL. Set Version at
// start-up.
var UserAgent = "Vitrine/dev (+https://github.com/usevitrine/vitrine; hello@vitrine.software)"

// Client makes a plugin's HTTP requests: only to the plugin's domains, with
// its rate limit for API calls and its connection limit for files. Errors
// never contain request URLs, which may carry credentials.
type Client struct {
	m      Manifest
	credMu sync.RWMutex // credentials can change while the server runs
	creds  map[string]string
	http   *http.Client
	solver *Solver // FlareSolverr for Cloudflare challenges (nil: none)

	mu         sync.Mutex
	next       time.Time // earliest time for the next API request
	foreground int       // foreground requests waiting for a turn (they go first: see Foreground)
	// tooMany is how long all requests wait after a 429 without Retry-After.
	tooMany time.Duration

	files    chan struct{} // connection slots for file downloads
	pics     chan struct{} // ... and for browsing's small pictures (Picture)
	picsOnce sync.Once
	urgent   chan struct{} // one more, for a download someone waits for (see Urgent)
	log      *slog.Logger  // the site's requests (debug), nil: none
	// fileIdle: a file download with no answer for this long, or less than
	// fileMin bytes in that long (a stall or a trickle mid-file), is given up -
	// a hung or throttled connection never holds a slot for good.
	fileIdle time.Duration
	fileMin  int64
	// The site's usual speed for a file (bytes a second, a running average of files over 4 MB that came in one go;
	// 0: not known yet): a connection far below it - a CDN server trickling at 100 KB/s while a fresh connection gets
	// 30 MB/s - is given up and carried on on a fresh one (see slowMin).
	rateMu sync.Mutex
	rate   float64
	brk    breaker // pauses the site when it keeps failing to answer (breaker.go)
}

// NewClient returns a client for a plugin with the user's credentials for
// it (may be nil).
func NewClient(m Manifest, creds map[string]string) *Client {
	c := &Client{
		m: m, creds: creds,
		brk: breaker{now: time.Now, after: PauseAfter},
		http: &http.Client{
			Transport: newTransport(),
			Timeout:   0, // per-request contexts set deadlines
			// A redirect must stay on the plugin's domains too.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("%s: too many redirects", m.Site)
				}
				if !m.allows(req.URL.Hostname()) {
					return fmt.Errorf("%s: redirected to %s, not one of the plugin's domains", m.Site, req.URL.Hostname())
				}
				return nil
			},
		},
		files:    make(chan struct{}, m.Limits.Connections),
		urgent:   make(chan struct{}, 1),
		tooMany:  time.Minute,
		fileIdle: 20 * time.Second, // (short: a file that breaks off carries on - see resumingBody)
		fileMin:  256 << 10,
	}
	if m.Auth.Login == "shimmie2" || m.Auth.Login == "ehentai" || m.Auth.Login == "gelbooru" {
		c.http.Jar = newSessionJar() // the site's session after logging in
	}
	return c
}

// WithCredentials is a copy of the client with other credentials and a fresh state (no waits, no pause, a login
// session of its own) on the same connections: to try typed credentials without touching the client in use.
func (c *Client) WithCredentials(creds map[string]string) *Client {
	n := NewClient(c.m, creds)
	n.solver, n.log, n.tooMany = c.solver, c.log, c.tooMany
	h := *c.http
	h.Jar = n.http.Jar
	n.http = &h
	return n
}

// newTransport is the default one with a connection of its own per request (HTTP/1.1): over HTTP/2 every file
// from a host shared one connection - a CDN throttling it slowed all of them to a crawl, and a retry came back
// on the same slow connection.
func newTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.ForceAttemptHTTP2 = false
	t.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{} // (non-nil and empty: no HTTP/2)
	// ... and the TLS handshake offers only HTTP/1.1 (a copy of the default can still offer h2 there: the server
	// then answered in HTTP/2 frames that couldn't be read).
	if t.TLSClientConfig == nil {
		t.TLSClientConfig = &tls.Config{}
	}
	t.TLSClientConfig.NextProtos = []string{"http/1.1"}
	t.MaxIdleConnsPerHost = 16 // (kept between files - and pictures: 16 at once, each a new TLS handshake otherwise)
	return t
}

// sessionJar keeps a site's cookies (its login session) and can be emptied
// while requests use it.
type sessionJar struct {
	mu  sync.Mutex
	jar *cookiejar.Jar
}

func newSessionJar() *sessionJar {
	j, _ := cookiejar.New(nil)
	return &sessionJar{jar: j}
}

func (s *sessionJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jar.SetCookies(u, cookies)
}

func (s *sessionJar) Cookies(u *url.URL) []*http.Cookie {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.jar.Cookies(u)
}

func (s *sessionJar) reset() {
	j, _ := cookiejar.New(nil)
	s.mu.Lock()
	s.jar = j
	s.mu.Unlock()
}

// ErrLoginRefused means the site didn't accept the user name and password.
var ErrLoginRefused = errors.New("the site didn't accept the user name and password: check them in Settings > Sites")

// hasSession reports whether a login session is kept for the site.
func (c *Client) hasSession() bool {
	if c.http.Jar == nil {
		return false
	}
	u, err := url.Parse(c.m.Base)
	if err != nil {
		return false
	}
	for _, ck := range c.http.Jar.Cookies(u) {
		if ck.Name == "shm_session" || ck.Name == "pass_hash" { // (Shimmie2; Gelbooru 0.2)
			return true
		}
	}
	return false
}

// sessionCookie is a cookie of the signed-in session (e.g. Gelbooru's user_id); "" without one.
func (c *Client) sessionCookie(name string) string {
	if c.http.Jar == nil {
		return ""
	}
	u, err := url.Parse(c.m.Base)
	if err != nil {
		return ""
	}
	for _, ck := range c.http.Jar.Cookies(u) {
		if ck.Name == name {
			return ck.Value
		}
	}
	return ""
}

// PostForm posts a form (a login) to an allowed host and returns the reply,
// at most 2 MB. Errors never contain the form's values.
func (c *Client) PostForm(ctx context.Context, u *url.URL, form url.Values) ([]byte, error) {
	if !c.m.allows(u.Hostname()) || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("%s: %s is not one of the plugin's domains", c.m.Site, u.Hostname())
	}
	if err := c.waitTurn(ctx); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("%s: bad request", c.m.Site)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", UserAgent)
	if c.solver != nil {
		c.solver.apply(req)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: request to %s failed: %w", c.m.Site, u.Hostname(), unwrapURL(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &StatusError{Site: c.m.Site, Host: u.Hostname(), Code: resp.StatusCode}
	}
	return io.ReadAll(io.LimitReader(resp.Body, 2<<20))
}

// PostJSON posts a JSON request (an API call) to an allowed host under the
// rate limit and returns the reply, at most 8 MB.
func (c *Client) PostJSON(ctx context.Context, u *url.URL, body []byte) ([]byte, error) {
	if !c.m.allows(u.Hostname()) || u.Scheme != "https" {
		return nil, fmt.Errorf("%s: %s is not one of the plugin's domains", c.m.Site, u.Hostname())
	}
	if err := c.waitTurn(ctx); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%s: bad request", c.m.Site)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	if c.solver != nil {
		c.solver.apply(req)
	}
	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		err = fmt.Errorf("%s: request to %s failed: %w", c.m.Site, u.Hostname(), unwrapURL(err))
		c.logRequest(u, 0, start, err)
		return nil, err
	}
	defer resp.Body.Close()
	c.logRequest(u, resp.StatusCode, start, nil)
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusTooManyRequests {
			c.backOff(resp.Header.Get("Retry-After"))
		}
		return nil, &StatusError{Site: c.m.Site, Host: u.Hostname(), Code: resp.StatusCode}
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

// Manifest is the client's plugin.
func (c *Client) Manifest() Manifest { return c.m }

// ErrNotAFile means the site answered a file request with a web page (a
// hotlink or login page); trying again won't help.
var ErrNotAFile = errors.New("the site sent a web page instead of the file")

// ErrNoCredentials means the plugin needs credentials the user hasn't set.
var ErrNoCredentials = errors.New("this site needs an account: add its credentials in Settings > Sites")

// SetCredentials replaces the credentials (takes effect with the next request).
func (c *Client) SetCredentials(creds map[string]string) {
	c.credMu.Lock()
	c.creds = creds
	c.credMu.Unlock()
	if j, ok := c.http.Jar.(*sessionJar); ok {
		j.reset() // new credentials: log in again
	}
}

// CredentialSet reports which auth fields have a value (never the values).
func (c *Client) CredentialSet() map[string]bool {
	c.credMu.RLock()
	defer c.credMu.RUnlock()
	out := map[string]bool{}
	for _, f := range c.m.Auth.Fields {
		out[f] = c.creds[f] != ""
	}
	return out
}

// HasCredentials reports whether every auth field is set (ehentai: a user
// name and password, or the two login cookies).
func (c *Client) HasCredentials() bool {
	c.credMu.RLock()
	defer c.credMu.RUnlock()
	if c.m.Auth.Login == "ehentai" {
		return (c.creds["username"] != "" && c.creds["password"] != "") || (c.creds["ipb_member_id"] != "" && c.creds["ipb_pass_hash"] != "")
	}
	for _, f := range c.m.Auth.Fields {
		if c.creds[f] == "" {
			return false
		}
	}
	return true
}

type foregroundKey struct{}

// Foreground marks requests someone is looking at the screen for (Comics > Browse): they take the site's next turn
// before the ones in the background (downloads, subscription checks) - the rate stays the same.
func Foreground(ctx context.Context) context.Context {
	return context.WithValue(ctx, foregroundKey{}, true)
}

// waitTurn blocks until the rate limit allows another API request: the next turn goes to a foreground request
// when one waits. A request called off while it waits never takes a turn (a search replaced by a newer one doesn't
// hold the next one up).
func (c *Client) waitTurn(ctx context.Context) error {
	fg, _ := ctx.Value(foregroundKey{}).(bool)
	interval := time.Duration(float64(time.Second) / c.m.Limits.RequestsPerSec)
	if fg {
		c.mu.Lock()
		c.foreground++
		c.mu.Unlock()
		defer func() {
			c.mu.Lock()
			c.foreground--
			c.mu.Unlock()
		}()
	}
	for {
		c.mu.Lock()
		now := time.Now()
		if !now.Before(c.next) && (fg || c.foreground == 0) {
			c.next = now.Add(interval)
			c.mu.Unlock()
			return nil
		}
		wait := time.Until(c.next)
		if wait <= 0 {
			wait = 25 * time.Millisecond // (this turn is a foreground request's: a look again in a moment)
		}
		c.mu.Unlock()
		t := time.NewTimer(wait)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		}
	}
}

// API fetches an API URL (credentials added) and returns the body, at most
// 32 MB.
func (c *Client) API(ctx context.Context, u *url.URL) ([]byte, error) {
	body, _, err := c.APIHeader(ctx, u)
	return body, err
}

// APIHeader is API with the reply's headers too (e.g. a Mastodon list's Link to its next page).
func (c *Client) APIHeader(ctx context.Context, u *url.URL) ([]byte, http.Header, error) {
	if c.m.Auth.Required && !c.HasCredentials() {
		return nil, nil, ErrNoCredentials
	}
	if c.m.Auth.Login == "" { // API keys go in the query; a login never does
		q := u.Query()
		c.credMu.RLock()
		for _, f := range c.m.Auth.Fields {
			if v := c.creds[f]; v != "" {
				q.Set(f, v)
			}
		}
		c.credMu.RUnlock()
		u.RawQuery = q.Encode()
	}
	// "Too many requests" is tried again (get has pushed back every request
	// to the site); other errors aren't.
	for attempt := 1; ; attempt++ {
		body, h, err := c.apiHeader(ctx, u)
		var se *StatusError
		if errors.As(err, &se) && se.Code == http.StatusTooManyRequests && attempt < 3 {
			continue
		}
		return body, h, err
	}
}

func (c *Client) api(ctx context.Context, u *url.URL) ([]byte, error) {
	body, _, err := c.apiHeader(ctx, u)
	return body, err
}

func (c *Client) apiHeader(ctx context.Context, u *url.URL) ([]byte, http.Header, error) {
	if err := c.waitTurn(ctx); err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	resp, err := c.getWith(ctx, u, "", true)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	return body, resp.Header, err
}

// WebPage fetches one of the site's own web pages - never with the plugin's
// credentials in it (an API key belongs to API requests only) - under the same
// rate limit, through FlareSolverr when the page is behind Cloudflare.
func (c *Client) WebPage(ctx context.Context, u *url.URL) ([]byte, error) {
	return c.api(ctx, u)
}

// credential is one credential field's value ("" = not set), for the engines
// (e.g. whose favorites "fav:me" means) - never logged or shown.
func (c *Client) credential(field string) string {
	c.credMu.RLock()
	defer c.credMu.RUnlock()
	return c.creds[field]
}

// backOff makes the site's next request wait: Retry-After if the site sent
// one (at most 10 minutes), else a minute.
func (c *Client) backOff(retryAfter string) {
	wait := c.tooMany
	if s, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && s > 0 {
		wait = min(time.Duration(s)*time.Second, 10*time.Minute)
	}
	c.mu.Lock()
	if at := time.Now().Add(wait); at.After(c.next) {
		c.next = at
	}
	c.mu.Unlock()
}

// File starts downloading a file. Close the body to free the connection
// slot. A file that breaks off on the way (a stall, a trickle, a cut
// connection) carries on where it stopped when the site allows (a Range
// request), on the same slot - the reader sees one whole file.
func (c *Client) File(ctx context.Context, rawURL string) (io.ReadCloser, int64, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, 0, fmt.Errorf("bad file URL from %s", c.m.Site)
	}
	slots, have := c.files, false
	if ctx.Value(urgentKey{}) != nil {
		// Someone waits for it (a Retry): a free slot, or the one kept for that - not behind a search's files.
		select {
		case c.files <- struct{}{}:
			have = true
		default:
			slots = c.urgent
		}
	}
	if !have {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return nil, 0, ctx.Err()
		}
	}
	part, err := c.openFile(context.WithValue(ctx, relativeKey{}, true), u, 0)
	if err != nil {
		<-slots
		return nil, 0, err
	}
	// A web page instead of the file (a hotlink or login page): never imported.
	if ct := part.resp.Header.Get("Content-Type"); strings.HasPrefix(ct, "text/html") {
		part.close()
		<-slots
		return nil, 0, fmt.Errorf("%s: %w", c.m.Site, ErrNotAFile)
	}
	b := &resumingBody{c: c, ctx: ctx, u: u, part: part, total: part.resp.ContentLength,
		ranges: part.resp.Header.Get("Accept-Ranges") == "bytes", start: time.Now()}
	modified, _ := http.ParseTime(part.resp.Header.Get("Last-Modified"))
	return &slotBody{ReadCloser: b, release: func() { <-slots }, modified: modified}, part.resp.ContentLength, nil
}

// Modified asks for a file only for its Last-Modified time (zero if the site sends none): the request is dropped as
// soon as the headers are in. It waits its turn like any request and takes a file slot (the site's connections).
func (c *Client) Modified(ctx context.Context, rawURL string) (time.Time, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return time.Time{}, fmt.Errorf("bad file URL from %s", c.m.Site)
	}
	if err := c.waitTurn(ctx); err != nil {
		return time.Time{}, err
	}
	select {
	case c.files <- struct{}{}:
	case <-ctx.Done():
		return time.Time{}, ctx.Err()
	}
	defer func() { <-c.files }()
	resp, err := c.getWith(ctx, u, c.m.Referer, false)
	if err != nil {
		return time.Time{}, err
	}
	resp.Body.Close()
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		return time.Time{}, fmt.Errorf("%s: %w", c.m.Site, ErrNotAFile)
	}
	t, _ := http.ParseTime(resp.Header.Get("Last-Modified"))
	return t, nil
}

// Picture reads a small picture from the site's own addresses (a cover, a sheet of page thumbnails - browsing):
// with its login and referer, but apart from the downloads (not their connection slots; a failure doesn't count
// towards pausing the site), up to 8 at once, at most 4 MB.
func (c *Client) Picture(ctx context.Context, rawURL string) ([]byte, string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, "", fmt.Errorf("bad picture URL from %s", c.m.Site)
	}
	if !c.m.allows(u.Hostname()) {
		return nil, "", fmt.Errorf("%s: %s is not one of the plugin's domains", c.m.Site, u.Hostname())
	}
	// At most 8 at once (as a browser does): a page of covers all at once opened as many TLS handshakes, which came
	// back in bursts of timeouts (through a VPN).
	c.picsOnce.Do(func() { c.pics = make(chan struct{}, 8) })
	select {
	case c.pics <- struct{}{}:
	case <-ctx.Done():
		return nil, "", ctx.Err()
	}
	defer func() { <-c.pics }()
	// A network failure that may pass (a handshake timed out, the connection dropped) is tried again twice.
	var resp *http.Response
	for attempt := 0; ; attempt++ {
		resp, err = c.send(ctx, u, c.m.Referer)
		if err == nil || ctx.Err() != nil || attempt == 2 {
			break
		}
		select {
		case <-time.After(time.Duration(attempt+1) * 500 * time.Millisecond):
		case <-ctx.Done():
			return nil, "", ctx.Err()
		}
	}
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", &StatusError{Site: c.m.Site, Host: u.Hostname(), Code: resp.StatusCode}
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "image/") {
		return nil, "", fmt.Errorf("%s: %w", c.m.Site, ErrNotAFile)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return b, ct, err
}

// maxResumes is how often one file carries on after breaking off (each time it must have brought something).
const maxResumes = 20

// filePart is one request for a file (the whole of it, or the rest from a byte on) with its own watchdog: no
// answer for fileIdle, or less than fileMin bytes in it, gives it up.
type filePart struct {
	resp   *http.Response
	body   io.Reader
	w      *watchdog
	cancel context.CancelFunc
}

func (p *filePart) close() {
	p.w.t.Stop()
	p.resp.Body.Close()
	p.cancel()
}

type rangeKey struct{}

// relativeKey marks a file request held to the site's usual speed (slowMin), not only to fileMin.
type relativeKey struct{}

// Speeds held to: a tenth of the site's usual one, at most 2 MB/s (a fast line's usual speed doesn't make an ordinary
// one look throttled); and a file is held to it this many times at most - slow again on fresh connections, it's the
// line that's slow (or busy), not one server: then only fileMin.
const (
	slowShare   = 10
	slowCap     = 2 << 20
	slowResumes = 3
)

// rateFileBytes: files from this size count towards a site's usual speed (smaller ones are mostly the request's own
// time; a var for tests).
var rateFileBytes int64 = 4 << 20

// slowMin is the bytes a file connection must bring in each fileIdle: fileMin, or more once the site's usual speed is
// known (a tenth of it, at most slowCap a second).
func (c *Client) slowMin() int64 {
	c.rateMu.Lock()
	r := c.rate
	c.rateMu.Unlock()
	min := c.fileMin
	if r > 0 {
		if v := int64(math.Min(r/slowShare, slowCap) * c.fileIdle.Seconds()); v > min {
			min = v
		}
	}
	return min
}

// noteRate adds a file's speed to the site's usual one (a running average).
func (c *Client) noteRate(bytesPerSec float64) {
	c.rateMu.Lock()
	defer c.rateMu.Unlock()
	if c.rate == 0 {
		c.rate = bytesPerSec
	} else {
		c.rate = 0.7*c.rate + 0.3*bytesPerSec
	}
}

type urgentKey struct{}

// Urgent marks a file download someone is waiting for (a Retry): it doesn't queue behind a search's files
// (one connection more than the site's limit at most, for it).
func Urgent(ctx context.Context) context.Context { return context.WithValue(ctx, urgentKey{}, true) }

func (c *Client) openFile(parent context.Context, u *url.URL, from int64) (*filePart, error) {
	ctx, cancel := context.WithCancel(parent)
	if from > 0 {
		ctx = context.WithValue(ctx, rangeKey{}, from)
	}
	w := &watchdog{d: c.fileIdle, min: c.fileMin, cancel: cancel}
	if relative, _ := ctx.Value(relativeKey{}).(bool); relative {
		w.min = c.slowMin()
	}
	w.t = time.AfterFunc(c.fileIdle, w.fire)
	resp, err := c.getWith(ctx, u, c.m.Referer, false)
	if err != nil {
		w.t.Stop()
		cancel()
		return nil, w.explain(c.m.Site, err)
	}
	w.t.Reset(c.fileIdle)
	return &filePart{resp: resp, body: &idleBody{ReadCloser: resp.Body, w: w, site: c.m.Site}, w: w, cancel: cancel}, nil
}

// resumingBody reads a file across its parts.
type resumingBody struct {
	c       *Client
	ctx     context.Context
	u       *url.URL
	part    *filePart
	got     int64 // bytes read so far
	total   int64 // the file's size (-1: unknown - then no carrying on)
	ranges  bool  // the site said it takes Range requests
	resumes int
	since   int64     // bytes the current part brought
	start   time.Time // when it started (its speed, for the site's usual one)
}

func (b *resumingBody) Read(p []byte) (int, error) {
	n, err := b.part.body.Read(p)
	b.got += int64(n)
	b.since += int64(n)
	// (also one that came slowly after the slow connections were let be: the line itself is slower now - the usual
	// speed follows it down, and a slow line isn't held to its old speed file after file)
	if err == io.EOF && (b.resumes == 0 || b.resumes >= slowResumes) && b.got >= rateFileBytes {
		if d := time.Since(b.start).Seconds(); d > 0 {
			b.c.noteRate(float64(b.got) / d)
		}
	}
	if err == nil || err == io.EOF || !b.ranges || b.total <= 0 || b.got >= b.total || b.resumes >= maxResumes || b.ctx.Err() != nil {
		return n, err
	}
	if b.resumes > 0 && b.since == 0 {
		// Carried on, and nothing more came: the site's copy stops there.
		return n, fmt.Errorf("%s: %w - it always stops at %s of %s", b.c.m.Site, ErrCutShort, megabytes(b.got), megabytes(b.total))
	}
	// Broke off: the rest, from where it stopped (only if the site answers with exactly that part).
	ctx := b.ctx
	if b.resumes+1 < slowResumes { // (the first connection was held to it too)
		ctx = context.WithValue(ctx, relativeKey{}, true) // (after that: only fileMin - see slowResumes)
	}
	next, oerr := b.c.openFile(ctx, b.u, b.got)
	if oerr != nil {
		return n, err
	}
	if next.resp.StatusCode != http.StatusPartialContent || !strings.HasPrefix(next.resp.Header.Get("Content-Range"),
		fmt.Sprintf("bytes %d-", b.got)) {
		next.close()
		return n, err
	}
	b.part.close()
	b.part = next
	b.resumes++
	b.since = 0
	if b.c.log != nil {
		b.c.log.Debug("file carried on", "site", b.c.m.Site, "from", b.got, "of", b.total, "after", err)
	}
	return n, nil
}

func (b *resumingBody) Close() error {
	b.part.close()
	return nil
}

// SetFileIdle changes how long a file download may go without data (tests).
func (c *Client) SetFileIdle(d time.Duration) { c.fileIdle = d }

// SetFileMin changes how much data a file download needs in each fileIdle (tests).
func (c *Client) SetFileMin(n int64) { c.fileMin = n }

// ErrCutShort means the site stops sending a file at the same place every time (carrying on from there brings
// nothing): its copy looks broken - trying again won't help.
var ErrCutShort = errors.New("the site's copy of this file looks broken")

func megabytes(n int64) string { return fmt.Sprintf("%.1f MB", float64(n)/(1<<20)) }

// ErrStalled means a file download got no data, or hardly any, for a while and was given up.
var ErrStalled = errors.New("stalled (hardly any data for a while): given up")

// watchdog cancels a download that stops getting data (or gets only a trickle).
type watchdog struct {
	d      time.Duration
	min    int64 // bytes needed in each d
	got    int64 // ... so far in this one
	t      *time.Timer
	cancel context.CancelFunc
	fired  atomic.Bool
}

func (w *watchdog) fire() {
	w.fired.Store(true)
	w.cancel()
}

// explain turns the cancellation the watchdog caused into ErrStalled.
func (w *watchdog) explain(site string, err error) error {
	if err != nil && w.fired.Load() {
		return fmt.Errorf("%s: %w (%s)", site, ErrStalled, w.d)
	}
	return err
}

// idleBody restarts the watchdog each time enough data has come (fileMin since the last restart).
type idleBody struct {
	io.ReadCloser
	w    *watchdog
	site string
}

func (b *idleBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if b.w.got += int64(n); n > 0 && b.w.got >= b.w.min {
		b.w.got = 0
		b.w.t.Reset(b.w.d)
	}
	return n, b.w.explain(b.site, err)
}

type slotBody struct {
	io.ReadCloser
	once     sync.Once
	release  func()
	modified time.Time
}

// Modified is the file's Last-Modified time, if the site sent one (zero if not). Some sites keep it as the upload
// time - realbooru's pages say only the day of a post, its files' Last-Modified the moment.
func (b *slotBody) Modified() time.Time { return b.modified }

func (b *slotBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.release)
	return err
}

// getWith sends a GET to an allowed host and checks the status. A
// Cloudflare challenge is passed with FlareSolverr when one is set up (page
// says it's a page or API request, not a file: see passCloudflare).
func (c *Client) getWith(ctx context.Context, u *url.URL, referer string, page bool) (*http.Response, error) {
	if u.Scheme != "https" && u.Scheme != "http" {
		return nil, fmt.Errorf("%s: refusing a %q URL", c.m.Site, u.Scheme)
	}
	if !c.m.allows(u.Hostname()) {
		return nil, fmt.Errorf("%s: %s is not one of the plugin's domains", c.m.Site, u.Hostname())
	}
	// A site that keeps failing to answer is paused (breaker.go): nothing goes out meanwhile.
	if err := c.brk.allow(c.m.Site); err != nil {
		return nil, err
	}
	resp, err := c.getOnce(ctx, u, referer, page)
	if failure, counts := connectionFailure(ctx, resp, err); counts {
		c.brk.done(failure)
	} else {
		c.brk.release()
	}
	return resp, err
}

func (c *Client) getOnce(ctx context.Context, u *url.URL, referer string, page bool) (*http.Response, error) {
	resp, err := c.send(ctx, u, referer)
	if err != nil {
		return nil, err
	}
	challenged, head := isChallenge(resp)
	if challenged {
		resp.Body.Close()
		if resp, err = c.passCloudflare(ctx, u, referer, page, resp.Request); err != nil {
			return nil, err
		}
		head = nil
	}
	// (the rest of a file that broke off: 206)
	if from, _ := ctx.Value(rangeKey{}).(int64); resp.StatusCode != http.StatusOK && !(from > 0 && resp.StatusCode == http.StatusPartialContent) {
		if resp.StatusCode == http.StatusTooManyRequests {
			c.backOff(resp.Header.Get("Retry-After"))
		}
		if head == nil {
			head, _ = io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		}
		resp.Body.Close()
		return nil, &StatusError{Site: c.m.Site, Host: u.Hostname(), Code: resp.StatusCode, Title: pageTitle(head)}
	}
	return resp, nil
}

// pageTitle is an HTML page's <title> (e.g. a site's "Temporarily down"), short; "" if none.
func pageTitle(page []byte) string {
	s := string(page)
	i := strings.Index(strings.ToLower(s), "<title>")
	if i < 0 {
		return ""
	}
	s = s[i+len("<title>"):]
	if j := strings.Index(strings.ToLower(s), "</title>"); j >= 0 {
		s = s[:j]
	}
	s = strings.Join(strings.Fields(html.UnescapeString(s)), " ")
	if r := []rune(s); len(r) > 80 {
		s = string(r[:80]) + "…"
	}
	return s
}

// send makes one request (with a Cloudflare clearance for the host, if there is one).
func (c *Client) send(ctx context.Context, u *url.URL, referer string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%s: bad request", c.m.Site)
	}
	req.Header.Set("User-Agent", UserAgent)
	if from, _ := ctx.Value(rangeKey{}).(int64); from > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", from)) // (the rest of a file that broke off)
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	if c.solver != nil {
		c.solver.apply(req) // the clearance cookies and the browser's User-Agent
	}
	if c.m.Auth.Login == "bearer" && len(c.m.Auth.Fields) == 1 {
		c.credMu.RLock()
		token := c.creds[c.m.Auth.Fields[0]]
		c.credMu.RUnlock()
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		// url.Error repeats the URL (and so any api_key): keep only the cause.
		err = fmt.Errorf("%s: request to %s failed: %w", c.m.Site, u.Hostname(), unwrapURL(err))
		c.logRequest(u, 0, start, err)
		return nil, err
	}
	c.logRequest(u, resp.StatusCode, start, nil)
	return resp, nil
}

// passCloudflare gets a clearance from FlareSolverr and sends the request
// again with it. FlareSolverr visits the URL without credentials (they never
// leave Vitrine; the clearance is for the whole site anyway). If the site
// still refuses (it checks more than cookies), a page request can use the
// page FlareSolverr's browser got - when that was the same URL.
// refused: the request that was turned away.
func (c *Client) passCloudflare(ctx context.Context, u *url.URL, referer string, page bool, refused *http.Request) (*http.Response, error) {
	if c.solver == nil || c.solver.Endpoint() == "" {
		return nil, &CloudflareError{Site: c.m.Site}
	}
	c.solver.forget(u.Hostname(), refused) // (its clearance, if it carried the one there is, was refused)
	target, same := c.withoutCredentials(u)
	got, err := c.solver.solve(ctx, target)
	if err != nil {
		return nil, &CloudflareError{Site: c.m.Site, Reason: err.Error()}
	}
	resp, err := c.send(ctx, u, referer)
	if err != nil {
		return nil, err
	}
	if challenged, _ := isChallenge(resp); !challenged {
		return resp, nil
	}
	resp.Body.Close()
	if page && same && len(got) > 0 {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(bytes.NewReader(got))}, nil
	}
	return nil, &CloudflareError{Site: c.m.Site, Reason: "the site still refused the cleared requests"}
}

// withoutCredentials is the URL without the plugin's credential fields (same:
// it had none).
func (c *Client) withoutCredentials(u *url.URL) (*url.URL, bool) {
	cp := *u
	q := cp.Query()
	same := true
	for _, f := range c.m.Auth.Fields {
		if q.Has(f) {
			q.Del(f)
			same = false
		}
	}
	cp.RawQuery = q.Encode()
	if same {
		cp.RawQuery = u.RawQuery
	}
	return &cp, same
}

// StatusError is a non-200 reply.
type StatusError struct {
	Site, Host string
	Code       int
	Title      string // the error page's title, if it had one (e.g. "Temporarily down")
}

func (e *StatusError) Error() string {
	if e.Title != "" {
		return fmt.Sprintf("%s: %s answered %d %s (its page says: %s)", e.Site, e.Host, e.Code, http.StatusText(e.Code), e.Title)
	}
	return fmt.Sprintf("%s: %s answered %d %s", e.Site, e.Host, e.Code, http.StatusText(e.Code))
}

// SetSolver shares the FlareSolverr solver with the client.
func (c *Client) SetSolver(s *Solver) { c.solver = s }

// SetHTTPClient replaces the HTTP client (tests use it for fake sites).
func (c *Client) SetHTTPClient(h *http.Client) { c.http = h }

// SetTooManyWait sets how long requests wait after a 429 without Retry-After
// (tests use a short one).
func (c *Client) SetTooManyWait(d time.Duration) { c.tooMany = d }

// SetLogger logs the site's requests (debug: host and path - never the query,
// which may carry an API key - status and time to the answer).
func (c *Client) SetLogger(l *slog.Logger) { c.log = l }

// logInfo logs something worth knowing about the site (when a logger is set).
func (c *Client) logInfo(msg string, args ...any) {
	if c.log != nil {
		c.log.Info(msg, args...)
	}
}

func (c *Client) logRequest(u *url.URL, status int, start time.Time, err error) {
	if c.log == nil {
		return
	}
	// "url" (host and path, never the query): private in the problem report (a path can name a search).
	args := []any{"site", c.m.ID, "url", u.Host + u.Path, "took", time.Since(start).Round(time.Millisecond)}
	if err != nil {
		args = append(args, "err", err)
	} else {
		args = append(args, "status", status)
	}
	c.log.Debug("site request", args...)
}
