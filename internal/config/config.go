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
	// Paths to external tools; empty = the copies shipped with the program (Bundled), else looked up on PATH.
	// vipsthumbnail (libvips) is optional - images fall back to ffmpeg without it.
	FFmpeg        string `toml:"ffmpeg"`
	FFprobe       string `toml:"ffprobe"`
	VipsThumbnail string `toml:"vipsthumbnail"`
	// HWAccel is the graphics hardware ffmpeg decodes videos with (preview frames, video thumbnails): "auto" (the
	// default: the first that works - VA-API on Intel and AMD, CUDA on Nvidia, D3D11 on Windows, VideoToolbox on a
	// Mac), "off" (the processor only), or one of them: "vaapi", "vaapi:/dev/dri/renderD129", "cuda", "d3d11va",
	// "videotoolbox".
	HWAccel string `toml:"hwaccel"`
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
	// WatchtowerURL and WatchtowerToken: Watchtower's HTTP API (its update endpoint on, with that token) - then
	// Settings has an Update button that has Watchtower pull the newest image and recreate Vitrine's container.
	// Empty = no button (updating stays a docker compose pull).
	WatchtowerURL   string `toml:"watchtower_url"`
	WatchtowerToken string `toml:"watchtower_token"`
	// ReleasesURL is where the daily update check asks for the latest release (default: GitHub's API for
	// usevitrine/vitrine); "off" never asks.
	ReleasesURL string `toml:"releases_url"`
	// ChangelogURL is where a new release's CHANGELOG.md is read from (%s: its tag; default: the repository's on
	// GitHub) - Settings > Updates shows what's new in it.
	ChangelogURL string `toml:"changelog_url"`
}

// envKeys maps each environment variable to the field it sets.
func (c *Config) envKeys() map[string]*string {
	return map[string]*string{
		"VITRINE_DATA_DIR":         &c.DataDir,
		"VITRINE_FILES_DIR":        &c.FilesDir,
		"VITRINE_LISTEN":           &c.Listen,
		"VITRINE_FFMPEG":           &c.FFmpeg,
		"VITRINE_FFPROBE":          &c.FFprobe,
		"VITRINE_VIPSTHUMBNAIL":    &c.VipsThumbnail,
		"VITRINE_HWACCEL":          &c.HWAccel,
		"VITRINE_PLUGIN_INDEX":     &c.PluginIndex,
		"VITRINE_BACKUP_DIR":       &c.BackupDir,
		"VITRINE_LANGUAGE":         &c.Language,
		"VITRINE_SOCKET":           &c.Socket,
		"VITRINE_WATCHTOWER_URL":   &c.WatchtowerURL,
		"VITRINE_WATCHTOWER_TOKEN": &c.WatchtowerToken,
		"VITRINE_RELEASES_URL":     &c.ReleasesURL,
		"VITRINE_CHANGELOG_URL":    &c.ChangelogURL,
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

// PortableFile is a config.toml next to the vitrine program, if there is one: Vitrine then runs portable - that
// config is used, and its relative paths are from that folder (data_dir = "data": a data folder beside the
// program), so the folder can be moved or copied with everything in it. "" when there's none.
func PortableFile() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	f := filepath.Join(filepath.Dir(exe), "config.toml")
	if st, err := os.Stat(f); err == nil && !st.IsDir() {
		return f
	}
	return ""
}

// DefaultFile is where the config file is looked for when none is given: a config.toml next to the program
// (PortableFile), else the OS's usual config directory, e.g. ~/.config/vitrine/config.toml on Linux.
func DefaultFile() string {
	if f := PortableFile(); f != "" {
		return f
	}
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
			c.fromFolder(filepath.Dir(path))
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

// fromFolder makes the config file's relative paths relative to its folder - not to wherever Vitrine was started -
// so a config's folder can move with what it points at (portable: see PortableFile). Programs given by name alone
// (ffmpeg) are still looked up on PATH; a portable folder's own copies, beside the config, are used when not set.
func (c *Config) fromFolder(dir string) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return
	}
	at := func(p *string) {
		if *p != "" && !filepath.IsAbs(*p) && !strings.HasPrefix(*p, "~") && !strings.ContainsAny((*p)[:1], `/\`) { // (rooted - /srv - as given)
			*p = filepath.Join(abs, *p)
		}
	}
	at(&c.DataDir)
	at(&c.FilesDir)
	at(&c.BackupDir)
	if c.Socket != "off" {
		at(&c.Socket)
	}
	if c.PluginIndex != "" && !strings.Contains(c.PluginIndex, "://") {
		at(&c.PluginIndex)
	}
	for _, tool := range []struct {
		p    *string
		name string
	}{{&c.FFmpeg, "ffmpeg"}, {&c.FFprobe, "ffprobe"}, {&c.VipsThumbnail, "vipsthumbnail"}} {
		if strings.ContainsAny(*tool.p, `/\`) {
			at(tool.p) // (a path, not a name)
		} else if *tool.p == "" {
			exe := tool.name
			if runtime.GOOS == "windows" {
				exe += ".exe"
			}
			if st, err := os.Stat(filepath.Join(abs, exe)); err == nil && !st.IsDir() {
				*tool.p = filepath.Join(abs, exe)
			}
		}
	}
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
	for _, tool := range []struct {
		p    *string
		name string
	}{{&c.FFmpeg, "ffmpeg"}, {&c.FFprobe, "ffprobe"}, {&c.VipsThumbnail, "vipsthumbnail"}} {
		if *tool.p == "" {
			*tool.p = Bundled(tool.name)
		}
	}
	return nil
}

// bundledIn is the tools folder's subfolder each tool ships in (with its libraries).
var bundledIn = map[string]string{"ffmpeg": "ffmpeg", "ffprobe": "ffmpeg", "vipsthumbnail": "vips"}

// Bundled is a tool shipped with the program - as the Windows installer lays it out: tools\ffmpeg\ffmpeg.exe,
// tools\vips\vipsthumbnail.exe beside the program, or beside the folder it's in (the command line's bin\vitrine.exe).
// "" when there's none.
func Bundled(name string) string {
	exe, err := os.Executable()
	if err != nil || bundledIn[name] == "" {
		return ""
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	dir := filepath.Dir(exe)
	for _, base := range []string{dir, filepath.Dir(dir)} {
		f := filepath.Join(base, "tools", bundledIn[strings.TrimSuffix(name, ".exe")], name)
		if st, err := os.Stat(f); err == nil && !st.IsDir() {
			return f
		}
	}
	return ""
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
