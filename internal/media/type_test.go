package media

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

// ftyp builds an ISO BMFF ftyp box with the given brands.
func ftyp(major string, compat ...string) []byte {
	size := 16 + 4*len(compat)
	b := []byte{0, 0, 0, byte(size)}
	b = append(b, "ftyp"+major+"\x00\x00\x00\x00"...)
	for _, c := range compat {
		b = append(b, c...)
	}
	return b
}

func encoded(t *testing.T, enc func(*bytes.Buffer, image.Image) error) []byte {
	t.Helper()
	img := image.NewPaletted(image.Rect(0, 0, 4, 4), color.Palette{color.Black, color.White})
	var buf bytes.Buffer
	if err := enc(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDetect(t *testing.T) {
	cases := []struct {
		name string
		head []byte
		want Type
	}{
		{"jpeg", encoded(t, func(b *bytes.Buffer, i image.Image) error { return jpeg.Encode(b, i, nil) }), JPEG},
		{"png", encoded(t, func(b *bytes.Buffer, i image.Image) error { return png.Encode(b, i) }), PNG},
		{"gif", encoded(t, func(b *bytes.Buffer, i image.Image) error { return gif.Encode(b, i, nil) }), GIF},
		{"webp", []byte("RIFF\x10\x00\x00\x00WEBPVP8 "), WebP},
		{"webm", []byte("\x1a\x45\xdf\xa3\x9f\x42\x86\x81\x01\x42\x82\x84webm\x42\x87"), WebM},
		{"mkv", []byte("\x1a\x45\xdf\xa3\xa3\x42\x86\x81\x01\x42\x82\x88matroska"), Matroska},
		{"mp4 isom", ftyp("isom", "isom", "iso2", "avc1", "mp41"), MP4},
		{"mp4 mp42", ftyp("mp42", "mp42", "isom"), MP4},
		{"m4v", ftyp("M4V ", "M4V ", "mp42"), MP4},
		{"mov ftyp", ftyp("qt  ", "qt  "), QuickTime},
		{"mov old", []byte("\x00\x00\x00\x08wide\x00\x00\x00\x00mdat"), QuickTime},
		{"avif", ftyp("avif", "mif1", "avif", "miaf"), AVIF},
		{"avif via mif1", ftyp("mif1", "mif1", "avif", "miaf"), AVIF},
	}
	for _, c := range cases {
		got, err := Detect(c.head[:min(len(c.head), SniffLen)])
		if err != nil || got != c.want {
			t.Errorf("%s: got %v, %v; want %v", c.name, got, err, c.want)
		}
	}
}

func TestDetectUnsupported(t *testing.T) {
	for name, head := range map[string][]byte{
		"empty": nil,
		"text":  []byte("hello world, this is text"),
		"heic":  ftyp("heic", "mif1", "heic"),
		"mif1":  ftyp("mif1", "mif1", "heic"),
		"short": {0xFF, 0xD8},
		"pdf":   []byte("%PDF-1.7\n"),
	} {
		if got, err := Detect(head); !errors.Is(err, ErrUnsupported) || got != Unknown {
			t.Errorf("%s: got %v, %v; want unsupported", name, got, err)
		}
	}
}

func TestTypeInfo(t *testing.T) {
	if JPEG.Ext() != "jpg" || MP4.MIME() != "video/mp4" || WebM.String() != "webm" {
		t.Fatal("type table mismatch")
	}
	if !MP4.IsVideo() || MP4.IsImage() || !GIF.IsImage() || GIF.IsVideo() {
		t.Fatal("IsVideo/IsImage mismatch")
	}
	if Type(200).Ext() != "bin" {
		t.Fatal("out-of-range type should be unknown")
	}
	// Stored values must never change.
	if JPEG != 1 || PNG != 2 || GIF != 3 || WebP != 4 || AVIF != 5 || MP4 != 6 ||
		QuickTime != 7 || WebM != 8 || Matroska != 9 {
		t.Fatal("type codes changed - they are stored in databases")
	}
}
