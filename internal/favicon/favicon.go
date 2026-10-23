// Package favicon fetches and keeps sites' icons, shown beside the links to
// where a file came from. An icon is fetched once per site (the home page's
// <link rel="icon">, else /favicon.ico; an image host like img.example.com
// falls back to example.com), kept on disk and fetched again after a month;
// a site without one is asked again after a week. Only public addresses are
// fetched and only bitmap images kept (no SVG: it can carry scripts).
package favicon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ErrNone means the site has no usable icon (the caller shows its own).
var ErrNone = errors.New("favicon: none")

const (
	keepFor     = 30 * 24 * time.Hour // an icon is fetched again after this
	retryNoneIn = 7 * 24 * time.Hour  // a site without one is asked again after this
	maxPage     = 512 << 10
	maxIcon     = 256 << 10
	wantSize    = 64 // px: shown at 32 px, sharp on 2x screens
)

// Cache keeps icons in a directory: <host>.icon (the image) or <host>.none
// (no icon found; its time says when it was asked).
type Cache struct {
	dir       string
	userAgent string
	client    *http.Client

	mu       sync.Mutex
	inflight map[string]*call
}

type call struct {
	done chan struct{}
	data []byte
	err  error
}

// Open uses dir for the icons (created if needed). userAgent identifies
// Vitrine to the sites.
func Open(dir, userAgent string) (*Cache, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("favicon: %w", err)
	}
	return &Cache{dir: dir, userAgent: userAgent, client: newClient(publicOnly), inflight: map[string]*call{}}, nil
}

func newClient(control func(network, address string, c syscall.RawConn) error) *http.Client {
	dialer := &net.Dialer{Timeout: 8 * time.Second, Control: control}
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil // straight out (a proxy would get around the public-address check)
	t.DialContext = dialer.DialContext
	return &http.Client{
		Transport: t,
		Timeout:   15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "https" && req.URL.Scheme != "http" {
				return errors.New("not a web address")
			}
			return nil
		},
	}
}

// publicOnly refuses connections to addresses that aren't on the public
// internet (this machine, the local network), whatever a name resolves to.
func publicOnly(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || sharedSpace.Contains(ip) {
		return fmt.Errorf("favicon: %s is not a public address", host)
	}
	return nil
}

// 100.64.0.0/10: carrier-grade NAT, also used by some VPNs for private devices.
var sharedSpace = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

var hostname = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

// ValidHost reports whether host is a plain domain name (not an address, no port).
func ValidHost(host string) bool {
	return len(host) <= 253 && hostname.MatchString(host) && net.ParseIP(host) == nil
}

// Get returns a site's icon, fetching it if it isn't kept (or is old).
// also: other addresses of the same site, tried last (e.g. its API's, when
// the main one is behind a bot check). ErrNone: the site has none.
func (c *Cache) Get(ctx context.Context, host string, also ...string) ([]byte, error) {
	host = strings.ToLower(host)
	if !ValidHost(host) {
		return nil, ErrNone
	}
	icon := filepath.Join(c.dir, host+".icon")
	none := filepath.Join(c.dir, host+".none")
	var kept []byte
	if info, err := os.Stat(icon); err == nil {
		if kept, err = os.ReadFile(icon); err == nil && time.Since(info.ModTime()) < keepFor {
			return kept, nil
		}
	} else if info, err := os.Stat(none); err == nil && time.Since(info.ModTime()) < retryNoneIn {
		return nil, ErrNone
	}

	// One fetch per site at a time; others asking meanwhile wait for it.
	c.mu.Lock()
	if cl, ok := c.inflight[host]; ok {
		c.mu.Unlock()
		select {
		case <-cl.done:
			return cl.data, cl.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	cl := &call{done: make(chan struct{})}
	c.inflight[host] = cl
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.inflight, host)
		c.mu.Unlock()
		close(cl.done)
	}()

	// The fetch isn't tied to this request: a page closed mid-way still gets it kept.
	fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	data := c.fetch(fctx, host)
	if data == nil {
		for p := parent(host); p != "" && data == nil; p = parent(p) {
			data = c.fetch(fctx, p)
		}
		for _, h := range also {
			if h = strings.ToLower(h); data == nil && ValidHost(h) && h != host {
				data = c.fetch(fctx, h)
			}
		}
	}
	switch {
	case data != nil:
		if err := writeFile(icon, data); err != nil {
			cl.err = err
			return nil, err
		}
		os.Remove(none)
		cl.data = data
	case kept != nil:
		os.Chtimes(icon, time.Now(), time.Now()) // still the old one: asked again in a month
		cl.data = kept
	default:
		writeFile(none, nil)
		cl.err = ErrNone
	}
	return cl.data, cl.err
}

// parent is the domain one level up (img.example.com -> example.com), or ""
// at a site's own domain (example.com, example.co.uk).
func parent(host string) string {
	labels := strings.Split(host, ".")
	if len(labels) < 3 {
		return ""
	}
	up := labels[1:]
	// Not a country's second level (co.uk, com.au): that isn't anyone's site.
	if len(up) == 2 && len(up[0]) <= 3 && len(up[1]) == 2 {
		return ""
	}
	return strings.Join(up, ".")
}

// fetch finds and downloads one host's icon (nil: none found).
func (c *Cache) fetch(ctx context.Context, host string) []byte {
	home := &url.URL{Scheme: "https", Host: host, Path: "/"}
	var tries []string
	if page, final, err := c.get(ctx, home.String(), maxPage); err == nil {
		tries = iconLinks(page, final)
	}
	tries = append(tries, home.JoinPath("favicon.ico").String())
	for _, u := range tries {
		if data, _, err := c.get(ctx, u, maxIcon); err == nil && isBitmap(data) {
			return data
		}
		if ctx.Err() != nil {
			return nil
		}
	}
	return nil
}

func (c *Cache) get(ctx context.Context, u string, limit int64) ([]byte, *url.URL, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("favicon: %s: %s", u, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, nil, err
	}
	if int64(len(data)) > limit {
		if limit == maxPage {
			return data[:limit], resp.Request.URL, nil // a long page: its head (where the links are) is enough
		}
		return nil, nil, fmt.Errorf("favicon: %s: too big", u)
	}
	return data, resp.Request.URL, nil
}

var (
	linkTag = regexp.MustCompile(`(?is)<link\b[^>]*>`)
	attr    = regexp.MustCompile(`(?is)\b(rel|href|sizes|type)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)
)

// iconLinks lists a page's icon links, the best first: the size nearest 64 px
// at or above it, then the biggest; SVG ones left out.
func iconLinks(page []byte, base *url.URL) []string {
	type cand struct {
		url  string
		size int // 0: not given
	}
	var cands []cand
	for _, tag := range linkTag.FindAll(page, -1) {
		a := map[string]string{}
		for _, m := range attr.FindAllSubmatch(tag, -1) {
			a[strings.ToLower(string(m[1]))] = string(m[2]) + string(m[3]) + string(m[4])
		}
		rel := strings.Fields(strings.ToLower(a["rel"]))
		if !slices.Contains(rel, "icon") && !slices.Contains(rel, "apple-touch-icon") {
			continue // (mask-icon is a one-color SVG)
		}
		href := strings.TrimSpace(htmlUnescape(a["href"]))
		if href == "" || strings.Contains(strings.ToLower(a["type"]), "svg") {
			continue
		}
		u, err := base.Parse(href)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || strings.HasSuffix(strings.ToLower(u.Path), ".svg") {
			continue
		}
		size := 0
		for _, s := range strings.Fields(strings.ToLower(a["sizes"])) {
			if w, _, ok := strings.Cut(s, "x"); ok {
				if n, err := strconv.Atoi(w); err == nil && n > size {
					size = n
				}
			}
		}
		if size == 0 && slices.Contains(rel, "apple-touch-icon") {
			size = 180 // its usual size
		}
		cands = append(cands, cand{u.String(), size})
	}
	score := func(c cand) int {
		switch {
		case c.size >= wantSize:
			return c.size - wantSize // the nearest above
		case c.size == 0:
			return 10_000 // unknown: after the sized ones
		default:
			return 20_000 - c.size // too small: the biggest of those
		}
	}
	slices.SortStableFunc(cands, func(a, b cand) int { return score(a) - score(b) })
	out := make([]string, 0, len(cands))
	for _, c := range cands {
		if !slices.Contains(out, c.url) {
			out = append(out, c.url)
		}
	}
	return out
}

func htmlUnescape(s string) string {
	return strings.NewReplacer("&amp;", "&", "&#38;", "&", "&quot;", `"`, "&#39;", "'").Replace(s)
}

// isBitmap: PNG, JPEG, GIF, WebP, ICO or BMP (by the bytes, not what the site says).
func isBitmap(b []byte) bool {
	switch ContentType(b) {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/x-icon", "image/bmp":
		return true
	}
	return false
}

// ContentType is the image type of an icon's bytes.
func ContentType(b []byte) string {
	if len(b) >= 4 && b[0] == 0 && b[1] == 0 && b[2] == 1 && b[3] == 0 {
		return "image/x-icon"
	}
	return http.DetectContentType(b)
}

func writeFile(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("favicon: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("favicon: %w", err)
	}
	return nil
}
