package download

import (
	"context"

	"github.com/usevitrine/vitrine/internal/plugin"
)

// IsGallerySite tells whether a site's downloads are comics (its engine downloads galleries: e-hentai, ExHentai), not
// library files.
func (d *Downloader) IsGallerySite(id string) bool {
	st := d.all()[id]
	if st == nil {
		return false
	}
	_, ok := st.Engine.(plugin.GalleryEngine)
	return ok
}

// SectionUse counts a section's subscriptions and downloads (comics: the gallery sites'; else the others').
func (d *Downloader) SectionUse(ctx context.Context, comics bool) (subs, downloads int, err error) {
	ss, err := d.Subscriptions(ctx)
	if err != nil {
		return 0, 0, err
	}
	for _, s := range ss {
		if d.IsGallerySite(s.Site) == comics {
			subs++
		}
	}
	list, err := d.List(ctx)
	if err != nil {
		return 0, 0, err
	}
	for _, s := range list {
		if s.SubscriptionID == 0 && d.IsGallerySite(s.Site) == comics {
			downloads++
		}
	}
	return subs, downloads, nil
}

// RemoveSection removes a section's subscriptions and downloads (queued, running and finished) - the section is
// switched off. It returns how many of each went.
func (d *Downloader) RemoveSection(ctx context.Context, comics bool) (subs, downloads int, err error) {
	ss, err := d.Subscriptions(ctx)
	if err != nil {
		return 0, 0, err
	}
	for _, s := range ss {
		if d.IsGallerySite(s.Site) != comics {
			continue
		}
		if err := d.Unsubscribe(ctx, s.ID); err != nil {
			return subs, downloads, err
		}
		subs++
	}
	list, err := d.List(ctx)
	if err != nil {
		return subs, downloads, err
	}
	for _, s := range list {
		if d.IsGallerySite(s.Site) != comics {
			continue
		}
		if err := d.Remove(ctx, s.ID); err != nil {
			return subs, downloads, err
		}
		if s.SubscriptionID == 0 {
			downloads++
		}
	}
	return subs, downloads, nil
}
