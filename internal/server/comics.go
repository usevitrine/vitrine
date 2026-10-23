package server

import (
	"errors"
	"net/http"
	"slices"
	"strconv"

	"github.com/usevitrine/vitrine/internal/index"
	"github.com/usevitrine/vitrine/internal/library"
	"github.com/usevitrine/vitrine/internal/tag"
)

// Comics (their own page; their pages are never in the library's grid):
//   GET  /api/comics?q=&sort=&offset=&limit=   -> {comics, total}  (sort: added, updated, read, title, pages, posted)
//   GET  /api/comics/{id}                      -> {comic, pages, tags}   (pages: files in order; tags: with their origins)
//   POST /api/comics/{id} {read_page?, title?, crop?, tags?, add_tags?}  where reading stopped; the user's own title ("" = the
//                                              site's); the cover's crop ([] = none); the user's tags (replacing them, or more)
//   POST /api/comics/delete {ids}              undo with /api/comics/undelete {ids}
//   GET  /api/comics/tags?q=&site=&limit=      -> {suggestions}  (the comics' tags: never the library's)
//   GET  /api/comics/info?ids=                 -> {comics}  (details for the IDs a search with scope=comics gave)
//   POST /api/comics/do/{action} {ids, ...}    favorite, read, tags, tags-of, refresh (see comicsAction)
// Searches (/api/search, results, dates, autocomplete, namespaces) take scope=comics.

// comicJSON is a comic with its cover's thumbnail (for the shelf).
type comicJSON struct {
	library.Comic
	Thumb       string `json:"thumb,omitempty"`
	Placeholder []byte `json:"placeholder,omitempty"`
	CoverW      int    `json:"cover_w,omitempty"`
	CoverH      int    `json:"cover_h,omitempty"`
	CoverURL    string `json:"cover_url,omitempty"`
	CoverMIME   string `json:"cover_mime,omitempty"`
	// Stack: this entry is a collection (its tile; ID = collectionTile + its ID): its name and counts. The rest is
	// its cover comic's.
	Stack *library.Collection `json:"stack,omitempty"`
	// Mosaic: a collection's tile is made of its first comics' covers (up to four).
	Mosaic []mosaicJSON `json:"mosaic,omitempty"`
}

type mosaicJSON struct {
	Thumb string    `json:"thumb"`
	Crop  []float64 `json:"crop,omitempty"`
	W     int       `json:"w,omitempty"` // (the cover's size: its crop's shape)
	H     int       `json:"h,omitempty"`
}

// collectionTile marks a search result that is a collection (its comics folded into one tile): its ID plus this.
const collectionTile = 1 << 31

// foldCollections turns a collection's comics in a search's result into one entry (its tile), where the first of
// them was.
func (s *Server) foldCollections(r *http.Request, ids []uint32) ([]uint32, error) {
	of, err := s.lib.CollectionOf(r.Context())
	if err != nil || len(of) == 0 {
		return ids, err
	}
	out := make([]uint32, 0, len(ids))
	seen := map[int64]bool{}
	for _, id := range ids {
		k, ok := of[int64(id)]
		switch {
		case !ok:
			out = append(out, id)
		case !seen[k]:
			seen[k] = true
			out = append(out, collectionTile+uint32(k))
		}
	}
	return out, nil
}

// tileShapes is the masonry shapes of a result (width/height x 1024, ix.Shapes). A collection's tile is its covers'
// mosaic in the shape of its tallest cover (as tall as that cover's own tile; the covers fill their cells, cropped).
func (s *Server) tileShapes(r *http.Request, ix *index.Index, ids []uint32) []uint16 {
	sh := ix.Shapes(ids)
	var cols map[int64]library.Collection
	for i, id := range ids {
		if id < collectionTile {
			continue
		}
		if cols == nil {
			cols = map[int64]library.Collection{}
			list, _ := s.lib.Collections(r.Context())
			for _, k := range list {
				cols[k.ID] = k
			}
		}
		k := cols[int64(id-collectionTile)]
		if len(k.Covers) == 0 {
			continue
		}
		covers := make([]uint32, len(k.Covers))
		for j, c := range k.Covers {
			covers[j] = uint32(c)
		}
		sh[i] = slices.Min(ix.Shapes(covers))
	}
	return sh
}

// inCollection keeps a collection's comics of a search's result (its own page), in the collection's order.
func (s *Server) inCollection(r *http.Request, ids []uint32, collection int64) ([]uint32, error) {
	order, err := s.lib.CollectionOrder(r.Context(), collection)
	if err != nil {
		return nil, err
	}
	found := make(map[uint32]bool, len(ids))
	for _, id := range ids {
		found[id] = true
	}
	out := make([]uint32, 0, len(order))
	for _, c := range order {
		if found[uint32(c)] {
			out = append(out, uint32(c))
		}
	}
	return out, nil
}

// orderCollection: POST /api/comics/collections/{id}/order {ids} - its comics in that order.
func (s *Server) orderCollection(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.lib.OrderCollection(r.Context(), id, req.IDs); err != nil {
		collectionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) comicsJSON(r *http.Request, list []library.Comic) ([]comicJSON, error) {
	covers := make([]uint32, 0, len(list))
	for _, c := range list {
		if c.Cover != 0 {
			covers = append(covers, c.Cover)
		}
	}
	rows, err := s.lib.Files(r.Context(), covers)
	if err != nil {
		return nil, err
	}
	byID := map[uint32]fileJSON{}
	for _, f := range rows {
		byID[f.ID] = s.toJSON(f)
	}
	out := make([]comicJSON, len(list))
	for i, c := range list {
		out[i] = comicJSON{Comic: c}
		if f, ok := byID[c.Cover]; ok {
			out[i].Thumb, out[i].Placeholder, out[i].CoverW, out[i].CoverH = f.Thumb, f.Placeholder, f.Width, f.Height
			out[i].CoverURL, out[i].CoverMIME = f.URL, f.MIME
		}
	}
	return out, nil
}

func (s *Server) listComics(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	offset, _ := strconv.Atoi(q.Get("offset"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	list, total, err := s.lib.Comics(r.Context(), library.ComicQuery{Search: q.Get("q"), Sort: q.Get("sort"), Offset: offset, Limit: limit})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	out, err := s.comicsJSON(r, list)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"comics": out, "total": total})
}

func comicID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("bad comic ID"))
		return 0, false
	}
	return id, true
}

func comicError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, library.ErrNoComic):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, library.ErrInvalid):
		writeError(w, http.StatusBadRequest, err)
	default:
		writeError(w, http.StatusInternalServerError, err)
	}
}

func (s *Server) getComic(w http.ResponseWriter, r *http.Request) {
	id, ok := comicID(w, r)
	if !ok {
		return
	}
	c, err := s.lib.Comic(r.Context(), id)
	if err != nil {
		comicError(w, err)
		return
	}
	ids, err := s.lib.ComicPages(r.Context(), id)
	if err != nil {
		comicError(w, err)
		return
	}
	rows, err := s.lib.Files(r.Context(), ids)
	if err != nil {
		comicError(w, err)
		return
	}
	pages := make([]fileJSON, len(rows))
	for i, f := range rows {
		pages[i] = s.toJSON(f)
	}
	cj, err := s.comicsJSON(r, []library.Comic{c})
	if err != nil {
		comicError(w, err)
		return
	}
	tags, err := s.lib.ComicTags(r.Context(), id)
	if err != nil {
		comicError(w, err)
		return
	}
	type tagJSON struct {
		Tag     string   `json:"tag"`
		Origins []string `json:"origins"`
	}
	detail := make([]tagJSON, len(tags))
	for i, tg := range tags {
		detail[i] = tagJSON{tg.Tag, tg.Origins}
	}
	writeJSON(w, http.StatusOK, map[string]any{"comic": cj[0], "pages": pages, "tags": detail})
}

func (s *Server) updateComic(w http.ResponseWriter, r *http.Request) {
	id, ok := comicID(w, r)
	if !ok {
		return
	}
	var req struct {
		ReadPage *int       `json:"read_page"`
		Title    *string    `json:"title"`    // the user's own title ("" = the site's again)
		Crop     *[]float64 `json:"crop"`     // the cover's crop (x, y, w, h fractions; [] = all of it)
		Tags     []string   `json:"tags"`     // the user's tags (null: unchanged)
		AddTags  []string   `json:"add_tags"` // more tags of the user's
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.ReadPage != nil {
		if err := s.lib.SetComicRead(r.Context(), id, *req.ReadPage); err != nil {
			comicError(w, err)
			return
		}
	}
	if req.Title != nil {
		if err := s.lib.SetComicTitle(r.Context(), id, *req.Title); err != nil {
			comicError(w, err)
			return
		}
	}
	if req.Crop != nil {
		crop := *req.Crop
		if len(crop) == 0 {
			crop = nil
		}
		if err := s.lib.SetComicCrop(r.Context(), id, crop); err != nil {
			comicError(w, err)
			return
		}
	}
	if req.Tags != nil {
		if err := s.lib.SetComicTags(r.Context(), id, s.parseTags(req.Tags)); err != nil {
			comicError(w, err)
			return
		}
	}
	if len(req.AddTags) > 0 {
		if err := s.lib.AddComicTags(r.Context(), id, s.parseTags(req.AddTags)); err != nil {
			comicError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) parseTags(list []string) []tag.Tag {
	tags := make([]tag.Tag, 0, len(list))
	for _, t := range list {
		if t := s.lib.ParseTag(t); t.Name != "" {
			tags = append(tags, t)
		}
	}
	return tags
}

func (s *Server) deleteComics(w http.ResponseWriter, r *http.Request, undo bool) {
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := readJSON(r, &req); err != nil || len(req.IDs) == 0 || len(req.IDs) > 1000 {
		writeError(w, http.StatusBadRequest, errors.New("ids: 1 to 1000 comic IDs"))
		return
	}
	req.IDs = slices.DeleteFunc(req.IDs, func(id int64) bool { return id >= collectionTile }) // (a collection's tile isn't a comic)
	var err error
	if undo {
		err = s.lib.UndeleteComics(r.Context(), req.IDs)
	} else {
		err = s.lib.DeleteComics(r.Context(), req.IDs)
	}
	if err != nil {
		comicError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"done": len(req.IDs)})
}

func (s *Server) comicTags(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = 12
	}
	sugg, err := s.lib.ComicSiteTags(r.Context(), q.Get("q"), q.Get("site"), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"suggestions": sugg})
}

// searchIndex is the index a search request runs on: the library's, or with
// scope=comics the comics' (the Comics page searches, pages through results
// and suggests tags exactly like the library, over comics).
func (s *Server) searchIndex(w http.ResponseWriter, r *http.Request) (*index.Index, bool) {
	var ix *index.Index
	var err error
	switch r.URL.Query().Get("scope") {
	case "comics":
		ix, err = s.lib.ComicIndex(r.Context())
	case "trash": // (the deleted files: the Trash page)
		ix, err = s.lib.TrashIndex(r.Context())
	case "comics_trash": // (the deleted comics: the Trash's Comics page)
		ix, err = s.lib.ComicsTrashIndex(r.Context())
	default:
		return s.lib.Index(), true
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return nil, false
	}
	return ix, true
}

// comicInfo: GET /api/comics/info?ids=1,2 -> {comics} in that order (the
// Comics grid's details for the IDs a search gave).
func (s *Server) comicInfo(w http.ResponseWriter, r *http.Request) {
	ids, err := parseIDs(r.URL.Query().Get("ids"), 1000)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	// (collection tiles: their cover comic, named and counted as the collection)
	var cols map[int64]library.Collection
	want := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id < collectionTile {
			want = append(want, int64(id))
			continue
		}
		if cols == nil {
			list, err := s.lib.Collections(r.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			cols = map[int64]library.Collection{}
			for _, k := range list {
				cols[k.ID] = k
			}
		}
		if k, ok := cols[int64(id-collectionTile)]; ok && k.Cover != 0 {
			want = append(want, k.Cover)
		}
	}
	got, err := s.lib.ComicsByID(r.Context(), want)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	byID := map[int64]library.Comic{}
	for _, c := range got {
		byID[c.ID] = c
	}
	list := make([]library.Comic, 0, len(ids))
	stacks := map[int]library.Collection{}
	for _, id := range ids {
		if id < collectionTile {
			if c, ok := byID[int64(id)]; ok {
				list = append(list, c)
			}
			continue
		}
		k, ok := cols[int64(id-collectionTile)]
		c, has := byID[k.Cover]
		if !ok || !has {
			continue
		}
		c.ID, c.Title, c.SiteTitle, c.Collection = int64(id), k.Name, "", k.ID
		stacks[len(list)] = k
		list = append(list, c)
	}
	out, err := s.comicsJSON(r, list)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if len(stacks) > 0 {
		// The mosaics: the covers of each collection's first comics.
		var more []int64
		for _, k := range stacks {
			more = append(more, k.Covers...)
		}
		covers, err := s.lib.ComicsByID(r.Context(), more)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		thumbs, err := s.comicsJSON(r, covers)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		byCover := map[int64]mosaicJSON{}
		for _, c := range thumbs {
			if c.Thumb != "" {
				byCover[c.ID] = mosaicJSON{c.Thumb, c.Crop, c.CoverW, c.CoverH}
			}
		}
		for i, k := range stacks {
			out[i].Stack = &k
			for _, id := range k.Covers {
				if m, ok := byCover[id]; ok {
					out[i].Mosaic = append(out[i].Mosaic, m)
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"comics": out})
}

// comicsAction: POST /api/comics/do/{action} {ids, on?, read?, add?, remove?} for
// selections - favorite {on}, read {read: true = read, false = unread}, tags
// {add, remove: the user's own tags}, refresh (their sites' tags again),
// redownload (their pages cleared and fetched again from the site).
func (s *Server) comicsAction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs        []int64  `json:"ids"`
		On         bool     `json:"on"`
		Read       bool     `json:"read"`
		Add        []string `json:"add"`
		Remove     []string `json:"remove"`
		Collection int64    `json:"collection"` // collection: the one to put them in (0: out of theirs)
	}
	if err := readJSON(r, &req); err != nil || len(req.IDs) == 0 || len(req.IDs) > 5000 {
		writeError(w, http.StatusBadRequest, errors.New("ids: 1 to 5000 comic IDs"))
		return
	}
	// (comics only: a collection's tile isn't one)
	req.IDs = slices.DeleteFunc(req.IDs, func(id int64) bool { return id >= collectionTile })
	if len(req.IDs) == 0 {
		writeJSON(w, http.StatusOK, map[string]int{"changed": 0})
		return
	}
	ctx := r.Context()
	switch r.PathValue("action") {
	case "collection":
		if err := s.lib.SetCollection(ctx, req.IDs, req.Collection); err != nil {
			collectionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"changed": len(req.IDs)})
	case "favorite":
		n, err := s.lib.SetComicFavorite(ctx, req.IDs, req.On)
		if err != nil {
			comicError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"changed": n})
	case "read":
		if err := s.lib.SetComicsRead(ctx, req.IDs, req.Read); err != nil {
			comicError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"changed": len(req.IDs)})
	case "tags":
		if err := s.lib.EditComicTags(ctx, req.IDs, s.parseTags(req.Add), s.parseTags(req.Remove)); err != nil {
			comicError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"changed": len(req.IDs)})
	case "tags-of":
		tags, err := s.lib.ComicTagsOf(ctx, req.IDs)
		if err != nil {
			comicError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"files": len(req.IDs), "tags": orEmpty(tags)})
	case "refresh":
		if s.dl == nil {
			writeError(w, http.StatusServiceUnavailable, errNoDownloader)
			return
		}
		n, err := s.dl.RefreshComics(ctx, req.IDs)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"found": n})
	case "redownload":
		// Their pages fetched again: cleared, then a download of each gallery.
		if s.dl == nil {
			writeError(w, http.StatusServiceUnavailable, errNoDownloader)
			return
		}
		list, err := s.lib.ComicsByID(ctx, req.IDs)
		if err != nil {
			comicError(w, err)
			return
		}
		var ids []int64
		for _, c := range list {
			if c.Site != "" && c.URL != "" {
				ids = append(ids, c.ID)
			}
		}
		if err := s.lib.ClearComicPages(ctx, ids); err != nil {
			comicError(w, err)
			return
		}
		for _, c := range list {
			if c.Site == "" || c.URL == "" {
				continue
			}
			if _, err := s.dl.Add(ctx, c.Site, c.URL, 0); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]int{"queued": len(ids)})
	default:
		writeError(w, http.StatusNotFound, errors.New("no such action"))
	}
}

func collectionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, library.ErrNoCollection):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, library.ErrInvalid):
		writeError(w, http.StatusBadRequest, err)
	default:
		writeError(w, http.StatusInternalServerError, err)
	}
}

// collections: GET /api/comics/collections -> {collections} (by name, with their counts and cover comic).
func (s *Server) collections(w http.ResponseWriter, r *http.Request) {
	list, err := s.lib.Collections(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"collections": orEmpty(list)})
}

// createCollection: POST /api/comics/collections {name, ids} -> {id}.
func (s *Server) createCollection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string  `json:"name"`
		IDs  []int64 `json:"ids"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	req.IDs = slices.DeleteFunc(req.IDs, func(id int64) bool { return id >= collectionTile })
	id, err := s.lib.CreateCollection(r.Context(), req.Name, req.IDs)
	if err != nil {
		collectionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

// renameCollection: POST /api/comics/collections/{id} {name}.
func (s *Server) renameCollection(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.lib.RenameCollection(r.Context(), id, req.Name); err != nil {
		collectionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// deleteCollectionComics: POST /api/comics/collections/{id}/delete -> {ids}: the collection with its comics (into the
// trash; ids: the comics, for an undo).
func (s *Server) deleteCollectionComics(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	ids, err := s.lib.DeleteCollectionComics(r.Context(), id)
	if err != nil {
		collectionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ids": orEmpty(ids)})
}

// deleteCollection: POST /api/comics/collections/{id}/remove - its comics stay, on their own again.
func (s *Server) deleteCollection(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := s.lib.DeleteCollection(r.Context(), id); err != nil {
		collectionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
