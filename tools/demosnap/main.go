// Command demosnap exports the demo library (tools/demolib) for the website's demo: snapshot.json (internal/demo's
// Snapshot - what the index is built from and what the app shows of each file) and a media folder - each file's
// thumbnail (t<id>.webp, the library's own), pictures at screen size (<id>.jpg, long side 1600 px: the demo is static,
// it can't make screen-sized copies on request like the server) and the clips as they are (<id>.mp4).
//
//	go run ./tools/demosnap [-dir %LOCALAPPDATA%\VitrineDemo] [-out <dir>\snapshot]
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/draw"

	"github.com/usevitrine/vitrine/internal/db"
	"github.com/usevitrine/vitrine/internal/demo"
	"github.com/usevitrine/vitrine/internal/favicon"
	"github.com/usevitrine/vitrine/internal/index"
	"github.com/usevitrine/vitrine/internal/library"
	"github.com/usevitrine/vitrine/internal/media"
	"github.com/usevitrine/vitrine/internal/store"
	"github.com/usevitrine/vitrine/internal/thumbs"
)

const screen = 1600 // the pictures' long side

func main() {
	dir := flag.String("dir", filepath.Join(os.Getenv("LOCALAPPDATA"), "VitrineDemo"), "demolib's work `folder`")
	out := flag.String("out", "", "where to write (default: <dir>/snapshot)")
	flag.Parse()
	if *out == "" {
		*out = filepath.Join(*dir, "snapshot")
	}
	if err := run(context.Background(), filepath.Join(*dir, "library"), *out); err != nil {
		fmt.Fprintln(os.Stderr, "demosnap:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, libDir, out string) error {
	s, err := store.Open(filepath.Join(libDir, "files"))
	if err != nil {
		return err
	}
	d, err := db.Open(ctx, filepath.Join(libDir, "data", "meta.db"))
	if err != nil {
		return err
	}
	defer d.Close()
	th, err := thumbs.Open(ctx, filepath.Join(libDir, "data", "thumbs.db"))
	if err != nil {
		return err
	}
	defer th.Close()
	lib, err := library.Open(ctx, library.Deps{DB: d, Store: s, Thumbs: th, Tools: media.FindTools("", "", "")})
	if err != nil {
		return err
	}
	if err := os.RemoveAll(out); err != nil {
		return err
	}
	mediaDir := filepath.Join(out, "media")
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		return err
	}

	var snap demo.Snapshot
	q := func(query string, scan func(*sql.Rows) error) error {
		rows, err := d.R.QueryContext(ctx, query)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err := scan(rows); err != nil {
				return err
			}
		}
		return rows.Err()
	}
	if err := q("SELECT id, name FROM namespaces ORDER BY id", func(r *sql.Rows) error {
		var n demo.Namespace
		err := r.Scan(&n.ID, &n.Name)
		snap.Namespaces = append(snap.Namespaces, n)
		return err
	}); err != nil {
		return err
	}
	if err := q("SELECT id, namespace_id, name FROM tags ORDER BY id", func(r *sql.Rows) error {
		var t index.Tag
		err := r.Scan(&t.ID, &t.NamespaceID, &t.Name)
		snap.Tags = append(snap.Tags, t)
		return err
	}); err != nil {
		return err
	}
	if err := q("SELECT id, name FROM sites ORDER BY id", func(r *sql.Rows) error {
		var x demo.Site
		err := r.Scan(&x.ID, &x.Name)
		snap.Sites = append(snap.Sites, x)
		return err
	}); err != nil {
		return err
	}
	// The index's columns, as index.Load reads them (the library's files: not deleted, not only a comic page).
	byID := map[uint32]*demo.File{}
	var order []uint32
	if err := q(`SELECT id, size, type, coalesce(width, 0), coalesce(height, 0), coalesce(duration_ms, 0), coalesce(has_audio, 0),
		imported_at, modified_at, coalesce(site_rating, 0), coalesce(posted_at, 0), bulk
		FROM files WHERE deleted_at IS NULL AND flags & 64 = 0 ORDER BY id`, func(r *sql.Rows) error {
		var f index.File
		var typ, rating uint8
		if err := r.Scan(&f.ID, &f.Size, &typ, &f.Width, &f.Height, &f.DurationMS, &f.HasAudio, &f.ImportedAt, &f.ModifiedAt,
			&rating, &f.PostedAt, &f.Bulk); err != nil {
			return err
		}
		f.Type, f.Rating = media.Type(typ), media.Rating(rating)
		byID[f.ID] = &demo.File{Index: f}
		order = append(order, f.ID)
		return nil
	}); err != nil {
		return err
	}
	if err := q("SELECT DISTINCT file_id, tag_id FROM file_tags ORDER BY file_id, tag_id", func(r *sql.Rows) error {
		var f, t uint32
		if err := r.Scan(&f, &t); err != nil {
			return err
		}
		if x := byID[f]; x != nil {
			x.Tags = append(x.Tags, t)
		}
		return nil
	}); err != nil {
		return err
	}
	if err := q("SELECT DISTINCT file_id, site_id FROM file_urls ORDER BY file_id, site_id", func(r *sql.Rows) error {
		var f, site uint32
		if err := r.Scan(&f, &site); err != nil {
			return err
		}
		if x := byID[f]; x != nil {
			x.Sites = append(x.Sites, site)
		}
		return nil
	}); err != nil {
		return err
	}

	kinds := map[int]string{library.URLPost: "post", library.URLSource: "source", library.URLFile: "file"}
	var total int64
	for i, id := range order {
		f := byID[id]
		rows, err := lib.Files(ctx, []uint32{id})
		if err != nil || len(rows) == 0 {
			return fmt.Errorf("file %d: %v", id, err)
		}
		row := rows[0]
		// The media: a picture at screen size, a clip as it is; the thumbnail.
		name := fmt.Sprintf("%d.%s", id, row.Type.Ext())
		if row.Type.IsImage() && row.Type != media.GIF {
			name = fmt.Sprintf("%d.jpg", id)
			if err := screenCopy(s.Path(row.SHA256, row.Type), filepath.Join(mediaDir, name)); err != nil {
				return fmt.Errorf("file %d: %w", id, err)
			}
		} else if err := copyFile(s.Path(row.SHA256, row.Type), filepath.Join(mediaDir, name)); err != nil {
			return err
		}
		thumb := ""
		if t, err := lib.Thumb(ctx, id); err == nil && len(t.Data) > 0 {
			thumb = fmt.Sprintf("media/t%d.webp", id)
			if err := os.WriteFile(filepath.Join(out, thumb), t.Data, 0o644); err != nil {
				return err
			}
		}
		info := map[string]any{
			"id": id, "sha256": row.SHA256.String(), "type": row.Type.String(), "mime": row.Type.MIME(), "size": row.Size,
			"width": row.Width, "height": row.Height, "duration_ms": row.DurationMS, "has_audio": row.HasAudio,
			"imported_at": row.ImportedAt, "modified_at": row.ModifiedAt, "url": "media/" + name,
		}
		if row.PostedAt > 0 {
			info["posted_at"] = row.PostedAt
		}
		if len(row.Placeholder) > 0 {
			info["placeholder"] = row.Placeholder // ([]byte: base64, as the server sends it)
		}
		if thumb != "" {
			info["thumb"] = thumb
		}
		if f.Info, err = json.Marshal(info); err != nil {
			return err
		}
		// The details, as GET /api/files/{id}/tags gives them.
		tags, err := lib.FileTags(ctx, id)
		if err != nil {
			return err
		}
		type tagJSON struct {
			Tag     string   `json:"tag"`
			Origins []string `json:"origins"`
		}
		tj := make([]tagJSON, len(tags))
		for i, t := range tags {
			tj[i] = tagJSON{t.Tag, t.Origins}
		}
		links, err := lib.FileURLs(ctx, id)
		if err != nil {
			return err
		}
		type urlJSON struct {
			Kind string `json:"kind"`
			Site string `json:"site"`
			URL  string `json:"url"`
		}
		uj := make([]urlJSON, len(links))
		for i, u := range links {
			uj[i] = urlJSON{kinds[u.Kind], u.Site, u.URL}
		}
		sources, err := lib.FileSources(ctx, id)
		if err != nil {
			return err
		}
		if sources == nil {
			sources = []library.FileSource{}
		}
		if f.Details, err = json.Marshal(map[string]any{"tags": tj, "urls": uj, "sources": sources}); err != nil {
			return err
		}
		snap.Files = append(snap.Files, *f)
		if st, err := os.Stat(filepath.Join(mediaDir, name)); err == nil {
			total += st.Size()
		}
		if (i+1)%50 == 0 {
			fmt.Printf("  %d/%d\n", i+1, len(order))
		}
	}
	// The links' site icons (the details' badges), fetched once the way the server does (internal/favicon): media/favicon/<host>.
	hosts := map[string]bool{}
	for _, f := range snap.Files {
		var d struct {
			URLs []struct{ URL string } `json:"urls"`
		}
		json.Unmarshal(f.Details, &d)
		for _, u := range d.URLs {
			if p, err := url.Parse(u.URL); err == nil && p.Hostname() != "" {
				hosts[strings.ToLower(p.Hostname())] = true
			}
		}
	}
	icons, err := favicon.Open(filepath.Join(out, ".favicons"), "VitrineDemoBuilder/1.0 (https://github.com/usevitrine/vitrine)")
	if err != nil {
		return err
	}
	iconDir := filepath.Join(mediaDir, "favicon")
	if err := os.MkdirAll(iconDir, 0o755); err != nil {
		return err
	}
	snap.Icons = []string{}
	for h := range hosts {
		b, err := icons.Get(ctx, h)
		if err != nil {
			continue // (none: the app shows its link icon)
		}
		if err := os.WriteFile(filepath.Join(iconDir, h), b, 0o644); err != nil {
			return err
		}
		snap.Icons = append(snap.Icons, h)
	}
	os.RemoveAll(filepath.Join(out, ".favicons"))
	fmt.Printf("site icons: %v\n", snap.Icons)
	b, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, "snapshot.json"), b, 0o644); err != nil {
		return err
	}
	fmt.Printf("snapshot: %d files, %d tags, %.1f MB of media, %.0f KB of data in %s\n", len(snap.Files), len(snap.Tags),
		float64(total)/1e6, float64(len(b))/1e3, out)
	return nil
}

// screenCopy writes a picture with its long side at most screen px, as a JPEG.
func screenCopy(src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if long := max(w, h); long > screen {
		w, h = w*screen/long, h*screen/long
		dst2 := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.CatmullRom.Scale(dst2, dst2.Bounds(), img, b, draw.Src, nil)
		img = dst2
	}
	o, err := os.Create(dst)
	if err != nil {
		return err
	}
	if err := jpeg.Encode(o, img, &jpeg.Options{Quality: 82}); err != nil {
		o.Close()
		return err
	}
	return o.Close()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	o, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(o, in); err != nil {
		o.Close()
		return err
	}
	return o.Close()
}
