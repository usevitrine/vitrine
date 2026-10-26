// Package demo is the website's demo (vitrine.software/demo): the web app in the browser with a library made by
// tools/demolib. Snapshot is that library as data - written by tools/demosnap from its meta.db, read by the WebAssembly
// module (cmd/demowasm) that answers the app's searches with this program's own index and query parser, and by the
// web app's demo backend (web/src/demo) for what the details show.
package demo

import (
	"encoding/json"

	"github.com/usevitrine/vitrine/internal/index"
)

// Snapshot is a library: what the index is built from, and what the app shows of each file.
type Snapshot struct {
	Namespaces []Namespace `json:"namespaces"`
	Tags       []index.Tag `json:"tags"`
	Sites      []Site      `json:"sites"`
	Files      []File      `json:"files"`
	// Icons: the hosts whose site icon is in media/favicon/<host> (the details' link badges).
	Icons []string `json:"icons"`
}

type Namespace struct {
	ID   uint32 `json:"id"`
	Name string `json:"name"`
}

type Site struct {
	ID   uint32 `json:"id"`
	Name string `json:"name"`
}

// File is one file: its index columns, tags and sites, and the app's views of it.
type File struct {
	Index index.File `json:"index"`
	Tags  []uint32   `json:"tags"`  // tag IDs (its own: site tags and the user's)
	Sites []uint32   `json:"sites"` // site IDs it came from
	// Info is the file as GET /api/files gives it, with its url and thumb relative to the demo's media folder.
	Info json.RawMessage `json:"info"`
	// Details is GET /api/files/{id}/tags: {tags: [{tag, origins}], urls: [{kind, site, url}], sources: [...]}.
	Details json.RawMessage `json:"details"`
}
