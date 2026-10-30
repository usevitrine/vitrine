package media

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
)

// Info is what probing a file finds out.
type Info struct {
	Width, Height int // as displayed (rotation applied)
	DurationMS    int64
	Frames        int    // > 1 for animations; 0 if unknown
	HasAudio      bool   // an audio stream exists
	Codec         string // video/image codec, e.g. "h264", "vp9", "mjpeg"
}

// Animated reports whether the file moves (a video, or an image with frames).
func (i Info) Animated() bool { return i.Frames > 1 || i.DurationMS > 0 }

// Tools are the external programs Vitrine runs. Empty paths mean "not
// available"; FindTools fills them from PATH.
type Tools struct {
	FFmpeg        string
	FFprobe       string
	VipsThumbnail string   // optional: images fall back to ffmpeg
	HW            *HWAccel // graphics hardware that decodes videos (DetectHW); nil: the processor
}

// FindTools looks up the programs on PATH, preferring configured paths.
func FindTools(ffmpeg, ffprobe, vipsthumbnail string) Tools {
	find := func(configured, name string) string {
		if configured != "" {
			return configured
		}
		p, err := exec.LookPath(name)
		if err != nil {
			return ""
		}
		return p
	}
	return Tools{
		FFmpeg:        find(ffmpeg, "ffmpeg"),
		FFprobe:       find(ffprobe, "ffprobe"),
		VipsThumbnail: find(vipsthumbnail, "vipsthumbnail"),
	}
}

// Check reports a missing required tool.
func (t Tools) Check() error {
	switch {
	case t.FFmpeg == "":
		return fmt.Errorf("ffmpeg not found - install it or set its path in the config (ffmpeg = ...)")
	case t.FFprobe == "":
		return fmt.Errorf("ffprobe not found - it comes with ffmpeg; install it or set ffprobe = ... in the config")
	}
	return nil
}

type probeOutput struct {
	Streams []struct {
		CodecType     string `json:"codec_type"`
		CodecName     string `json:"codec_name"`
		Width         int    `json:"width"`
		Height        int    `json:"height"`
		NbFrames      string `json:"nb_frames"`
		NbReadPackets string `json:"nb_read_packets"`
		Duration      string `json:"duration"`
		Disposition   struct {
			AttachedPic int `json:"attached_pic"`
		} `json:"disposition"`
		Tags struct {
			Rotate string `json:"rotate"`
		} `json:"tags"`
		SideData []struct {
			Rotation float64 `json:"rotation"`
		} `json:"side_data_list"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

// Probe reads a file's dimensions, duration, frames and audio with ffprobe.
func (t Tools) Probe(ctx context.Context, path string, typ Type) (Info, error) {
	args := []string{"-v", "error", "-hide_banner", "-print_format", "json", "-show_format", "-show_streams"}
	if typ == GIF || typ == PNG || typ == WebP || typ == AVIF {
		// Counting packets finds animation frames; cheap for images (no
		// decoding), but it would read whole videos, so only for images.
		args = append(args, "-count_packets")
	}
	args = append(args, "--", path)
	out, stderr, _, err := run(ctx, "probe", false, t.FFprobe, args...)
	if err != nil {
		return Info{}, fmt.Errorf("ffprobe: %w: %s", err, firstLine(stderr))
	}
	return parseProbe(out, typ)
}

func parseProbe(out []byte, typ Type) (Info, error) {
	var p probeOutput
	if err := json.Unmarshal(out, &p); err != nil {
		return Info{}, fmt.Errorf("ffprobe output: %w", err)
	}
	var info Info
	found := false
	for _, s := range p.Streams {
		switch s.CodecType {
		case "audio":
			info.HasAudio = true
		case "video":
			if found || s.Disposition.AttachedPic == 1 {
				continue // first real video stream only (not cover art)
			}
			found = true
			info.Codec, info.Width, info.Height = s.CodecName, s.Width, s.Height
			rot := 0.0
			if len(s.SideData) > 0 {
				rot = s.SideData[0].Rotation
			} else if s.Tags.Rotate != "" {
				rot, _ = strconv.ParseFloat(s.Tags.Rotate, 64)
			}
			if r := int(math.Abs(math.Round(rot))) % 180; r == 90 {
				info.Width, info.Height = info.Height, info.Width
			}
			info.Frames = atoi(s.NbFrames)
			if n := atoi(s.NbReadPackets); n > info.Frames {
				info.Frames = n
			}
			if d := seconds(s.Duration); d > 0 {
				info.DurationMS = d
			}
		}
	}
	if !found || info.Width <= 0 || info.Height <= 0 {
		return Info{}, fmt.Errorf("no picture found in the file (damaged or incomplete?)")
	}
	if info.DurationMS == 0 {
		info.DurationMS = seconds(p.Format.Duration)
	}
	if typ.IsImage() && info.Frames <= 1 {
		info.DurationMS = 0 // a still image has no duration (some formats report one)
	}
	if typ.IsVideo() && info.Frames == 0 && info.DurationMS > 0 {
		info.Frames = 2 // unknown count, but it moves
	}
	return info, nil
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// seconds turns ffprobe's "12.345000" into milliseconds (0 if absent).
func seconds(s string) int64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < 0 || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return int64(math.Round(f * 1000))
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}
