// Package plugin loads site plugins and talks to their sites.
//
// A plugin is a manifest naming a built-in engine (one per booru software,
// e.g. gelbooru-v02) plus the site's details: API address, limits, how its
// ratings map to Vitrine's, which domains it may contact. Scripted plugins
// (JavaScript) come later.
package plugin

import (
	"embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/usevitrine/vitrine/internal/media"
)

// Manifest describes a site plugin (plugin.yaml).
type Manifest struct {
	ID   string `yaml:"id"`   // short, stable: safebooru, safebooru
	Name string `yaml:"name"` // the site's own name ("Safebooru")
	// Site is how the site is shown everywhere: its web address ("safebooru.org").
	// Defaults to the host of post_url (or base), without www. / api.
	Site    string `yaml:"site"`
	Version string `yaml:"version"` // the plugin's own version
	Engine  string `yaml:"engine"`  // gelbooru-v02
	Base    string `yaml:"base"`    // API base URL, e.g. https://api.safebooru.org
	// PostURL is the web page of a post; {id} is replaced by the post ID.
	PostURL string   `yaml:"post_url"`
	Domains []string `yaml:"domains"` // hosts it may contact (subdomains included)
	Auth    struct {
		Fields   []string `yaml:"fields"`   // e.g. [api_key, user_id]
		Required bool     `yaml:"required"` // searches fail without them
		Help     string   `yaml:"help"`     // where to get them
		// Login: "" = the fields go in each request's query (API keys);
		// "shimmie2" = the engine signs in through the site's login form with
		// the two fields (user name, password) and keeps the session cookie;
		// "gelbooru" = the same through a Gelbooru 0.2 site's login form (its
		// session cookies user_id and pass_hash; fav:me is that user);
		// "ehentai" = the e-hentai forum login (username, password) or its two
		// cookies (ipb_member_id, ipb_pass_hash) pasted from a browser;
		// "bearer" = the one field is an access token sent as
		// "Authorization: Bearer" (Mastodon).
		Login string `yaml:"login"`
	} `yaml:"auth"`
	Limits struct {
		RequestsPerSec float64 `yaml:"requests_per_sec"` // API requests
		Connections    int     `yaml:"connections"`      // files at once
		// MaxConnections above Connections: Vitrine tries more files at once, up to this, keeping more only while
		// they bring more in total (tune.go). 0: Connections, fixed.
		MaxConnections int `yaml:"max_connections"`
	} `yaml:"limits"`
	Search struct {
		PerPage int `yaml:"per_page"`
		// Hint replaces the new-download box's placeholder for a site with its
		// own search syntax (e.g. "#tag or @profile"). Keep it short.
		Hint string `yaml:"hint"`
		// TagQuery says how a tag from the library becomes this site's search
		// (a tag's "New download"): a namespace, or "*" for every other tag,
		// mapped to a template with {tag} (the tag without its namespace; {name}: with
		// spaces for underscores; {ns}: the namespace), e.g.
		// {creator: "@{tag}", "*": "#{tag}"}. Missing: the engine's defaults
		// (TagQueries).
		TagQuery map[string]string `yaml:"tag_query"`
		// Or says how the site searches for posts with any of several tags (an
		// OR search), if it can: a template with {tags} - the tags joined by
		// OrSep (default " ~ ") - e.g. "( {tags} )" for "( a ~ b ~ c )".
		// Empty: the site has no OR search (picked tags download all together).
		Or    string `yaml:"or"`
		OrSep string `yaml:"or_sep"`
	} `yaml:"search"`
	// Ratings maps the site's rating values to general, sensitive,
	// questionable or explicit.
	Ratings map[string]string `yaml:"ratings"`
	// Tags says how the engine learns tag types and which become namespaces.
	Tags struct {
		// Types: "tag_info" = gelbooru 0.2's JSON API with fields=tag_info
		// (one extra request per page); "page" = read from each post's page
		// (gelbooru-v02-html); "post" = they come with every post (danbooru);
		// empty = tag types aren't known.
		Types string `yaml:"types"`
		// Namespaces maps the site's tag types to Vitrine namespaces (e.g.
		// artist: creator). Other types (general tags) get no namespace.
		Namespaces map[string]string `yaml:"namespaces"`
	} `yaml:"tags"`
	// Referer is sent with file downloads, for sites whose hotlink protection
	// wants one (without it, some sites answer a file URL with a web page).
	Referer string `yaml:"referer"`
	// FileQuery is added to file URLs as a query ({id}: the post's ID), the way the site's own pages link them:
	// a CDN caches each URL apart, and the plain URL's copy can be broken (rule34's US cache: some files stopped
	// part-way, crawling, for everyone fetching the bare URL from there).
	FileQuery string `yaml:"file_query"`
	// BuiltIn is set for the plugins shipped inside Vitrine (not in the file).
	BuiltIn bool `yaml:"-"`
}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

var nsPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

// Engines are the built-in site engines.
var Engines = map[string]bool{"gelbooru-v02": true, "gelbooru-v02-html": true, "shimmie2": true, "mastodon": true, "danbooru": true, "ehentai": true}

// Parse reads and checks a manifest, filling defaults.
func Parse(data []byte) (Manifest, error) {
	var m Manifest
	if err := yaml.UnmarshalWithOptions(data, &m, yaml.DisallowUnknownField()); err != nil {
		return m, fmt.Errorf("plugin manifest: %w", err)
	}
	if !idPattern.MatchString(m.ID) || m.ID == "user" {
		return m, fmt.Errorf("plugin manifest: bad id %q (lower-case letters, digits, - and _)", m.ID)
	}
	if !Engines[m.Engine] {
		return m, fmt.Errorf("plugin %s: unknown engine %q", m.ID, m.Engine)
	}
	u, err := url.Parse(m.Base)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return m, fmt.Errorf("plugin %s: base must be an https URL", m.ID)
	}
	if !m.allows(u.Hostname()) {
		m.Domains = append(m.Domains, u.Hostname())
	}
	if m.Name == "" {
		m.Name = m.ID
	}
	if m.Site == "" {
		m.Site = siteAddress(m)
	}
	if m.Search.PerPage <= 0 {
		m.Search.PerPage = 100
	}
	if m.Limits.RequestsPerSec <= 0 {
		m.Limits.RequestsPerSec = 1
	}
	if m.Limits.Connections <= 0 {
		m.Limits.Connections = 1
	}
	m.Limits.MaxConnections = min(max(m.Limits.MaxConnections, m.Limits.Connections), 16)
	if m.Search.Or != "" && !strings.Contains(m.Search.Or, "{tags}") {
		return m, fmt.Errorf("plugin %s: search.or %q has no {tags}", m.ID, m.Search.Or)
	}
	if m.Search.Or != "" && m.Search.OrSep == "" {
		m.Search.OrSep = " ~ "
	}
	for ns, tmpl := range m.Search.TagQuery {
		if !strings.Contains(tmpl, "{tag}") && !strings.Contains(tmpl, "{name}") {
			return m, fmt.Errorf("plugin %s: search.tag_query %q: %q has no {tag}", m.ID, ns, tmpl)
		}
	}
	switch m.Auth.Login {
	case "":
	case "shimmie2", "gelbooru":
		if len(m.Auth.Fields) != 2 {
			return m, fmt.Errorf("plugin %s: auth.login %q needs two fields (user name, password)", m.ID, m.Auth.Login)
		}
	case "ehentai":
		if strings.Join(m.Auth.Fields, ",") != "username,password,ipb_member_id,ipb_pass_hash" {
			return m, fmt.Errorf("plugin %s: auth.login %q needs the fields username, password, ipb_member_id, ipb_pass_hash", m.ID, m.Auth.Login)
		}
	case "bearer":
		if len(m.Auth.Fields) != 1 {
			return m, fmt.Errorf("plugin %s: auth.login %q needs one field (the access token)", m.ID, m.Auth.Login)
		}
	default:
		return m, fmt.Errorf("plugin %s: unknown auth.login %q", m.ID, m.Auth.Login)
	}
	switch m.Tags.Types {
	case "", "tag_info", "page", "post":
	default:
		return m, fmt.Errorf("plugin %s: unknown tags.types %q", m.ID, m.Tags.Types)
	}
	for typ, ns := range m.Tags.Namespaces {
		if !nsPattern.MatchString(ns) {
			return m, fmt.Errorf("plugin %s: tag type %q maps to bad namespace %q (lower-case letters, digits, _)", m.ID, typ, ns)
		}
	}
	for site, ours := range m.Ratings {
		if _, ok := media.ParseRating(ours); !ok {
			return m, fmt.Errorf("plugin %s: rating %q maps to unknown %q", m.ID, site, ours)
		}
	}
	return m, nil
}

// siteAddress is a site's web address for display: post_url's host (the
// site people visit), else base's, without a leading www. or api.
func siteAddress(m Manifest) string {
	for _, raw := range []string{m.PostURL, m.Base} {
		u, err := url.Parse(strings.ReplaceAll(raw, "{id}", "0"))
		if err != nil || u.Host == "" {
			continue
		}
		h := strings.ToLower(u.Host)
		for _, p := range []string{"www.", "api."} {
			h = strings.TrimPrefix(h, p)
		}
		return h
	}
	return m.ID
}

// allows reports whether the plugin may contact host.
func (m Manifest) allows(host string) bool {
	host = strings.ToLower(host)
	for _, d := range m.Domains {
		d = strings.ToLower(d)
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

// Rating maps a site rating value to Vitrine's.
func (m Manifest) Rating(site string) media.Rating {
	r, _ := media.ParseRating(m.Ratings[site])
	return r
}

// engineTagQueries: how each engine searches for a tag, unless the plugin says
// otherwise. Boorus: the plain tag. Mastodon: a hashtag, or the author's profile.
var engineTagQueries = map[string]map[string]string{
	"mastodon": {"creator": "@{tag}", "*": "#{tag}"},
	// E-Hentai's exact-tag syntax: namespace:"name with spaces$" (artist and parody are Vitrine's creator and series).
	"ehentai": {"creator": `artist:"{name}$"`, "series": `parody:"{name}$"`, "": `"{name}$"`, "*": `{ns}:"{name}$"`},
}

// TagQueries is how a tag becomes this site's search: the plugin's
// search.tag_query over the engine's defaults; "*" is always there.
func (m Manifest) TagQueries() map[string]string {
	out := map[string]string{"*": "{tag}"}
	for ns, t := range engineTagQueries[m.Engine] {
		out[ns] = t
	}
	for ns, t := range m.Search.TagQuery {
		out[ns] = t
	}
	return out
}

// engineOr: OR searches an engine has without the plugin saying so - the
// Mastodon engine's "either" hashtags (~#a ~#b; not for @profiles) and Danbooru's ~a ~b.
var engineOr = map[string][2]string{"mastodon": {"~{tags}", " ~"}, "danbooru": {"~{tags}", " ~"}}

// OrQuery is the site's OR search template and separator (the plugin's, else
// its engine's; "" = none).
func (m Manifest) OrQuery() (tmpl, sep string) {
	if m.Search.Or != "" {
		return m.Search.Or, m.Search.OrSep
	}
	if d, ok := engineOr[m.Engine]; ok {
		return d[0], d[1]
	}
	return "", ""
}

// TagQuery turns a library tag (with or without its namespace) into this
// site's search for it.
func (m Manifest) TagQuery(ns, name string) string {
	q := m.TagQueries()
	t, ok := q[ns]
	if !ok {
		t = q["*"]
	}
	return strings.ReplaceAll(t, "{tag}", name)
}

// Namespace returns the namespace for a site tag type ("" = none).
func (m Manifest) Namespace(siteType string) string { return m.Tags.Namespaces[siteType] }

// Post returns the web page URL of a post.
func (m Manifest) Post(id int64) string {
	return strings.ReplaceAll(m.PostURL, "{id}", fmt.Sprint(id))
}

// FileFetchURL is the URL a post's file is fetched from: with FileQuery added (when it has no query of its own).
func (m Manifest) FileFetchURL(fileURL string, id int64) string {
	if m.FileQuery == "" || strings.Contains(fileURL, "?") {
		return fileURL
	}
	return fileURL + "?" + url.QueryEscape(strings.ReplaceAll(m.FileQuery, "{id}", fmt.Sprint(id)))
}

//go:embed builtin/*.yaml
var builtin embed.FS

// Builtin returns the built-in plugin with that ID, if there is one.
func Builtin(id string) (Manifest, bool) {
	entries, _ := builtin.ReadDir("builtin")
	for _, e := range entries {
		data, _ := builtin.ReadFile("builtin/" + e.Name())
		if m, err := Parse(data); err == nil && m.ID == id {
			m.BuiltIn = true
			return m, true
		}
	}
	return Manifest{}, false
}

// Load returns the built-in plugins plus those in dir (*.yaml or
// */plugin.yaml); a plugin in dir replaces a built-in one with the same ID.
func Load(dir string) (map[string]Manifest, error) {
	out := map[string]Manifest{}
	entries, _ := builtin.ReadDir("builtin")
	for _, e := range entries {
		data, _ := builtin.ReadFile("builtin/" + e.Name())
		m, err := Parse(data)
		if err != nil {
			return nil, err
		}
		m.BuiltIn = true
		out[m.ID] = m
	}
	if dir == "" {
		return out, nil
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "*.yaml"))
	more, _ := filepath.Glob(filepath.Join(dir, "*", "plugin.yaml"))
	for _, p := range append(paths, more...) {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		m, err := Parse(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		out[m.ID] = m
	}
	return out, nil
}

// ErrUnknownSite means no plugin has that ID.
var ErrUnknownSite = errors.New("no plugin for that site")
