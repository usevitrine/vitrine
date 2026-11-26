// Package locales holds Vitrine's language files: one JSON file per language,
// named by its language tag (en-US.json). en-US is the main file and has every
// text; the others list only what differs from it (anything missing falls back
// to en-US). The web app bundles these files when it's built, and the server
// embeds them for what it writes itself (push notifications). See
// CONTRIBUTING.md to add a language.
package locales

import "embed"

// FS holds the language files.
//
//go:embed *.json
var FS embed.FS
