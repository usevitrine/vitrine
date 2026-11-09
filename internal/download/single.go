package download

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/usevitrine/vitrine/internal/plugin"
)

// ErrSinglePost means a subscription was asked for one post: there's nothing new to check for.
var ErrSinglePost = errors.New("a single post can only be downloaded once, not subscribed to")

func singleQuery(post int64) string { return "id:" + strconv.FormatInt(post, 10) }

// SingleCheck says what a search is before it's queued: one post (Query: as it will be kept; Files: how many) or not.
type SingleCheck struct {
	Single bool   `json:"single"`
	Query  string `json:"query,omitempty"`
	Files  int    `json:"files,omitempty"`
}

// CheckSingle reads a search as one post and asks the site for it: a link for another site, or a post it doesn't
// have, is said now, in the dialog (plugin.IsPostRef, plugin.IsNoPost). Ordinary searches ask the site nothing.
func (d *Downloader) CheckSingle(ctx context.Context, site, query string) (SingleCheck, error) {
	st, ok := d.all()[site]
	if !ok {
		return SingleCheck{}, fmt.Errorf("%w: %q", ErrUnknownSite, site)
	}
	m := st.Client.Manifest()
	post, single, err := plugin.SinglePost(m, query)
	if err != nil || !single {
		return SingleCheck{}, err
	}
	page, err := plugin.FetchSingle(plugin.Foreground(ctx), m, st.Engine, post, 0)
	if err != nil {
		return SingleCheck{}, err
	}
	return SingleCheck{Single: true, Query: singleQuery(post), Files: len(page.Posts)}, nil
}
