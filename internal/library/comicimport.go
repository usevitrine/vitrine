package library

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/usevitrine/vitrine/internal/media"
)

// ImportComic adds a comic from a folder of pictures or a zip / cbz archive:
// the pictures in name order (page2 before page10) are its pages; other files
// are skipped. title "" = the folder's or archive's name. It returns the
// comic's ID and how many pages it got.
func (l *Library) ImportComic(ctx context.Context, src, title string) (int64, int, error) {
	st, err := os.Stat(src)
	if err != nil {
		return 0, 0, err
	}
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(filepath.Clean(src)), filepath.Ext(src))
		if st.IsDir() {
			title = filepath.Base(filepath.Clean(src))
		}
	}
	type page struct {
		name string
		open func() (io.ReadCloser, error)
	}
	var pages []page
	if st.IsDir() {
		err = filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(src, p)
			pages = append(pages, page{filepath.ToSlash(rel), func() (io.ReadCloser, error) { return os.Open(p) }})
			return nil
		})
		if err != nil {
			return 0, 0, err
		}
	} else {
		z, err := zip.OpenReader(src)
		if err != nil {
			return 0, 0, fmt.Errorf("library: %s: not a folder or a zip/cbz archive: %w", src, err)
		}
		defer z.Close()
		for _, f := range z.File {
			if !f.FileInfo().IsDir() {
				pages = append(pages, page{f.Name, f.Open})
			}
		}
	}
	slices.SortFunc(pages, func(a, b page) int { return naturalCompare(a.name, b.name) })

	id, _, err := l.AddComic(ctx, ComicInfo{Title: title})
	if err != nil {
		return 0, 0, err
	}
	n := 0
	for _, p := range pages {
		if err := ctx.Err(); err != nil {
			return id, n, err
		}
		if !pictureName(p.name) {
			continue
		}
		r, err := p.open()
		if err != nil {
			return id, n, err
		}
		_, err = l.AddComicPage(ctx, id, n, r)
		r.Close()
		switch {
		case errors.Is(err, media.ErrUnsupported):
			continue // (a picture type the library doesn't take)
		case err != nil:
			return id, n, err
		}
		n++
	}
	if n == 0 {
		l.DeleteComics(ctx, []int64{id})
		return 0, 0, fmt.Errorf("library: %w: no pictures in %s", ErrInvalid, src)
	}
	return id, n, nil
}

// pictureName says whether a file name looks like a picture (by its extension; the store checks the content).
func pictureName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".avif", ".jxl", ".bmp":
		return !strings.HasPrefix(filepath.Base(name), ".") // (not macOS ._ files)
	}
	return false
}

// naturalCompare orders names with numbers by value: "p2" before "p10".
func naturalCompare(a, b string) int {
	a, b = strings.ToLower(a), strings.ToLower(b)
	for a != "" && b != "" {
		ra, rb := rune(a[0]), rune(b[0])
		if unicode.IsDigit(ra) && unicode.IsDigit(rb) {
			na, restA := leadingNumber(a)
			nb, restB := leadingNumber(b)
			if na != nb {
				// longer (more significant digits, leading zeros stripped) is bigger
				ta, tb := strings.TrimLeft(na, "0"), strings.TrimLeft(nb, "0")
				if len(ta) != len(tb) {
					return len(ta) - len(tb)
				}
				if ta != tb {
					return strings.Compare(ta, tb)
				}
			}
			a, b = restA, restB
			continue
		}
		if ra != rb {
			return int(ra) - int(rb)
		}
		a, b = a[1:], b[1:]
	}
	return len(a) - len(b)
}

func leadingNumber(s string) (string, string) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return s[:i], s[i:]
}
