# Background

## Prior art - study, don't copy
- **Stash** (https://github.com/stashapp/stash, ~13k stars, Go + web UI, AGPL-3.0, active): self-hosted organizer for
  adult media - scans existing library folders, tags, performers/studios/scenes, metadata "scrapers" in a separate
  community repo (the same plugin model planned here). Video/scene-centric, built around scraping metadata for files
  you already have; no booru-style tag searches/subscriptions that download. Worth studying for its scraper/plugin
  system, UI and performance.
- **monbooru** (https://github.com/leqwin/monbooru, Go, AGPL-3.0, active - v1.14 in July 2026): self-hosted booru for
  local collections - single Go binary with embedded assets, SQLite per gallery, aliases + implications (= siblings +
  parents), wildcard/OR/exclusion search with system filters, batch tagging, inbox, duplicates review, REST API with
  OpenAPI, Hydrus export import, Docker; downloading via a separate companion (monloader) using gallery-dl.
  Differences: server-rendered UI (not a phone-first app), Linux-only, aimed at local folders and AI images, no native
  downloader. Vitrine is built anyway; monbooru is a reference only.
- **Hydrus** (https://github.com/hydrusnetwork/hydrus) and **hydrus-web** (https://github.com/floogulinc/hydrus-web,
  Angular + PhotoSwipe): the main thing Vitrine is an alternative to. Hydrus Client API docs
  (https://hydrusnetwork.github.io/hydrus/developer_api.html) are useful for search-predicate ideas.
- **gallery-dl** (https://github.com/mikf/gallery-dl - `gallery_dl/extractor/gelbooru_v02.py`;
  range/skip logic in `job.py`) and **hydownloader** (https://gitgud.io/thatfuckingbird/hydownloader - queue and
  import design): download tools Vitrine learns from.

## Libraries
- Roaring bitmaps: https://github.com/RoaringBitmap/roaring
- SQLite: modernc.org/sqlite (decided); fallback github.com/ncruces/go-sqlite3; not mattn/go-sqlite3 (cgo)
- Images: libvips CLI (https://www.libvips.org); video: ffmpeg
- Plugin scripts: goja https://github.com/dop251/goja (alternative considered: starlark-go)
- Web Push: github.com/SherClockHolmes/webpush-go
- Web client: Svelte 5 (https://svelte.dev), Vite

## Name
**Vitrine** ("vi-TREEN"; a glass display cabinet for showing a collection), chosen 2026-10-14. Earlier names:
"Lerna" (dropped: clashes with a very popular JavaScript monorepo tool) and "Iolaus" (Hydra myth; dropped once the
project grew from a Hydrus downloader into a whole library). Checked 2026-10-14: no notable software project is
called Vitrine (biggest same-named GitHub repo: 96 stars). GitHub `vitrine`/`vitrinehq`, npm `vitrine`/`@vitrine` are
taken (empty accounts / unrelated); **`getvitrine` is free on GitHub, Docker Hub and npm and is the chosen org name**
(`usevitrine` as backup). Plan: GitHub org `getvitrine` (repos `vitrine`, `vitrine-plugins`), images
`ghcr.io/getvitrine/vitrine` (+ optional Docker Hub mirror), npm scope `@getvitrine` only if the plugin SDK is
published; the command is `vitrine`. **GitHub org `getvitrine` created 2026-10-14** (https://github.com/getvitrine; repos `vitrine` and
`vitrine-plugins`, private until the first public release) and **Docker Hub `getvitrine`** (a free personal account - Docker Hub orgs need a paid plan;
names are shared between users and orgs, so it reserves the namespace for `getvitrine/vitrine`). Publishing later:
a Docker Hub access token stored as a secret in the GitHub org, never in the repo. Other names considered: Bowerbird (clear), Wunderkammer, Corvid; avoid Bower,
Burrow, Curio, Cairn, Trove, Hoard, Stash, Magpie (notable projects).

**Moved to `usevitrine` (2026-10-20)**: the project's home became a GitHub user account, `usevitrine` (the earlier
backup name), with the website and email on `vitrine.software`; the `getvitrine` org goes once the repos have moved.
Module path, User-Agent, Web Push contact, plugin index and image names all use `usevitrine` from then on.
**2026-10-23**: the `getvitrine` GitHub org and its repos are deleted, and the Docker Hub `getvitrine` account is gone too.
Images: `ghcr.io/usevitrine/vitrine`; Docker Hub `usevitrine` was still unclaimed then (an optional mirror at v1).
Contact: hello@vitrine.software (Web Push subject, User-Agent).

## License
AGPL-3.0-or-later for the server and web client (a network service: hosted modified versions must share their
changes - same choice as Stash, Immich and monbooru); MIT for the plugin SDK, example plugins and plugin manifests,
so plugin authors aren't bound by the AGPL.
