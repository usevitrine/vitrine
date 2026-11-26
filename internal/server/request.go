package server

import (
	"errors"
	"net/http"
	"strconv"
)

// decode reads the request's JSON body into v (false: it wasn't, a 400 written).
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := readJSON(r, v); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return false
	}
	return true
}

// needDL reports whether the downloader runs (false: a 503 written).
func (s *Server) needDL(w http.ResponseWriter) bool {
	if s.dl == nil {
		writeError(w, http.StatusServiceUnavailable, errNoDownloader)
		return false
	}
	return true
}

// pathID reads the route's {id} (false: not a number, a 400 "bad <what> ID" written).
func pathID(w http.ResponseWriter, r *http.Request, what string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("bad "+what+" ID"))
		return 0, false
	}
	return id, true
}

// pathFileID is pathID for a file (IDs are 32-bit).
func pathFileID(w http.ResponseWriter, r *http.Request) (uint32, bool) {
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 32)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("bad file ID"))
		return 0, false
	}
	return uint32(id), true
}
