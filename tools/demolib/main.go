// Command demolib builds the library behind the website's demo and screenshots (vitrine.software): public-domain
// artworks from the Art Institute of Chicago, short clips from the Blender open movies (CC-BY) and public-domain
// comics (Little Nemo in Slumberland, from Wikimedia Commons) - each with booru-style tags and its source page,
// stored the way a download stores them. Downloads are cached in the work folder, so a second run only rebuilds the
// library; "vitrine process -data-dir <dir>/library/data -files-dir <dir>/library/files" then makes the thumbnails.
//
//	go run ./tools/demolib [-dir %LOCALAPPDATA%\VitrineDemo] [-art 300] [-ffmpeg path]
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/usevitrine/vitrine/internal/db"
	"github.com/usevitrine/vitrine/internal/library"
	"github.com/usevitrine/vitrine/internal/media"
	"github.com/usevitrine/vitrine/internal/store"
	"github.com/usevitrine/vitrine/internal/tag"
	"github.com/usevitrine/vitrine/internal/thumbs"
)

const userAgent = "VitrineDemoBuilder/1.0 (https://github.com/usevitrine/vitrine; hello@vitrine.software)"

func main() {
	dir := flag.String("dir", filepath.Join(os.Getenv("LOCALAPPDATA"), "VitrineDemo"), "work `folder` (downloads, clips, the library)")
	artN := flag.Int("art", 300, "how many artworks")
	ffmpeg := flag.String("ffmpeg", "", "ffmpeg `path` (default: on PATH, or the Windows installer's)")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, *dir, *artN, findFFmpeg(*ffmpeg)); err != nil {
		fmt.Fprintln(os.Stderr, "demolib:", err)
		os.Exit(1)
	}
}

func findFFmpeg(p string) string {
	if p != "" {
		return p
	}
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		return p
	}
	return filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Vitrine", "tools", "ffmpeg", "ffmpeg.exe")
}

func run(ctx context.Context, dir string, artN int, ffmpeg string) error {
	cache := filepath.Join(dir, "cache")
	art, err := fetchArt(ctx, filepath.Join(cache, "art"), artN)
	if err != nil {
		return err
	}
	clips, err := makeClips(ctx, filepath.Join(cache, "films"), filepath.Join(cache, "clips"), ffmpeg)
	if err != nil {
		return err
	}
	comics, err := fetchComics(ctx, filepath.Join(cache, "comics"))
	if err != nil {
		return err
	}
	return build(ctx, filepath.Join(dir, "library"), art, clips, comics)
}

// ---- Downloads ----

var client = &http.Client{Timeout: 2 * time.Minute}

func get(ctx context.Context, u string, header ...string) ([]byte, error) {
	for try := 1; ; try++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", userAgent)
		for i := 0; i+1 < len(header); i += 2 {
			req.Header.Set(header[i], header[i+1])
		}
		resp, err := client.Do(req)
		if err == nil {
			b, rerr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if rerr == nil && resp.StatusCode == http.StatusOK {
				return b, nil
			}
			if rerr == nil {
				err = fmt.Errorf("%s: %s", u, resp.Status)
			} else {
				err = rerr
			}
			if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
				return nil, err
			}
		}
		if try == 4 || ctx.Err() != nil {
			return nil, err
		}
		time.Sleep(time.Duration(try) * 3 * time.Second)
	}
}

// download saves u at path (skipped when it's there already: the cache).
func download(ctx context.Context, u, path string, header ...string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := get(ctx, u, header...)
	if err != nil {
		return err
	}
	tmp := path + ".part"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// downloadBig streams a large file (a film) to path.
func downloadBig(ctx context.Context, u, path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", u, resp.Status)
	}
	tmp := path + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// booru makes a tag the way the sites write them: lower case, words joined by underscores, no brackets.
func booru(s string) string {
	s = regexp.MustCompile(`\s*\([^)]*\)`).ReplaceAllString(s, "")
	s = strings.ToLower(strings.TrimSpace(s))
	s = regexp.MustCompile(`[^\p{L}\p{N}]+`).ReplaceAllString(s, "_")
	return strings.Trim(s, "_")
}

// ---- Artworks: the Art Institute of Chicago (CC0 images, artic.edu/open-access) ----

type artwork struct {
	ID             int      `json:"id"`
	Title          string   `json:"title"`
	Artist         string   `json:"artist_title"`
	Date           string   `json:"date_display"`
	Classification string   `json:"classification_title"`
	Place          string   `json:"place_of_origin"`
	Styles         []string `json:"style_titles"`
	Subjects       []string `json:"subject_titles"`
	ImageID        string   `json:"image_id"`
	Thumb          struct {
		Width  int    `json:"width"`
		Height int    `json:"height"`
		Alt    string `json:"alt_text"`
	} `json:"thumbnail"`
	File string `json:"-"`
}

// A mix of what the collection holds, so the grid has every shape and color.
var artQueries = []struct {
	q string
	n int
}{
	{"impressionism landscape", 40}, {"japanese woodblock print", 50}, {"photograph", 30}, {"botanical", 20},
	{"portrait", 30}, {"still life", 25}, {"cat", 15}, {"dog", 10}, {"ship sea", 20}, {"city street", 20},
	{"poster", 15}, {"bird", 15}, {"mountain", 15}, {"textile pattern", 10},
}

// Left out after looking at every picture (their data doesn't say): nudes the text filter misses (photographs,
// sculpture, engravings), a shunga-style print, a propaganda poster with a slur in its title.
var excluded = map[int]bool{
	66269: true, 66275: true, 66278: true, 66394: true, 66489: true, 66702: true, 20579: true, 6727: true, 153: true,
	76077: true, 67308: true, 110507: true, 19021: true, 7945: true,
}

func fetchArt(ctx context.Context, dir string, want int) ([]artwork, error) {
	index := filepath.Join(dir, "index.json")
	var list []artwork
	if b, err := os.ReadFile(index); err == nil {
		if err := json.Unmarshal(b, &list); err != nil {
			return nil, err
		}
	} else {
		seen := map[int]bool{}
		for _, q := range artQueries {
			u := "https://api.artic.edu/api/v1/artworks/search?" + url.Values{
				"q":                             {q.q},
				"query[term][is_public_domain]": {"true"},
				"limit":                         {"100"},
				"fields":                        {"id,title,artist_title,date_display,classification_title,place_of_origin,style_titles,subject_titles,image_id,thumbnail"},
			}.Encode()
			b, err := get(ctx, u, "AIC-User-Agent", userAgent)
			if err != nil {
				return nil, err
			}
			var res struct{ Data []artwork }
			if err := json.Unmarshal(b, &res); err != nil {
				return nil, err
			}
			n := 0
			for _, a := range res.Data {
				if n == q.n {
					break
				}
				if seen[a.ID] || a.ImageID == "" || a.Thumb.Width < 600 || a.Thumb.Height < 600 {
					continue
				}
				seen[a.ID] = true
				list = append(list, a)
				n++
			}
			time.Sleep(time.Second) // (the API asks for no more than about one request a second)
		}
		b, _ := json.MarshalIndent(list, "", " ")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(index, b, 0o644); err != nil {
			return nil, err
		}
	}
	// The site is safe for work: no nudes, classical or not.
	nude := regexp.MustCompile(`(?i)\b(nudes?|nudity|naked|erotic|bathers?)\b`)
	list = slices.DeleteFunc(list, func(a artwork) bool {
		return nude.MatchString(a.Title) || slices.ContainsFunc(a.Subjects, nude.MatchString) || excluded[a.ID]
	})
	if len(list) > want {
		list = list[:want]
	}
	fmt.Printf("artworks: %d\n", len(list))
	for i := range list {
		a := &list[i]
		w := 843 // (the size the museum suggests; twice that for big originals)
		if a.Thumb.Width >= 1686 {
			w = 1686
		}
		a.File = filepath.Join(dir, fmt.Sprintf("%d.jpg", a.ID))
		if _, err := os.Stat(a.File); err == nil {
			continue
		}
		if err := download(ctx, fmt.Sprintf("https://www.artic.edu/iiif/2/%s/full/%d,/0/default.jpg", a.ImageID, w), a.File, "AIC-User-Agent", userAgent); err != nil {
			fmt.Printf("  skipped %d (%s): %v\n", a.ID, a.Title, err)
			a.File = ""
			continue
		}
		if i%25 == 0 {
			fmt.Printf("  %d/%d\n", i+1, len(list))
		}
		time.Sleep(700 * time.Millisecond)
	}
	return slices.DeleteFunc(list, func(a artwork) bool { return a.File == "" }), nil
}

func (a artwork) post() library.SitePost {
	tags := []string{}
	ns := map[string]string{}
	add := func(t, space string) {
		t = booru(t)
		if t == "" || len(t) > 40 || slices.Contains(tags, t) {
			return
		}
		tags = append(tags, t)
		if space != "" {
			ns[t] = space
		}
	}
	if a.Artist != "" {
		add(a.Artist, "creator")
	}
	add(a.Classification, "")
	add(a.Place, "")
	for _, s := range a.Styles {
		add(s, "")
	}
	for i, s := range a.Subjects {
		if i == 14 {
			break
		}
		add(s, "")
	}
	add("public_domain", "meta")
	return library.SitePost{Tags: tags, Namespaces: ns, PostURL: fmt.Sprintf("https://www.artic.edu/artworks/%d", a.ID)}
}

// ---- Clips: the Blender open movies (CC-BY, studio.blender.org) ----

type film struct {
	name, url, page string
	clips           int
	from, to        float64 // the part of it the clips come from (no titles or credits)
	tags            []string
}

var films = []film{
	{"big_buck_bunny", "https://download.blender.org/peach/trailer/trailer_720p.mov", "https://peach.blender.org/", 5, 0.15, 0.8,
		[]string{"bunny", "forest", "comedy", "cartoon"}},
	{"sintel", "https://download.blender.org/durian/trailer/sintel_trailer-720p.mp4", "https://durian.blender.org/", 5, 0.25, 0.8,
		[]string{"dragon", "fantasy", "snow", "adventure"}},
	{"elephants_dream", "https://download.blender.org/ED/elephantsdream-720-h264-st-aac.mov", "https://orange.blender.org/", 7, 0.22, 0.72,
		[]string{"surreal", "machines", "science_fiction"}},
	{"tears_of_steel", "https://download.blender.org/demo/movies/ToS/tears_of_steel_720p.mov", "https://mango.blender.org/", 7, 0.15, 0.75,
		[]string{"science_fiction", "robots", "visual_effects"}},
}

type clip struct {
	film film
	file string
}

func makeClips(ctx context.Context, filmDir, clipDir, ffmpeg string) ([]clip, error) {
	var out []clip
	for _, f := range films {
		var have []string
		for i := range f.clips {
			p := filepath.Join(clipDir, fmt.Sprintf("%s-%02d.mp4", f.name, i+1))
			if _, err := os.Stat(p); err == nil {
				have = append(have, p)
			}
		}
		if len(have) < f.clips {
			src := filepath.Join(filmDir, f.name+filepath.Ext(f.url))
			fmt.Printf("film: %s\n", f.name)
			if err := downloadBig(ctx, f.url, src); err != nil {
				return nil, err
			}
			dur, err := duration(ctx, ffmpeg, src)
			if err != nil {
				return nil, err
			}
			if err := os.MkdirAll(clipDir, 0o755); err != nil {
				return nil, err
			}
			have = have[:0]
			// Evenly through the film's middle (not the titles or the credits), 6 s each.
			for i := range f.clips {
				at := dur * (f.from + (f.to-f.from)*float64(i)/float64(max(1, f.clips-1)))
				p := filepath.Join(clipDir, fmt.Sprintf("%s-%02d.mp4", f.name, i+1))
				cmd := exec.CommandContext(ctx, ffmpeg, "-y", "-v", "error", "-ss", strconv.FormatFloat(at, 'f', 2, 64), "-i", src, "-t", "6",
					"-vf", "scale=1280:-2", "-c:v", "libx264", "-preset", "slow", "-crf", "26", "-pix_fmt", "yuv420p",
					"-c:a", "aac", "-b:a", "96k", "-movflags", "+faststart", p)
				if b, err := cmd.CombinedOutput(); err != nil {
					return nil, fmt.Errorf("ffmpeg: %v: %s", err, b)
				}
				have = append(have, p)
			}
			if err := os.Remove(src); err != nil { // (the clips are all that's kept)
				return nil, err
			}
		}
		for _, p := range have {
			out = append(out, clip{f, p})
		}
	}
	fmt.Printf("clips: %d\n", len(out))
	return out, nil
}

// duration reads a video's length (ffmpeg prints "Duration: 00:09:56.46").
func duration(ctx context.Context, ffmpeg, path string) (float64, error) {
	b, _ := exec.CommandContext(ctx, ffmpeg, "-i", path).CombinedOutput()
	m := regexp.MustCompile(`Duration: (\d+):(\d+):(\d+\.\d+)`).FindSubmatch(b)
	if m == nil {
		return 0, fmt.Errorf("no duration for %s", path)
	}
	h, _ := strconv.ParseFloat(string(m[1]), 64)
	mi, _ := strconv.ParseFloat(string(m[2]), 64)
	s, _ := strconv.ParseFloat(string(m[3]), 64)
	return h*3600 + mi*60 + s, nil
}

func (c clip) post() library.SitePost {
	tags := append([]string{c.film.name, "blender_studio", "blender", "3d_animation", "open_movie", "cc_by"}, c.film.tags...)
	return library.SitePost{
		Tags:       tags,
		Namespaces: map[string]string{c.film.name: "series", "blender_studio": "creator", "cc_by": "meta"},
		PostURL:    c.film.page,
		Sources:    []string{c.film.url},
	}
}

// ---- Comics: Winsor McCay's strips (public domain; Wikimedia Commons) ----

type comic struct {
	title, category, series, key string
	file                         *regexp.Regexp // its strips' file names (the date first; a colored one preferred)
	tags                         []string
	posted                       int64
	pages                        []string
}

// Little Nemo from 1905 only: from 1906 on it has a racist caricature (Impie) - not for the site.
var comicList = []comic{
	{title: "Little Nemo in Slumberland", category: "Category:Little_Nemo_in_Slumberland,_1905", series: "little_nemo_in_slumberland", key: "nemo-1905",
		file: regexp.MustCompile(`^File:Little Nemo (\d{4}-\d{2}-\d{2})\.jpg$`), tags: []string{"dreams", "fantasy"}},
	{title: "Little Sammy Sneeze", category: "Category:Little_Sammy_Sneeze,_1904", series: "little_sammy_sneeze", key: "sammy-1904",
		file: regexp.MustCompile(`^File:Little Sammy Sneeze (\d{4}-\d{2}-\d{2})( \(color\))?\.jpg$`), tags: []string{"sneeze", "gag_strip"}},
	{title: "Little Sammy Sneeze", category: "Category:Little_Sammy_Sneeze,_1905", series: "little_sammy_sneeze", key: "sammy-1905",
		file: regexp.MustCompile(`^File:Little Sammy Sneeze (\d{4}-\d{2}-\d{2})( \(color\))?\.jpg$`), tags: []string{"sneeze", "gag_strip"}},
}

func (c comic) tagList() []tag.Tag {
	t := []tag.Tag{{Namespace: "creator", Name: "winsor_mccay"}, {Namespace: "series", Name: c.series},
		{Name: "comic_strip"}, {Name: "newspaper"}, {Namespace: "meta", Name: "public_domain"}}
	for _, s := range c.tags {
		t = append(t, tag.Tag{Name: s})
	}
	return t
}

func fetchComics(ctx context.Context, dir string) ([]comic, error) {
	var out []comic
	for _, c := range comicList {
		u := "https://commons.wikimedia.org/w/api.php?" + url.Values{
			"action": {"query"}, "format": {"json"}, "generator": {"categorymembers"}, "gcmtitle": {c.category},
			"gcmtype": {"file"}, "gcmlimit": {"100"}, "prop": {"imageinfo"}, "iiprop": {"url|extmetadata"},
			"iiextmetadatafilter": {"LicenseShortName"}, "iiurlwidth": {"1600"},
		}.Encode()
		b, err := get(ctx, u)
		if err != nil {
			return nil, err
		}
		var res struct {
			Query struct {
				Pages map[string]struct {
					Title     string
					ImageInfo []struct {
						ThumbURL    string `json:"thumburl"`
						ExtMetadata map[string]struct{ Value string }
					}
				}
			}
		}
		if err := json.Unmarshal(b, &res); err != nil {
			return nil, err
		}
		type strip struct {
			date, url string
			color     bool
		}
		byDate := map[string]strip{}
		for _, p := range res.Query.Pages {
			m := c.file.FindStringSubmatch(p.Title)
			if m == nil || len(p.ImageInfo) == 0 || !strings.Contains(p.ImageInfo[0].ExtMetadata["LicenseShortName"].Value, "Public domain") {
				continue
			}
			color := len(m) > 2 && m[2] != ""
			if had, ok := byDate[m[1]]; ok && (had.color || !color) {
				continue
			}
			byDate[m[1]] = strip{m[1], p.ImageInfo[0].ThumbURL, color}
		}
		strips := make([]strip, 0, len(byDate))
		for _, s := range byDate {
			strips = append(strips, s)
		}
		sort.Slice(strips, func(i, j int) bool { return strips[i].date < strips[j].date })
		// A comic a month ("Little Nemo in Slumberland, November 1905"): more of them, of a few pages each, like a
		// real library's.
		var month *comic
		for _, s := range strips {
			t, _ := time.Parse("2006-01-02", s.date)
			if month == nil || !strings.HasSuffix(month.key, t.Format("2006-01")) {
				if month != nil {
					out = append(out, *month)
				}
				m := c
				m.title = c.title + ", " + t.Format("January 2006")
				m.key = c.key + "-" + t.Format("2006-01")
				m.posted = t.UnixMilli()
				m.pages = nil
				month = &m
			}
			p := filepath.Join(dir, c.key, s.date+".jpg")
			if err := download(ctx, s.url, p); err != nil {
				return nil, err
			}
			month.pages = append(month.pages, p)
			time.Sleep(300 * time.Millisecond)
		}
		if month != nil {
			out = append(out, *month)
		}
	}
	for _, c := range out {
		fmt.Printf("comic: %s, %d pages\n", c.title, len(c.pages))
	}
	return out, nil
}

// ---- The library ----

func build(ctx context.Context, dir string, art []artwork, clips []clip, comics []comic) error {
	if err := os.RemoveAll(dir); err != nil { // (always from scratch: the same library every run)
		return err
	}
	dataDir, filesDir := filepath.Join(dir, "data"), filepath.Join(dir, "files")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	s, err := store.Open(filesDir)
	if err != nil {
		return err
	}
	d, err := db.Open(ctx, filepath.Join(dataDir, "meta.db"))
	if err != nil {
		return err
	}
	defer d.Close()
	th, err := thumbs.Open(ctx, filepath.Join(dataDir, "thumbs.db"))
	if err != nil {
		return err
	}
	defer th.Close()
	lib, err := library.Open(ctx, library.Deps{DB: d, Store: s, Thumbs: th, Tools: media.FindTools("", "", "")})
	if err != nil {
		return err
	}
	if err := lib.EnsureNamespaces(ctx, []string{"creator", "character", "series", "meta"}); err != nil {
		return err
	}
	add := func(site, path string, p library.SitePost) error {
		f, err := lib.ImportFile(ctx, path)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		id, err := lib.Site(ctx, site)
		if err != nil {
			return err
		}
		return lib.SetSitePost(ctx, f.ID, id, p)
	}
	// Interleaved, so the newest-first grid mixes paintings, prints, photographs and clips; in reverse, so it opens on
	// the first searches' (impressionist landscapes, woodblock prints) - the screenshots' and the demo's first rows.
	slices.Reverse(art)
	ci := 0
	for i, a := range art {
		if err := add("artic", a.File, a.post()); err != nil {
			return err
		}
		if (i+1)%12 == 0 && ci < len(clips) {
			if err := add("blender", clips[ci].file, clips[ci].post()); err != nil {
				return err
			}
			ci++
		}
	}
	for ; ci < len(clips); ci++ {
		if err := add("blender", clips[ci].file, clips[ci].post()); err != nil {
			return err
		}
	}
	for _, c := range comics {
		id, _, err := lib.AddComic(ctx, library.ComicInfo{
			Title: c.title, Site: "commons", Gallery: c.key, URL: "https://commons.wikimedia.org/wiki/" + c.category,
			Category: "Comic strip", Pages: len(c.pages), PostedAt: c.posted,
			Tags: c.tagList(),
		})
		if err != nil {
			return err
		}
		for i, p := range c.pages {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			if _, err := lib.AddComicPage(ctx, id, i, bytes.NewReader(b)); err != nil && !errors.Is(err, library.ErrDeleted) {
				return fmt.Errorf("%s page %d: %w", c.title, i+1, err)
			}
		}
	}
	fmt.Printf("library: %d artworks, %d clips, %d comics in %s\n", len(art), len(clips), len(comics), dir)
	return nil
}
