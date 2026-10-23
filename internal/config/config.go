// Package config loads Vitrine's settings.
//
// Settings come from, in increasing priority: built-in defaults, a TOML config
// file, and VITRINE_* environment variables. Command-line flags, applied by the
// caller, override all of them.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config holds every setting. The TOML keys are the field tags; the matching
// environment variable is VITRINE_ plus the key in upper case.
type Config struct {
	// DataDir holds the databases, thumbnails and previews. Put it on fast
	// storage (SSD).
	DataDir string `toml:"data_dir"`
	// FilesDir holds the original files. It can live on bulk storage (HDD).
	// Defaults to DataDir/files.
	FilesDir string `toml:"files_dir"`
	// Listen is the HTTP listen address.
	Listen string `toml:"listen"`
	// Paths to external tools; empty = look them up on PATH. vipsthumbnail
	// (libvips) is optional - images fall back to ffmpeg without it.
	FFmpeg        string `toml:"ffmpeg"`
	FFprobe       string `toml:"ffprobe"`
	VipsThumbnail string `toml:"vipsthumbnail"`
	// RenditionsMaxGB caps the cache of resized copies of huge images (in
	// DataDir/renditions; regenerable).
	RenditionsMaxGB float64 `toml:"renditions_max_gb"`
	// PluginIndex is the list of plugins Settings offers to install or update:
	// an https URL of an index.json, or a local file or folder with one.
	PluginIndex string `toml:"plugin_index"`
	// BackupDir holds the database backups (Settings > Backups). Defaults to
	// DataDir/backups; another disk is safer.
	BackupDir string `toml:"backup_dir"`
	// Language is used where a browser's own languages have no language file
	// (a language tag, e.g. "en-GB"; default en-US). Settings > Viewing picks one
	// for every device.
	Language string `toml:"language"`
	// Socket is the running server's local socket: the vitrine command talks to the server through it (vitrine
	// status, queue, subs, ...) - no login, only this machine (in Docker: this container) and only users who can
	// open the file. Defaults to DataDir/vitrine.sock; "off" turns it off.
	Socket string `toml:"socket"`
}

// envKeys maps each environment variable to the field it sets.
func (c *Config) envKeys() map[string]*string {
	return map[string]*string{
		"VITRINE_DATA_DIR":      &c.DataDir,
		"VITRINE_FILES_DIR":     &c.FilesDir,
		"VITRINE_LISTEN":        &c.Listen,
		"VITRINE_FFMPEG":        &c.FFmpeg,
		"VITRINE_FFPROBE":       &c.FFprobe,
		"VITRINE_VIPSTHUMBNAIL": &c.VipsThumbnail,
		"VITRINE_PLUGIN_INDEX":  &c.PluginIndex,
		"VITRINE_BACKUP_DIR":    &c.BackupDir,
		"VITRINE_LANGUAGE":      &c.Language,
		"VITRINE_SOCKET":        &c.Socket,
	}
}

// DefaultPluginIndex is the official plugin list (the vitrine-plugins repo).
const DefaultPluginIndex = "https://raw.githubusercontent.com/usevitrine/vitrine-plugins/main/index.json"

// Defaults returns the built-in settings for this OS.
func Defaults() Config {
	return Config{
		DataDir:         defaultDataDir(),
		Listen:          ":8080",
		RenditionsMaxGB: 10,
		PluginIndex:     DefaultPluginIndex,
	}
}

// DefaultFile is where the config file is looked for when none is given:
// the OS's usual config directory, e.g. ~/.config/vitrine/config.toml on Linux.
func DefaultFile() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "vitrine", "config.toml")
}

// Load reads the settings. path is the config file; if empty, VITRINE_CONFIG
// or DefaultFile is used, and a missing default file is not an error.
// getenv is os.Getenv in normal use (a parameter so tests can control it).
// Non-empty fields of overrides (command-line flags) win over everything.
func Load(path string, getenv func(string) string, overrides Config) (Config, error) {
	c := Defaults()

	explicit := path != ""
	if !explicit {
		if p := getenv("VITRINE_CONFIG"); p != "" {
			path, explicit = p, true
		} else {
			path = DefaultFile()
		}
	}
	if path != "" {
		md, err := toml.DecodeFile(path, &c)
		switch {
		case errors.Is(err, fs.ErrNotExist) && !explicit:
			// No config file: defaults and environment only.
		case err != nil:
			return Config{}, fmt.Errorf("config file %s: %w", path, err)
		default:
			if keys := md.Undecoded(); len(keys) > 0 {
				return Config{}, fmt.Errorf("config file %s: unknown setting %q", path, keys[0].String())
			}
		}
	}

	for name, field := range c.envKeys() {
		if v := getenv(name); v != "" {
			*field = v
		}
	}
	if v := getenv("VITRINE_RENDITIONS_MAX_GB"); v != "" {
		gb, err := strconv.ParseFloat(v, 64)
		if err != nil || gb <= 0 {
			return Config{}, fmt.Errorf("VITRINE_RENDITIONS_MAX_GB: bad number %q", v)
		}
		c.RenditionsMaxGB = gb
	}
	over := overrides.envKeys()
	for name, field := range c.envKeys() {
		if v := *over[name]; v != "" {
			*field = v
		}
	}

	if err := c.finish(); err != nil {
		return Config{}, err
	}
	return c, nil
}

// finish fills derived defaults and checks the result.
func (c *Config) finish() error {
	if c.DataDir == "" {
		return errors.New("config: data_dir is not set")
	}
	c.DataDir = expandHome(c.DataDir)
	if c.FilesDir == "" {
		c.FilesDir = filepath.Join(c.DataDir, "files")
	}
	c.FilesDir = expandHome(c.FilesDir)
	if c.BackupDir == "" {
		c.BackupDir = filepath.Join(c.DataDir, "backups")
	}
	c.BackupDir = expandHome(c.BackupDir)
	if c.Socket == "" {
		c.Socket = filepath.Join(c.DataDir, "vitrine.sock")
	}
	if c.Socket != "off" {
		c.Socket = expandHome(c.Socket)
	}
	return nil
}

// expandHome turns a leading "~" into the user's home directory.
func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") && !strings.HasPrefix(p, `~\`) {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, p[1:])
}

// defaultDataDir follows each OS's convention for application data.
func defaultDataDir() string {
	switch runtime.GOOS {
	case "windows":
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, "Vitrine")
		}
	case "darwin":
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "Library", "Application Support", "Vitrine")
		}
	default:
		if d := os.Getenv("XDG_DATA_HOME"); d != "" {
			return filepath.Join(d, "vitrine")
		}
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, ".local", "share", "vitrine")
		}
	}
	return "vitrine-data"
}
