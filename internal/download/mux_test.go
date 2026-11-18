package download

import (
	"context"
	"crypto/md5"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/db"
	"github.com/usevitrine/vitrine/internal/library"
	"github.com/usevitrine/vitrine/internal/media"
	"github.com/usevitrine/vitrine/internal/plugin"
	"github.com/usevitrine/vitrine/internal/proc"
	"github.com/usevitrine/vitrine/internal/store"
)

// A video whose sound is a file of its own: joined into one file (and with no sound file, the video as it is).
func TestFetchMuxed(t *testing.T) {
	tools := media.FindTools("", "", "")
	if tools.FFmpeg == "" || tools.FFprobe == "" {
		t.Skip("no ffmpeg")
	}
	dir := t.TempDir()
	video, audio := filepath.Join(dir, "v.mp4"), filepath.Join(dir, "a.m4a")
	for _, args := range [][]string{
		{"-f", "lavfi", "-i", "testsrc=size=64x48:rate=10:duration=1", "-c:v", "libx264", "-pix_fmt", "yuv420p", video},
		{"-f", "lavfi", "-i", "sine=frequency=440:duration=1", "-c:a", "aac", audio},
	} {
		if out, err := proc.Hide(exec.Command(tools.FFmpeg, append([]string{"-hide_banner", "-loglevel", "error", "-y"}, args...)...)).CombinedOutput(); err != nil {
			t.Skipf("ffmpeg can't make the test files: %v %s", err, out)
		}
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v/DASH_720.mp4", "/silent/DASH_720.mp4":
			w.Header().Set("Content-Type", "video/mp4")
			http.ServeFile(w, r, video)
		case "/v/DASH_AUDIO_64.mp4":
			w.Header().Set("Content-Type", "audio/mp4")
			http.ServeFile(w, r, audio)
		default:
			http.NotFound(w, r) // (DASH_AUDIO_128.mp4 isn't there: the next name is tried)
		}
	}))
	t.Cleanup(srv.Close)

	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(dir, "meta.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	s, _ := store.Open(filepath.Join(dir, "files"))
	lib, err := library.Open(ctx, library.Deps{DB: d, Store: s, Tools: tools})
	if err != nil {
		t.Fatal(err)
	}
	m, err := plugin.Parse([]byte("id: fake\nengine: gelbooru-v02\nbase: " + srv.URL + "\nlimits: {requests_per_sec: 1000}\n"))
	if err != nil {
		t.Fatal(err)
	}
	dl, err := New(ctx, lib, d, map[string]plugin.Manifest{"fake": m}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	st := dl.Site("fake")
	st.Client.SetHTTPClient(srv.Client())
	plain, _ := os.ReadFile(video)

	f, _, err := dl.fetchMuxed(ctx, st, srv.URL+"/v/DASH_720.mp4", []string{srv.URL + "/v/DASH_AUDIO_128.mp4", srv.URL + "/v/DASH_AUDIO_64.mp4"}, 0, true, time.Time{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !f.New || f.Size <= int64(len(plain)) || f.MD5 == md5.Sum(plain) {
		t.Fatalf("joined: %+v (the video alone is %d bytes)", f, len(plain))
	}
	streams, err := proc.Hide(exec.Command(tools.FFprobe, "-v", "error", "-show_entries", "stream=codec_type", "-of", "csv=p=0",
		s.Path(f.SHA256, f.Type))).Output()
	if err != nil || strings.Join(strings.Fields(string(streams)), " ") != "video audio" {
		t.Fatalf("its streams: %q %v", streams, err)
	}

	f, _, err = dl.fetchMuxed(ctx, st, srv.URL+"/silent/DASH_720.mp4", []string{srv.URL + "/silent/DASH_AUDIO_128.mp4"}, 0, true, time.Time{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if f.MD5 != md5.Sum(plain) {
		t.Fatalf("without sound: %+v, want the video as it came", f)
	}
	if left, _ := os.ReadDir(filepath.Join(s.Root(), "tmp")); len(left) != 0 {
		t.Fatalf("left in tmp: %v", left)
	}
}
