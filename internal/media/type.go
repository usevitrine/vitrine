// Package media knows the file types Vitrine stores and detects them from a
// file's first bytes.
package media

import (
	"bytes"
	"errors"
)

// Type is a file type. The numeric values are stored in the database and the
// search index, so they must never change; add new types at the end.
type Type uint8

const (
	Unknown Type = iota
	JPEG
	PNG
	GIF
	WebP
	AVIF
	MP4
	QuickTime
	WebM
	Matroska
)

var types = [...]struct{ name, mime, ext string }{
	Unknown:   {"unknown", "application/octet-stream", "bin"},
	JPEG:      {"jpeg", "image/jpeg", "jpg"},
	PNG:       {"png", "image/png", "png"},
	GIF:       {"gif", "image/gif", "gif"},
	WebP:      {"webp", "image/webp", "webp"},
	AVIF:      {"avif", "image/avif", "avif"},
	MP4:       {"mp4", "video/mp4", "mp4"},
	QuickTime: {"quicktime", "video/quicktime", "mov"},
	WebM:      {"webm", "video/webm", "webm"},
	Matroska:  {"matroska", "video/x-matroska", "mkv"},
}

func (t Type) info() struct{ name, mime, ext string } {
	if int(t) < len(types) {
		return types[t]
	}
	return types[Unknown]
}

// String is the short name, e.g. "jpeg".
func (t Type) String() string { return t.info().name }

// MIME is the media type served to browsers.
func (t Type) MIME() string { return t.info().mime }

// Ext is the file extension used in the store, without a dot.
func (t Type) Ext() string { return t.info().ext }

// IsVideo reports whether the type is a video container.
func (t Type) IsVideo() bool { return t >= MP4 && t <= Matroska }

// IsImage reports whether the type is an image format (animated or not).
func (t Type) IsImage() bool { return t >= JPEG && t <= AVIF }

// ErrUnsupported is returned for files Vitrine doesn't store.
var ErrUnsupported = errors.New("unsupported file type")

// SniffLen is how many leading bytes Detect needs at most.
const SniffLen = 64

// Detect identifies a file from its first bytes (up to SniffLen). It looks only
// at content, never at a file name.
func Detect(head []byte) (Type, error) {
	switch {
	case bytes.HasPrefix(head, []byte{0xFF, 0xD8, 0xFF}):
		return JPEG, nil
	case bytes.HasPrefix(head, []byte("\x89PNG\r\n\x1a\n")):
		return PNG, nil
	case bytes.HasPrefix(head, []byte("GIF87a")), bytes.HasPrefix(head, []byte("GIF89a")):
		return GIF, nil
	case len(head) >= 12 && bytes.Equal(head[:4], []byte("RIFF")) && bytes.Equal(head[8:12], []byte("WEBP")):
		return WebP, nil
	case bytes.HasPrefix(head, []byte{0x1A, 0x45, 0xDF, 0xA3}):
		// EBML header; its DocType says WebM or Matroska.
		if bytes.Contains(head, []byte("webm")) {
			return WebM, nil
		}
		return Matroska, nil
	case len(head) >= 12 && bytes.Equal(head[4:8], []byte("ftyp")):
		return detectISOBMFF(head)
	case len(head) >= 8 && isQuickTimeAtom(head[4:8]):
		// Old QuickTime files have no ftyp box.
		return QuickTime, nil
	}
	return Unknown, ErrUnsupported
}

// detectISOBMFF tells MP4, QuickTime and AVIF apart by the ftyp box's brands.
func detectISOBMFF(head []byte) (Type, error) {
	major := string(head[8:12])
	switch major {
	case "qt  ":
		return QuickTime, nil
	case "avif", "avis":
		return AVIF, nil
	case "heic", "heix", "heim", "heis", "hevc", "hevx":
		return Unknown, ErrUnsupported // HEIC: browsers can't show it
	case "mif1", "msf1":
		// Generic image brands: AVIF only if listed as compatible.
		if hasCompatibleBrand(head, "avif") || hasCompatibleBrand(head, "avis") {
			return AVIF, nil
		}
		return Unknown, ErrUnsupported
	}
	return MP4, nil
}

// hasCompatibleBrand looks through the ftyp box's compatible-brand list, as far
// as it fits in head.
func hasCompatibleBrand(head []byte, brand string) bool {
	size := int(head[0])<<24 | int(head[1])<<16 | int(head[2])<<8 | int(head[3])
	end := min(size, len(head))
	for i := 16; i+4 <= end; i += 4 {
		if string(head[i:i+4]) == brand {
			return true
		}
	}
	return false
}

func isQuickTimeAtom(a []byte) bool {
	switch string(a) {
	case "moov", "mdat", "wide", "free", "skip", "pnot":
		return true
	}
	return false
}
