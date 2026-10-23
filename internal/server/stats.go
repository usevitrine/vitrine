package server

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/usevitrine/vitrine/internal/diskfree"
	"github.com/usevitrine/vitrine/internal/library"
)

// Info is what the server knows about its setup (Settings > Stats).
type Info struct {
	Version   string
	DataDir   string // databases, thumbnails, previews (fast storage)
	FilesDir  string // originals (can be bulk storage)
	BackupDir string
}

// SetInfo tells the server about its setup (version, where things are stored).
func (s *Server) SetInfo(i Info) { s.info = i }

var started = time.Now()

type storageStat struct {
	Originals int64 `json:"originals"`
	Database  int64 `json:"database"`
	Thumbs    int64 `json:"thumbs"`
	Previews  int64 `json:"previews"`
	Backups   int64 `json:"backups"`
	// Where they are (the welcome's storage check shows them).
	DataDir  string `json:"data_dir"`
	FilesDir string `json:"files_dir"`
	// The disks: data (databases, thumbnails, previews) and files (originals);
	// one entry when both are on the same disk.
	Disks []diskStat `json:"disks"`
}

type diskStat struct {
	For string `json:"for"` // "data", "files" or "both"
	diskfree.Usage
}

type serverStat struct {
	Version  string `json:"version"`
	Uptime   int64  `json:"uptime_s"`
	Go       string `json:"go"`
	OS       string `json:"os"`
	CPUs     int    `json:"cpus"`
	Memory   uint64 `json:"memory"`    // held from the system
	Heap     uint64 `json:"heap"`      // in use
	FFmpeg   string `json:"ffmpeg"`    // version, "" = not found
	Vips     string `json:"vips"`      // version, "" = not found (images use ffmpeg)
	InDocker bool   `json:"in_docker"` // running in a container
}

type statsReply struct {
	library.Stats
	Storage storageStat `json:"storage"`
	Server  serverStat  `json:"server"`
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	days = min(max(days, 0), 366)
	tz, _ := strconv.ParseInt(r.URL.Query().Get("tz"), 10, 64) // minutes east of UTC
	tz = min(max(tz, -14*60), 14*60)
	st, err := s.lib.Stats(r.Context(), days, tz*60)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	out := statsReply{Stats: st}
	out.Storage = s.storage(st)
	out.Server = s.serverInfo(r.Context())
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) storage(st library.Stats) storageStat {
	out := storageStat{Originals: st.Bytes + st.Trash.Bytes, DataDir: s.info.DataDir, FilesDir: s.info.FilesDir}
	if s.info.DataDir != "" {
		out.Database = fileSizes(filepath.Join(s.info.DataDir, "meta.db"))
		out.Thumbs = fileSizes(filepath.Join(s.info.DataDir, "thumbs.db"))
	}
	if s.rend != nil {
		out.Previews = s.rend.Size()
	}
	if s.info.BackupDir != "" {
		out.Backups = dirSize(s.info.BackupDir)
	}
	data, errD := diskfree.Of(s.info.DataDir)
	files, errF := diskfree.Of(s.info.FilesDir)
	switch {
	case s.info.DataDir == "" || errD != nil:
		if errF == nil && s.info.FilesDir != "" {
			out.Disks = []diskStat{{For: "files", Usage: files}}
		}
	case errF != nil || s.info.FilesDir == "" || data.Total == files.Total && near(data.Free, files.Free):
		out.Disks = []diskStat{{For: "both", Usage: data}}
	default:
		out.Disks = []diskStat{{For: "files", Usage: files}, {For: "data", Usage: data}}
	}
	return out
}

// near: two free-space readings of what is likely the same disk (taken a moment apart).
func near(a, b uint64) bool {
	d := int64(a) - int64(b)
	return d > -(64<<20) && d < 64<<20
}

// fileSizes adds up an SQLite database and its WAL.
func fileSizes(db string) int64 {
	var n int64
	for _, p := range []string{db, db + "-wal"} {
		if fi, err := os.Stat(p); err == nil {
			n += fi.Size()
		}
	}
	return n
}

func dirSize(dir string) int64 {
	var n int64
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if fi, err := e.Info(); err == nil && !e.IsDir() {
			n += fi.Size()
		}
	}
	return n
}

// toolVersions: read once they're known (a failed read is tried again next time).
var toolVersions struct {
	sync.Mutex
	ffmpeg, vips string
}

func (s *Server) serverInfo(ctx context.Context) serverStat {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	toolVersions.Lock()
	t := s.lib.Tools()
	if toolVersions.ffmpeg == "" || toolVersions.ffmpeg == "found" {
		toolVersions.ffmpeg = toolVersion(ctx, t.FFmpeg, "-version", "ffmpeg version ")
	}
	if toolVersions.vips == "" || toolVersions.vips == "found" {
		toolVersions.vips = toolVersion(ctx, t.VipsThumbnail, "--vips-version", "vips-")
	}
	ffmpeg, vips := toolVersions.ffmpeg, toolVersions.vips
	toolVersions.Unlock()
	_, err := os.Stat("/.dockerenv")
	return serverStat{
		Version:  s.info.Version,
		Uptime:   int64(time.Since(started).Seconds()),
		Go:       runtime.Version(),
		OS:       runtime.GOOS + "/" + runtime.GOARCH,
		CPUs:     runtime.NumCPU(),
		Memory:   m.Sys - m.HeapReleased,
		Heap:     m.HeapAlloc,
		FFmpeg:   ffmpeg,
		Vips:     vips,
		InDocker: err == nil,
	}
}

// toolVersion runs a tool for its version: on the first line (after prefix,
// if it has it), the first word with a digit. "" only when there's no such
// tool; a tool that's there but doesn't answer in time is "found".
func toolVersion(ctx context.Context, path, flag, prefix string) string {
	if path == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, flag).Output()
	if err != nil && len(out) == 0 {
		return "found" // (there, but no answer: busy, or no version flag)
	}
	line, _, _ := bytes.Cut(out, []byte("\n"))
	v := strings.TrimSpace(string(line))
	if i := strings.Index(v, prefix); i >= 0 {
		v = v[i+len(prefix):]
	}
	// The first word with a digit in it ("libvips 8.16.0" -> "8.16.0").
	for _, w := range strings.Fields(v) {
		if strings.ContainsAny(w, "0123456789") {
			return w
		}
	}
	return "found"
}
