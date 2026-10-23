package media

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"golang.org/x/image/webp"
)

// Thumbnail recipe. The grid shows square tiles cropped to fill (like iOS
// Photos), so the short side is what matters: 400 px is sharp for three
// columns on a 3x phone screen. Extreme shapes are capped by the long side.
// Bump ThumbVersion when the recipe changes so old thumbnails get remade.
const (
	ThumbShort   = 400
	ThumbLong    = 1000
	ThumbQuality = 80
	ThumbVersion = 1
)

// ThumbSize is the thumbnail size for a w x h picture: short side
// ThumbShort, long side at most ThumbLong, never enlarged.
func ThumbSize(w, h int) (int, int) {
	if w <= 0 || h <= 0 {
		return ThumbShort, ThumbShort
	}
	short, long := min(w, h), max(w, h)
	scale := min(1, float64(ThumbShort)/float64(short), float64(ThumbLong)/float64(long))
	tw, th := max(1, int(float64(w)*scale+0.5)), max(1, int(float64(h)*scale+0.5))
	return tw, th
}

// Thumbnail makes a WebP thumbnail. Images use libvips when available
// (shrink-on-load: fast and light even for huge images) and ffmpeg otherwise;
// videos use a frame from 10% in, past most black intros.
func (t Tools) Thumbnail(ctx context.Context, path string, typ Type, info Info) ([]byte, error) {
	tw, th := ThumbSize(info.Width, info.Height)
	if typ.IsImage() && t.VipsThumbnail != "" {
		return t.vipsThumb(ctx, path, tw, th, ThumbQuality)
	}
	seek := 0.0
	if typ.IsVideo() && info.DurationMS > 0 {
		seek = float64(info.DurationMS) / 1000 / 10
	}
	out, err := t.ffmpegThumb(ctx, path, seek, tw, th, ThumbQuality)
	if (err != nil || len(out) == 0) && seek > 0 {
		out, err = t.ffmpegThumb(ctx, path, 0, tw, th, ThumbQuality) // some files can't seek
	}
	if err == nil && len(out) == 0 {
		err = fmt.Errorf("ffmpeg made no thumbnail")
	}
	return out, err
}

func (t Tools) ffmpegThumb(ctx context.Context, path string, seek float64, tw, th, quality int) ([]byte, error) {
	args := []string{"-v", "error", "-hide_banner", "-nostdin"}
	if seek > 0 {
		args = append(args, "-ss", strconv.FormatFloat(seek, 'f', 3, 64))
	}
	args = append(args, "-i", path, "-frames:v", "1", "-an", "-sn",
		"-vf", fmt.Sprintf("scale=%d:%d:flags=lanczos", tw, th),
		"-c:v", "libwebp", "-quality", strconv.Itoa(quality),
		"-f", "image2pipe", "pipe:1")
	cmd := exec.CommandContext(ctx, t.FFmpeg, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg: %w: %s", err, firstLine(stderr.String()))
	}
	return out, nil
}

func (t Tools) vipsThumb(ctx context.Context, path string, tw, th, quality int) ([]byte, error) {
	dir, err := os.MkdirTemp("", "vitrine-thumb-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "t.webp")
	cmd := exec.CommandContext(ctx, t.VipsThumbnail, path,
		"--size", fmt.Sprintf("%dx%d", tw, th),
		"-o", out+fmt.Sprintf("[Q=%d,strip]", quality))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("vipsthumbnail: %w: %s", err, firstLine(stderr.String()))
	}
	return os.ReadFile(out)
}

// Placeholder shrinks a WebP thumbnail to 4x4 pixels (48 bytes of RGB) that
// the web client blurs into a backdrop while the real thumbnail loads.
func Placeholder(thumb []byte) ([]byte, error) {
	img, err := webp.Decode(bytes.NewReader(thumb))
	if err != nil {
		return nil, fmt.Errorf("placeholder: %w", err)
	}
	return shrink4x4(img), nil
}

func shrink4x4(img image.Image) []byte {
	b := img.Bounds()
	out := make([]byte, 0, 48)
	for gy := 0; gy < 4; gy++ {
		for gx := 0; gx < 4; gx++ {
			x0, x1 := b.Min.X+b.Dx()*gx/4, b.Min.X+b.Dx()*(gx+1)/4
			y0, y1 := b.Min.Y+b.Dy()*gy/4, b.Min.Y+b.Dy()*(gy+1)/4
			var r, g, bl, n uint64
			for y := y0; y < max(y1, y0+1); y++ {
				for x := x0; x < max(x1, x0+1); x++ {
					cr, cg, cb, _ := img.At(x, y).RGBA()
					r, g, bl, n = r+uint64(cr>>8), g+uint64(cg>>8), bl+uint64(cb>>8), n+1
				}
			}
			out = append(out, byte(r/n), byte(g/n), byte(bl/n))
		}
	}
	return out
}

// RenditionQuality is the WebP quality of resized copies for viewing.
const RenditionQuality = 85

// Rendition resizes a still image so its long side is at most maxSide
// (never enlarging) and returns it as WebP. Phones view these instead of
// decoding huge originals, which can crash Safari.
func (t Tools) Rendition(ctx context.Context, path string, w, h, maxSide int) ([]byte, error) {
	scale := min(1, float64(maxSide)/float64(max(w, h, 1)))
	tw, th := max(1, int(float64(w)*scale+0.5)), max(1, int(float64(h)*scale+0.5))
	if t.VipsThumbnail != "" {
		return t.vipsThumb(ctx, path, tw, th, RenditionQuality)
	}
	return t.ffmpegThumb(ctx, path, 0, tw, th, RenditionQuality)
}
