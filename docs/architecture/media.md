# Media processing

Probing, thumbnails, previews and scrub frames (ffmpeg, libvips): how files are processed after import.

## Media processing as built (step 1 item 5, 2026-10-14)
- **Probe** with ffprobe (`internal/media`): dimensions (rotation applied, so phone videos are portrait), duration,
  frame count (packets counted for GIF/PNG/WebP/AVIF only - for videos that would read the whole file), audio stream,
  codec. A file with no picture size is reported as damaged.
- **Thumbnails**: WebP, short side 400 px (sharp for a 3-column grid of square tiles on a 3x phone), long side at most
  1000 px, never enlarged, quality 80 (`media.ThumbVersion` = 1; bump to regenerate). Images: `vipsthumbnail` when
  installed, else ffmpeg (fine, but decodes huge images fully and ignores JPEG EXIF rotation). Videos: ffmpeg, a frame
  at 10% of the duration (past black intros; falls back to the first frame). Stored in `thumbs.db` (own SQLite file
  and migrations, `thumbs` table: file ID, recipe version, size, WebP blob) - regenerable, not backed up.
- **Placeholder**: the thumbnail shrunk to 4x4 RGB (48 bytes) in `files.placeholder`, for the client to blur while
  the thumbnail loads.
- **State in `files.flags`**: bit 0 probed, bit 1 thumbnail, bit 2 failed (not retried automatically; `vitrine process
  -retry`). Pending work = live files without both bits - survives restarts with no queue table. Probing and
  thumbnailing run outside the library lock; only the final row update + index update take it.
- Tool paths: config `ffmpeg`, `ffprobe`, `vipsthumbnail` (default: PATH). CI installs ffmpeg + libvips on Linux; the
  media tests make their samples with ffmpeg's test sources and skip where ffmpeg is missing.
- Measured: 34 wallpapers (1920x1200 to 6400x4000 JPEGs) thumbnailed in 0.57 s with ffmpeg on a desktop PC (8
  workers). Not done yet: animated previews. (Preview frames and hardware decoding: below.)
- **Hardware video decoding (2026-10-30, the user: "use hardware acceleration whenever it can be used" - up to 80% of
  8 cores busy while a big library imports; "don't just add intel support, add amd and nvidia support too")**
  (`media.HWAccel`, `hwaccel.go`): preview frames and video thumbnails are decoded on the GPU. Config `hwaccel` /
  `VITRINE_HWACCEL`: `auto` (default), `off`, or one: `vaapi`, `vaapi:/dev/dri/renderD129`, `cuda`, `d3d11va`,
  `videotoolbox`. Auto tries, in order: Linux - VA-API on every `/dev/dri/renderD*` (Intel, AMD), then CUDA (Nvidia);
  Windows - Direct3D 11 (any maker), then CUDA; macOS - VideoToolbox. Each candidate decodes an embedded 5 KB H.264
  clip (`hwtest.mp4`) with its frames kept on the GPU and copied back (`hwdownload`) - ffmpeg quietly falls back to
  the processor otherwise, which then fails the test, so only real GPU decoding passes. Looked for once, when a video
  first needs it (`serve` at start-up, logged: "videos decoded by the graphics hardware"; `vitrine config` shows it
  too). The frames stay on the GPU until the ones kept (preview frames: one a second) are copied back; VA-API also
  scales there (`scale_vaapi`, 8-bit nv12), the others scale on the processor after the copy (10-bit too:
  `nv12|p010le`). A file the GPU fails is made again on the processor; 3 failures in a row of one codec leave that
  codec to the processor, 20 in a row everything, until the next start (a warning in the log). Settings > Stats:
  "Video decoding". Docker image: `mesa-va-drivers` (AMD), on amd64 also `intel-media-va-driver` + `i965-va-driver`;
  `NVIDIA_DRIVER_CAPABILITIES=compute,video,utility` for Nvidia through the NVIDIA Container Toolkit
  (compose.example.yaml: `devices` + `group_add`, or `gpus: all`). Measured: a sample of 13 videos of a real library
  (all H.264, 84% a minute or shorter) on an Intel UHD 7xx (VA-API): preview frames in half the time, a tenth of the
  processor time (49 -> 5 CPU-s); the media tests' 1080p clip: Intel VA-API 0.28 vs 2.0 CPU-s (faster too), AMD
  RX 6700 XT D3D11 0.31 vs 2.86 CPU-s (a little slower in wall time on a fast desktop CPU); a 4K video thumbnail:
  3.6-5x less processor time, about the same wall time. Nvidia untested on hardware (no such GPU here; the
  candidate fails cleanly without one). Images stay on libvips (no GPU path worth it).
- **Load in the log (2026-10-30, the user: "expand logging to make diagnosing this stuff easier")**: every ffmpeg,
  ffprobe and vipsthumbnail run adds its processor and wall time to its job's totals (`media.Usage`); every 5 minutes
  with load, a "load" line (`cmd/vitrine/load.go`): Vitrine's own cores (Go's runtime/metrics: its code + GC, and the
  GC's share), the tools' cores and per job "thumbnail: 120 in 45s cpu (60 on the GPU)". Each preview frame sheet:
  a debug line with its time, processor time and GPU or not. `vitrine profile` records Go's CPU (or heap, goroutine)
  profile through the local socket only (`/debug/pprof/...`, never the network port) for `go tool pprof`. Seen first
  on a 571k-file library while importing ~2 files a second: Vitrine's own code 1-2.3 cores (to be profiled), ffmpeg
  bursts of up to 3.3 cores.
- CLI: `vitrine import` makes thumbnails right after importing (`-no-thumbs` to skip); `vitrine process` catches
  up; `vitrine search` and `vitrine tag` exist for trying the library before the web client.
- A user-typed `ns:tag` only uses a namespace that already exists (plugins declare them); otherwise the whole text is
  one tag (`creator:x` with no `creator` namespace stays literal). The web client should offer "make `ns` a
  namespace" for such tags.
