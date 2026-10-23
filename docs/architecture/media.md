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
  workers). Not done yet: video preview sprite sheets, hardware decoding (VA-API), animated previews.
- CLI: `vitrine import` makes thumbnails right after importing (`-no-thumbs` to skip); `vitrine process` catches
  up; `vitrine search` and `vitrine tag` exist for trying the library before the web client.
- A user-typed `ns:tag` only uses a namespace that already exists (plugins declare them); otherwise the whole text is
  one tag (`creator:x` with no `creator` namespace stays literal). The web client should offer "make `ns` a
  namespace" for such tags.
