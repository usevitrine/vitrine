# Engines

The site engines: gelbooru-v02 (API) and gelbooru-v02-html, Shimmie2, Mastodon, Danbooru, and the ehentai gallery engine (comics).

## Shimmie2 engine (2026-10-16)
- `engine: shimmie2` reads a Shimmie2 site's web pages (many have no API on). List pages `/post/list/<tags>/1`: tags
  joined with `+`, **`order=id_desc` always added** (some sites mix older posts in otherwise; a search with its own
  `order=` keeps it), `id<N` to resume; an empty result is a 404 page. Thumbnail links give `data-post-id`,
  `data-tags`, width/height and the md5 (`/_thumbs/<md5>/`), so seen posts are skipped unopened. Post pages
  (`/post/view/<id>`): the file from the Download form (else the video source / main image), md5 from `/_images/<md5>/`,
  `a.tag_name` tags (lower-cased; no tag types), source links, rating (mapped by the manifest), upload time.
- **Login** (`auth: {fields: [username, password], login: shimmie2}`): the engine posts the site's login form
  (`/user_admin/login`, `user`/`pass`) when credentials are set and keeps the session (`shm_session` cookie, a
  per-client cookie jar that empties when the credentials change); it signs in again when the session is gone.
  Credentials of a `login:` plugin never go into request URLs. A refused login: "the site didn't accept the user name
  and password". Settings labels the fields User name (shown as typed) and Password.
- No OR search on Shimmie2 sites seen so far (`~a ~b`, `a || b` find nothing).

## Mastodon engine (2026-10-16)
- `engine: mastodon` uses a Mastodon server's API. Searches: `#tag` (the hashtag timeline), `#tag #other` (both:
  `all[]`), `#tag ~#other` (either: `any[]`), `#tag -#other` (without: `none[]`), `@name` (a profile's own media posts,
  `exclude_reblogs`), `@name #tag` (`tagged=`, further tags checked locally); pasted `/tags/<tag>` and `/@name` URLs
  work; a word without `#` is a hashtag too. Always `only_media=true`, 40 statuses a page, `max_id` paging; pages whose posts are all filtered out are
  passed over (up to 10 requests) so an empty page means the end.
- Each image / gifv / video of a status is one post: **ID = status ID * 10 + its place** (Mastodon allows 4; status IDs
  ~1.2e17 leave room); resuming below ID N asks `max_id=N/10`. Tags: the hashtags (lower-cased) + the author's
  `acct` (lower-cased) with tag type `artist` (the plugin maps it to `creator`); rating from `sensitive`; the post's
  own URL; no md5 (files are recognized after download by SHA-256, as usual). Lookup reads `/api/v1/statuses/<id>`.
- Guests: hashtag timelines get `local=true` (many servers show only their own users' posts to guests). An optional
  access token (`auth: {fields: [access_token], login: bearer}`, sent as `Authorization: Bearer`, never in URLs) lifts
  that.

- **`search.hint`** (2026-10-16): a plugin can replace the new-download box's placeholder for its own search syntax
  (`/api/sites` `search_hint`; e.g. a Mastodon server: "#tag or @profile, e.g. #art #painting -#ai" - keep it short, it must fit a
  phone). The Downloads screen reloads site names and hints when it opens (sites installed meanwhile).
- **`search.tag_query`** (2026-10-19): how a library tag becomes the site's search, for a tag's "New download" (from
  the details or a search chip): a namespace, or `"*"` for every other tag, mapped to a template with `{tag}` (the tag
  without its namespace), e.g. `{creator: "@{tag}", "*": "#{tag}"}`; a template without `{tag}` is refused. Engines
  have defaults (`plugin.TagQueries`): Mastodon `@{tag}` for creators and `#{tag}` for the rest, every other engine
  the plain tag - so a plugin only sets it for a site with its own rules. `/api/sites` sends the result as
  `tag_query`; the New download form writes the tag the chosen site's way, and again when another site is picked
  before the search was changed by hand.
- **`search.or`** (2026-10-20): how the site searches for posts with any of several tags, if it can - a template with
  `{tags}`, the tags (each written the `tag_query` way) joined by `search.or_sep` (default `" ~ "`); e.g. a Gelbooru 0.2 site's
  `or: "( {tags} )"` gives `( a ~ b ~ c )`. A template without `{tags}` is refused; no `or`: the site has no OR
  search - except the Mastodon engine's own "either" hashtags, `~{tags}` joined by ` ~` (`~#a ~#b`; hashtags only:
  with a tag written `@name` there's no Any). A Shimmie2 site checked 2026-10-20 has none: ten forms tried
  (`~a ~b`, `a OR b`, `(a OR b)`, `a|b`, `{a,b}`, `or:a or:b`, `( a ~ b )`, ...) all answered 404, and its search
  help lists no OR. `/api/sites` sends it as `or_query`/`or_sep`; the details' picked tags offer Download > Any of them
  (`tagsQuery` in lib/sites); a site without one gets a search for each combination (`splitSearches` in lib/siteSearch).
- **Blacklisted-tag notification (2026-10-18)**: when a file already in the library gets a tag the blacklist
  matches - a search rerun or tag refresh updating its site tags, or the same file found on another site - the
  downloader sends `EventBlacklisted` (site + the matching entry), once: only when the file didn't match before
  (`setSitePost` checks the file's tags before and after). New downloads never cause one (they're skipped).
  Push: "A file got a blacklisted tag" / "N files got blacklisted tags", the entries with counts, opens Tags > Tag
  rules (the blacklist card shows them with Show them / Delete them). Setting `notifyBlacklisted` (on unless off).
- **Sites off / built-in removable (2026-10-18)**: Settings > Sites, a site's ⋯ menu: **Switch off** keeps the site
  but nothing runs for it - no new downloads (`Add` refuses, the new-download picker leaves it out), no subscription
  checks, no tag refreshes; its worker stops (a running search resumes where it was) and its queue/subscriptions show
  "Off" on the Downloads page. **Switch on** starts it again (due checks start). `POST /api/sites/{id}/off {off}`;
  kept in the settings table as `sitesOff`. **Remove site** works for the built-in safebooru too: remembered as
  `removedSites` (not loaded at start-up) until it's installed again - it's in the plugins repo's index for that.
- **Source links only (2026-10-19)**: a post's source is free text on the sites - often several links with labels
  ("Twitter: https://... Patreon: https://..."), sometimes words. `plugin.SourceLinks` keeps only the links (a bare
  "x.com/someone" gets https://; a lone domain, labels and words are dropped) for every engine, and the library
  stores only http(s) sources. Migration 0016 removed the non-link sources saved before (over a thousand in a real library
  - backed up first).
- **HTML codes in tag names (2026-10-19)**: some Gelbooru 0.2 APIs send some tags with HTML character codes ("kai&#039;sa"
  for "kai'sa", "&lt;" / "&gt;"); `plugin.Tags` / `CleanTag` decode them (the gelbooru engine's tags and tag
  types). At start-up `library.fixEscapedTags` repairs tags saved before: renamed, or merged into the tag with the
  right name (files, hidden marks, parents, siblings move; tag types too) - hundreds of such tags in a real library,
  159 of them merged.
- **Stopping a search whose site hangs (2026-10-19)**: a running search's page request has its own context;
  pausing or removing the search cancels it at once (`Downloader.stop` / `trackPage`), so the site's queue goes on
  straight away instead of waiting up to a minute for the request to time out. A canceled request counts as
  stopped, not failed. Files already downloading still finish (as before).

## Danbooru engine (2026-10-20)

- `engine: danbooru` (`internal/plugin/danbooru.go`): danbooru.donmai.us and the sites running the same software,
  through the JSON API. `/posts.json?tags=&limit=` (up to 200), newest first, resumed with the site's own
  `page=b<ID>` (posts before that one: new uploads can't shift pages). The total comes from `/counts/posts.json`
  (first page only, best effort). Lookup: `/posts/<id>.json` (404 = gone).
- Tags with their types in every post (`tag_string_artist`, `_copyright`, `_character`, `_meta`, `_general`) -
  `tags.types: post` (no extra requests); the plugins map artist/copyright/character/meta to creator/series/character/meta.
- Account optional: `login` + `api_key` in the query. Without one the site allows two tags per search (its 422 is
  explained) and hides some files - a post without `file_url` is passed over (a page of only those doesn't end the
  search). OR: `~a ~b` (the engine's default). Favorites: `fav:me` = the login's, `fav:<name>` anyone's - the site's
  own fav: search, newest post first (so the favorites-removed check works as for the others).
- Plugins (in the plugins repo): `danbooru` (adult content) and `safebooru-donmai` (Danbooru's safe-for-work mirror,
  rated general only). `TestDanbooruLive` (VITRINE_LIVE=1) asks safebooru.donmai.us: search, the next page, a lookup -
  passed 2026-10-20.

## Gallery sites: the ehentai engine (2026-10-20)
- Engine `ehentai` (plugins in the plugins repo: e-hentai.org, exhentai.org): each search result is a **gallery** post
  (`plugin.Post.Gallery`: token, titles, category, page count, `Key` = the first version's ID) and comes in as a
  **comic** (Comics page), never as library files. Search: the site's own syntax, newest first, resuming with the
  list's `next=<gid>` cursor; details for 25 galleries per call from the JSON API (`gdata`, all tags with their
  namespaces - stored booru-style, `female:big breasts` -> `female:big_breasts`; artist/parody/character map to
  creator/series/character through the plugin's `tags.namespaces`, the rest keep the site's names). A **gallery's
  link** as the search = that one gallery, followed to its newest version (the gallery page's "newer versions"); a
  subscription to it brings new versions in - the same comic (key = first version); the pages it has stay and only the
  new ones are fetched, from where it got to (2026-10-20; an ongoing gallery adds pages - before, every page was
  fetched again); a version with fewer pages trims the rest. A gallery is never skipped as "seen": its comic says which
  pages it lacks. **Thumbnails only**: the gallery page's comments can link pages too (an uploader's chapter index) -
  only links in the thumbnail area (`#gdt`) count, each placed by the page number in its link (a chapter index once
  made pages come in out of order and asked for one that doesn't exist: "Invalid page."). **Download again** (a comic's
  details; `POST /api/comics/do/redownload`): its pages cleared (`ClearComicPages`) and a download of the gallery queued.
- Download (`download/gallery.go`): the comic is made or updated (title, tags of the site's origin, page count), then
  its pages in order - the gallery's thumbnail pages list every picture page (streamed: `GalleryEngine.GalleryPages`
  hands each page over as soon as its thumbnail page is read, so pictures download while the rest are listed; a
  stopped download lists from the thumbnail page holding the first page it lacks), each picture page names the picture;
  a picture that fails is asked for again with the page's reload key (another image server). Pages the comic has
  are skipped, so a stopped download carries on where it was. **Speed** (2026-10-20): the picture pages are read
  one after another at the plugin's `requests_per_sec` (1/s: at 2/s ExHentai stopped answering properly after ~55
  pages, likely a ban for loading pages too fast - now `plugin.ErrBanned`, read from the ban's plain-text answer) and read ahead while the pictures download, up to its
  `connections` (3) at once - pictures come from many image servers, not the site; one gallery at a time per search
  (its pages share the connections). Measured before: ~1.4 s a page whatever its size (1 page/s, then the picture,
  one at a time). The downloads list counts a running gallery's pages ("12 of 75 pages", live: `FileProgress.pages/done`),
  its bar follows them, and a gallery site's posts are "galleries" (its new ones "comics"). **Counts by what was asked**
  (2026-10-22, the user: "a gallery: per page, a tag search: per gallery"): a download or subscription of one gallery
  (its link) counts pages - the pages it added are kept (`searches.pages`, `subscriptions.pages`: migration 0036, added
  as pages are stored, so a stopped download keeps its count), shown as "N pages" / "page X of Y" while running /
  "had it all"; a tag search counts galleries (new / had), with the running gallery's "page X of Y" beside them.
- **Faster where it can be (2026-10-23, #105)** (the user: the Android apps "are much faster at loading content" - JHenTai,
  EhViewer: their sources read). Measured from the VPN: a picture page answers in ~0.3 s (the apps' api.php
  `showpage` the same, so not used); 3 vs 6 connections with a look-ahead made no difference (no lookup ever waited for a
  slot, the slowest picture < 3 s) - **the site's 1 page/s is the whole wait** (EhViewer paces the same; JHenTai's 2/s is
  what got ExHentai to ban). The apps feel fast because they show the page being read first. Two changes:
  - **The multi-page viewer** (`/mpv/{gid}/{token}/`, its `var imagelist` JSON - each page's key `k`): every page in
    one request instead of a thumbnail page per 20/40 - only for an account with the site's "Multi-Page Viewer" Hath
    Perk; anyone else gets "eeenope", remembered for a day (one request lost), and the thumbnail pages list them. Not
    tried without an account.
  - **Pages by their keys**: a page link's key (`/s/{key}/{gid}-{n}`) is the start of the original picture's SHA-1 - the
    same in every gallery and on both sites (EhViewer reuses an older version's pages by it). Kept on each comic page
    (`comic_pages.site_key`, migration 0038; `GalleryPages` hands it over with each page): a page whose key the library has
    - in this comic (moved in a newer version) or another comic - is linked as it is (`LinkComicPage`), neither its page
    nor its picture asked for. A newer version is checked page by page by key (pages added in the middle, or changed,
    land right - before, only by page number); fewer pages: the rest go. Not the stored file's own hash: the site often
    serves a resized picture. Pages from before the keys get theirs on the next check of the gallery.
- **The image limit**: a picture page serving the "509" picture -> `plugin.ErrImageLimit`, wrapped in
  `errStopSearch`: the search stops as failed (retried later) **without recording the gallery**, so the resume point
  stays before it. Same for a refused login, ExHentai's blank "sad panda" page (`ErrSadPanda`: no access) and a
  blocked country (`ErrRegionBlocked`: e-hentai answers 451 to the United Kingdom - Online Safety Act).
  Logged in, the account's own limit applies (it can be raised on the site; Vitrine never spends the account's
  points).
- **Login** (`auth.login: ehentai`, fields `username, password, ipb_member_id, ipb_pass_hash` - either pair): the
  e-hentai forum's login form; if it asks for a captcha, the two cookies pasted from a browser. The cookies (and
  `nw=1`: no content-warning page) go to e-hentai.org, its API and forum hosts, and exhentai.org.
- **Tag suggestions**: a gallery site's tags are the comics' only - never in the library's suggestions or namespace
  lists (`Library.Namespaces` leaves out namespaces only comics use). The Comics page's search and New download for
  a gallery site suggest from them (`GET /api/comics/tags?q=&site=`); a pick is written the site's way
  (`tag_query` templates now take `{name}` - spaces for underscores - and `{ns}`; the engine's default:
  `artist:"name$"`, `parody:"name$"`, `{ns}:"name$"`).

## As built (from step 1, 2026-10-14 on)
- **gelbooru-v02 engine**: XML dapi (`page=dapi&s=post&q=index&limit=<per_page>&pid=0&tags=...`), reads `count`,
  `id`, `md5`, `file_url`, `tags`, `source` (split on spaces), `rating`, size. Resume = the query plus `id:<low_id`.
  Tags are stored verbatim (namespaces from tag types: see "Tag namespaces" below; plain-name search finds
  any namespace).
- **2026-10-14**: the engine stores each post's upload time (`created_at`). A post the downloader has seen before is
  no longer skipped silently: its tags, links, rating and upload time are written again (`SetSitePost`), so running a
  search again works as a simple tag refresh and fills in upload times for files downloaded before this.
- **The gelbooru-v02-html engine (2026-10-15)**: for Gelbooru 0.2 sites that switched their API off; instead of the planned JavaScript plugin layer a
  Go engine reads Gelbooru 0.2 web pages - reusable for any Gelbooru site without an API; the scripted layer stays
  planned for genuinely odd sites. **List pages** (`page=post&s=list&tags=...&pid=0`, 42 posts; resume with the
  site's `id:<N` search, which works there) give each post's ID and md5 - the thumbnail is named
  `thumbnail_<md5>.jpg` - as *partial* posts: the downloader skips seen posts and deleted files without opening
  them, and opens a post page (`page=post&s=view&id=N`) only to download it or to attach its tags to a file the
  library has. **Post pages**: tags from `<a class="TYPE" href="index.php?page=post&amp;s=list&amp;tags=NAME">`
  (a type per class, e.g. `tag-type-general`, `artist`, `character`, `copyright`, `metadata`),
  the file from the video's `<source>`s (mp4 first, webm as fallback - some mp4s are missing or empty; then the
  "Original" link, if it's a video) or the "Original" link (an image's original, not its sample) with the image shown on
  the page as the fallback (2026-10-20: the Original link sometimes answers 404 while the shown file is there), the md5 from the original's file name, the source from the
  edit form's `source` field, the upload date from "Posted at Oct, 14 2026" (day only) - given its moment by the downloaded file's
  `Last-Modified` when that falls on the same day (2026-10-21, any engine: `refinePosted`; realbooru keeps it as the
  upload time, checked on posts from 2019 to 2026, images and videos). A day-only time read again later (a tag refresh)
  leaves a moment on that day as it is (`SetSitePost`). Files downloaded before that get theirs from a one-off pass in the background work
  (`FixPosted`, before the tag refresh, only while the queue is idle): each file still at midnight on a
  gelbooru-v02-html site has its file URL asked for the headers only (`Client.Modified`: its turn, a file slot,
  dropped once the headers are in); progress in meta `posted_fix:<site>` (last file ID, then "done"). **What it can't fix** (measured
  2026-10-23 on realbooru: 8,702 of 10,009 files got their moment): files posted before 30 June 2020 all answer
  Last-Modified = that day (the site re-saved its files then), so the day check rightly refuses it; ~1,300 stay day-only.
  The index sorts a day-only file by its post ID within that day (`index.postedKey`: midnight + post ID ms; the stored
  date unchanged - the user's choice over estimated times). No rating on the pages:
  the manifest's `ratings` maps the empty rating. Total = the last pagination `pid` plus a page.
  **Hotlink protection**: file URLs redirect to `hotlink.php` (an HTML page) unless the request carries the site's
  Referer - manifests can set `referer:`; the client now refuses `text/html` where a file is expected (never
  imported) and refuses redirects off the plugin's domains. Tag types `page` (read from post pages) feed namespaces
  through the manifest. Live-tested 2026-10-15 (3 newest posts in 5 s). Per-site details: the plugins repo.
  **`login: gelbooru`** (2026-10-21): an account (user name, password) signs in through the Gelbooru 0.2 login form
  (`page=account&s=login&code=00`), the session cookies kept (`user_id`, `pass_hash`; again when they run out);
  `fav:me` is the signed-in user (no user ID to type). realbooru 1.1.0 uses it (optional).
  **`file_query:`** (2026-10-21): files are fetched with it as a query (`{id}`: the post), the way the site's pages
  link them; the plain URL is what's kept. rule34 (`"{id}"`): its US Cloudflare cache served broken copies of some
  files under the bare URL - a "HIT" crawling to a stop at the same byte each time, holding the queue up (measured
  through the VPN: the bare URL 2.7 of 5 MB in 40 s; `?<id>` the whole file in 2 s; from London both were fine).
