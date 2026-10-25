package media

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"

	"github.com/usevitrine/vitrine/internal/proc"
)

// Preview frames for the video player's scrub bar: one WebP "sprite sheet"
// per video, frames in rows of SpriteCols, each at most SpriteTile px on its
// long side. A frame a second for short clips, at most SpriteMax frames for
// long videos. Clips up to a minute are fully decoded (each frame really is
// that second); longer ones use keyframes only, so even long videos take seconds. Bump SpriteVersion when the recipe changes.
const (
	SpriteTile        = 320 // the scrub frame fills the player: 160 was blurry there
	SpriteCols        = 10
	SpriteMax         = 120
	SpriteMinInterval = 1000 // ms: smooth steps while scrubbing a short clip
	SpriteQuality     = 70
	SpriteVersion     = 3 // 2: sharper (320 px, quality 70); 3: a frame a second (120 at most)
)

// Sprite is a video's preview frames.
type Sprite struct {
	Data         []byte // WebP
	Frames, Cols int
	TileW, TileH int
	IntervalMS   int64 // frame k shows the video at k*IntervalMS
}

// SpritePlan works out a sprite's layout for a video of this length and size.
func SpritePlan(durationMS int64, w, h int) Sprite {
	s := Sprite{Cols: SpriteCols}
	s.IntervalMS = max(SpriteMinInterval, (durationMS+SpriteMax-1)/SpriteMax)
	s.Frames = int(max(1, min(SpriteMax, (durationMS+s.IntervalMS-1)/s.IntervalMS)))
	s.Cols = min(SpriteCols, s.Frames)
	even := func(v int) int { return max(2, v/2*2) }
	if w <= 0 || h <= 0 {
		w, h = 16, 9
	}
	if w >= h {
		s.TileW, s.TileH = SpriteTile, even(SpriteTile*h/w)
	} else {
		s.TileW, s.TileH = even(SpriteTile*w/h), SpriteTile
	}
	return s
}

// MakeSprite makes a video's preview frames.
func (t Tools) MakeSprite(ctx context.Context, path string, info Info) (Sprite, error) {
	if info.DurationMS <= 0 {
		return Sprite{}, fmt.Errorf("no duration")
	}
	s := SpritePlan(info.DurationMS, info.Width, info.Height)
	rows := (s.Frames + s.Cols - 1) / s.Cols
	vf := fmt.Sprintf("fps=%s,scale=%d:%d:flags=bicubic,tile=%dx%d",
		strconv.FormatFloat(1000/float64(s.IntervalMS), 'f', 6, 64), s.TileW, s.TileH, s.Cols, rows)
	// Short clips: every frame decoded (keyframes are often seconds apart, and the
	// preview would repeat itself). Longer: keyframes only (fast); a video with
	// too few keyframes for that gives no image, so then every frame is decoded.
	full := info.DurationMS <= 60_000
	out, err := t.sprite(ctx, path, vf, !full)
	if err == nil && len(out) == 0 && !full {
		out, err = t.sprite(ctx, path, vf, false)
	}
	if err != nil {
		return s, err
	}
	if len(out) == 0 {
		return s, fmt.Errorf("ffmpeg made no preview frames")
	}
	s.Data = out
	return s, nil
}

func (t Tools) sprite(ctx context.Context, path, vf string, keyframesOnly bool) ([]byte, error) {
	// Half the cores: preview frames are background work (thousands after a
	// recipe change) and must leave room for downloads and the app.
	threads := strconv.Itoa(max(1, runtime.NumCPU()/2))
	args := []string{"-v", "error", "-hide_banner", "-nostdin", "-threads", threads, "-filter_threads", "1"}
	if keyframesOnly {
		args = append(args, "-skip_frame", "nokey")
	}
	args = append(args, "-i", path, "-an", "-sn", "-vf", vf, "-frames:v", "1",
		"-c:v", "libwebp", "-quality", strconv.Itoa(SpriteQuality), "-f", "image2pipe", "pipe:1")
	cmd := proc.Hide(exec.CommandContext(ctx, t.FFmpeg, args...))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg: %w: %s", err, firstLine(stderr.String()))
	}
	return out, nil
}
