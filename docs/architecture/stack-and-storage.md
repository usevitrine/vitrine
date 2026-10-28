# Tech stack, performance and storage

The stack (Go, Svelte, SQLite, ffmpeg/libvips), the speed targets and how they're met, and where everything is stored (originals, meta.db, thumbs.db, the in-memory index).

## Tech stack (decided 2026-10-14)
- **Server: Go**, headless; the built web client is embedded (`embed`), so one binary serves everything. Other
  clients (CLI, apps) can use the same API later.
- **Pure Go, no cgo** - keeps static cross-compiling for every OS/arch easy.
- **SQLite driver: modernc.org/sqlite** (pure Go). Slower than the cgo driver (mattn) on raw queries, which doesn't
  matter because search runs from RAM. Fallback if a benchmark disappoints: github.com/ncruces/go-sqlite3 (no cgo).
- **Images: libvips command-line tools** (`vipsthumbnail` / `vips`), run as separate processes: fastest,
  shrink-on-load (decodes huge JPEGs at reduced size, streams big images instead of holding them whole - a 500 MP
  image would need ~2 GB RAM fully decoded), encodes WebP/AVIF (Go's standard library can't). The ~20 ms process
  start is irrelevant at download speed.
- **Video: ffmpeg/ffprobe** (thumbnails, preview frames, metadata, codec detection, transcodes).
- **Web client: Svelte 5** (5.57 in Oct 2026; plain SPA built with Vite, no SvelteKit server) - compiled, tiny
  runtime, fine-grained reactivity, bigger ecosystem than SolidJS. Check versions/docs when coding starts.
- **Plugin scripts: JavaScript via goja** (pure Go) - see `docs/downloader/plugins.md`.
- **Distribution - Docker first-class, native equally supported**:
  - Docker: official multi-arch image (amd64 + arm64), ready `compose.yaml` example (incl. an optional
    VPN-container variant), config via env vars and/or a mounted file, data in one volume (`/data`), non-root
    (PUID/PGID), healthcheck endpoint, clean shutdown on SIGTERM (finish or park the current file). ffmpeg + libvips
    bundled.
  - Native: binaries for Linux, Windows, macOS (amd64 + arm64), config in the OS's usual config dir, optional service
    install (systemd unit / Windows service); ffmpeg/libvips found on PATH or downloaded on first run.
  - One code path for both; CI builds and tests both.
- **A VPN is optional** - an opt-in egress check ("refuse to download unless the public IP is ...").

## Performance plan ("lightning fast" - the top requirement)
Targets, held from the first prototype with benchmarks in CI: **search < 20 ms server-side at 1M files**,
autocomplete < 10 ms, thumbnail grid at 60 fps on an iPhone, first screen < 1 s on a phone over LAN, web client
JS < ~150 KB gzipped.

**Speed experiment results (step 0, 2026-10-14, `spike/indexbench`, Go 1.27, roaring v1.9.4, a desktop PC,
16 threads; synthetic Zipf-distributed tags, 55 tags per file, times per search incl. picking the first 1000 results):**

| | 100k files / 85k tags / 5.5M links | 1M files / 300k tags / 55M links |
|---|---|---|
| Index build (from in-memory data) | 0.6 s | 5.5 s |
| Memory (Go heap) | 35 MB (tag bitmaps 9 MB) | 349 MB (tag bitmaps 90 MB) |
| Snapshot of tag bitmaps: size / load | 10 MB / 13 ms | 111 MB / 159 ms |
| Typical searches (tags, AND/NOT, OR group, parent + audio + last 5 days) | < 0.1 ms | 0.05-0.3 ms |
| Popular tag, first 1000 by size | 0.05-0.09 ms | 0.08-0.13 ms |
| Popular tag, **whole** result sorted by size | 0.9-1.0 ms (31k results) | **15-18 ms** (292k results) |
| Whole result as a newest-first ID list | 0.05-0.2 ms | 0.4-0.8 ms |
| Wildcard `*xyz*` as a search term | 0.15-0.33 ms | 2.4-3.7 ms |
| Autocomplete (1-letter prefix = worst case) | 0.18-0.25 ms | 0.8-1.1 ms |

Verdict: **targets met with a wide margin** (search < 20 ms at 1M, autocomplete < 10 ms); the design holds. The only
case near the limit is sorting a huge result by a non-ID field at 1M files - improve before the real build: turn the
result into a dense bitset (125 KB per 1M files) before scanning the presorted array instead of per-file roaring
`Contains`, or only sort as far as the client has scrolled. (Windows' clock ticks in ~0.5-1 ms steps, so the
experiment times batches of 20 runs; "0 ns" in its output = under ~25 µs per search.)

Server:
- **Search index in memory**: per-tag file sets as **roaring bitmaps** (AND/OR/NOT = bitmap ops) plus column arrays
  per file for filters and sorting (import time, modified time, size, width/height, duration, has-audio, filetype,
  site rating, tag count). Built at start-up, updated incrementally.
- **Parents/siblings in the index**: siblings resolved to the ideal tag at write time; parents as implied tags -
  "effective" bitmaps (own tags + all ancestors) so a parent search is one bitmap read; recompute only affected tags
  when a relation changes; reject cycles.
- **Autocomplete**: sorted tag table with counts (prefix = binary search); wildcards (`*sky*`) via a trigram index.
- **OR groups** are bitmap unions - about the cost of one tag.
- **Presorted ID arrays per sort order** - a 1M-file search + sort is a filter over an already sorted list.
- **Result lists held server-side** under a result ID (1M IDs = 4 MB; small LRU cache), clients fetch slices by
  position - see `docs/ui/grid-and-search.md` "Browsing large results".
- **SQLite only as the durable store** (WAL, one writer goroutine, batched transactions) - never queried for search.
- **HTTP**: net/http, HTTP/2, range requests for video, sendfile for originals; hash-addressed URLs with
  `Cache-Control: immutable` for files and thumbnails.
- **API**: JSON; search results as compact ID lists (delta-encoded + compressed when sent whole); batched metadata
  fetches; live updates (downloads, imports) over SSE or WebSocket.

Web client:
- Svelte 5, Vite; **virtualised thumbnail grid** (only visible tiles in the DOM) with a custom fast scroller;
  prefetch ahead of scroll/swipe; **service worker** caching immutable thumbnails; metadata cached in IndexedDB;
  optimistic UI for edits.

## Storage design (agreed 2026-10-14; details tunable by benchmarks)
Three tiers (all paths configurable): **originals** on bulk storage (HDD), **database + thumbnails + preview data** on
fast storage (SSD), **search index** in RAM.
- **IDs**: every file gets a dense, sequential `uint32` ID at import (never reused). Bitmaps and column arrays index
  straight into it; ID order = import order (newest-first sort for free). SHA-256 is for addressing/dedupe; the md5 is
  stored too (sites give it before download).
- **Originals**: content-addressed `files/<first 2 hex of sha256>/<sha256>.<ext>`, immutable, never modified (the
  hash must not change). Written to a temp file on the same filesystem, fsynced, atomically renamed - a half-written
  file never has a real name. SHA-256 computed while downloading.
- **Metadata DB** `meta.db` (SQLite, WAL, `synchronous=NORMAL`, mmap), integer keys, strings stored once:
  - `files(id PK, sha256 BLOB UNIQUE, md5 BLOB, size, type, codec, width, height, duration_ms, frames, has_audio,
    imported_at, modified_at, deleted_at, site_rating, flags, placeholder)`
  - `tags(id PK, namespace_id, name, UNIQUE(namespace_id, name))`, `namespaces(id, name)` - site tags verbatim; a
    namespace only when the plugin maps one
  - `file_tags(file_id, tag_id, origin)`, `PRIMARY KEY (file_id, tag_id, origin) WITHOUT ROWID` - origin = `user` or a
    site ID; the reverse direction (tag -> files) lives only in RAM; a file has a tag if any origin gives it
  - `hidden_tags(file_id, tag_id)` - site tags the user removed (a re-sync doesn't bring them back)
  - `tag_parents(child_id, parent_id)`, `tag_siblings(from_id, to_id)`
  - `file_urls(file_id, kind {post, source, file}, site_id, url_id)`, `urls(id, url UNIQUE)`
  - `tombstones(sha256 PK, md5, deleted_at, reason)` - never download again
  - downloader: searches, subscriptions, groups, cursors, "seen posts" (site + post ID), queue, tag-refresh schedule
- **Thumbnails** in their own SQLite file `thumbs.db` (BLOB per file ID, ~15-25 KB WebP; ~2 GB per 100k files, fits
  in the page cache). Small blobs read faster from SQLite than from individual files (SQLite's own "35% faster than
  the filesystem" measurement), no inode sprawl. A tiny placeholder (blurhash / dominant color) in `files` ships with
  search results so the grid paints instantly.
- **Video preview data**: per video a sprite sheet of ~10-20 frames (one WebP; for scrub/hover previews) and
  optionally a short low-res clip, in `previews/` on SSD, made at import with ffmpeg.
- **Renditions cache** (huge images resized for phones, transcodes, fast-start remuxes): on-demand directory on SSD
  with a size cap and LRU eviction - regenerable, never backed up.
- **In-memory index**: built from `meta.db` at start or loaded from a snapshot file (checked against a DB generation
  counter) for instant start-up. At ~100k files / 85k tags / 5M mappings: tens of MB, microsecond-to-millisecond
  searches.
- **Write order**: original (temp -> fsync -> rename) -> DB transaction -> index -> notify clients. On start, originals
  without a DB row (crash between steps) are cleaned. Delete: `deleted_at` + tombstone first; the original is purged
  after the undo window.
- **Backups**: `meta.db` (online backup API / `VACUUM INTO`) + config + originals; thumbnails, previews, renditions and
  the index snapshot are regenerable. A consistent backup command is a feature.
- **Backups as built (2026-10-16, `internal/backup`)**: `VACUUM INTO` from a connection of its own (a WAL snapshot:
  downloads keep writing), gzipped (BestSpeed) to `backup_dir` (default `<data_dir>/backups`) as
  `vitrine-YYYYMMDD-HHMMSS.db.gz`; daily when the newest is 23.5 h old (checked hourly, first 5 min after start;
  setting `backupAuto`, default on), keeping the newest `backupKeep` (default 7). **Each backup is read back
  (2026-10-28)**: unpacked from the file on disk beside it and checked as a restore would (`verify` -> `Check`), before
  older ones are pruned - one that can't be restored is an error (and the daily one's a notice, category errors) and
  never pushes good ones out. Not in a backup: thumbs.db,
  previews, renditions (regenerable), `secrets.toml` (site credentials). Restore never swaps a live database: the
  file (one of ours or an upload, gzipped or plain) is checked - opens read-only, has files/tags/settings,
  `user_version` <= this build, `PRAGMA quick_check` - and staged as `meta.db.restore`; `serve` puts it in place
  before opening anything, keeping the old one (with its WAL) as `meta.db.before-restore`. The web app's Restart
  stops the server (exit 0) so Docker/systemd start it again. CLI: `vitrine backup [-keep N]`, `vitrine restore
  <file>`. The raw upload is the one non-JSON POST: it needs the `X-Vitrine-Upload: 1` header (no CORS, so a
  cross-site page can't send it).
- **Health check (2026-10-28, `library.HealthCheck`, run from `serve`)**: looked at hourly from 15 min after the start.
  Weekly: `PRAGMA quick_check(20)` on meta.db and an `os.Stat` of every original (in the library or comics, not
  deleted); daily: originals read back and hashed against their SHA-256, from where the last day stopped - a 28th of
  the library's bytes (at least 10 GB) at 40 MB/s, so a whole pass takes about four weeks and an hour or less a day
  on the disk that also serves and downloads. State in `meta` ("health": times, the cursor, counts and the first
  1,000 file IDs missing/damaged; a new pass finds damaged ones again, the weekly look missing ones). A file newly
  found: a warning in the log (the warnings' log keeps it for weeks) with its ID and path, and one notice per kind
  (category errors); a database problem: an error and a notice. Settings > Stats has a Health check row (counts in
  red; else how far this round has read). Why: one HDD with no offsite copy of the originals - a failing disk or a
  damaged file is found while the backups still hold a good copy. Test: `TestHealthCheck`.
- **Speed and memory audit (2026-10-28)**, the user: "optimize things for speed and system resources ... without
  changing current functionality". Measured on the main instance mid-download (40k files, 56k tags, 111k seen posts):
  786 MB resident, of which the Go heap ~260 MB and **~405 MB SQLite's own memory** (modernc's allocator, outside Go's
  numbers: 447 blocks of ~1 MB). Every change below was measured before and after; the tests guard them.
  - **`thumbs.db` page cache 16 MB per connection** (`thumbsCacheKB`; meta.db keeps 64 MB, `metaCacheKB` - no evidence
    there): thumbnails are blobs read straight from the file past the cache, except the newest, still in the WAL,
    which every reader copied in. Thumbnails written while 8 connections read them: 154 MB of SQLite memory at 64 MB,
    44 MB at 16, just as fast. Test: `TestCacheSizes`.
  - **`thumbs.db` keeps its 8 readers open** (`SetMaxIdleConns(8)`; Go's default keeps 2): a grid screen asks for
    dozens of thumbnails at once, and each burst opened and set up connections again - 4000 thumbnails in bursts of
    40: 602 connections and 147 ms before, 8 and 69 ms after, the same memory.
  - **Statements prepared once per transaction** in the loops over a post's tags (`library.txStmts`: `lookupTags`,
    `nsOverride`, the tag edits): each tag parsed the same few statements again.
  - **Indexes** (migration 0044): `tags(name)` - a tag new to the library was looked up by name alone, reading every
    tag (nearly every post of a big first download); `seen_posts(file_id)` and `fav_members(file_id)` - a file removed
    for good (the trash emptied) cleared its foreign keys by reading those whole tables (1000 files at 111k seen posts:
    7.9 s under the write lock before, 0.19 s after). Together with the statements: recording a post with 45 tags
    (`BenchmarkSetSitePost`) went from 2.95 ms to ~1.5 ms of database work, more on a big library (the scan grew with it).
  - Found to be fine already: the index (25 MB, 0.6 s to load at 40k files), files streamed (never read whole into
    memory), thumbnails (shrink-on-load, half the cores), the web app's assets compressed once and cached for good,
    every other full-table read rare (start-up, stats, a site wiped, cleanups - checked by logging every distinct
    statement of the test suite and asking SQLite's query plan). Kept on purpose: the fsync of every original before
    its row (crash safety; most of a download's wait), the Downloads page's tag-refresh count read on every change
    (~13 ms at 111k seen posts; cached, it would lag).
- Considered and rejected: an embedded key-value store (Pebble/bbolt) - faster raw writes, but SQLite is fast enough
  when the hot path is in RAM, and its tooling/backups are better.
