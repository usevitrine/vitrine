# Duplicates

The near-duplicate finder (perceptual hashes) and the Duplicates screen.

## Duplicates as built (2026-10-16)
- **Finding**: every file's thumbnail (videos: the frame at 10% of the length) gets a 64-bit DCT perceptual hash
  (`internal/dupes`, own implementation: 32x32 grays, 8x8 lowest frequencies, bit = above the median; flat pictures
  get none). A background job hashes new files within a minute and the existing library in batches of 500, and
  compares each new hash with all live hashed files: alike = both pictures or both videos (length within 3% /
  0.5 s), shape within 5%, at most 10 bits apart (`dupes.MaxStored`). Pairs go into `dupe_pairs` (a < b; status 0 to
  review, 1 kept both). Hash in `files.phash`, flag `FlagPHash` = tried. Migration 0015.
- **Review** (`/duplicates`, in the menu with a count): one pair at a time, newest file first; levels per device:
  same picture (0: the thumbnails hash the same; byte-identical files never pair - they are one file already),
  nearly identical (<= 2), very similar (<= 6, default), similar (<= 10). Flip (tap / Space / arrows; videos play
  muted and keep the moment) or side by side (default on wide screens). Facts per file (size, file size and type,
  length, tags, sites, added) with the better value in green; a suggestion (bigger picture, longer, bigger file of
  the same type, more tags, added first) - none when the colors differ (compared on the 4x4 color previews): then
  "maybe a variant" and Keep both is highlighted. Keys: A/1, B/2, K/3, S skip, Z undo. No notice per choice: an
  Undo button (next to Skip, also on the "all done" screen) takes back the last choice only.
- **Keeping one** copies the other's visible tags (same origin), links and download sources onto it, takes the
  earlier posting date, and deletes the other (tombstone: never downloaded again; the usual undo window). What was
  copied is stored with the pair (`dupe_pairs.merged`), so Undo removes exactly that and brings the file back.
  Pairs of a deleted file just stop being listed.
- **A tag's namespace, set by the user (2026-10-19)**: `Library.SetNamespace` (`POST /api/tags/namespace {tag,
  namespace}`; "" = general) moves a tag to another namespace on every file, from sites and the user alike. No tag
  of that name there: it changes in place (same ID - files, parents, siblings, hidden marks stay). One there: they
  become one (files and hidden marks move; refused when the moved tag has parents or siblings). `ns_overrides`
  (migration 0017) remembers "what a site calls namespace:name is the user's namespace now": the tag lookup
  (`lookupTags`) follows it, so a tag refresh keeps the user's choice instead of bringing back the site's
  namespace as a second tag (chains follow; moving it back removes the override). The app: "Namespace" in a
  tag's menu in the details (a page of namespaces, the current one ticked) and in each tag's ⋯ menu on All tags;
  the usual namespaces (creator, series, character, meta, general) plus the library's others. No free-text new
  namespace in the app (hardly anyone would; the API takes any [a-z0-9_] name).
- **Shimmie's caret codes (2026-10-19)**: Shimmie2 writes some characters in its tag links as caret codes (`^`
  `/` `\` `?` `&` `.` as `^^` `^s` `^b` `^q` `^a` `^d`), so a post page's tags came in as `bloom_^a_rage` for
  `bloom_&_rage`. The engine decodes them (`decaret`); at start-up `fixCaretTags` renames (or merges) tags saved
  with the letter codes - not `^^`: real tags have carets (`^_^`).

## As built (web client, from 2026-10-14 on)
- **Duplicates screen (2026-10-16)**: a full-height screen (stage, facts cards A blue / B peach, actions), see
  "Duplicates as built" above. Flip on phones, side by side on wide screens (remembered per device).
  The Sort and filters menu now also closes on Escape when nothing in it has focus (e.g. right after a page slid in).
- **Duplicates: videos in step (2026-10-19)**: both videos start together once each can play (once per pair -
  canplay also comes after seeks), then a rAF loop keeps the other on the leading one's clock (drift < 0.3 s: its
  playbackRate nudged by up to 10 %; more: it seeks; play/pause follow). Leader: the one on show in flip mode (the
  hidden one keeps going, so a flip shows the same moment), the longer one side by side. One bar under the pictures
  (play/pause, time, a range input) scrubs both; its length is the leader's.
- **Duplicates open on "Same picture" (2026-10-20)**: the default how-alike level (no choice made on the device yet)
  is Same picture, not Very similar.
- **Duplicates: videos that play (2026-10-22)** (the user: "they often don't even play until I pause and play, and I
  don't even know if swapping between A and B works"): each video is asked to play at once - the first version
  waited for both to report "ready" together, which an iPhone never does (it loads nothing before a play), and the
  bar said Pause meanwhile. The keep-in-step loop seeked the other video every frame while it was loading (each
  seek restarted its loading: it stayed frozen, `seeking`, for good - reproduced at 300 KB/s); now at most one seek
  a second, never while one is in flight, and while either video loads (or seeks) both wait (250 ms of grace: no
  play/pause flutter), a spinner over the stage after 400 ms. A flip seeks only when the hidden one fell behind
  (a seek stalls the picture just shown); the hidden layer is `opacity: 0`, not `visibility: hidden` (still drawn);
  the badge pops on each flip. Each new element joins in at the moment on show from the loop, retried until it
  lands (WebKit sometimes dropped a seek made on loadedmetadata, and the leader's 0 then pulled the other back);
  no syncing until the leader has joined. A refused autoplay shows Play on the bar (its tap plays both). Test: "on a slow
  connection both videos still play" (Chrome's throttling; the old code was seeking in 10 of 10 samples).
- **Duplicates: no endless spinner (2026-10-22)** (the user: "the spinner just spins forever"): while one video
  loaded the other was paused and the loading one only nudged when it had nothing at all - a browser that loads
  little or nothing of a paused video (an iPhone stops after the metadata) left both waiting for good. Now the
  loading one keeps playing (so it keeps loading) and only the ready one waits; a video the browser can't play
  (`error`) takes no part; the wait never lasts more than 4 s (`HOLD_MAX`, a safety net: 1.5 s made the slow one
  seek-thrash again on a slow connection); a new element's join gives up after a second. Not reproduced here
  (desktop browsers load the test videos paused too); the library is ~all H.264 (13.8k of 13.9k videos).
- **Still looking (2026-10-23)**: while the library is being looked through, the progress is a thin bar along the
  header's bottom edge (its numbers in the tooltip and for screen readers) - it takes no room. It used to be a line
  above the pictures that slid open and shut; the pictures (a flexible stage) shrank and grew with it, which showed as
  a kept pair's picture shrinking (the user). The empty card and the video bar fade in place for the same reason.
- **The library follows deletes made elsewhere (2026-10-23, the user: deleted blacklisted files stayed on the media
  page until a reload)**: the library stays loaded behind other pages; the blacklist's clean-up, Duplicates' Keep A /
  B and their Undo now run its search again (`results.changedElsewhere()` - only if it has searched).
