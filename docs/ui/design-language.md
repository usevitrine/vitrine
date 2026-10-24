# Design language

How Vitrine looks and feels: the bar to meet, Catppuccin Mocha colors, sizes per input device, layout, gestures, the shared building blocks (controls, cards, menus, sheets, dropdowns, spinners), motion, hover, hints, icons and the logo. Read this before any UI work.

## Tokens (web/src/app.css)
- **Colors**: Catppuccin Mocha only (no light theme). Surfaces `--crust` #11111b, `--mantle` #181825, `--base` #1e1e2e
  (the page), `--surface0/1/2` (#313244 / #45475a / #585b70: fills, borders), `--overlay0/1`; text `--text` #cdd6f4,
  muted `--subtext` #a6adc8. **Accent `--mauve` #cba6f7** (primary buttons, focus, selection, links in the app);
  `--red` danger and favorites, `--green` done, `--peach` warnings/paused, `--yellow` wildcards, `--teal` system
  filters, `--blue` info. Tag namespaces: `--tag-general` blue, `--tag-creator` red, `--tag-character` green,
  `--tag-series` mauve, `--tag-meta` peach.
- **Sizes follow the input device, not the window**: with a mouse `--control-h` 32px, `--font` 14px, `--font-small`
  12px, `--tile` 170px; on touch (`pointer: coarse`) 44px / 16px (iOS doesn't zoom into 16px inputs) / 13px / 120px.
  `--radius` 8px, `--gap` 8px. Every input, select and button is one height (`--control-h`).
- **Safe areas**: `--safe-top/bottom/left/right` (env insets; the home-screen app adds `--top-extra`), `--home-bar`
  and `--safe-corner` for bars along the bottom in the iOS home-screen app; `--sheet-max-h` caps phone sheets.
  **`--bottom-room`** (2026-10-23, the user: Duplicates' bottom too close to "the massive curved screen", "fix similar
  situations site wide such as the sheets ... and the sidebar"): the reported bottom inset or `--home-bar`, whichever
  is bigger - everything that reaches the bottom uses it: `.page`, sheets (menus, chip editor, tag menu, filters, New
  download, confirmations, the details), the sidebar (+ `--safe-corner` at its side), Duplicates (+ `--safe-corner`
  at the sides: its Keep row spans the screen), the first steps, the reader's slider (+ corners at its ends).
  Checked with the screen's 55 px corners drawn over standalone iPhone shots.
- **Placeholders that alternate with something full-height** (an empty result vs the grid, Duplicates' empty card vs
  the pictures, its video bar): fade in place (`in:swap`), never a height slide - a collapsing placeholder squeezed
  what replaced it, so Duplicates' picture started small and grew (the user noticed).
- **Small things in a row use `nudge`, not `pop`** (2026-10-23, the user: New download's title "jumps back instead of
  animates" when Back went - "make sure that doesn't happen anywhere else"): pop only scales, so the element takes
  (or keeps) its full width at once and what's beside it jumps; `nudge` slides its width open and shut as it grows
  and fades, so neighbors glide (a gap of its own goes in a slot that slides with it, as New download's Back). `pop`
  stays for what floats (badges on tiles, dots on buttons, spinners).
- **A gap goes with what slides shut (2026-10-23, the user: the Sources header "jumps" as it goes - "make sure none of
  these animation issues are anywhere else")**: reveal and nudge slide the gap beside the element away too (a negative
  margin as it closes) - else the height closed but the gap went at once at the end. A flex box takes a negative
  margin back; a grid doesn't (a track never goes below 0), so a single-column grid lays out as the same flex column
  while something in it moves (its alignment carried over, and its width held: as a flex column its widest content - a strip
  that scrolls sideways - made it grow past its place for a moment, and the details' tags overflowed as picking began). Measured: the details' Tags glide 166 px with no step at
  the end. The details' facts: the ones every file has in one card (fixed layout), the ones only some have (duration,
  uploaded, came) in a second card that slides as a whole - in one two-column grid a fact coming or going moved all
  the ones after it to the other column. Grid tiles sit on whole pixels (their edges rounded): one between pixels was
  snapped onto them as its hover zoom began - a jump up and left (the user).
- **Content that follows something else (the details as the viewer moves on)** isn't emptied while the next one
  loads: the old stays until the new arrives, then rows that match stay put and the others slide (`FilePanel`,
  `ComicPanel`: stale meanwhile - not pressable - and cleared after 250 ms if the answer is slow).
- **Buttons**: filled (the main action, mauve), `.quiet` (outlined), `.icon` (just an icon), `.danger`; square icon
  buttons in toolbars (the selection bar). **Cards** (`.card`, `.card-head`, `.hint`, `.field`, `.empty`, `.pill`)
  on the screens other than the library (`.page`, centered). Floating controls over a picture: `.float-bar`, `.chip`,
  `.side-nav`, `.spinner` (viewer and comic reader). One `Modal` frame for small dialogs; `ActionMenu` for every ⋯
  menu (a sheet along the bottom on phones); `Dropdown` instead of native selects; menus with pages via
  `lib/pages.svelte.ts`.
- **Motion**: the standard ease is `cubic-bezier(0.2, 0.8, 0.2, 1)` (~180-320 ms); menus drop in with a fade and a
  few px of movement; everything honors `prefers-reduced-motion`. Grid tiles share one motion for any change (slide / appear / leave, `MOTION` in
  Grid.svelte; see grid-and-search.md). Hover changes are instant (no fades).
- **Everything animates in and out (2026-10-23, the user: "everything should be smoothly animated in an out of
  view/reshuffling in all situations")**: nothing pops. `lib/motion.ts` has the shared ones - `reveal` (a block in the
  flow: its height opens/closes as it fades, so what's below glides; `axis: 'x'` beside others), `pop` (small things:
  grow in, shrink out), `swap` (a cross-fade) and `moved` (`animate:moved` on keyed lists). Every `{#if}`/`{#each}`
  that shows or hides something gets one; closing is checked as much as opening. Exceptions: a whole list replaced
  by a new search (the tag browser) fades its rows in rather than sliding 100 rows; the library grid has its own
  `MOTION`; screens and menus keep their page slides. Search chips keep their identity across changes (same text, or
  the same place for an edit), so they glide, pop and change in place. Dialogs leave as plainly as they come (200 ms:
  shrink to 94 %, sink 8 px, fade); menus and panels fade out over 180-200 ms.
- **No text shift when a motion ends (2026-10-23, user: text "slightly shifts after the opening animation")**: two
  causes, both fixed. (1) An ease-out spends its last frames moving by fractions of a pixel - text drawn between
  pixels, then snapping onto them: every transition now moves in whole pixels (`px()`, `glide` instead of Svelte's
  fly; sheets slide by their height in pixels, not %) and a scale is 1 once within half a pixel of the element's
  size (`settle`). (2) Desktop draws a moving layer's text with other smoothing than the page's: what floats (role
  dialog / alertdialog / menu / listbox, toasts, the tour card) keeps a layer of its own (`will-change: opacity` -
  not transform, which would become fixed children's containing block). Measured: the last animation frame vs at
  rest, pixels that change strongly - 0 for the downloads and bell panels, Downloads tabs, Filters, Saved searches,
  New download, the login dialog, a confirmation (from 630-11,000 before); a third of a pixel left on a sheet.
- **Type**: the system font stack (system-ui, -apple-system, Segoe UI, Roboto), line height 1.4.
- **Words**: American spelling; texts in `locales/` (see ui/languages.md); hints only where a label could mislead, a
  few words.

## Bar to meet (2026-10-14)
- **"5-star UI and UX"** - beauty and ease of use count as much as speed.
- **iOS is a first-class citizen** (2026-10-14): Safari on iPhone - above all the home-screen web
  app (standalone PWA) - gets the same care as desktop browsers, and nothing ships that is only tested on desktop.
  That means: designing within iOS limits (no element fullscreen on iPhone, autoplay with sound blocked, memory limits
  that crash Safari on huge images, the on-screen keyboard resizing the viewport, safe areas/notch/home indicator,
  rubber-band scrolling, long-press callouts, double-tap zoom), testing gestures and video on WebKit, and treating
  an iOS-only bug as seriously as any other.
- **Mobile-first**: every screen designed for the phone first, then scaled up - but desktop must be just as beautiful
  and pleasant, not a stretched phone layout (wider grids, side panels, hover previews, keyboard shortcuts).
- **Density follows the input device, not the window width** (no giant inputs on desktop): touch sizes (~44 px tap
  targets, 16 px input text so iOS doesn't zoom) only on touch screens (`pointer: coarse`); with a mouse
  (`pointer: fine`) compact controls (~32 px inputs/buttons, 13-14 px text), forms sized to their content instead of
  full width, more on screen.
- Smooth 60 fps motion and transitions (respecting reduced-motion); gestures that feel native on iOS (swipe between
  files, pull to refresh, long-press menus); instant feedback (optimistic updates, blur placeholders/skeletons, never
  a blank screen); clear empty and error states; keyboard shortcuts on desktop; accessible contrast and focus rings;
  safe-area/notch support for the home-screen app (PWA).

## Colors: Catppuccin Mocha only - no light theme
Palette: https://catppuccin.com/palette
- Backgrounds: Crust `#11111b`, Mantle `#181825`, Base `#1e1e2e`; surfaces Surface0-2 `#313244` `#45475a` `#585b70`;
  text `#cdd6f4`, subtext `#a6adc8`.
- **Primary/accent: Mauve `#cba6f7`** (buttons, selection, focus, active tab; text on it: Crust `#11111b`; contrast on
  Base ~8:1). Runner-up: Lavender `#b4befe`.
- Status colors, never used as the accent: Green `#a6e3a1` done/ok, Yellow `#f9e2af` warning, Red `#f38ba8`
  error/delete, Blue `#89b4fa` links/info, Peach `#fab387` running/in progress.

## Layout: one web app for everything
- **Library** (main screen): search bar with editable chips, thumbnail grid, viewer (swipe between files, video, the
  file's tags and post/source links), selection for bulk tagging/deleting.
- **Downloads**: searches with drag-to-reorder, subscriptions and groups, live progress, blacklist, delete by tags, tag
  refresh.
- **Tags**: parent/sibling groups, tag counts, renames.
- **Settings**: site plugins (install, update, credentials), defaults, notifications, login, storage and maintenance.
- **Navigation (2026-10-14, like hydrus-web)**: no bottom tabs and no permanent sidebar - a
  floating hamburger button (top left, every device) opens the navigation as a drawer from the left; tapping outside,
  Esc or picking a screen closes it. The screens get the full width. Desktop still has a details side panel in the
  viewer, drag-select and shortcuts.
- Cross-links: a downloaded file links to the search/subscription that fetched it ("Found by", built 2026-10-15); finished downloads appear in the
  Library immediately.

## Gestures (chosen from hydrus-web's set, 2026-10-14)
- **Viewer, images**: pinch to zoom + drag to pan while zoomed; double-tap to zoom in / back out; swipe **up** opens
  the details panel (not close); swipe down closes; swipe left/right = next/previous; tap = controls. Not wanted:
  pinch-in to close.
- **Viewer, videos**: double-tap left/right third = -/+10 s, middle = play/pause; press and hold = 2x.
- **Viewer, desktop**: browser Back (and the iPhone edge swipe) closes the viewer instead of leaving the page; mouse
  wheel = next/previous; Ctrl + wheel = zoom; middle-click closes; keys (arrows, Esc, i, Delete, space, m).
- **Grid**: long-press (iPhone too - iOS sends no contextmenu event, so it is Vitrine's own) or right-click = a menu
  for the file (select, view, details, delete); once anything is selected, taps select/deselect and an action bar
  offers bulk actions (add/remove tags, delete); Ctrl+click toggles one, Shift+click selects a range; drag across
  thumbnails to select many (mouse, or a finger after a long-press).
- **Details panel starts hidden** on every device (2026-10-14); open with the i button, a swipe up or `i`.
- **Details panel overhaul (2026-10-15)**: a header "Details" with a close button (x); closed also by Escape (the
  details first, then the viewer), a tap on the picture outside them, a swipe down on the picture, or the i button.
  Phones: a sheet over the bottom 75% (slides up, a grab handle; drag the header down to close). Content: a facts card
  (Type "MP4 video", Size, Dimensions, Length "0:08 · silent", Uploaded, Imported - two to a row, dates as "Today at
  22:14" / "3 Oct at 09:12", the full date on hover); "Where it's from": one row each for the post page and
  the searches/subscriptions that found it; **Sources** its own section below (2026-10-23, a file with 32 sources filled
  the panel): the site as the row's name, the link's path under it, the same site's links together, a **stack** like the
  Downloads page's (`Stack`; the user: "the stacking effect you used on the subs page"): the first in front, the rest
  peeking out behind it ("+N"), fanning out on a tap (a subscription row opens it), Refresh tags in the section header; then Tags
  (add box, groups by namespace).
- Built 2026-10-14 (viewer part): pinch zoom with the fingers' midpoint kept in place, pan clamped to the image
  edges, double-tap zooms to 2.5x at the tapped point (again = back), max zoom 4x or enough for the original's own
  pixels (up to 16x); Ctrl+wheel / trackpad pinch zooms around the cursor, plain wheel = next/previous (one step per
  300 ms) or scrolls a zoomed image; middle-click closes; opening the viewer pushes a history entry, so Back closes
  it (closing otherwise pops it; a tag search started in the viewer runs after that step). E2E-tested on WebKit.
- Built 2026-10-14 (grid part): one pointer handler on the grid finds the tile under the finger/cursor. Touch: tap =
  open (or toggle while selecting), long-press 450 ms then lift = file menu (Select/Deselect, View, Details, Delete),
  long-press then slide = select the range slid over (scrolling blocked by a non-passive touchmove, auto-scroll near
  the edges). Mouse: click = open, right-click = menu, drag = range select, Ctrl/Cmd+click = toggle, Shift+click =
  range from the last picked file (IDs of unloaded parts fetched by position). Selection bar under the grid: count,
  All, add tags, remove tags (autocomplete), delete (Undo); a new search clears the selection. Selected tiles shrink
  with a mauve check; others dim. 35 E2E tests on iPhone + desktop WebKit.
- Viewer slides are keyed by file (not by slot), so the image that slides in keeps its decoded element when it
  becomes current - no reload flash. A swipe or key press during a slide finishes it
  instantly instead of being ignored, so there is no speed limit when flicking through files.

## As built (web client, from 2026-10-14 on)
- **Design pass (2026-10-14)**: shared building blocks in `app.css`, used by
  every screen instead of per-screen styles - controls (inputs, selects and buttons share `--control-h`, radius,
  border and a mauve focus ring; selects drawn like inputs with a chevron, since WebKit's native look ignores the
  height; number inputs without spinners), buttons (filled = main action, `.quiet` outlined, `.icon`, `.danger`,
  `.quiet.danger`), pages (`.page` + `.page-inner`: a centered 760 px column of `.card`s with `.card-head` h2 +
  `.hint`), `.field` (label, hint, control), `.empty` (dashed "nothing yet" box), `.pill` (removable tag). Phone
  forms: inputs first, then a full-width main button. Sort in the top bar is an icon button (the shape of
  Material's "sort", as in hydrus-web) with the native picker laid invisibly over it, plus a dot when the order
  isn't the default. `e2e/shots.spec.ts` takes screenshots of every screen on request
  (`SHOTS=<folder> npx playwright test shots`) for design reviews.
- **Custom dropdowns (2026-10-14)**: `components/Dropdown.svelte` - a
  trigger (input-styled with a chevron, or a custom one such as the sort icon) and a floating list in the app's
  style (optional heading, check on the current option, keyboard: arrows/Home/End/Enter/Esc, closes on an outside
  press). Used for the sort menu (top bar and Settings) and the Downloads site picker; no native `<select>` is left
  (this replaces the earlier "native picker laid over the icon").
- **Tag namespace colors (2026-10-14)**: every namespace has its own color (`--tag-*` in `app.css`): creator red,
  series mauve, character green, meta peach, general (no namespace) blue; other namespaces get a stable spare color.
  Used in the details panel, search chips (a negated tag keeps its color with a red minus; system filters moved to
  teal), autocomplete and the Tags screen. The details panel lists tags **grouped by namespace** in booru order
  (Creator, Series, Character, other namespaces A-Z, General, Meta), A-Z within a group, names shown without the
  prefix (tapping searches `namespace:name`); a tag the user added has an outline. `lib/namespaces.svelte.ts` knows
  which prefixes are namespaces (from `/api/namespaces`), so `9:16` stays a plain tag.
- **Menu motion (2026-10-16)**: Sort and filters, the chip editor and Saved searches drop in (180 ms, fade + 8 px +
  slight scale, `|global` so they play inside their {#if}/{#key}) and fade out (120 ms). Inside Sort and filters,
  pages slide: deeper (Sort by, a direction page, a filter form) from the right, back from the left (out 120 ms,
  in 220 ms after 60 ms), stacked in one grid cell; the panel height animates between them (only the visible part -
  capped at the panel's max-height; the old height is laid out first). A leaving page or menu is `inert` +
  aria-hidden; each move gets a fresh page (a counter key), so a leaving one is never reused. Reduced motion: none.
  Note: the global `.page` class (screens) mustn't be reused for anything else (it sets height: 100%).
- **More ⋯ menus (2026-10-16)**: Backups rows (Download, Restore…, Delete) and All tags rows (tapping a tag searches
  it; ⋯: Search, Rename…, Alias… - Rename disabled for site tags, "alias it"). Left as they are: the download queue's
  button strip (asked for earlier), the selection toolbar, saved searches' Edit mode, tag-rule × chips, the file menu.
- **Hover and chips (2026-10-18)**: filled buttons lighten on hover with a thin white background layer instead of
  `filter: brightness` (a filter makes Chromium repaint the element differently as it starts/ends - the menu items
  flickered); the search bar's chip row has 4 px padding so an editing/drop-target chip's outline isn't clipped by its
  sideways scroll. Settings > This device says whether the background helper (service worker) runs and, if it
  doesn't, why (it tries to start it) - saved thumbnails need it.
- **Phone sheets like the details panel (2026-10-19)**: action menus (`ActionMenu`) and the tag menu (`TagMenu`) on
  phones sit edge to edge along the bottom with rounded top corners, a grab bar (`lib/sheetDrag.ts`: drag down
  > 80 px or flick to close, else it springs back; it leaves from where it was let go) and bottom padding of
  30 px + the home bar (`--safe-bottom`; 16 px was too little on an iPhone), so the last row clears the iPhone's rounded corners.
- **One spinner style (2026-10-19)**: every ring spinner (thumbnail being made, thumbnail on its way, the viewer's
  loading) is a faint ring with the accent (mauve) going round.
- **Hints: few and short (2026-10-19)**: a hint only where a name alone could mislead, in a few words (the filter
  list: "Added to the library", "Posted on its site", "Tags last changed", "First results only"; none under Width,
  Has audio, ...); no "pick one or more" lines. The same everywhere: Settings, Downloads, Tags, Sites, Backups,
  Notifications, the login page and empty states were cut to a few words or dropped where the label says it.
- **Phone sheets, one height and one motion (2026-10-19)**: every sheet (action menus, a tag's menu, the chip menu,
  the filter menu, the details) stops at `--sheet-max-h` (app.css: 70 % of the screen, less if the top bar needs
  it); longer content scrolls inside. They come and go the same way (`lib/sheet.ts`): up from the bottom, and back
  down when closed - from where a drag left them - while the page behind dims and clears (no motion with reduced
  motion).
- **Sheets drag by their header; the details leave from where let go (2026-10-19)**: `sheetDrag` goes on the sheet
  and starts on its grab bar, its header (`.head`) or `[data-drag]` (the whole confirmation), never on a control;
  those areas don't scroll the page (`touch-action: none`). The details sheet has separate in/out transitions (a
  two-way one played its way in backwards, from where the sheet rests: a dragged sheet jumped up first) and keeps
  the dragged offset until it has gone.
- **Menus with pages move alike (2026-10-19)**: `lib/pages.svelte.ts` (`Pages`) is the one way menus move
  between pages - the action menus' choice pages, the Sort and filters menu, a tag's Namespace page: the new page
  slides in (from the right going deeper, from the left going back) while the old one slides out, both fading,
  and the menu's height follows (0.24 s). Base styles `.mpages` / `.mpage` in app.css.
- **Floating scrollbar (2026-10-19)**: with a mouse, the screens' pages (Downloads, Settings) and the details
  panel hide the browser's own scrollbar (it took room as it came and went, so the page shifted sideways) and get
  a thin thumb over the content at the right edge (`lib/overlayScroll.ts`, `{@attach overlayScroll}`): shown
  while scrolling (fades after ~1 s) and while the pointer is at the edge, wider under the pointer, draggable;
  gone when there's nothing to scroll. Touch screens keep their own (it already floats). The library grid has
  its own fast scroller.
- **Hover changes are instant everywhere (2026-10-19)**: no element fades its background on hover (the shared
  button and icon-link styles, chips, chart columns): moving between two neighbors, one fading out while the
  other faded in looked like a flicker. A test checks every clickable element on each screen for a background
  transition.
- **The logo (2026-10-20)**: the letter V cut like a crystal - each arm a lit and a shaded face, in a mauve-to-pink
  gradient (#cba6f7 -> #f5c2e7) on the base color - picked from a sheet of ideas (a gem as the V). The wordmark is the
  mark as the V of "Vitrine" ("itrine" in the app's font, `Logo.svelte`; `size` = the mark's height). Sources in
  web/public: icon.svg (rounded square: favicon, the splash - inline in index.html too), icon-square.svg (full-bleed:
  icon-180.png for iOS and the launch images), icon-maskable.svg (Android's crop, mark in the safe zone), badge.svg
  (notification icon, a white V). PNGs: `vips copy "x.svg[scale=N]" x.png`. The service worker's shell cache became
  v2 so devices fetch the new icons, and every icon and launch-image address carries ?v=2 (raise it when the logo
  changes: browsers, Android - a changed manifest refreshes an installed app's icon - and saved copies then fetch it
  again). An iPhone keeps its Home Screen icon and launch screen from when the app was added: re-add it.
- **Icons picked (2026-10-20)**: Library = stacked pictures with a mountain (`library`, also Settings > Library);
  "I came" = three outlined droplets as in 💦, tails curling back (`drop`; made by a small drop() helper: round end,
  radius, tail length and angle, and a bend that curls the tail - sheets of options in the session's scratchpad).
- **Menus fading out ignore presses (2026-10-19)**: a press outside an open menu closes it and is swallowed (it
  never reaches what's underneath); a menu already closed and fading out (inert) no longer does that, so the next
  press - e.g. the menu button right after closing the filter menu - works (`leaving()` in lib/outside.ts).
- **Sheets drag down from anywhere (2026-10-21)**: `lib/sheetDrag.ts` - `dragDown` (the gesture) and `sheetDrag`
  (a sheet that moves itself). A drag starts on the grab bar or header at once, and anywhere else (a list, a button)
  once the finger goes down more than sideways with whatever it's on scrolled to the top - otherwise that scrolls.
  Fields and sliders keep their drags; a press that became a drag doesn't press its button (the click is
  swallowed); touch: `touchmove` is canceled once it's a drag (no scroll or bounce). Action menus, the tag and chip
  menus, Sort and filters, the site picker, the confirm dialog and both details panels (the viewer's, the Comics
  page's) use it.
- **Tile overlays (2026-10-21)**: everything on a tile sits in one `.face`, clipped to the rounded shape as one
  (`clip-path`; a hover card or a comic: also `mask-image`, a layer of its own) - clipping each piece showed a sliver
  of the picture around a comic's dark foot; the foot reaches a pixel past the edges; a tile has no background of its
  own once its picture is in. Back after the viewer or reader closed (`landed`), its overlays fade in (0.3 s).
- **Dialogs above the keyboard (2026-10-22)** (the user: "some dialogue menus open behind the keyboard, e.g. the
  delete confirmation"): iOS doesn't shrink the page for the on-screen keyboard, so a dialog centered on the screen
  sat under it. `lib/viewport` now also sets `--vv-top` (how far the visible part is scrolled) next to `--kb`, and
  app.css derives `--view-h` / `--view-mid`: Modal (Rename, Crop, prompts), ConfirmDialog and SitePicker center at
  `--view-mid` with `max-height: calc(var(--view-h) - …)` (scrolling inside when taller); a phone sheet that holds a
  text box sits at `bottom: var(--kb)` (the New download dialog). Test: "dialogs open above the on-screen keyboard"
  (a keyboard of half the screen; the old centering failed it).
- **Dropdowns in dialogs (2026-10-22)** (the user: "dropdowns stuck inside the dialogue"): a list opened in a
  dialog was cropped by it (the dialog scrolled to make room and its title went), and a floating list - fixed to the
  screen - was misplaced inside a dialog moved with `translate` (such an ancestor becomes what "fixed" is relative
  to). A floating Dropdown list now lives at the top of the page (`toBody`, `data-floating`, z-index 90 over the
  dialogs); its own outside-press check and SystemMenu's count it as inside. The New download dialog's dropdowns
  float always. Long searches: the dialog never scrolls sideways (`overflow: hidden auto`, its form grid
  `minmax(0, 1fr)`, the left-out list wraps anywhere). Tests: the Repeat list over the dialog (the dialog not
  scrolled, its title on screen); a very long search on every page.
- **Empty results box (2026-10-22)**: Library, Comics and Browse put the search bar's room in its top margin (it
  was padding: the dashed border's top edge sat under the floating search bar).
- **⋮ menus (2026-10-22)** (the user: "vertical dots, they take up less room"): ActionMenu's own trigger is vertical
  dots in a 24 px wide button everywhere (a menu with an icon of its own keeps its width).
