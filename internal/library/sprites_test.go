package library

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/usevitrine/vitrine/internal/media"
	"github.com/usevitrine/vitrine/internal/thumbs"
)

func TestSprites(t *testing.T) {
	l := newTest(t)
	if err := l.tools.Check(); err != nil {
		t.Skip(err)
	}
	ctx := context.Background()
	clip := filepath.Join(t.TempDir(), "clip.mp4")
	if out, err := exec.Command(l.tools.FFmpeg, "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc=size=320x180:rate=25:duration=9",
		"-c:v", "libx264", "-g", "25", "-pix_fmt", "yuv420p", clip).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	data, _ := os.ReadFile(clip)
	f, err := l.Import(ctx, bytesReader(data))
	if err != nil {
		t.Fatal(err)
	}
	img, _ := l.Import(ctx, bytesReader(realPNG(t, 100, 100)))
	if p, _ := l.PendingSprites(ctx, 10); len(p) != 0 {
		t.Fatalf("pending before probing: %v", p) // needs the duration first
	}
	l.ProcessMany(ctx, []uint32{f.ID, img.ID}, 1, nil)
	if p, _ := l.PendingSprites(ctx, 10); !slices.Equal(p, []uint32{f.ID}) {
		t.Fatalf("pending: %v (images never have frames)", p)
	}
	if took, err := l.MakeSprite(ctx, f.ID); err != nil || took.Wall <= 0 {
		t.Fatal(took, err)
	}
	sp, err := l.Sprite(ctx, f.ID, true)
	if err != nil || sp.Frames != 9 || sp.TileW != 320 || sp.TileH != 180 || sp.IntervalMS != 1000 || len(sp.Data) == 0 {
		t.Fatalf("sprite: %+v (%d bytes) %v", sp, len(sp.Data), err)
	}
	if meta, _ := l.Sprite(ctx, f.ID, false); len(meta.Data) != 0 || meta.Frames != 9 {
		t.Fatalf("layout only: %+v", meta)
	}
	if p, _ := l.PendingSprites(ctx, 10); len(p) != 0 {
		t.Fatalf("pending after: %v", p)
	}
	// Made by an older recipe: queued to be made again (the old one serves meanwhile).
	if err := l.thumbs.PutSprite(ctx, f.ID, thumbs.Sprite{Version: media.SpriteVersion - 1, Frames: 5, Cols: 5, TileW: 160, TileH: 90, IntervalMS: 2000, Data: sp.Data}); err != nil {
		t.Fatal(err)
	}
	if n, err := l.RequeueOldSprites(ctx); err != nil || n != 1 {
		t.Fatalf("requeued %d, %v", n, err)
	}
	if p, _ := l.PendingSprites(ctx, 10); !slices.Equal(p, []uint32{f.ID}) {
		t.Fatalf("pending after requeue: %v", p)
	}
	if _, err := l.MakeSprite(ctx, f.ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := l.RequeueOldSprites(ctx); n != 0 {
		t.Fatalf("still old after making again: %d", n)
	}
	// A deleted file's old frames are leftovers: not queued (nor counted).
	if err := l.thumbs.PutSprite(ctx, f.ID, thumbs.Sprite{Version: media.SpriteVersion - 1, Frames: 5, Cols: 5, TileW: 160, TileH: 90, IntervalMS: 2000, Data: sp.Data}); err != nil {
		t.Fatal(err)
	}
	if err := l.Delete(ctx, []uint32{f.ID}, ""); err != nil {
		t.Fatal(err)
	}
	if n, _ := l.RequeueOldSprites(ctx); n != 0 {
		t.Fatalf("deleted file requeued: %d", n)
	}
}
func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }
