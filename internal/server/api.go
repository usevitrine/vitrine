package server

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/usevitrine/vitrine/internal/auth"
	"github.com/usevitrine/vitrine/internal/i18n"
	"github.com/usevitrine/vitrine/internal/index"
	"github.com/usevitrine/vitrine/internal/library"
	"github.com/usevitrine/vitrine/internal/media"
	"github.com/usevitrine/vitrine/internal/search"
	"github.com/usevitrine/vitrine/internal/store"
	"github.com/usevitrine/vitrine/internal/tag"
	"github.com/usevitrine/vitrine/internal/thumbs"
)

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// isHTTPS tells whether the browser reached us over HTTPS (directly or via a
// reverse proxy), so the session cookie can be marked Secure.
func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	need, err := s.auth.SetupNeeded(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	// language: the server's default (VITRINE_LANGUAGE), for browsers whose own languages have no file.
	// anonymous: signed out, the app sends the visitor to the plain sign-in page (/signin) instead of its own.
	// passkey_only: the sign-in screen offers the passkey alone.
	only, err := s.auth.PasskeyOnly(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	out := map[string]any{"setup_needed": need, "logged_in": false, "language": i18n.Default(), "anonymous": s.anonymous.Load(), "passkey_only": only}
	if c, err := r.Cookie(sessionCookie); err == nil {
		if u, err := s.auth.Session(r.Context(), c.Value); err == nil {
			out["logged_in"], out["user"] = true, u.Name
		}
	}
	writeJSON(w, http.StatusOK, out)
}

type credentials struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if err := readJSON(r, &c); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.auth.Setup(r.Context(), c.Name, c.Password); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, auth.ErrSetupDone) {
			status = http.StatusConflict
		}
		writeError(w, status, err)
		return
	}
	s.startSession(w, r, c)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if err := readJSON(r, &c); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.startSession(w, r, c)
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, c credentials) {
	token, err := s.auth.Login(r.Context(), c.Name, c.Password, r.UserAgent())
	// (every sign-in in the log, as it went: someone guessing shows - 2026-10-31; never the password, the name only as
	// typed, cut short)
	name := c.Name
	if len(name) > 60 {
		name = name[:60] + "…"
	}
	switch {
	case errors.Is(err, auth.ErrBadLogin):
		s.log.Warn("sign-in failed: wrong name or password", "name", name, "from", clientOf(r))
		writeError(w, http.StatusUnauthorized, err)
		return
	case errors.Is(err, auth.ErrTooMany):
		s.log.Warn("sign-in refused: too many failed tries", "from", clientOf(r))
		writeError(w, http.StatusTooManyRequests, err)
		return
	case errors.Is(err, auth.ErrPasskeyOnly):
		s.log.Warn("sign-in refused: passkey only", "name", name, "from", clientOf(r))
		writeError(w, http.StatusForbidden, err)
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.log.Info("signed in", "user", c.Name, "with", "a password", "from", clientOf(r))
	setSessionCookie(w, r, token)
	writeJSON(w, http.StatusOK, map[string]any{"logged_in": true, "user": c.Name})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.auth.Logout(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode})
	writeJSON(w, http.StatusOK, map[string]bool{"logged_in": false})
}

func intParam(r *http.Request, name string, def, lo, hi int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil {
		return def
	}
	return min(max(v, lo), hi)
}

// search runs a query, keeps the whole result server-side and returns its
// ID with the first slice.
func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q, err := search.Parse(r.URL.Query().Get("q"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.withNew(r, &q)
	ix, ok := s.searchIndex(w, r)
	if !ok {
		return
	}
	limit := intParam(r, "limit", 200, 0, 5000)
	start := time.Now()
	res := ix.Search(q, time.Now())
	// New files (imported after new_since): counted, and with pin=1 put first,
	// ordered by new_sort (default newest imported first).
	newSince, _ := strconv.ParseInt(r.URL.Query().Get("new_since"), 10, 64)
	newCount, pinned := 0, 0
	if newSince > 0 {
		if r.URL.Query().Get("pin") == "1" {
			ns := search.Sort{} // newest imported first
			if v := r.URL.Query().Get("new_sort"); v != "" {
				nq, err := search.Parse("sort:" + v)
				if err != nil {
					writeError(w, http.StatusBadRequest, err)
					return
				}
				ns = nq.Sort
			}
			res.IDs, pinned = ix.PinNew(res.IDs, newOpts(r, newSince), ns)
			newCount = pinned
		} else {
			newCount = ix.CountNew(res.IDs, newOpts(r, newSince))
		}
	}
	comics := -1
	if r.URL.Query().Get("scope") == "comics" {
		comics = len(res.IDs) // (every comic found, a collection's each one: the count shown)
		// A collection's own page: its comics, in its own order; elsewhere the comics being read first ("continue
		// reading"), and a collection's comics fold into its tile.
		var err error
		if k, _ := strconv.ParseInt(r.URL.Query().Get("collection"), 10, 64); k > 0 {
			res.IDs, err = s.inCollection(r, res.IDs, k)
		} else {
			res.IDs = ix.PinReading(res.IDs)
			res.IDs, err = s.foldCollections(r, res.IDs)
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	took := time.Since(start)
	id := s.results.add(res.IDs)
	out := map[string]any{
		"result":  id,
		"total":   len(res.IDs),
		"matched": res.Total, // before limit:N
		"ids":     orEmpty(res.IDs[:min(limit, len(res.IDs))]),
		"unknown": orEmpty(res.Unknown),
		"seed":    res.Seed,
		"took_ms": float64(took.Microseconds()) / 1000,
		"query":   q.String(),
		"now":     start.UnixMilli(), // the snapshot's time, for /api/new
		"new":     newCount,          // files imported after new_since
		"pinned":  pinned,            // of them, how many are at the front (pin=1)
	}
	// find=<ids>: where those files are in this list (files that arrived since the last search can land anywhere in
	// an order other than newest first: the page keeps the open file and what's on screen by them, not by how far the
	// first file moved).
	if f := r.URL.Query().Get("find"); f != "" {
		want := map[uint32]bool{}
		for _, s := range strings.Split(f, ",") {
			if n, err := strconv.ParseUint(strings.TrimSpace(s), 10, 32); err == nil && len(want) < 8 {
				want[uint32(n)] = true
			}
		}
		found := map[string]int{}
		for i, id := range res.IDs {
			if want[id] {
				found[strconv.FormatUint(uint64(id), 10)] = i
				if len(found) == len(want) {
					break
				}
			}
		}
		out["found"] = found
	}
	if comics >= 0 {
		out["comics"] = comics // (total: the tiles, a collection one)
	}
	// shapes=1 (the masonry grid): every file's width/height, in result order -
	// little-endian uint16s (ratio x 1024), base64.
	if r.URL.Query().Get("shapes") == "1" {
		sh := s.tileShapes(r, ix, res.IDs) // (a collection's tile: its covers' mosaic)
		b := make([]byte, 2*len(sh))
		for i, v := range sh {
			binary.LittleEndian.PutUint16(b[2*i:], v)
		}
		out["shapes"] = base64.StdEncoding.EncodeToString(b)
	}
	writeJSON(w, http.StatusOK, out)
}

// newFiles counts files matching a search that were imported after a
// result was taken (the grid's "N new files" pill):
// GET /api/new?q=...&since=<ms> -> {new}.
func (s *Server) newFiles(w http.ResponseWriter, r *http.Request) {
	q, err := search.Parse(r.URL.Query().Get("q"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ix, ok := s.searchIndex(w, r)
	if !ok {
		return
	}
	s.withNew(r, &q)
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	q.Limit = 0
	res := ix.Search(q, time.Now())
	writeJSON(w, http.StatusOK, map[string]int{"new": ix.CountNew(res.IDs, newOpts(r, since))})
}

// withNew fills in what system:new compares with: the request's new_since and new_bulk=0 (the NEW pills' own: a saved
// search opened from the menu has its own time), else the shared settings seenAt and newFromBulk (missing = off).
func (s *Server) withNew(r *http.Request, q *search.Query) {
	var atoms []*search.Atom
	for i := range q.Terms {
		for j := range q.Terms[i].Any {
			if a := &q.Terms[i].Any[j]; a.Kind == search.KindSystem && a.Sys.Field == search.FieldNew {
				atoms = append(atoms, a)
			}
		}
	}
	if len(atoms) == 0 {
		return
	}
	since, _ := strconv.ParseInt(r.URL.Query().Get("new_since"), 10, 64)
	skip := r.URL.Query().Get("new_bulk") == "0"
	if since <= 0 {
		if set, err := s.lib.Settings(r.Context()); err == nil {
			json.Unmarshal(set["seenAt"], &since)
			skip = string(set["newFromBulk"]) != "true"
		}
	}
	for _, a := range atoms {
		a.Sys.Since, a.Sys.SkipBulk = since, skip
	}
}

// newOpts: which files count as new - imported after since, and without
// bulk ones when the request says new_bulk=0 (the newFromBulk setting).
func newOpts(r *http.Request, since int64) index.NewOpts {
	return index.NewOpts{Since: since, SkipBulk: r.URL.Query().Get("new_bulk") == "0"}
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func (s *Server) resultSlice(w http.ResponseWriter, r *http.Request) {
	ids, ok := s.results.get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, errors.New("result expired, search again"))
		return
	}
	offset := intParam(r, "offset", 0, 0, len(ids))
	limit := intParam(r, "limit", 200, 0, 5000)
	writeJSON(w, http.StatusOK, map[string]any{
		"total": len(ids), "offset": offset, "ids": orEmpty(ids[offset:min(offset+limit, len(ids))]),
	})
}

func parseIDs(s string, maxN int) ([]uint32, error) {
	var ids []uint32
	for _, part := range strings.Split(s, ",") {
		if part == "" {
			continue
		}
		n, err := strconv.ParseUint(part, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("bad file ID %q", part)
		}
		ids = append(ids, uint32(n))
		if len(ids) > maxN {
			return nil, fmt.Errorf("at most %d IDs per request", maxN)
		}
	}
	return ids, nil
}

// fileJSON is a file as the client sees it.
type fileJSON struct {
	ID          uint32 `json:"id"`
	SHA256      string `json:"sha256"`
	Type        string `json:"type"`
	MIME        string `json:"mime"`
	Size        int64  `json:"size"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	DurationMS  int64  `json:"duration_ms"`
	HasAudio    bool   `json:"has_audio"`
	Favorite    bool   `json:"favorite,omitempty"`
	Climaxes    int    `json:"came,omitempty"`
	ImportedAt  int64  `json:"imported_at"`
	ModifiedAt  int64  `json:"modified_at"`
	PostedAt    int64  `json:"posted_at,omitempty"`   // when posted on its site (upload time)
	Placeholder []byte `json:"placeholder,omitempty"` // 4x4 RGB, base64
	URL         string `json:"url"`
	Thumb       string `json:"thumb,omitempty"`
}

func (s *Server) toJSON(f library.FileRow) fileJSON {
	j := fileJSON{
		ID: f.ID, SHA256: f.SHA256.String(), Type: f.Type.String(), MIME: f.Type.MIME(), Size: f.Size,
		Width: f.Width, Height: f.Height, DurationMS: f.DurationMS, HasAudio: f.HasAudio, Favorite: f.Favorite, Climaxes: f.Climaxes,
		ImportedAt: f.ImportedAt, ModifiedAt: f.ModifiedAt, PostedAt: f.PostedAt, Placeholder: f.Placeholder,
		URL: "/files/" + f.SHA256.String() + "." + f.Type.Ext(),
	}
	sig := s.linkSuffix(f.SHA256)
	if sig != "" {
		j.URL += "?" + sig
	}
	if f.Flags&library.FlagThumbnail != 0 {
		// The fingerprint makes the URL unique to this file, so a long-cached
		// thumbnail can never show up for another file with the same ID (e.g.
		// after starting a new library).
		j.Thumb = fmt.Sprintf("/thumbs/%d.webp?v=%d&f=%s", f.ID, media.ThumbVersion, thumbKey(f.SHA256))
		if sig != "" {
			j.Thumb += "&" + sig
		}
	}
	return j
}

func (s *Server) files(w http.ResponseWriter, r *http.Request) {
	ids, err := parseIDs(r.URL.Query().Get("ids"), 1000)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	rows, err := s.lib.Files(r.Context(), ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]fileJSON, len(rows))
	for i, f := range rows {
		out[i] = s.toJSON(f)
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": out})
}

func (s *Server) fileTags(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 32)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("bad file ID"))
		return
	}
	tags, err := s.lib.FileTags(r.Context(), uint32(id))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	type tagJSON struct {
		Tag     string   `json:"tag"`
		Origins []string `json:"origins"`
	}
	out := make([]tagJSON, len(tags))
	for i, t := range tags {
		out[i] = tagJSON{t.Tag, t.Origins}
	}
	links, err := s.lib.FileURLs(r.Context(), uint32(id))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	type urlJSON struct {
		Kind string `json:"kind"` // post, source or file
		Site string `json:"site"`
		URL  string `json:"url"`
	}
	kinds := map[int]string{library.URLPost: "post", library.URLSource: "source", library.URLFile: "file"}
	urls := make([]urlJSON, len(links))
	for i, u := range links {
		urls[i] = urlJSON{kinds[u.Kind], u.Site, u.URL}
	}
	sources, err := s.lib.FileSources(r.Context(), uint32(id))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": out, "urls": urls, "sources": orEmpty(sources)})
}

// libError picks the status for a library error.
func libError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, library.ErrNoFile), errors.Is(err, library.ErrPurged):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, library.ErrInvalid), errors.Is(err, library.ErrBadPattern):
		writeError(w, http.StatusBadRequest, err) // e.g. a relation loop
	default:
		writeError(w, http.StatusInternalServerError, err)
	}
}

func (s *Server) editTags(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs    []uint32 `json:"ids"`
		Add    []string `json:"add"`
		Remove []string `json:"remove"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	parse := func(texts []string) []tag.Tag {
		var out []tag.Tag
		for _, t := range texts {
			if pt := s.lib.ParseTag(t); pt.Name != "" {
				out = append(out, pt)
			}
		}
		return out
	}
	if add := parse(req.Add); len(add) > 0 {
		if err := s.lib.AddTags(r.Context(), req.IDs, add); err != nil {
			libError(w, err)
			return
		}
	}
	if rm := parse(req.Remove); len(rm) > 0 {
		if err := s.lib.RemoveTags(r.Context(), req.IDs, rm); err != nil {
			libError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]int{"updated": len(req.IDs)})
}

func (s *Server) deleteFiles(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs    []uint32 `json:"ids"`
		Reason string   `json:"reason"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.lib.Delete(r.Context(), req.IDs, req.Reason); err != nil {
		libError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"deleted": len(req.IDs)})
}

// favorite: POST /api/favorite {ids, on} -> {changed}.
func (s *Server) favorite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []uint32 `json:"ids"`
		On  bool     `json:"on"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	n, err := s.lib.SetFavorite(r.Context(), req.IDs, req.On)
	if err != nil {
		libError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"changed": n})
}

// seenFiles: POST /api/files/seen {ids} -> {seen}: files opened in the viewer are never new again (seen: how many
// weren't seen before).
func (s *Server) seenFiles(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []uint32 `json:"ids"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if len(req.IDs) > 1000 {
		writeError(w, http.StatusBadRequest, errors.New("at most 1000 files at a time"))
		return
	}
	n, err := s.lib.SeeFiles(r.Context(), req.IDs)
	if err != nil {
		libError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"seen": n})
}

func (s *Server) undeleteFiles(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []uint32 `json:"ids"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.lib.Undelete(r.Context(), req.IDs); err != nil {
		libError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"restored": len(req.IDs)})
}

func (s *Server) autocomplete(w http.ResponseWriter, r *http.Request) {
	ix, ok := s.searchIndex(w, r)
	if !ok {
		return
	}
	limit := intParam(r, "limit", 20, 1, 200)
	start := time.Now()
	sugg := ix.Autocomplete(r.URL.Query().Get("q"), limit, r.URL.Query().Get("sites")) // (sites: only their files' tags)
	writeJSON(w, http.StatusOK, map[string]any{
		"suggestions": orEmpty(sugg), "took_ms": float64(time.Since(start).Microseconds()) / 1000,
	})
}

func (s *Server) relations(w http.ResponseWriter, r *http.Request) {
	parents, siblings := s.lib.Relations()
	writeJSON(w, http.StatusOK, map[string]any{"parents": orEmpty(parents), "siblings": orEmpty(siblings)})
}

func (s *Server) editRelation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind   string `json:"kind"`   // "parent" or "sibling"
		Action string `json:"action"` // "add" or "remove"
		From   string `json:"from"`   // child, or the sibling
		To     string `json:"to"`     // parent, or the ideal tag
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	from, to := s.lib.ParseTag(req.From), s.lib.ParseTag(req.To)
	ctx := r.Context()
	var err error
	switch req.Kind + "/" + req.Action {
	case "parent/add":
		err = s.lib.AddParent(ctx, from, to)
	case "parent/remove":
		err = s.lib.RemoveParent(ctx, from, to)
	case "sibling/add":
		err = s.lib.SetSibling(ctx, from, to)
	case "sibling/remove":
		err = s.lib.RemoveSibling(ctx, from)
	default:
		writeError(w, http.StatusBadRequest, errors.New(`kind must be "parent" or "sibling", action "add" or "remove"`))
		return
	}
	if err != nil {
		libError(w, err)
		return
	}
	s.relations(w, r)
}

// serveFile sends an original. URLs name the content hash, so the response
// never changes and browsers may cache it for good. Range requests (video
// seeking) are handled by ServeContent.
func (s *Server) serveFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	hexHash, _, _ := strings.Cut(name, ".")
	h, err := store.ParseHash(hexHash)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !s.mayRead(r, h) {
		writeError(w, http.StatusUnauthorized, auth.ErrNoSession)
		return
	}
	f, err := s.lib.FileBySHA(r.Context(), h)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if size, err := strconv.Atoi(r.URL.Query().Get("size")); err == nil && size > 0 {
		if p, ok := s.rendition(r, f, size); ok {
			s.sendFile(w, r, p, "image/webp", f.ImportedAt)
			return
		}
	}
	fh, err := s.lib.OpenOriginal(f)
	if err != nil {
		s.log.Error("open original", "file", f.ID, "err", err)
		http.Error(w, "file missing from the store", http.StatusInternalServerError)
		return
	}
	defer fh.Close()
	w.Header().Set("Content-Type", f.Type.MIME())
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeContent(w, r, "", time.UnixMilli(f.ImportedAt), fh)
}

// previewFile reads a thumbnail's or sprite's file from /{name}.webp?f=<key>: its ID, if it's this library's (the key,
// the start of its SHA-256, matches - needKey: it must be there) and the request may see it; else it has answered.
func (s *Server) previewFile(w http.ResponseWriter, r *http.Request, needKey bool) (uint32, bool) {
	idText, ok := strings.CutSuffix(r.PathValue("name"), ".webp")
	id, err := strconv.ParseUint(idText, 10, 32)
	if !ok || err != nil {
		http.NotFound(w, r)
		return 0, false
	}
	rows, err := s.lib.Files(r.Context(), []uint32{uint32(id)})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return 0, false
	}
	q := r.URL.Query()
	if len(rows) == 0 || ((needKey || q.Has("f")) && q.Get("f") != thumbKey(rows[0].SHA256)) {
		http.NotFound(w, r) // a URL for a file this library doesn't have
		return 0, false
	}
	if !s.mayRead(r, rows[0].SHA256) {
		writeError(w, http.StatusUnauthorized, auth.ErrNoSession)
		return 0, false
	}
	return uint32(id), true
}

func (s *Server) serveThumb(w http.ResponseWriter, r *http.Request) {
	id, ok := s.previewFile(w, r, false)
	if !ok {
		return
	}
	q := r.URL.Query()
	t, err := s.lib.Thumb(r.Context(), id)
	if errors.Is(err, thumbs.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "image/webp")
	if q.Get("v") == strconv.Itoa(t.Version) && q.Has("f") {
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "private, no-cache")
	}
	w.Write(t.Data)
}

// thumbKey is the part of a file's SHA-256 put in its thumbnail URL.
func thumbKey(h store.Hash) string { return h.String()[:16] }

// namespaces lists the tag namespaces (the web client colors and groups
// tags by them; a "x:" prefix that isn't one is part of the tag's name).
func (s *Server) namespaces(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("scope") == "comics" {
		ix, ok := s.searchIndex(w, r)
		if ok {
			writeJSON(w, http.StatusOK, map[string]any{"namespaces": ix.UsedNamespaces()})
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"namespaces": orEmpty(s.lib.Namespaces())})
}

// resultDates samples a result's dates for the grid's fast scroller:
// GET /api/results/{id}/dates?field=imported|uploaded&n=200 -> {total, dates}
// with n times (ms) at evenly spaced positions (first and last included).
func (s *Server) resultDates(w http.ResponseWriter, r *http.Request) {
	ids, ok := s.results.get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, errors.New("result expired, search again"))
		return
	}
	n := min(intParam(r, "n", 200, 2, 2000), len(ids))
	sample := make([]uint32, n)
	for k := range n {
		pos := 0
		if n > 1 {
			pos = k * (len(ids) - 1) / (n - 1)
		}
		sample[k] = ids[pos]
	}
	ix, ok := s.searchIndex(w, r)
	if !ok {
		return
	}
	dates := ix.Times(sample, r.URL.Query().Get("field") == "uploaded")
	writeJSON(w, http.StatusOK, map[string]any{"total": len(ids), "dates": orEmpty(dates)})
}

// spriteInfo: GET /api/files/{id}/sprite -> the video's preview-frame layout
// and image URL (404 while they're not made yet).
func (s *Server) spriteInfo(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 32)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("bad file ID"))
		return
	}
	rows, err := s.lib.Files(r.Context(), []uint32{uint32(id)})
	if err != nil || len(rows) == 0 {
		writeError(w, http.StatusNotFound, errors.New("no such file"))
		return
	}
	sp, err := s.lib.Sprite(r.Context(), uint32(id), false)
	if errors.Is(err, thumbs.ErrNotFound) {
		writeError(w, http.StatusNotFound, errors.New("no preview frames yet"))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"url":    fmt.Sprintf("/sprites/%d.webp?f=%s&v=%d", id, thumbKey(rows[0].SHA256), sp.Version),
		"frames": sp.Frames, "cols": sp.Cols, "tile_w": sp.TileW, "tile_h": sp.TileH, "interval_ms": sp.IntervalMS,
	})
}

// serveSprite: GET /sprites/{id}.webp?f=<key>&v=<version>, like thumbnails.
func (s *Server) serveSprite(w http.ResponseWriter, r *http.Request) {
	id, ok := s.previewFile(w, r, true)
	if !ok {
		return
	}
	q := r.URL.Query()
	sp, err := s.lib.Sprite(r.Context(), id, true)
	if errors.Is(err, thumbs.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "image/webp")
	if q.Get("v") == strconv.Itoa(sp.Version) {
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "private, no-cache")
	}
	w.Write(sp.Data)
}

// siteCounts: GET /api/search/sites?q= -> {sites: [{site, count}]}: how many
// of a search's files came from each site (the library's site picker).
func (s *Server) siteCounts(w http.ResponseWriter, r *http.Request) {
	q, err := search.Parse(r.URL.Query().Get("q"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.withNew(r, &q)
	ix, ok := s.searchIndex(w, r)
	if !ok {
		return
	}
	q.Limit = 0
	res := ix.Search(q, time.Now())
	names, err := s.lib.SiteNames(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	type row struct {
		Site  string `json:"site"`
		Count int    `json:"count"`
	}
	out := []row{}
	for id, n := range ix.SiteCounts(res.IDs) {
		if name, ok := names[id]; ok {
			out = append(out, row{name, n})
		}
	}
	slices.SortFunc(out, func(a, b row) int { return b.Count - a.Count })
	writeJSON(w, http.StatusOK, map[string]any{"total": len(res.IDs), "sites": out})
}

// tagsOf lists the tags of the selected files with how many have each (the
// selection's tag editor): POST /api/tags/of {ids} -> {files, tags}.
func (s *Server) tagsOf(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []uint32 `json:"ids"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	tags, err := s.lib.TagsOf(r.Context(), req.IDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": len(req.IDs), "tags": orEmpty(tags)})
}

// tagSites says which sites gave tags (a new download from tags picks its
// site from them): GET /api/tags/sites?t=<tag>&t=... -> {tags: {tag: [{site, count}]}}.
func (s *Server) tagSites(w http.ResponseWriter, r *http.Request) {
	texts := r.URL.Query()["t"]
	if len(texts) == 0 || len(texts) > 50 {
		writeError(w, http.StatusBadRequest, errors.New("1 to 50 tags (t=...)"))
		return
	}
	tags, err := s.lib.TagSites(r.Context(), texts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": tags})
}
