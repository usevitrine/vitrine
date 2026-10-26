# Site plugins

The plugin system: manifests, the HTTP client per plugin (domains, rate limits), credentials, tag namespaces from the site's tag types, installing and updating plugins. Per-site notes live in the plugins repo's docs/sites.md.

## Plugin system (decided)
Not Go's `plugin` package (Linux-only, needs the exact Go version). Two layers:
1. **Declarative manifest** for standard booru engines (gelbooru-v02, danbooru, moebooru, ...), each engine written
   once in Go:
   ```yaml
   id: safebooru
   version: 1.2.0
   engine: gelbooru-v02
   base: https://safebooru.org
   auth: {fields: [api_key, user_id]}
   limits: {requests_per_sec: 1, connections: 1}
   search: {per_page: 100, cursor: page}
   tags: {namespaces: {artist: creator, character: character, copyright: series}}
   ```
2. **Sandboxed script** for odd sites: **JavaScript run by goja** (pure Go). Only Vitrine's own helpers are exposed
   (HTTP to the domains the manifest lists, HTML parsing, JSON); no `require`, no file access, CPU/time limits per
   call. (Starlark was the alternative: safer by design but niche.)
- **Plugins live in their own repo**: `usevitrine/vitrine-plugins`, which doubles as the default plugin index (an
  index file listing plugins, versions, checksums); users can add other index URLs. The core repo has the engines and
  one neutral example plugin. Separate repo = a plugin can be fixed the day a site changes, without a Vitrine
  release, and NSFW site plugins stay out of the core repo.
- Install from a URL, file or index; versioned, update check with changelog; permissions (domains) shown on install;
  consider signing. Each plugin ships a **self-test** (e.g. "tag X returns >= 1 post") so a site change shows up as a
  failing plugin.
- License: plugin SDK, example plugins and manifests are MIT.

## Site facts
Per-site notes (APIs, rate limits, quirks) live with the plugins in the `vitrine-plugins` repo (`docs/sites.md`).
Request URLs to sites with API keys carry the key - never log or print them unfiltered.

## As built (from step 1, 2026-10-14 on)
- **Plugins** (`internal/plugin`): YAML manifests (`id`, `name`, `version`, `engine`, `base`, `post_url` with `{id}`,
  `domains`, `auth {fields, required, help}`, `limits {requests_per_sec, connections}`, `search {per_page}`,
  `ratings` site value -> general/sensitive/questionable/explicit); unknown keys are errors. Built in: the neutral
  example **safebooru** (gelbooru 0.2, SFW, no account) - also the live test target. User plugins: `.yaml` files (or
  `*/plugin.yaml`) in `<data_dir>/plugins`, overriding built-ins with the same ID. The plugins repo
  (`usevitrine/vitrine-plugins`, MIT) holds the site plugins.
- **HTTP client per plugin**: only the plugin's domains (and their subdomains), http(s) only; API requests spaced by
  `requests_per_sec`; file downloads capped at `connections`; `User-Agent: Vitrine/<version>
  (+https://github.com/usevitrine/vitrine; hello@vitrine.software)` (a site's admins can write to the project). Errors never contain request URLs (they carry the api_key): Go's
  `url.Error` is unwrapped to its cause.
- **Credentials**: `<data_dir>/secrets.toml` (owner-only, atomic writes), one table per site; set with `vitrine
  credentials <site>` (typed input hidden). Never in meta.db or logs.
- **Tag namespaces (2026-10-14)**: plugins declare `tags: {types: tag_info, namespaces:
  {artist: creator, character: character, copyright: series, metadata: meta}}` (Hydrus-style names). The
  gelbooru-v02 engine then asks the JSON API for the same page with `fields=tag_info` (one extra request per page;
  the XML API has no types, the JSON one has no upload time or total) - checked live on two gelbooru 0.2 sites. If that
  request fails the page still downloads without types. Types are saved per site in `tag_types` (migration `0007`).
  A site-typed tag **takes over the plain tag of the same name** (same tag ID: its files, parents, siblings and user
  tags all become `creator:some_artist`); a plain name typed or sent without a type resolves to the single namespaced tag
  of that name, so no duplicate plain tag appears. The plugins' namespaces are created at start-up. **Older files**:
  while a site's queue is idle the downloader looks up, one at a time, the post with the most site tags of unknown
  type (`id:N`, 2 requests) and writes it again with namespaces (also a tag refresh); tags of posts the site no
  longer has are marked looked up (`''`). Sites like this have no batch tag lookup (`names=` is ignored) and no OR of IDs,
  so this greedy per-post lookup is the cheapest. API `GET /api/namespaces`.
- **Rate limits (2026-10-14)**: back-to-back lookups (1 request/s, nonstop) got `429 Too Many Requests` from a site
  after about two minutes. Now: the background lookups pause 6 s between posts and stop for 15 minutes after an
  error; and any 429 makes every request to that site wait (`Retry-After`, at most 10 min, else 1 min) and the API
  request is tried again (up to 3 times), so a search page doesn't fail on one 429.
- **Plugin install/update in Settings (built 2026-10-15)**: Settings > Sites > "Add site" lists the plugins in the
  plugin index that aren't installed (name, address, version, description, an "adult" badge) with Install; an
  installed site with a newer version in the index shows "Version X is available" + Update; "Install from file"
  takes a `.yaml` manifest. All without a restart: `Downloader.InstallPlugin` parses the manifest, writes
  `<plugins>/<id>.yaml` (temp file + rename), builds the site (credentials from secrets.toml) and swaps it into the
  site map (now an atomically replaced copy - `all()` / `putSite`); each queue reads its site per search, so a running
  search finishes with the old version; a new site gets its worker at once. Index: config `plugin_index`
  (`VITRINE_PLUGIN_INDEX`; an https URL or a local file/folder with `index.json`; default
  `https://raw.githubusercontent.com/usevitrine/vitrine-plugins/main/index.json`), cached 10 min, entries
  `{id, name, site, version, file, description, kind, nsfw}` with `file` relative to the index (`kind`: booru,
  gallery or social - Add site's filters; optional, others only under All); versions compared
  numerically per dot part. API: `GET /api/plugins[?refresh=1]` (an unreadable index is reported in `error`, not a
  failure), `POST /api/plugins/install {id}`, `POST /api/plugins/upload {yaml}`. The plugins repo has `index.json`
  (bump an entry's version with the plugin's). **Remove** (2026-10-15): "Remove site" on an installed site, with a confirmation naming its subscriptions/downloads; deletes only the plugin file - files, tags, history, searches, subscriptions and credentials stay, and its worker stops (per-site context; a running search resumes when it is reinstalled) - unless the user then chooses to erase every trace of it (2026-10-26: `POST .../remove {wipe, backups}`, see `docs/library/model.md`, "Wiping a removed site"). A file replacing a built-in plugin offers "Use the built-in version" instead; built-in sites have no Remove. `POST /api/plugins/{id}/remove`.
