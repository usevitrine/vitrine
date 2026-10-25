package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Cloudflare challenges ("Checking your browser…") and FlareSolverr
// (https://github.com/FlareSolverr/FlareSolverr): a separate service that
// passes the challenge in a real browser and hands back the clearance cookies
// and the browser's User-Agent. Requests then carry both until the clearance
// runs out; a site behind Cloudflare costs one browser visit now and then,
// not one per request. Without FlareSolverr, a challenge is a clear error.

// ErrCloudflare means a site answered with a Cloudflare challenge and no
// FlareSolverr is set up (or it couldn't get through).
var ErrCloudflare = errors.New("blocked by Cloudflare")

// CloudflareError says which site blocked and why it wasn't passed.
type CloudflareError struct {
	Site   string
	Reason string // empty: no FlareSolverr set up
}

func (e *CloudflareError) Error() string {
	if e.Reason == "" {
		return fmt.Sprintf("%s: blocked by Cloudflare - set up FlareSolverr in Settings > Sites to get past it", e.Site)
	}
	return fmt.Sprintf("%s: blocked by Cloudflare, and FlareSolverr couldn't get past it: %s", e.Site, e.Reason)
}

func (e *CloudflareError) Unwrap() error { return ErrCloudflare }

// isChallenge reports whether a reply is a Cloudflare challenge page (it
// reads at most the start of the body and gives it back for the caller). Not
// Cloudflare's "challenge-platform" script alone: Cloudflare puts it in other
// pages too (a site's own "we are down" page, 2026-10-21).
func isChallenge(resp *http.Response) (bool, []byte) {
	if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusServiceUnavailable {
		return false, nil
	}
	if strings.EqualFold(resp.Header.Get("cf-mitigated"), "challenge") {
		return true, nil
	}
	if !strings.EqualFold(resp.Header.Get("Server"), "cloudflare") {
		return false, nil
	}
	head, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	s := string(head)
	return strings.Contains(s, "cf-chl") || strings.Contains(s, "<title>Just a moment"), head
}

// Solver asks a FlareSolverr service for Cloudflare clearances and keeps
// them per domain. Safe for concurrent use; shared by all plugin clients.
type Solver struct {
	http *http.Client
	now  func() time.Time

	mu       sync.Mutex
	endpoint string                // e.g. http://flaresolverr:8191 ("" = off)
	cleared  map[string]*clearance // by cookie domain (no leading dot)
	inflight map[string]*solveCall // by host
}

type clearance struct {
	cookies   []*http.Cookie
	userAgent string
	expires   time.Time
}

type solveCall struct {
	done chan struct{}
	page []byte // the page FlareSolverr's browser got
	err  error
}

// NewSolver returns a solver (off until SetEndpoint).
func NewSolver() *Solver {
	return &Solver{
		http:     &http.Client{Timeout: 90 * time.Second},
		now:      time.Now,
		cleared:  map[string]*clearance{},
		inflight: map[string]*solveCall{},
	}
}

// SetEndpoint sets the FlareSolverr address ("" turns it off).
func (s *Solver) SetEndpoint(endpoint string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
}

// Endpoint is the FlareSolverr address ("" = off).
func (s *Solver) Endpoint() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.endpoint
}

// ValidEndpoint checks that an address can be a FlareSolverr endpoint.
func ValidEndpoint(endpoint string) error {
	if endpoint == "" {
		return nil
	}
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("the FlareSolverr address must look like http://flaresolverr:8191")
	}
	return nil
}

// Ping asks FlareSolverr for its version (a connection test).
func (s *Solver) Ping(ctx context.Context, endpoint string) (string, error) {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if err := ValidEndpoint(endpoint); err != nil || endpoint == "" {
		if err == nil {
			err = errors.New("no FlareSolverr address")
		}
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/", nil)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	resp, err := s.http.Do(req.WithContext(ctx))
	if err != nil {
		return "", fmt.Errorf("FlareSolverr didn't answer: %v", unwrapURL(err))
	}
	defer resp.Body.Close()
	var info struct {
		Msg     string `json:"msg"`
		Version string `json:"version"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&info) != nil || info.Version == "" {
		return "", fmt.Errorf("that address answered, but not as FlareSolverr (%d %s)", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	return info.Version, nil
}

// apply adds a matching clearance to a request (cookies and the browser's
// User-Agent, which must go together). It reports whether one was added.
func (s *Solver) apply(req *http.Request) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.endpoint == "" {
		return false
	}
	c := s.lookup(req.URL.Hostname())
	if c == nil {
		return false
	}
	for _, ck := range c.cookies {
		req.AddCookie(&http.Cookie{Name: ck.Name, Value: ck.Value})
	}
	req.Header.Set("User-Agent", c.userAgent)
	return true
}

// lookup finds the clearance for a host (its domain or a parent's).
func (s *Solver) lookup(host string) *clearance {
	host = strings.ToLower(host)
	for d := host; d != ""; {
		if c := s.cleared[d]; c != nil {
			if s.now().Before(c.expires) {
				return c
			}
			delete(s.cleared, d)
		}
		i := strings.IndexByte(d, '.')
		if i < 0 {
			break
		}
		d = d[i+1:]
	}
	return nil
}

// forget drops the clearance for a host when the refused request carried it (refused nil: whatever it was). A
// request sent before the clearance came was refused for having none: the clearance stays - several requests
// challenged at once, one cleared the way, and a later one forgot that fresh clearance, so the next went without it
// and was refused too (the race detector's slow runs found it).
func (s *Solver) forget(host string, refused *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.lookup(host)
	if c == nil || (refused != nil && !carries(refused, c)) {
		return
	}
	for d, x := range s.cleared {
		if x == c {
			delete(s.cleared, d)
		}
	}
}

// carries says whether a request had a clearance's cookies.
func carries(req *http.Request, c *clearance) bool {
	for _, ck := range c.cookies {
		if got, err := req.Cookie(ck.Name); err != nil || got.Value != ck.Value {
			return false
		}
	}
	return len(c.cookies) > 0
}

// solve gets a clearance for the URL's site from FlareSolverr (one visit at
// a time per host: others wait for it) and returns the page its browser got.
func (s *Solver) solve(ctx context.Context, target *url.URL) ([]byte, error) {
	host := strings.ToLower(target.Hostname())
	s.mu.Lock()
	endpoint := s.endpoint
	if endpoint == "" {
		s.mu.Unlock()
		return nil, nil
	}
	if call, ok := s.inflight[host]; ok {
		s.mu.Unlock()
		select {
		case <-call.done:
			return call.page, call.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	call := &solveCall{done: make(chan struct{})}
	s.inflight[host] = call
	s.mu.Unlock()

	call.page, call.err = s.ask(ctx, endpoint, target)
	s.mu.Lock()
	delete(s.inflight, host)
	s.mu.Unlock()
	close(call.done)
	return call.page, call.err
}

type flareReply struct {
	Status   string `json:"status"`
	Message  string `json:"message"`
	Solution struct {
		Status    int    `json:"status"`
		Response  string `json:"response"`
		UserAgent string `json:"userAgent"`
		Cookies   []struct {
			Name    string  `json:"name"`
			Value   string  `json:"value"`
			Domain  string  `json:"domain"`
			Expires float64 `json:"expires"` // Unix seconds; -1 or 0 for a session cookie
		} `json:"cookies"`
	} `json:"solution"`
}

func (s *Solver) ask(ctx context.Context, endpoint string, target *url.URL) ([]byte, error) {
	body, _ := json.Marshal(map[string]any{"cmd": "request.get", "url": target.String(), "maxTimeout": 60000})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/v1", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("FlareSolverr didn't answer: %v", unwrapURL(err))
	}
	defer resp.Body.Close()
	var r flareReply
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&r); err != nil {
		return nil, fmt.Errorf("FlareSolverr sent something unreadable (%d)", resp.StatusCode)
	}
	if r.Status != "ok" {
		// Its messages name the URL (it may carry credentials): keep only the gist.
		msg := r.Message
		if i := strings.Index(msg, "http"); i >= 0 {
			msg = strings.TrimSpace(msg[:i])
		}
		if msg == "" {
			msg = r.Status
		}
		return nil, errors.New(strings.TrimRight(msg, ": "))
	}
	expires := s.now().Add(30 * time.Minute) // without a cookie date
	c := &clearance{userAgent: r.Solution.UserAgent}
	domain := strings.ToLower(target.Hostname())
	for _, ck := range r.Solution.Cookies {
		c.cookies = append(c.cookies, &http.Cookie{Name: ck.Name, Value: ck.Value})
		if ck.Name == "cf_clearance" {
			if d := strings.TrimPrefix(strings.ToLower(ck.Domain), "."); d != "" {
				domain = d
			}
			if ck.Expires > 0 {
				expires = time.Unix(int64(ck.Expires), 0).Add(-time.Minute)
			}
		}
	}
	if c.userAgent == "" || len(c.cookies) == 0 {
		return nil, errors.New("FlareSolverr got the page but no cookies to reuse")
	}
	c.expires = expires
	s.mu.Lock()
	s.cleared[domain] = c
	s.mu.Unlock()
	return []byte(r.Solution.Response), nil
}

func unwrapURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
