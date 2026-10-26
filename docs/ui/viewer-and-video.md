# Viewer and video

The viewer (slides, gestures, zoom, open/close animation, details panel) and Vitrine's own video player (controls, scrubbing with preview frames, volume, loops, full screen).

## Video player: Vitrine's own, never the iOS player
- `<video playsinline>` without native controls; custom controls in the app's style: play/pause, mute, a scrub bar
  with preview frames (from the import-time sprite sheet), loop, speed.
- "Fullscreen" = an in-app immersive mode (iPhone Safari only allows element fullscreen via Apple's player; in the
  home-screen app it looks fully fullscreen). Apple's player only behind a menu item (AirPlay etc.).
- Gestures: swipe left/right = next/previous file, swipe down = close, tap = toggle controls, double-tap left/right =
  -/+10 s, press-and-hold = 2x speed; the scrub bar has its own zone so it never triggers a swipe. Lesson from
  earlier custom players (2026-10-14): put video controls **outside** the element that owns the swipe gesture (a layer
  above the slides), not inside it - stopping events on controls inside the swipe area still let scrubbing swipe on
  iOS.
- Sound: a **"Start videos muted" setting** (off by default since 2026-10-26 - the user: "it shouldnt default to muted") decides how every video starts; unmuting affects only the current video
  (a remembered "sound on" that overrides the setting is unwanted). If iOS blocks autoplay with sound (it allows sound only when playing starts from a tap: not after a swipe or the viewer opening), play muted - quietly, the speaker in the controls shows it (2026-10-26: the big "Tap for sound" button over the video went - with sound on by default it popped up at every swipe; the user: "what the hell is this tap for sound button") - unless the video was paused meanwhile (the refusal can come late: it started a paused video again). Loop and speed are
  remembered.
- Desktop: the same player with keyboard shortcuts (space, arrows, M, F) and hover previews in the grid.
- Preload the next file for instant starts; range-request streaming.
- Formats: codec stored at import (ffprobe). Anything a device can't play (e.g. AV1 on older iPhones) is transcoded
  once on demand into the renditions cache; MP4s with their index ("moov") at the end can get a fast-start remux copy
  there. Originals are never modified. (Tested on an iPhone: MP4 and WebM play inline.)

## Huge images
Phones get resized renditions (libvips, cached) instead of decoding originals of hundreds of megapixels; the viewer
lays them out at the original size.

## As built (web client, from 2026-10-14 on)
- **Viewer**: same result list and position as the grid (moves through every result, no pages); swipe left/right,
  swipe down to close, tap to toggle controls; desktop arrow buttons + keys (arrows, Esc, i, Delete, space, m);
  neighbors preloaded; URL holds `?q=&i=&f=` and a reload reopens the file; closing scrolls the grid to it.
  Details panel (desktop side panel, phone bottom sheet): facts, Post/Source links, tags (tap = search, x = remove
  with Undo, add with autocomplete). Delete with an Undo toast.
- **Video player**: own controls in a layer above the slides (scrub bar with buffered range, play/pause, time,
  loop, speed 0.5-2x, mute); "start muted" setting, unmuting only affects the current video; muted quietly when
  autoplay with sound is blocked; loop and speed remembered. Gestures on videos: double-tap the left/right third = -/+10 s (middle =
  play/pause), press and hold = 2x while held, single tap = controls (after a short wait to rule out a double tap).
  On iPhone/iPad an "iOS player (AirPlay)" button opens Apple's player on request, never by default.
- **Video slides (2026-10-14)**: done the way hydrus-web's
  PhotoSwipe setup does it - the `<video>` gets the thumbnail as `poster` (never a blank frame) and is created as
  soon as a move towards it starts (PhotoSwipe's `contentActivate` on release), so it loads during the slide; the
  thumbnail stays underneath, the same element that slid in; a removed video is paused and its `src` dropped so it
  stops downloading. (The first attempt - fading the video in on `canplay` - wasn't clean on iOS.)
  **2026-10-18: the video fades in (0.24 s) over the thumbnail** - the cut from the soft thumbnail to the sharp
  picture was too abrupt. It starts only once a frame is really on screen (`requestVideoFrameCallback` after
  `loadeddata`/`playing`; a 700 ms timer if that never comes), and the thumbnail is hidden once the fade is over.
- **Swipe down to close (fix 2026-10-14)**: the neighboring files are hidden while the image shrinks (and while it
  springs back after a canceled swipe) - the shrinking track used to reveal them.
- **Open/close animation (built 2026-10-14)** (`lib/hero.ts`): opening a file, a copy of its thumbnail grows from the
  tile to the image's place in the viewer (its crop opening from the tile's square to the whole picture) while the
  backdrop fades in; the real viewer is hidden until the last frame. Closing (button, Esc, Back, swipe down) does the
  reverse from wherever the image is - mid swipe-down too, keeping the swipe's fade - after the grid has scrolled the
  tile into view (so it lands right even after swiping far away in the viewer). Skipped when zoomed in, when opened
  with details, when the tile isn't on screen, and with reduced motion. 280 / 260 ms, Web Animations API.
- **"Found by" (built 2026-10-15)**: the details panel lists the searches and subscriptions that met a file - query,
  site, whether that search downloaded it or it was in the library already, and when ("15 Oct"). A subscription has
  **Open**, which goes to Downloads, scrolls to it and briefly highlights it (`router.focus = "sub:<id>"`). Recorded
  by the downloader in `file_sources` (migration `0010`: file, site, query text, subscription, downloaded, first
  time; one row per file and search, so reruns add nothing) whenever a post with a file is handled; the query text
  is kept, so it survives the search being removed. Files downloaded before this have no record (seen_posts never
  knew the search) - they keep their post link. Returned with a file's tags (`sources` in `/api/files/{id}/tags`).
- **Scrub-bar preview frames (built 2026-10-15)**: while scrubbing (and on desktop while the mouse is over the bar) a
  frame of the video at that point shows above it with the time. Frames come from one WebP sprite sheet per video
  (`media.MakeSprite`: up to 100 frames, every 2 s for short clips, tiles at most 160 px, 10 per row, quality 45;
  ffmpeg decodes keyframes only, and falls back to every frame when a video has too few keyframes), stored in
  thumbs.db (`sprites`, thumbs migration `0002`). A background loop in `serve` makes them one video at a time - new
  downloads within a minute, the existing library little by little (files flagged `FlagSprite` / `FlagSpriteErr`).
  API: `GET /api/files/{id}/sprite` (layout + URL; 404 until made), `GET /sprites/{id}.webp?f=&v=` (like thumbnails,
  cached for a year). The dev server proxies `/sprites` too.
- **Smooth scrubbing (fix 2026-10-15)**: every pointer move used to set `currentTime`, so seeks
  queued up behind the decoder. Now the bar, time and preview follow the finger at once; with preview frames the
  video doesn't seek until release (one exact seek); without them it uses `fastSeek` (keyframes) one seek at a time,
  only the latest position kept. Playback pauses while scrubbing and resumes after. Also fixed: the controls' setup
  effect tracked `scrubbing`, so every scrub start/end restarted (and re-muted) the video.
- **Scrub bar drawn with transforms (2026-10-15)**: the
  position is a 0..1 `--p` on the rail, set straight from the pointer handler (same frame) as well as from Svelte;
  the played bar is `scaleX(var(--p))` and the knob rides a full-width lane `translateX(calc(var(--p) * 100%))`, both
  `will-change: transform` - compositor-only, no layout per move over a large video.
- **Scrubbing like hydrus-web**: the picture itself follows the finger instead of a small
  frame above the bar. While scrubbing the viewer draws the sprite frame for that spot over the video, in the video's
  contained box (`.scrub-frame`, container-query sized), and the video seeks one seek at a time (`fastSeek` where
  available, only the latest position kept); the frame gives way as soon as the video shows the same moment
  (within 0.15 s) - when the finger pauses, and after the exact seek on release. The time label stays above the bar;
  the small floating frame is kept only for hovering with a mouse. Frames are 160 px, so the stand-in is soft on big
  screens until the real picture takes over (bigger tiles would cost ~4x the storage).
- **Still images decoded ahead (2026-10-16)**: decoding a big image when it's first drawn blocks the main thread on
  iOS, so a swipe started meanwhile lagged. The viewer decodes the neighbors (two ahead, one behind) in the
  background with `img.decode()` (a SvelteSet of decoded URLs, the newest 12 kept); a neighbor slide gets its `src`
  only once decoded - until then its thumbnail (the img's background) shows. The current slide always has its src.
  If swiping still lags on huge images, the next lever is a smaller phone rendition (viewURL's 4096 px limit).
- **Swiping fast through big images (2026-10-16)**: the viewer shows images at the screen's size (`viewURL`: the
  screen's long side in device pixels snapped to the server's sizes - 2560 on an iPhone; the server got 1600, 2560
  and 3072 besides 2048/4096/8192) and decodes three ahead and one behind in the background; zooming in (> 1.15x)
  swaps in the full size (`fullURL`, up to 4096 on phones / 8192 on desktop) once it's decoded. Many booru images
  are bigger than 2560 px. The address (?i=&f=) is written at once when a file opens, but while
  swiping only once it pauses (350 ms) or the app goes to the background - Safari limits and slows replaceState.
- **Opening a file without a hitch (2026-10-16)**: the open/close animation (lib/hero.ts) moves only transforms -
  the frame is the crop box scaled from its end size, the thumbnail inside counter-scaled to stay cover-fitted, 24
  precomputed steps (the two scales don't interpolate as inverses) - so the compositor runs it even while the page
  builds the viewer; it starts once the thumbnail is decoded. The viewer's own image (even the open one) gets its
  src only once decoded in the background (the thumbnail shows until then), and neighbors are decoded only after
  the animation.
- **Details column animates (2026-10-16)**: on wide screens the viewer grid is always two columns, the details one
  0 px when closed and 340 px open, with `transition: grid-template-columns` (0.26 s); the panel keeps 340 px, so it
  slides in from the edge while the picture makes room, and on closing it stays (an empty 260 ms outro) while its
  column shrinks away. Phones keep the sheet sliding up/down. **Over the picture, always** (2026-10-23, the user: on mobile it "sometimes appears
  behind the image with just the top 10% of the panel showing"; not reproducible in the browser - its layout put the
  sheet on top every time, so an iPhone compositing order): the picture's area is its own stacking context
  (`.stage-wrap`: `isolation: isolate`) and the sheet is above it with a layer of its own (`z-index: 3`, `will-change:
  transform`).
  **The picture above the sheet** (2026-10-26, found taking the website's screenshots): with the sheet up, the stage ends
  where the sheet begins (the sheet's own height in CSS: `min(75%, --sheet-max-h, 100% - keyboard - 56px)`), so the
  picture fits in the room above it, gliding there like the wide screens' (it stayed centered behind the sheet, the room
  above empty); its bottom controls go meanwhile (as they did behind the sheet).
- **Details: copy links; swipe down (2026-10-16, user)**: the post page and source rows have a copy button after the
  open-in-new-tab arrow (toast "Copied the post page link"). Swiping down with the details open closes the viewer on
  wide screens (the details are a column beside the picture); on phones the sheet covers the picture, so it goes first.
- **File ID in the details (2026-10-19)**: the last fact is Vitrine's own number for the file (the `f=` in the
  address), with a copy button - to point at one file when reporting something.
- **Volume (2026-10-19)**: each device remembers its video volume (`lib/volume.svelte.ts`, localStorage); a video
  starts at it and unmuting returns to it. Desktop: a slider opens beside the speaker on hover/focus (the wheel over
  the speaker works too; moving it unmutes). Phones: no slider - the volume buttons decide (iOS ignores a page
  setting the volume anyway; a Web Audio gain would work but goes silent with the ringer switch off, so not used).
  **One-handed unmute**: double-tap the middle of a video toggles sound (a round badge with the speaker icon); left/right thirds still
  seek 10 s; play/pause is the controls' button (and Space/K).
- **Viewer, mouse vs touch (2026-10-19)**: with a mouse (`(hover: hover) and (pointer: fine)`) moving between files
  is instant (no slide - arrows, buttons, wheel), a click zooms a picture to 100 % of its own pixels at the pointer
  (twice the fit if it's smaller than the screen) and a second click fits it again (cursor zoom-in/zoom-out); a click
  on a video plays/pauses it; the controls never hide. Touch: slides, a tap shows/hides the controls, a double tap
  zooms to 100 %. The top bar shows the size (% of its own pixels) and a spinner while the picture (or its full size
  when zoomed) loads; no gradient across the picture - each control has a small dark backing of its own.
- **Split controls on phones (2026-10-25, the user: with a six-figure library the top bar ran off a phone's screen -
  "need to rethink the ui of the viewer" -> option A, "like a phone's photo app")**: under 900 px wide the top bar has
  close, where you are (in full: 96,938 / 162,330, centered) and the details; the actions run along the bottom in reach of
  a thumb - favorite, I came, full screen (where there is one) - each in its own dark backing like the top bar's, over
  a video's own controls as one panel that shows and hides with them. Delete stays in the details only (the user). The
  size (%) shows a moment while you zoom (a pinch, a double tap), then goes - not on opening or going to another file
  (the user). The spinner sits right of close; the top bar is three columns, so where you are stays centered. Wide screens keep the one bar on top.
- **A tap right after a swipe (2026-10-25, the user: after swiping to the next picture the controls took two taps)**:
  the browser took the quick swipe for a fling and spent the next tap stopping it - fixed at the cause for the whole
  app: `lib/ownTouches` (docs/ui/app-shell.md, "Taps the browser dropped").
- **Trackpad gestures (2026-10-24, the user: "natural trackpad gestures ... 2 finger swipe to switch between images")**:
  two fingers sideways (wheel events with deltaX) - the file follows them like a touch swipe (`dx`), past
  min(140 px, 20 % of the width) it slides on to the next or previous one, and the rest of that gesture (its glide
  after the fingers lift: events less than 150 ms apart) does nothing more - one file per swipe; short of it, it springs
  back 160 ms after the last event; past the first or last file it gives a quarter. Zoomed, two fingers move the
  picture (a trackpad shows by sideways or small/fractional steps; a mouse wheel - big whole steps - still zooms).
  A pinch is Ctrl + wheel to the browser: zooms around the pointer, as before.
  Two fingers down (2026-10-25, the user): the viewer follows them as a finger's swipe down does and closes past
  110 px (a phone-sized details sheet goes first); up past 70 px, the details open. A mouse wheel still steps through
  files - a trackpad is told by sideways movement, fractional steps (Windows) or small steps with the old wheelDelta
  at -3x deltaY (a Mac's; not big steps - WebKit gives a wheel that ratio too); a Mac mouse's small steps mustn't
  close the viewer. Fixed the same day (the user: "subsequent swipes don't do anything until I move the cursor"):
  Chrome sends a scroll's wheel events to the element it began on until the pointer moves, and that slide was gone
  after a swipe - the slides take no pointer events now (`pointer-events: none`: the stage, which stays, gets them);
  and a new swipe can start during the last one's glide (its steps growing again after dying away).
- **Panning and pinching like Photos (2026-10-19)**: a zoomed picture keeps gliding after a flick (the speed of the
  last 100 ms, friction by elapsed time, stopping at the edges; a touch catches it). Pinching smaller than fitted or
  past the most zoom goes on with resistance (down to 0.55x, free under the fingers) and springs back on release
  (`.zoom-anim.spring`: a slight overshoot).
- **Scrub previews (2026-10-19)**: preview frames are 320 px on the long side at WebP quality 70 (were 160 / 45:
  blurry when filling the player) - `SpriteVersion` 2; at start-up `RequeueOldSprites` marks older ones to be made
  again (the background maker, newest first; old ones serve meanwhile). **Black frames while scrubbing**: the
  overlay dropped as soon as the video's last shown moment was near the scrub position, even while the next jump
  was still on its way (a seeking video can show black, iOS especially); it now stays up while a jump is in flight,
  and is used only once the sprite picture has loaded.
- **Scrubbing redone (2026-10-19, after flicker)**: one picture at a time. While the finger is on the bar the
  player shows the preview frame for that spot - always, never the video (which seeks silently underneath); after
  letting go the preview stays until the exact seek lands, then the video takes over once. (Before, the preview
  dropped whenever the video happened to be near the spot and came back on the next move: two frames flickering.)
  Preview frames: one a second (at most 120; `SpriteVersion` 3), clips up to a minute fully decoded so each frame
  is that second (keyframes are often seconds apart); longer videos keyframes only.
- **Open/close never held up (2026-10-19)**: the zoom between grid and viewer waits at most 80 ms for its picture
  to decode (a busy server or a thumbnail not in memory made opening and closing hang); the thumbnail under a
  video is trimmed by 1 px (`clip-path: inset(1px)`), so a bright one never peeks out around a dark first frame.
- **Site icons on link rows (2026-10-19)**: the details' "Where it's from" rows show each site's favicon
  (`SiteIcon.svelte`, from `GET /api/favicon?host=`) - the link icon until it loads, or when the site has none
  (remembered while the app runs). A dark icon on a see-through background (e.g. a black logo) gets a light
  badge so it doesn't vanish. The server (`internal/favicon`) fetches an icon once per site - the home page's
  `<link rel="icon">` (the size nearest 64 px at or above, SVG left out), else `/favicon.ico`; an image host
  like img.example.com falls back to example.com, and a site with a plugin also tries the plugin's other
  addresses (its API, its post pages - e.g. a site whose main address shows a CAPTCHA to VPNs) - keeps it in `<data>/favicons/` and asks again after 30 days
  (7 for a site without one). Only public addresses are fetched (no local network, whatever a name resolves to)
  and only bitmaps kept (checked by their bytes); the browser caches an icon for a week.
- **Delete in the details, one confirmation dialog (2026-10-19)**: the viewer's top bar has no delete button any
  more (too easy to press); a quiet bin icon in the details header, beside the close button (red when
  pointed at), deletes - after asking; Del still works. Every destructive
  action asks first in one dialog (`lib/confirm.svelte.ts` `confirmAction`, `ConfirmDialog.svelte` in App): the
  files' pictures fanned out with a red bin badge (or a red icon badge), a question, one short line, Cancel and
  the action in red. A centered card on wider screens, a sheet on phones (drag down to cancel). Escape or a press
  outside cancels; keys go only to it (the viewer behind doesn't move); Tab stays inside. Undoable (deleting
  files) - the action has the focus, so Del then Enter deletes; not undoable (restoring a backup, removing a
  site) - Cancel has it. Used for: deleting files (details, grid menu, selection, Del), the blacklist's matching
  files, removing a site or its login, unsubscribing, deleting a group, deleting or restoring a backup (also from
  a file). The inline "are you sure" boxes these had are gone.
- **NEW in the viewer (2026-10-19)**: a new file (one of the pinned new files at the top of the results) shows the
  same mauve NEW mark in the viewer's top bar, after the counter, as on its thumbnail.
- **Viewer controls on bright pictures (2026-10-19)**: the top bar's buttons and chips have a darker backing
  (72 %) that blurs the picture behind it, and full-strength text - readable on a white picture too.
- **Viewer controls fade when untouched (2026-10-19)**: after 3 s without a tap, the mouse moving or a key, the
  top bar, the video controls and the desktop arrows fade out (with a mouse the cursor hides too); moving the
  mouse or a key brings them back, on phones a tap (as before). They stay while a video is paused, the pointer is
  on them (scrubbing, the volume), the details are open or a dialog asks something.
- **Full screen, our own (2026-10-19)**: `lib/fullscreen.svelte.ts` puts the viewer element full screen (the
  browser's bars go; our player, controls, gestures and swiping stay) - a button at the end of the video controls,
  one in the top bar for a picture, and F; it ends when the viewer closes. Only where the browser lets a page do
  that (desktops, Android, iPads): an iPhone's Safari only lets Apple's player go full screen, so there's no
  button there - and Apple's player isn't offered any more (it was the iPhone's "full screen"; the home-screen app
  fills the screen anyway). AirPlay and picture-in-picture went with it.
- **Seamless loops (2026-10-19)**: the browser's own loop lets a video end and then starts it again (a pause - long
  in Safari). The player checks every frame (`requestVideoFrameCallback`, else animation frames) and, within a
  tenth of a second of the end (at the speed it plays), sends it back to the start itself - so it never ends. Not
  while scrubbing or paused; the browser's loop stays on underneath as a fallback.
- **Volume beside play (2026-10-19)**: the video controls run play/pause, the speaker (its slider opens to the right
  on hover or focus, where the browser allows), the time, then loop, speed and full screen on the right - so the
  slider opening moves only the time, never the buttons.
- **An empty place behind the viewer (2026-10-21)**: the file being viewed has no tile in the grid while the viewer
  is open (Grid `hideId`) - a swipe down fades the viewer and shows an empty place for the picture to shrink into
  (its thumbnail used to sit there already) - nor while it flies back on closing (`closingId`, until it lands). The
  comic reader does the same (its comic's tile hidden while it's open and as its cover lands).
- **The details' Sources (2026-10-23)**: the same rows as "Where it's from" (lib/details.css `.rows`: one card, a
  line between rows, the same background), grouped by site in the order they came; the first three shown, then
  "N more sources" slides the rest open (Show fewer folds them). It was a Stack (stacked cards) before - its own
  frame and background, and it measured itself only after appearing, so it didn't slide in with the panel (the user).
- **A video stays on screen when its details change (2026-10-25)** (the user: pressing "I came" - "the video pauses but the audio keeps playing"): the count (or the heart) gives the file new details, so `trackShown(f.id)` made a new attachment and Svelte re-attached it; set up while the video was ready, it read `videoShown` inside its effect - setting it re-ran it, which took it away again: the video played on hidden behind its thumbnail. A video's attachments are now made once per file (`once`, by ID) and the shown-tracking runs untracked. Pictures and comic pages checked: unaffected (same elements, shown throughout). Test: "viewer: a video stays on screen, playing, when I came or the heart is pressed".
