package media

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/image/webp"
)

// sample describes a test file made with ffmpeg's built-in test sources.
type sample struct {
	name     string
	typ      Type
	args     []string // ffmpeg arguments before the output file
	w, h     int
	audio    bool
	animated bool
}

var samples = []sample{
	{"clip.mp4", MP4, []string{"-f", "lavfi", "-i", "testsrc=size=320x240:rate=25:duration=2",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=2", "-c:v", "libx264", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-shortest"}, 320, 240, true, true},
	{"clip.webm", WebM, []string{"-f", "lavfi", "-i", "testsrc=size=256x144:rate=10:duration=2",
		"-c:v", "libvpx", "-b:v", "200k"}, 256, 144, false, true},
	{"wide.png", PNG, []string{"-f", "lavfi", "-i", "testsrc=size=1600x400", "-frames:v", "1"}, 1600, 400, false, false},
	{"photo.jpg", JPEG, []string{"-f", "lavfi", "-i", "testsrc=size=640x960", "-frames:v", "1"}, 640, 960, false, false},
	{"anim.gif", GIF, []string{"-f", "lavfi", "-i", "testsrc=size=160x120:rate=10:duration=1"}, 160, 120, false, true},
	{"still.gif", GIF, []string{"-f", "lavfi", "-i", "testsrc=size=100x80", "-frames:v", "1"}, 100, 80, false, false},
}

func testTools(t *testing.T) Tools {
	t.Helper()
	tools := FindTools("", "", "")
	if err := tools.Check(); err != nil {
		t.Skip(err)
	}
	return tools
}

// makeSamples writes the sample files once per test into a temp dir.
func makeSamples(t *testing.T, tools Tools) string {
	t.Helper()
	dir := t.TempDir()
	for _, s := range samples {
		args := append([]string{"-v", "error", "-y"}, s.args...)
		args = append(args, filepath.Join(dir, s.name))
		if out, err := exec.Command(tools.FFmpeg, args...).CombinedOutput(); err != nil {
			t.Fatalf("making %s: %v\n%s", s.name, err, out)
		}
	}
	return dir
}

func TestProbeAndThumbnail(t *testing.T) {
	tools := testTools(t)
	dir := makeSamples(t, tools)
	ctx := context.Background()

	variants := []Tools{tools}
	if tools.VipsThumbnail != "" {
		noVips := tools
		noVips.VipsThumbnail = ""
		variants = append(variants, noVips) // test the ffmpeg fallback too
	} else {
		t.Log("vipsthumbnail not found: testing the ffmpeg path only")
	}

	for _, s := range samples {
		path := filepath.Join(dir, s.name)
		head := make([]byte, SniffLen)
		f, _ := os.Open(path)
		n, _ := f.Read(head)
		f.Close()
		if typ, err := Detect(head[:n]); err != nil || typ != s.typ {
			t.Errorf("%s: detected %v, %v", s.name, typ, err)
		}

		info, err := tools.Probe(ctx, path, s.typ)
		if err != nil {
			t.Errorf("%s: probe: %v", s.name, err)
			continue
		}
		if info.Width != s.w || info.Height != s.h || info.HasAudio != s.audio || info.Animated() != s.animated {
			t.Errorf("%s: probed %+v (animated %v)", s.name, info, info.Animated())
		}
		if s.typ.IsVideo() && (info.DurationMS < 1800 || info.DurationMS > 2200) {
			t.Errorf("%s: duration %d ms", s.name, info.DurationMS)
		}
		if s.name == "anim.gif" && info.Frames != 10 {
			t.Errorf("%s: %d frames, want 10", s.name, info.Frames)
		}

		for _, tl := range variants {
			thumb, err := tl.Thumbnail(ctx, path, s.typ, info)
			if err != nil {
				t.Errorf("%s (vips=%v): thumbnail: %v", s.name, tl.VipsThumbnail != "", err)
				continue
			}
			cfg, err := webp.DecodeConfig(bytes.NewReader(thumb))
			tw, th := ThumbSize(s.w, s.h)
			if err != nil || cfg.Width != tw || cfg.Height != th {
				t.Errorf("%s (vips=%v): thumbnail %dx%d (%v), want %dx%d",
					s.name, tl.VipsThumbnail != "", cfg.Width, cfg.Height, err, tw, th)
			}
			ph, err := Placeholder(thumb)
			if err != nil || len(ph) != 48 {
				t.Errorf("%s: placeholder %d bytes, %v", s.name, len(ph), err)
			}
		}
	}
}

func TestProbeBadFile(t *testing.T) {
	tools := testTools(t)
	p := filepath.Join(t.TempDir(), "bad.jpg")
	os.WriteFile(p, []byte{0xFF, 0xD8, 0xFF, 0, 1, 2, 3}, 0o644)
	if _, err := tools.Probe(context.Background(), p, JPEG); err == nil {
		t.Error("probing a broken file succeeded")
	}
}

func TestThumbSize(t *testing.T) {
	cases := [][4]int{
		{1920, 1080, 711, 400},
		{1080, 1920, 400, 711},
		{400, 400, 400, 400},
		{200, 100, 200, 100},    // never enlarged
		{8000, 1000, 1000, 125}, // panorama: long side capped
		{0, 0, 400, 400},
	}
	for _, c := range cases {
		if w, h := ThumbSize(c[0], c[1]); w != c[2] || h != c[3] {
			t.Errorf("ThumbSize(%d, %d) = %d, %d; want %d, %d", c[0], c[1], w, h, c[2], c[3])
		}
	}
}

func TestParseProbeRotation(t *testing.T) {
	out := []byte(`{"streams":[
		{"codec_type":"video","codec_name":"jpeg","width":100,"height":100,"disposition":{"attached_pic":1}},
		{"codec_type":"video","codec_name":"h264","width":1920,"height":1080,"nb_frames":"300",
		 "side_data_list":[{"rotation":-90}]},
		{"codec_type":"audio","codec_name":"aac"}],
		"format":{"duration":"10.010000"}}`)
	info, err := parseProbe(out, MP4)
	if err != nil {
		t.Fatal(err)
	}
	want := Info{Width: 1080, Height: 1920, DurationMS: 10010, Frames: 300, HasAudio: true, Codec: "h264"}
	if info != want {
		t.Errorf("got %+v, want %+v", info, want)
	}
}
