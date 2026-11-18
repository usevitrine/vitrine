package download

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/usevitrine/vitrine/internal/library"
	"github.com/usevitrine/vitrine/internal/plugin"
	"github.com/usevitrine/vitrine/internal/proc"
)

// A video whose sound is a file of its own (Reddit's v.redd.it: the picture in DASH_720.mp4, the sound in
// DASH_AUDIO_128.mp4 next to it - plugin.Post.AudioURLs, 2026-11-18): both are downloaded into the store's tmp folder
// and joined by ffmpeg without re-encoding (-c copy) into one mp4, which goes into the library. The first sound file
// the site has is used (403/404: try the next name); none - a clip without sound - or no ffmpeg: the video alone.

// fetchMuxed is fetchFile for a video with its sound apart.
func (d *Downloader) fetchMuxed(ctx context.Context, st *Site, video string, audios []string, maxBytes int64, bulk bool,
	posted time.Time, postID int64) (library.File, time.Time, error) {
	ffmpeg := d.lib.Tools().FFmpeg
	if ffmpeg == "" {
		return d.fetchFile(ctx, st, video, [16]byte{}, maxBytes, bulk, posted, postID)
	}
	dir := d.lib.TempDir()
	vpath, modified, err := d.fetchTemp(ctx, st, dir, video, maxBytes, true)
	if err != nil {
		return library.File{}, modified, err
	}
	defer os.Remove(vpath)
	var apath string
	for _, a := range audios {
		p, _, err := d.fetchTemp(ctx, st, dir, a, 0, false)
		var se *plugin.StatusError
		if errors.As(err, &se) && (se.Code == 403 || se.Code == 404) || errors.Is(err, plugin.ErrNotAFile) {
			continue // (not under this name)
		}
		if err != nil {
			return library.File{}, modified, err
		}
		apath = p
		defer os.Remove(p)
		break
	}
	src := vpath
	if apath != "" {
		out, err := os.CreateTemp(dir, "mux-*.mp4")
		if err != nil {
			return library.File{}, modified, err
		}
		out.Close()
		defer os.Remove(out.Name())
		cmd := proc.Hide(exec.CommandContext(ctx, ffmpeg, "-nostdin", "-hide_banner", "-loglevel", "error", "-y",
			"-i", vpath, "-i", apath, "-map", "0:v:0", "-map", "1:a:0", "-c", "copy", "-movflags", "+faststart", "-f", "mp4", out.Name()))
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			if ctx.Err() != nil {
				return library.File{}, modified, ctx.Err()
			}
			// (kept as the video alone rather than failed: the picture is what the post is)
			d.log.Warn("joining a video and its sound failed: the video alone", "site", st.Client.Manifest().ID, "post", postID,
				"err", strings.TrimSpace(err.Error()+" "+lastLine(stderr.String())))
		} else {
			src = out.Name()
		}
	}
	f, err := os.Open(src)
	if err != nil {
		return library.File{}, modified, err
	}
	defer f.Close()
	var at int64
	if p := refinePosted(posted, modified); !p.IsZero() {
		at = p.UnixMilli()
	}
	file, err := d.lib.ImportPosted(ctx, f, bulk, at, postID, nil)
	return file, modified, err
}

// fetchTemp downloads a file into dir (tried again after short waits when the failure may pass, as fetchFile) and
// returns its path. counted: it shows as the post's progress.
func (d *Downloader) fetchTemp(ctx context.Context, st *Site, dir, u string, maxBytes int64, counted bool) (string, time.Time, error) {
	for attempt := 0; ; attempt++ {
		var modified time.Time
		body, size, err := st.Client.File(ctx, u)
		if err == nil && maxBytes > 0 && size > maxBytes {
			body.Close()
			return "", modified, errTooBig
		}
		var name string
		if err == nil {
			if m, ok := body.(interface{ Modified() time.Time }); ok {
				modified = m.Modified()
			}
			if counted {
				body = d.countBody(ctx, body, size)
			}
			name, err = saveTemp(dir, body)
			body.Close()
		}
		d.log.Debug("file", "site", st.Client.Manifest().ID, "url", u, "attempt", attempt+1, "bytes", size, "err", err)
		if err == nil {
			return name, modified, nil
		}
		if ctx.Err() != nil || !mayPass(err) || attempt >= len(d.fileWaits) {
			return "", modified, err
		}
		select {
		case <-time.After(d.fileWaits[attempt]):
		case <-ctx.Done():
			return "", modified, ctx.Err()
		}
	}
}

func saveTemp(dir string, r io.Reader) (string, error) {
	f, err := os.CreateTemp(dir, "part-*")
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// lastLine is the last non-empty line of a program's output (ffmpeg's error).
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
