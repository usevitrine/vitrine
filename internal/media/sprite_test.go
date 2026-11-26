package media

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSpritePlan(t *testing.T) {
	for _, c := range []struct {
		dur          int64
		w, h         int
		frames, cols int
		interval     int64
		tw, th       int
	}{
		{25_000, 240, 320, 25, 10, 1000, 240, 320}, // short portrait clip: every second
		{3_000, 1920, 1080, 3, 3, 1000, 320, 180},
		{3_600_000, 1280, 720, 120, 10, 30_000, 320, 180}, // an hour: 120 frames
		{500, 100, 100, 1, 1, 1000, 320, 320},
	} {
		s := SpritePlan(c.dur, c.w, c.h)
		if s.Frames != c.frames || s.Cols != c.cols || s.IntervalMS != c.interval || s.TileW != c.tw || s.TileH != c.th {
			t.Errorf("plan(%d, %dx%d) = %+v", c.dur, c.w, c.h, s)
		}
	}
}

func TestMakeSprite(t *testing.T) {
	tools := testTools(t)
	dir := t.TempDir()
	clip := filepath.Join(dir, "portrait.mp4")
	if out, err := exec.Command(tools.FFmpeg, "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc=size=240x320:rate=25:duration=25",
		"-c:v", "libx264", "-g", "25", "-pix_fmt", "yuv420p", clip).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	info, err := tools.Probe(context.Background(), clip, MP4)
	if err != nil {
		t.Fatal(err)
	}
	s, err := tools.MakeSprite(context.Background(), clip, info)
	if err != nil {
		t.Fatal(err)
	}
	if s.Frames != 25 || len(s.Data) == 0 {
		t.Fatalf("sprite: %+v (%d bytes)", s, len(s.Data))
	}
	out := filepath.Join(dir, "sprite.webp")
	os.WriteFile(out, s.Data, 0o644)
	dims, err := exec.Command(tools.FFprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height",
		"-of", "csv=p=0", out).Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(dims)); got != "2400,960" { // 10 x 240 wide, 3 rows of 320
		t.Fatalf("sprite size %q, want 2400,960", got)
	}
	t.Logf("sprite: %d frames, %d bytes", s.Frames, len(s.Data))
}

func TestMakeSpriteFewKeyframes(t *testing.T) {
	tools := testTools(t)
	clip := filepath.Join(t.TempDir(), "onekey.mp4")
	// Longer than a minute (keyframes only), one keyframe for the whole clip: that
	// pass gives nothing, so every frame is decoded.
	if out, err := exec.Command(tools.FFmpeg, "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc=size=180x320:rate=25:duration=70",
		"-c:v", "libx264", "-g", "1000", "-pix_fmt", "yuv420p", clip).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	info, _ := tools.Probe(context.Background(), clip, MP4)
	s, err := tools.MakeSprite(context.Background(), clip, info)
	if err != nil || s.Frames != 70 || len(s.Data) == 0 {
		t.Fatalf("sprite: %+v (%d bytes) %v", s, len(s.Data), err)
	}
}
