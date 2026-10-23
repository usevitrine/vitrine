package server

import (
	_ "embed"
	"io/fs"
	"net/http"
	"strings"
)

// The licenses: Vitrine's own (AGPL-3.0-or-later, with where its source is - the AGPL asks that people using it over
// a network can get it) and the notices of everything built into it - the Go standard library and modules
// (licenses/go.txt, web/scripts/go-notices.mjs) and the web app's bundled packages (the build's licenses.md).

// SourceURL is where Vitrine's source code is.
const SourceURL = "https://github.com/usevitrine/vitrine"

//go:embed licenses/go.txt
var goNotices string

// licenses: GET /licenses - plain text, open to anyone (no login: whoever reaches this Vitrine may read them).
func (s *Server) licenses(w http.ResponseWriter, r *http.Request) {
	var b strings.Builder
	b.WriteString("Vitrine")
	if s.info.Version != "" {
		b.WriteString(" " + s.info.Version)
	}
	b.WriteString("\nCopyright (C) 2026 The Vitrine authors\n\n")
	b.WriteString("This program is free software: you can redistribute it and/or modify it under the terms of the\n" +
		"GNU Affero General Public License as published by the Free Software Foundation, either version 3 of the\n" +
		"License, or (at your option) any later version. It is distributed in the hope that it will be useful, but\n" +
		"WITHOUT ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR A PARTICULAR\n" +
		"PURPOSE. See the license for more details: https://www.gnu.org/licenses/agpl-3.0.html\n\n")
	b.WriteString("Source code: " + SourceURL + "\n\n\n")
	if web, err := fs.ReadFile(webFiles, "web/app/licenses.md"); err == nil {
		b.WriteString("Third-party software in the web app\n\n")
		b.Write(web)
		b.WriteString("\n\n\n")
	}
	b.WriteString(goNotices)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write([]byte(b.String()))
}
