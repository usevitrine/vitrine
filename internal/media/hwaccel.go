package media

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/usevitrine/vitrine/internal/proc"
)

// Hardware video decoding (2026-10-30, the user: "use hardware acceleration whenever it can be used" - "a LOT of load
// on the cpu on large libraries, especially while importing"). Measured on an Intel iGPU (VA-API) with 13 videos of a
// real library: preview frames in half the time, a tenth of the processor time.
//
// At start-up each candidate decodes a tiny H.264 clip (hwtest.mp4) with its frames kept on the GPU and then copied
// back - a decoder that quietly fell back to the processor fails that, so only real hardware decoding passes. The
// first that works is used. A file it fails on is made again on the processor; a codec that fails a few times in a row
// (one the chip can't decode, e.g. AV1 on an older one) stays on the processor until the next start, and so does
// everything after many failures in a row.

//go:embed hwtest.mp4
var hwTestClip []byte

// HWLog receives the messages about giving up on the GPU for a codec or for good (the server logs them).
var HWLog func(msg string)

func hwLog(msg string) {
	if HWLog != nil {
		HWLog(msg)
	}
}

// HWAccel is the hardware decoder ffmpeg uses: found on first use (Detect) from the setting, shared by every copy of
// Tools (a pointer); safe for concurrent use. A nil one is the processor only.
type HWAccel struct {
	Kind   string // ffmpeg's -hwaccel: vaapi, cuda, d3d11va, videotoolbox ("" none found)
	Device string // vaapi: the render node (/dev/dri/renderD128)

	want   string // the setting
	ffmpeg string
	once   sync.Once
	tried  []string // what was tried, and why each failed

	mu        sync.Mutex
	streak    int             // failures in a row
	codecFail map[string]int  // failures in a row by codec
	codecOff  map[string]bool // codecs left to the processor
	off       bool            // too many failures: the processor only
}

// UseHW sets up hardware decoding as the setting says ("auto", "off", "vaapi", "vaapi:/dev/dri/renderD129", "cuda",
// ...); the GPU is looked for the first time a video needs it, or at Detect.
func (t *Tools) UseHW(want string) {
	t.HW = &HWAccel{want: want, ffmpeg: t.FFmpeg}
}

// Detect looks for the GPU now (once) and says what it found: the decoder in use ("" none) and what was tried.
func (h *HWAccel) Detect(ctx context.Context) (string, []string) {
	if h == nil {
		return "", nil
	}
	h.once.Do(func() { h.detect(ctx) })
	return h.Name(), h.tried
}

// Name is how it's shown (logs, Settings > Stats): "VA-API (/dev/dri/renderD128)"; "" none.
func (h *HWAccel) Name() string {
	if h == nil || h.Kind == "" {
		return ""
	}
	n := map[string]string{"vaapi": "VA-API", "cuda": "CUDA (Nvidia)", "d3d11va": "Direct3D 11", "videotoolbox": "VideoToolbox"}[h.Kind]
	if n == "" {
		n = h.Kind
	}
	if h.Device != "" {
		n += " (" + h.Device + ")"
	}
	return n
}

// use reports whether a video with this codec ("" unknown) should be tried on the GPU.
func (h *HWAccel) use(codec string) bool {
	if h == nil {
		return false
	}
	h.once.Do(func() { h.detect(context.Background()) })
	if h.Kind == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return !h.off && !h.codecOff[codec]
}

const (
	hwCodecTries = 3  // failures in a row before a codec stays on the processor
	hwTries      = 20 // ... before everything does
)

// done records how a GPU attempt went; it returns a message the first time a codec or the GPU is given up on.
func (h *HWAccel) done(codec string, ok bool) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ok {
		h.streak = 0
		delete(h.codecFail, codec)
		return ""
	}
	h.streak++
	if h.codecFail == nil {
		h.codecFail, h.codecOff = map[string]int{}, map[string]bool{}
	}
	h.codecFail[codec]++
	switch {
	case h.streak >= hwTries && !h.off:
		h.off = true
		return fmt.Sprintf("%s failed %d times in a row: videos are decoded by the processor until the next start", h.Name(), h.streak)
	case codec != "" && h.codecFail[codec] >= hwCodecTries && !h.codecOff[codec]:
		h.codecOff[codec] = true
		return fmt.Sprintf("%s can't decode %s here: those videos are decoded by the processor", h.Name(), codec)
	}
	return ""
}

// input is ffmpeg's arguments before -i: decode on the GPU, frames kept there.
func (h *HWAccel) input() []string {
	a := []string{"-hwaccel", h.Kind}
	if h.Device != "" {
		a = append(a, "-hwaccel_device", h.Device)
	}
	out := map[string]string{"vaapi": "vaapi", "cuda": "cuda", "d3d11va": "d3d11", "videotoolbox": "videotoolbox_vld"}[h.Kind]
	if out != "" {
		a = append(a, "-hwaccel_output_format", out)
	}
	return a
}

// scale is the filter that brings frames back from the GPU at w x h (8-bit, for the WebP encoder). VA-API scales on
// the GPU first (measured: less to copy); the others copy the (few) frames kept and scale them on the processor.
func (h *HWAccel) scale(w, ht int, flags string) string {
	if h.Kind == "vaapi" {
		return fmt.Sprintf("scale_vaapi=w=%d:h=%d:format=nv12,hwdownload,format=nv12", w, ht)
	}
	return fmt.Sprintf("hwdownload,format=pix_fmts=nv12|p010le,scale=%d:%d:flags=%s", w, ht, flags)
}

// hwCandidates are the decoders to try on this system, best first.
func hwCandidates(want string) []*HWAccel {
	kind, dev, _ := strings.Cut(strings.TrimSpace(want), ":") // (a device path keeps its case)
	kind = strings.ToLower(kind)
	if kind == "off" || kind == "none" || kind == "cpu" {
		return nil
	}
	if kind != "" && kind != "auto" {
		if kind == "vaapi" && dev == "" {
			return vaapiNodes()
		}
		return []*HWAccel{{Kind: kind, Device: dev}}
	}
	switch runtime.GOOS {
	case "linux":
		return append(vaapiNodes(), &HWAccel{Kind: "cuda"})
	case "windows":
		return []*HWAccel{{Kind: "d3d11va"}, {Kind: "cuda"}}
	case "darwin":
		return []*HWAccel{{Kind: "videotoolbox"}}
	}
	return nil
}

// vaapiNodes are the GPUs' render nodes (Intel and AMD - and Nvidia's, which VA-API can't use: CUDA follows).
func vaapiNodes() []*HWAccel {
	nodes, _ := filepath.Glob("/dev/dri/renderD*")
	sort.Strings(nodes)
	var out []*HWAccel
	for _, n := range nodes {
		out = append(out, &HWAccel{Kind: "vaapi", Device: n})
	}
	return out
}

// detect tries the candidates for the setting in order and keeps the first that decodes the test clip on the GPU.
// Takes a moment per candidate (seconds at most).
func (h *HWAccel) detect(ctx context.Context) {
	if h.ffmpeg == "" {
		return
	}
	cands := hwCandidates(h.want)
	if len(cands) == 0 {
		return
	}
	dir, err := os.MkdirTemp("", "vitrine-hw-")
	if err != nil {
		h.tried = []string{err.Error()}
		return
	}
	defer os.RemoveAll(dir)
	clip := filepath.Join(dir, "test.mp4")
	if err := os.WriteFile(clip, hwTestClip, 0o600); err != nil {
		h.tried = []string{err.Error()}
		return
	}
	for _, c := range cands {
		c.ffmpeg = h.ffmpeg
		if err := c.test(ctx, clip); err != nil {
			h.tried = append(h.tried, c.Name()+": "+err.Error())
			continue
		}
		h.Kind, h.Device = c.Kind, c.Device
		h.tried = append(h.tried, c.Name()+": works")
		return
	}
}

func (h *HWAccel) test(ctx context.Context, clip string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	args := append([]string{"-v", "error", "-hide_banner", "-nostdin"}, h.input()...)
	args = append(args, "-i", clip, "-an", "-vf", h.scale(64, 36, "bilinear"), "-f", "null", "-")
	cmd := proc.Hide(exec.CommandContext(ctx, h.ffmpeg, args...))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// (the first line says why - "Failed to initialise VAAPI connection"; the last only "Error opening output files")
		if line := firstLine(stderr.String()); line != "" {
			if strings.HasPrefix(line, "[") {
				if _, rest, ok := strings.Cut(line, "] "); ok {
					line = rest // ("[AVHWDeviceContext @ 0x...] ")
				}
			}
			return fmt.Errorf("%s", line)
		}
		return err
	}
	return nil
}
