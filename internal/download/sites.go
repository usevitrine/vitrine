package download

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/usevitrine/vitrine/internal/plugin"
)

// Sites and their credentials, as Settings shows them. Credentials are
// written to the secrets file and handed to the site's client; they are never
// read back out - only whether each field is set.

// SiteInfo describes a site plugin for Settings.
type SiteInfo struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Site     string   `json:"site"` // its web address, how it's shown ("safebooru.org")
	Version  string   `json:"version"`
	Engine   string   `json:"engine"`
	Host     string   `json:"host"`     // the API's host
	BuiltIn  bool     `json:"built_in"` // shipped with Vitrine (else installed in the plugins folder)
	TagTypes bool     `json:"tag_types"`
	Fields   []string `json:"fields"` // credential fields, e.g. api_key, user_id
	Required bool     `json:"required"`
	Help     string   `json:"help,omitempty"`
	Hint     string   `json:"search_hint,omitempty"` // the new-download box's hint
	// TagQuery: how a tag becomes this site's search - a namespace (or "*") to
	// a template with {tag}, e.g. {"creator": "@{tag}", "*": "#{tag}"}.
	TagQuery map[string]string `json:"tag_query"`
	// OrQuery and OrSep: how the site writes an OR search ("" = it can't), e.g. "( {tags} )" and " ~ ".
	OrQuery string `json:"or_query,omitempty"`
	OrSep   string `json:"or_sep,omitempty"`
	// FavBy: how the site names whose favorites a fav:<user> download is ("id", "name",
	// "me": only your own; "" = none); FavMe: fav:me works (the credentials say who you are).
	FavBy string          `json:"fav_by,omitempty"`
	FavMe bool            `json:"fav_me,omitempty"`
	Set   map[string]bool `json:"set"` // which fields have a value
	// SignedIn: enough credentials to use (a site with a choice - user name and password, or cookies - needs one set).
	SignedIn bool `json:"signed_in"`
	// Removable: always (a built-in one is remembered as removed); Overrides: the
	// installed file replaces a built-in plugin (removing it goes back to the built-in one).
	Removable bool `json:"removable"`
	Overrides bool `json:"overrides"`
	Off       bool `json:"off"` // switched off: nothing runs, its queue waits
	// Paused: it kept failing to answer - its downloads wait until it does (tried again now and then).
	Paused        *plugin.Pause `json:"paused,omitempty"`
	Subscriptions int           `json:"subscriptions"` // subscriptions to this site
	Downloads     int           `json:"downloads"`     // its downloads not done yet (queued, running, paused, failed)
}

// SiteInfos lists the sites, sorted by name.
func (d *Downloader) SiteInfos() []SiteInfo {
	var out []SiteInfo
	usage := d.siteUsage()
	for _, id := range d.Sites() {
		c := d.all()[id].Client
		m := c.Manifest()
		host := m.Base
		if u, err := url.Parse(m.Base); err == nil {
			host = u.Host
		}
		out = append(out, SiteInfo{
			ID: m.ID, Name: m.Name, Site: m.Site, Version: m.Version, Engine: m.Engine, Host: host, BuiltIn: m.BuiltIn,
			TagTypes: m.Tags.Types != "", Fields: orNone(m.Auth.Fields), Required: m.Auth.Required,
			Help: strings.TrimSpace(m.Auth.Help), Hint: m.Search.Hint, TagQuery: m.TagQueries(), Set: c.CredentialSet(), SignedIn: len(m.Auth.Fields) > 0 && c.HasCredentials(),
		})
		info := &out[len(out)-1]
		info.OrQuery, info.OrSep = m.OrQuery()
		info.FavBy, info.FavMe = plugin.FavoritesBy(m, info.Set)
		_, isBuiltin := plugin.Builtin(id)
		info.Removable = true // built-in ones too (installing one again from the plugin list brings it back)
		info.Off = d.IsOff(id)
		if p := c.Paused(); !p.Until.IsZero() {
			info.Paused = &p
		}
		info.Overrides = !m.BuiltIn && isBuiltin
		info.Subscriptions, info.Downloads = usage[id][0], usage[id][1]
	}
	slices.SortFunc(out, func(a, b SiteInfo) int { return strings.Compare(a.Site, b.Site) })
	return out
}

func orNone(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// ErrNoSecretsFile means the downloader wasn't told where credentials live.
var ErrNoSecretsFile = errors.New("credentials can't be saved here")

// ErrBadField means a credential field isn't one the plugin asks for.
var ErrBadField = errors.New("not a credential this site uses")

// SetCredentials saves a site's credentials (fields left out stay as they
// are; an empty value removes that field) and uses them from the next request.
func (d *Downloader) SetCredentials(site string, values map[string]string) error {
	st, ok := d.all()[site]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownSite, site)
	}
	if d.SecretsPath == "" {
		return ErrNoSecretsFile
	}
	fields := st.Client.Manifest().Auth.Fields
	for f := range values {
		if !slices.Contains(fields, f) {
			return fmt.Errorf("%w: %q", ErrBadField, f)
		}
	}
	d.credMu.Lock()
	defer d.credMu.Unlock()
	secrets, err := plugin.LoadSecrets(d.SecretsPath)
	if err != nil {
		return fmt.Errorf("reading the credentials file: %w", err)
	}
	cur := map[string]string{}
	for k, v := range secrets[site] {
		cur[k] = v
	}
	for f, v := range values {
		if v = strings.TrimSpace(v); v == "" {
			delete(cur, f)
		} else {
			cur[f] = v
		}
	}
	if len(cur) == 0 {
		delete(secrets, site)
	} else {
		secrets[site] = cur
	}
	if err := secrets.Save(d.SecretsPath); err != nil {
		return fmt.Errorf("saving the credentials file: %w", err)
	}
	st.Client.SetCredentials(cur)
	d.wake(site) // a search waiting for credentials can start
	return nil
}

// TestResult is what a test request found.
type TestResult struct {
	OK      bool   `json:"ok"`
	Posts   int    `json:"posts"`   // posts the test search returned
	Message string `json:"message"` // for the user, either way
	Detail  string `json:"detail,omitempty"`
}

// TestSite makes one small request with the current credentials.
func (d *Downloader) TestSite(ctx context.Context, site string) (TestResult, error) {
	st, ok := d.all()[site]
	if !ok {
		return TestResult{}, fmt.Errorf("%w: %q", ErrUnknownSite, site)
	}
	if st.Client.Manifest().Auth.Required && !st.Client.HasCredentials() {
		return TestResult{Message: "Add the credentials first."}, nil
	}
	page, err := st.Engine.Search(ctx, "", 0)
	if err != nil {
		return TestResult{Message: "The site didn't accept the request.", Detail: err.Error()}, nil
	}
	if len(page.Posts) == 0 {
		return TestResult{Message: "The site answered, but sent no posts - check the credentials."}, nil
	}
	return TestResult{OK: true, Posts: len(page.Posts), Message: "Connected: the site answered with posts."}, nil
}
