package media

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestHWCandidates(t *testing.T) {
	if c := hwCandidates("off"); c != nil {
		t.Fatalf("off: %v", c)
	}
	if c := hwCandidates("cuda"); len(c) != 1 || c[0].Kind != "cuda" {
		t.Fatalf("cuda: %+v", c)
	}
	if c := hwCandidates("vaapi:/dev/dri/renderD129"); len(c) != 1 || c[0].Kind != "vaapi" || c[0].Device != "/dev/dri/renderD129" {
		t.Fatalf("a named render node: %+v", c)
	}
}

// A GPU that fails every file: each is made on the processor after all; the codec, then everything, is given up on.
func TestHWFallback(t *testing.T) {
	tools := testTools(t)
	clip := filepath.Join(t.TempDir(), "clip.mp4")
	if out, err := exec.Command(tools.FFmpeg, "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc=size=320x180:rate=25:duration=5",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", clip).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	info, err := tools.Probe(context.Background(), clip, MP4)
	if err != nil {
		t.Fatal(err)
	}
	broken := &HWAccel{Kind: "vaapi", Device: "/no/such/gpu", ffmpeg: tools.FFmpeg}
	broken.once.Do(func() {}) // (found already: no detection)
	tools.HW = broken
	var said []string
	HWLog = func(msg string) { said = append(said, msg) }
	defer func() { HWLog = nil }()
	for i := range hwCodecTries {
		s, err := tools.MakeSprite(context.Background(), clip, info)
		if err != nil || len(s.Data) == 0 || s.Took.GPU {
			t.Fatalf("try %d: %v (%d bytes, gpu %v)", i, err, len(s.Data), s.Took.GPU)
		}
	}
	if broken.use("h264") || len(said) != 1 {
		t.Fatalf("h264 still tried on the GPU (%v)", said)
	}
	if thumb, err := tools.Thumbnail(context.Background(), clip, MP4, info); err != nil || len(thumb) == 0 {
		t.Fatalf("thumbnail: %v", err)
	}
}

// A video thumbnail (one frame) on the GPU against the processor: wall and processor time of 5 each.
func TestHWThumbnailCost(t *testing.T) {
	tools := testTools(t)
	tools.UseHW("auto")
	name, _ := tools.HW.Detect(context.Background())
	if name == "" || testing.Short() {
		t.Skip("no GPU decoding here")
	}
	clip := filepath.Join(t.TempDir(), "clip.mp4")
	if out, err := exec.Command(tools.FFmpeg, "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc2=size=3840x2160:rate=30:duration=10",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", clip).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	info, _ := tools.Probe(context.Background(), clip, MP4)
	cpuTools := tools
	cpuTools.HW = nil
	for _, c := range []struct {
		name string
		t    Tools
	}{{name, tools}, {"processor", cpuTools}} {
		Usage.Take()
		for range 5 {
			if _, err := c.t.Thumbnail(context.Background(), clip, MP4, info); err != nil {
				t.Fatal(err)
			}
		}
		u := Usage.Take()["thumbnail"]
		t.Logf("4K thumbnail x5 on %s: %v wall, %v cpu (%d on the GPU)", c.name, u.Wall, u.CPU, u.OnGPU)
	}
}

// With a GPU here: preview frames and a thumbnail made on it, like the processor's.
func TestHWSprite(t *testing.T) {
	tools := testTools(t)
	tools.UseHW("auto")
	name, tried := tools.HW.Detect(context.Background())
	if name == "" {
		t.Skipf("no GPU decoding here: %v", tried)
	}
	clip := filepath.Join(t.TempDir(), "clip.mp4")
	if out, err := exec.Command(tools.FFmpeg, "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc2=size=1920x1080:rate=30:duration=20",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", clip).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	info, err := tools.Probe(context.Background(), clip, MP4)
	if err != nil {
		t.Fatal(err)
	}
	gpu, err := tools.MakeSprite(context.Background(), clip, info)
	if err != nil || !gpu.Took.GPU || len(gpu.Data) == 0 {
		t.Fatalf("on %s: %v (gpu %v, %d bytes)", name, err, gpu.Took.GPU, len(gpu.Data))
	}
	cpuTools := tools
	cpuTools.HW = nil
	cpu, err := cpuTools.MakeSprite(context.Background(), clip, info)
	if err != nil {
		t.Fatal(err)
	}
	if gpu.Frames != cpu.Frames || len(gpu.Data) < len(cpu.Data)/2 || len(gpu.Data) > len(cpu.Data)*2 {
		t.Fatalf("GPU %d frames %d bytes, processor %d frames %d bytes", gpu.Frames, len(gpu.Data), cpu.Frames, len(cpu.Data))
	}
	t.Logf("%s: %v wall %v cpu; processor: %v wall %v cpu", name, gpu.Took.Wall, gpu.Took.CPU, cpu.Took.Wall, cpu.Took.CPU)
	if thumb, err := tools.Thumbnail(context.Background(), clip, MP4, info); err != nil || len(thumb) == 0 {
		t.Fatalf("thumbnail on %s: %v", name, err)
	}
}
