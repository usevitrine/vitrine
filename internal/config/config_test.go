package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func writeFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPrecedence(t *testing.T) {
	data := t.TempDir()
	p := writeFile(t, "data_dir = '"+filepath.ToSlash(data)+"'\nlisten = ':9000'\n")

	c, err := Load(p, env(nil), Config{})
	if err != nil {
		t.Fatal(err)
	}
	if c.DataDir != filepath.ToSlash(data) || c.Listen != ":9000" {
		t.Fatalf("file not applied: %+v", c)
	}
	if want := filepath.Join(c.DataDir, "files"); c.FilesDir != want {
		t.Fatalf("FilesDir = %q, want %q", c.FilesDir, want)
	}

	c, err = Load(p, env(map[string]string{"VITRINE_LISTEN": "127.0.0.1:1", "VITRINE_FILES_DIR": "/bulk"}), Config{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != "127.0.0.1:1" || c.FilesDir != "/bulk" {
		t.Fatalf("env not applied over file: %+v", c)
	}
}

func TestOverrides(t *testing.T) {
	p := writeFile(t, "data_dir = '/ssd'\nfiles_dir = '/hdd'\n")
	env := env(map[string]string{"VITRINE_DATA_DIR": "/env"})

	// A flag for data_dir must not reset a files_dir set in the file.
	c, err := Load(p, env, Config{DataDir: "/flag"})
	if err != nil {
		t.Fatal(err)
	}
	if c.DataDir != "/flag" || c.FilesDir != "/hdd" {
		t.Fatalf("got %+v", c)
	}

	// Without files_dir anywhere, it follows the final data_dir.
	p = writeFile(t, "data_dir = '/ssd'\n")
	c, err = Load(p, env, Config{DataDir: "/flag"})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/flag", "files"); c.FilesDir != want {
		t.Fatalf("FilesDir = %q, want %q", c.FilesDir, want)
	}
}

func TestMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.toml")

	// An explicitly named file must exist.
	if _, err := Load(missing, env(nil), Config{}); err == nil {
		t.Fatal("expected error for missing explicit file")
	}
	if _, err := Load("", env(map[string]string{"VITRINE_CONFIG": missing}), Config{}); err == nil {
		t.Fatal("expected error for missing VITRINE_CONFIG file")
	}
}

func TestUnknownKey(t *testing.T) {
	p := writeFile(t, "data_dirr = '/x'\n")
	_, err := Load(p, env(nil), Config{})
	if err == nil || !strings.Contains(err.Error(), "data_dirr") {
		t.Fatalf("expected unknown-key error, got %v", err)
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	if got, want := expandHome("~/v"), filepath.Join(home, "v"); got != want {
		t.Fatalf("expandHome = %q, want %q", got, want)
	}
	if got := expandHome("/abs/~x"); got != "/abs/~x" {
		t.Fatalf("expandHome changed %q", got)
	}
}
