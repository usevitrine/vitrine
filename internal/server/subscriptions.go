package server

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/usevitrine/vitrine/internal/download"
	"github.com/usevitrine/vitrine/internal/plugin"
)

// Subscriptions are listed with the downloads (GET /api/downloads).

// subscriptionTags lists every subscription's site and search, and whether it's paused, without the rest: the app
// marks the tags they follow wherever tags show (asked again on every "downloads" event, so kept small).
func (s *Server) subscriptionTags(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		writeJSON(w, http.StatusOK, map[string]any{"subscriptions": []any{}})
		return
	}
	subs, err := s.dl.Subscriptions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	type sub struct {
		ID     int64  `json:"id"`
		Site   string `json:"site"`
		Query  string `json:"query"`
		Paused bool   `json:"paused,omitempty"`
	}
	out := make([]sub, 0, len(subs))
	for _, x := range subs {
		out = append(out, sub{x.ID, x.Site, x.Query, x.Paused})
	}
	writeJSON(w, http.StatusOK, map[string]any{"subscriptions": out})
}

// addSubscription subscribes to a search: {site, query, interval,
// initial_max}, or {search_id, interval} to turn a search into a
// subscription. interval is in seconds. Optional group: group_id, or
// group_name (a new group on the site; an existing one of that name is used).
func (s *Server) addSubscription(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		writeError(w, http.StatusServiceUnavailable, errNoDownloader)
		return
	}
	var req struct {
		Site       string `json:"site"`
		Query      string `json:"query"`
		SearchID   int64  `json:"search_id"`
		Interval   int64  `json:"interval"`
		InitialMax int    `json:"initial_max"`
		GroupID    int64  `json:"group_id"`
		GroupName  string `json:"group_name"`
		Favorite   bool   `json:"favorite"` // its files become favorites too
		Label      string `json:"label"`    // its name where the query says little (a gallery's title)
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.GroupName != "" && req.SearchID == 0 {
		g, err := s.dl.CreateGroup(r.Context(), req.Site, req.GroupName)
		if err != nil {
			downloadError(w, err)
			return
		}
		req.GroupID = g
	}
	every := time.Duration(req.Interval) * time.Second
	var id int64
	var err error
	if req.SearchID != 0 {
		id, err = s.dl.SubscribeSearch(r.Context(), req.SearchID, every)
	} else {
		id, err = s.dl.SubscribeTo(r.Context(), req.Site, req.Query, every, req.InitialMax, req.Favorite)
	}
	if err == nil && req.Label != "" {
		err = s.dl.LabelSubscription(r.Context(), id, req.Label)
	}
	if err != nil {
		downloadError(w, err)
		return
	}
	if req.GroupID != 0 && !plugin.IsFavorites(req.Query) { // (favorites have their own group)
		if err := s.dl.SetGroup(r.Context(), id, req.GroupID); err != nil {
			downloadError(w, err)
			return
		}
	}
	sub, err := s.dl.GetSubscription(r.Context(), id)
	if err != nil {
		downloadError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sub)
}

func subscriptionID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("bad subscription ID"))
		return 0, false
	}
	return id, true
}

// updateSubscription changes a subscription: {interval?, paused?, group_id?, check_depth?}
// (group_id 0 = no group; check_depth -1 = its group's, site's or the setting for all).
func (s *Server) updateSubscription(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		writeError(w, http.StatusServiceUnavailable, errNoDownloader)
		return
	}
	id, ok := subscriptionID(w, r)
	if !ok {
		return
	}
	var req struct {
		Interval *int64 `json:"interval"`
		Paused   *bool  `json:"paused"`
		GroupID  *int64 `json:"group_id"`
		Depth    *int   `json:"check_depth"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var c download.SubscriptionChange
	if req.Interval != nil {
		d := time.Duration(*req.Interval) * time.Second
		c.Interval = &d
	}
	c.Paused = req.Paused
	c.CheckDepth = req.Depth
	if err := s.dl.UpdateSubscription(r.Context(), id, c); err != nil {
		downloadError(w, err)
		return
	}
	if req.GroupID != nil {
		if err := s.dl.SetGroup(r.Context(), id, *req.GroupID); err != nil {
			downloadError(w, err)
			return
		}
	}
	sub, err := s.dl.GetSubscription(r.Context(), id)
	if err != nil {
		downloadError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sub)
}

// subscriptionAction: POST /api/subscriptions/{id}/{check|remove}. Removing
// keeps the files.
func (s *Server) subscriptionAction(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		writeError(w, http.StatusServiceUnavailable, errNoDownloader)
		return
	}
	id, ok := subscriptionID(w, r)
	if !ok {
		return
	}
	var err error
	switch r.PathValue("action") {
	case "check":
		err = s.dl.CheckNow(r.Context(), id)
	case "remove":
		err = s.dl.Unsubscribe(r.Context(), id)
	case "fav-gone-delete": // the files no longer in the site's favorites: deleted (undo window)
		var n int
		if n, err = s.dl.FavGoneDelete(r.Context(), id); err == nil {
			writeJSON(w, http.StatusOK, map[string]int{"deleted": n})
			return
		}
	case "fav-gone-keep": // ... or kept (not asked about again)
		err = s.dl.FavGoneKeep(r.Context(), id)
	default:
		writeError(w, http.StatusNotFound, errors.New("unknown action"))
		return
	}
	if err != nil {
		downloadError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// favGone lists a favorites subscription's files no longer in the site's
// favorites: GET /api/subscriptions/{id}/fav-gone -> {files: [id]}.
func (s *Server) favGone(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		writeError(w, http.StatusServiceUnavailable, errNoDownloader)
		return
	}
	id, ok := subscriptionID(w, r)
	if !ok {
		return
	}
	files, err := s.dl.FavGone(r.Context(), id)
	if err != nil {
		downloadError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

type subPic struct {
	ID    uint32 `json:"id"`
	Thumb string `json:"thumb"`
	W     int    `json:"w"`
	H     int    `json:"h"`
}

type subLook struct {
	Recent []subPic `json:"recent,omitempty"` // the newest files it downloaded
	Comic  int64    `json:"comic,omitempty"`  // a gallery's comic in the library
	Cover  *subPic  `json:"cover,omitempty"`  // ... its cover
	Pages  int      `json:"pages,omitempty"`  // ... its pages
	Fresh  int      `json:"fresh,omitempty"`  // files since ?since=
}

// subscriptionLooks: GET /api/subscriptions/looks?since=<Unix ms> -> {looks: {id: {recent, comic, cover, fresh}}} -
// what the Downloads page shows of each subscription besides its numbers (asked when its tab opens).
func (s *Server) subscriptionLooks(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		writeError(w, http.StatusServiceUnavailable, errNoDownloader)
		return
	}
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	looks, err := s.dl.SubscriptionLooks(r.Context(), since)
	if err != nil {
		downloadError(w, err)
		return
	}
	var ids []uint32
	for _, l := range looks {
		ids = append(ids, l.Recent...)
		if l.Cover != 0 {
			ids = append(ids, l.Cover)
		}
	}
	pics := map[uint32]subPic{}
	for start := 0; start < len(ids); start += 500 {
		rows, err := s.lib.Files(r.Context(), ids[start:min(start+500, len(ids))])
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		for _, row := range rows {
			if j := s.toJSON(row); j.Thumb != "" {
				pics[row.ID] = subPic{row.ID, j.Thumb, j.Width, j.Height}
			}
		}
	}
	out := map[int64]subLook{}
	for id, l := range looks {
		var sl subLook
		for _, f := range l.Recent {
			if p, ok := pics[f]; ok {
				sl.Recent = append(sl.Recent, p)
			}
		}
		if p, ok := pics[l.Cover]; ok && l.Cover != 0 {
			sl.Comic, sl.Cover = l.Comic, &p
		} else if l.Comic != 0 {
			sl.Comic = l.Comic
		}
		sl.Fresh, sl.Pages = l.Fresh, l.Pages
		out[id] = sl
	}
	writeJSON(w, http.StatusOK, map[string]any{"looks": out})
}
