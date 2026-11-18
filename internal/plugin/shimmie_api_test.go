package plugin

import (
	"context"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/media"
)

// shmAPIPosts is find_posts' reply as rule34.paheal.net sends it (2026-11-18; the tags' case as the site has them).
const shmAPIPosts = `<posts count='71826' offset='0'><post id='7514897' md5='faea468c830ebfdaedbcb89c69818893' file_name='a.jpg' file_url='https://cdn.example.test/fa/ea/faea468c830ebfdaedbcb89c69818893' height='1300' width='2000' preview_url='https://cdn.example.test/t/faea' preview_height='124' preview_width='192' rating='?' date='2026-11-18 00:53:59' tags='some_artist Blue_Sky caf&eacute;' source='https://example.test/some_artist/1' score='0' author='someone'></post><post id='7514822' md5='5a66db6eec9dba80629cb1508097edf2' file_name='b.mp4' file_url='/_images/5a66db6eec9dba80629cb1508097edf2/b.mp4' height='720' width='1280' rating='s' date='2026-11-18 00:18:22' tags='blue_sky' source='' score='0' author='someone'></post></posts>`

func TestShimmieAPI(t *testing.T) {
	var asked []string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/danbooru/find_posts" {
			t.Errorf("asked %s (the web pages, with api: danbooru)", r.URL)
			http.NotFound(w, r)
			return
		}
		asked = append(asked, r.URL.RawQuery)
		switch {
		case r.URL.Query().Get("id") == "7514897":
			io.WriteString(w, shmAPIPosts[:strings.Index(shmAPIPosts, "<post id='7514822'")]+"</posts>")
		case r.URL.Query().Get("id") != "":
			io.WriteString(w, `<posts count='0' offset='0'></posts>`)
		default:
			io.WriteString(w, shmAPIPosts)
		}
	}))
	t.Cleanup(srv.Close)
	m, err := Parse([]byte(`
id: shimmie_api_test
engine: shimmie2
api: danbooru
base: ` + srv.URL + `
post_url: ` + srv.URL + `/post/view/{id}
limits: {requests_per_sec: 100}
ratings: {safe: general, questionable: questionable, explicit: explicit, unrated: explicit}
`))
	if err != nil {
		t.Fatal(err)
	}
	e, _ := NewEngine(testClient(m, srv, nil))
	ctx := context.Background()

	page, err := e.Search(ctx, "blue_sky -cloud", 7514900)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 71826 || len(page.Posts) != 2 {
		t.Fatalf("total %d, posts %d", page.Total, len(page.Posts))
	}
	p := page.Posts[0]
	if p.ID != 7514897 || p.Partial || hex.EncodeToString(p.MD5[:]) != "faea468c830ebfdaedbcb89c69818893" ||
		strings.Join(p.Tags, " ") != "some_artist blue_sky café" || p.Rating != media.RatingExplicit || p.Width != 2000 ||
		p.FileURL != "https://cdn.example.test/fa/ea/faea468c830ebfdaedbcb89c69818893" ||
		strings.Join(p.Sources, " ") != "https://example.test/some_artist/1" ||
		!p.Posted.Equal(time.Date(2026, 11, 18, 0, 53, 59, 0, time.UTC)) || p.PostURL != srv.URL+"/post/view/7514897" {
		t.Fatalf("first post %+v", p)
	}
	if q := page.Posts[1]; q.Rating != media.RatingGeneral || q.FileURL != srv.URL+"/_images/5a66db6eec9dba80629cb1508097edf2/b.mp4" {
		t.Fatalf("second post %+v", q)
	}
	// The search as through the pages: resuming adds id<N, newest first.
	if want := "limit=100&tags=blue_sky+-cloud+id%3C7514900+order%3Did_desc"; asked[0] != want {
		t.Errorf("asked %s, want %s", asked[0], want)
	}

	got, err := e.Lookup(ctx, 7514897)
	if err != nil || got == nil || got.ID != 7514897 || len(got.Tags) != 3 {
		t.Fatalf("lookup: %+v %v", got, err)
	}
	if got, err := e.Lookup(ctx, 1); got != nil || err != nil {
		t.Fatalf("a missing post: %+v %v", got, err)
	}

	if _, err := Parse([]byte("id: x\nengine: danbooru\napi: danbooru\nbase: https://example.test\n")); err == nil {
		t.Fatal("api: danbooru passed on another engine")
	}
}
