package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func writeImage(t *testing.T, path string, c uint8, asPNG bool) {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 8, 8))
	for i := range img.Pix {
		img.Pix[i] = c
	}
	img.Set(0, 0, color.Gray{Y: ^c})
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if asPNG {
		err = png.Encode(f, img)
	} else {
		err = jpeg.Encode(f, img, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func runCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	// An empty config of its own: not the machine's (an installed Vitrine's settings would leak in).
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VITRINE_CONFIG", cfg)
	var out, errOut bytes.Buffer
	err := run(context.Background(), args, &out, &errOut)
	return out.String() + errOut.String(), err
}

func TestImportCommand(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeImage(t, filepath.Join(src, "a.png"), 10, true)
	writeImage(t, filepath.Join(src, "sub", "b.jpg"), 200, false)
	writeImage(t, filepath.Join(src, "sub", "a-copy.png"), 10, true) // same content as a.png
	if err := os.WriteFile(filepath.Join(src, "notes.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	data := t.TempDir()
	out, err := runCmd(t, "import", "-data-dir", data, src)
	if err != nil {
		t.Fatalf("import failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "2 added, 1 already in the library, 0 previously deleted, 1 unsupported, 0 failed") {
		t.Fatalf("unexpected summary:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(data, "meta.db")); err != nil {
		t.Fatal("meta.db not created")
	}

	// Importing again adds nothing.
	out, err = runCmd(t, "import", "-data-dir", data, src)
	if err != nil || !strings.Contains(out, "0 added, 3 already") {
		t.Fatalf("second import: %v\n%s", err, out)
	}

	// Originals are left where they were.
	if _, err := os.Stat(filepath.Join(src, "a.png")); err != nil {
		t.Fatal("source file was moved")
	}
}

func TestConfigAndVersion(t *testing.T) {
	data := t.TempDir()
	out, err := runCmd(t, "config", "-data-dir", data)
	if err != nil || !strings.Contains(out, strconv.Quote(filepath.Join(data, "files"))) {
		t.Fatalf("config: %v\n%s", err, out)
	}
	if out, err := runCmd(t, "version"); err != nil || !strings.HasPrefix(out, "vitrine ") {
		t.Fatalf("version: %v %q", err, out)
	}
	if _, err := runCmd(t, "bogus"); err == nil {
		t.Fatal("unknown command accepted")
	}
}
