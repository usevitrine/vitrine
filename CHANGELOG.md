# Changelog

What changed in each version of Vitrine, newest first, sorted into new features, improvements and fixes. The app
shows this too (Settings > Updates).

## Unreleased

### Improved

- **Tag refresh fits on one line per site on phones**: how many posts are waiting to be refreshed is now in the site's status ("12 to refresh", "Paused · 12") instead of next to the post count.
- **Thumbnails come into view more smoothly**: each one fades in once it's ready to show (some used to pop in at the end of their fade instead), and ones you've already seen are there at once when you scroll back.

## 1.0.8 - 2026-10-29

### New

- **How long each download has been running**: on its card in the queue (counting on while it works, standing still while it's paused or waiting for any reason), and on finished downloads in History.

### Improved

- **Finished downloads and the downloads menu match the new download cards**: finished ones in History are cards like the queue's (tap one for its numbers and actions), and the top bar's menu shows each download's progress and speed the same way, more compactly.
- **The Subscriptions tab's order is an icon inside the filter box**, as in the library's search bar, instead of a dropdown on a line of its own.

### Fixed

- **The Subscriptions tab could stop working** (laggy, sites opening with nothing in them) when two subscriptions in a group had the same newest file.
- **New downloads no longer show at the top of the library for a moment** in upload order: each file comes in already in its place by upload time.

## 1.0.7 - 2026-10-29

### New

- **Plugin updates in the bell, with what changed**: when a site's plugin has a new version, the bell says so (checked once a day, with the same setting as Vitrine's own update check). Settings > Sites shows what the new version changes before you update, and each site's ⋯ menu has its Version history.

### Improved

- **Removing a site asks first whether to erase it**: after Remove, a plain question - Keep them or Erase everything… - comes before the word to type.
- **The Downloads summary stays one row on phones**: when its counts don't fit, the row scrolls sideways instead of wrapping onto a second line.
- **Faster downloads and less memory**: recording each downloaded post takes about half the database work, emptying the trash is many times faster on a big library (it no longer pauses downloads for long), and the server holds far less memory for thumbnails while downloading.
- **A steadier download speed**: the speed shown is the last 10 seconds' average of every byte, so it no longer jumps between a big video's speed and a run of small files'.
- **Faster downloads of files whose md5 the site lists wrongly** (common with rule34's older videos): the two small pieces that check such a file are asked for together, one wait instead of two.
- **Subscriptions keep checking during their first download**: while a subscription's initial download is still going (hours, for a big search), its regular checks run on schedule as usual and bring in new posts as they come out.
- **Blacklist changes apply to running downloads**: a tag added to the blacklist (or a new size limit) counts from the download's next post, not only from the next download.
- **Only a subscription's regular checks bring NEW files** on a new library: a first download (once, or a subscription's first check) no longer fills NEW and the top of the library. Settings > Library > "Bulk downloads count as new" brings the old way back.

### Fixed

- **Tapping a thumbnail while downloads add files could open the wrong file**: new files now come into the library only at a quiet moment - at the top of the grid, with nothing open or selected and nothing touched for a second - so nothing ever moves under your finger. Scrolled down, they wait until you're back at the top.
- A file's long-press menu could act on a different file if new files arrived while it was open.
- An opened download on the Downloads page showed its speed twice, and its hover highlight touched the line below it.

## 1.0.6-hotfix - 2026-10-28

### Fixed

- **Emptying the trash during a download no longer stops it**: a check that met a file just removed for good failed with "FOREIGN KEY constraint failed"; it now carries on.

## 1.0.6 - 2026-10-28

### New

- **Scrub a video right on its hover card**: press on a playing hover card and move left or right to jump through the video - the spot under the pointer, start to end across the card. Let go and it plays on from there; a plain click still opens it.
- **What's new after an update**: the first time you open your library after an update, a window shows what changed in the new version. Tick "Don't show again" to stop it, or switch it in Settings > Updates.
- **A native installer for Windows on ARM**: `Vitrine-Setup-<version>-arm64.exe` runs Vitrine (with its ffmpeg and libvips) natively on ARM64 PCs. If you installed the x64 version on one, its next update moves it to the ARM64 one by itself.

### Improved

- **Windows downloads**: the release has just the two installers now; the separate Windows zip files are gone.
- **A spinner over a video while it loads**: when a video opens, after you jump or scrub to a part not loaded yet, and whenever playback waits for the connection.
- **No more dark gaps when flicking fast**: when a fast flick outruns the thumbnails (iPhones especially), you see the shapes of the tiles that are about to fill in, exactly where they'll land, instead of an empty background.
- **Clearer counts in the downloads menu**: a subscription check says "Checking…" before it has looked at anything, then how many new files it found and how many posts it looked at ("3 new · 24 looked at") instead of a bare number; a first download says "Starting…" until it begins.
- **The trash is quick and smooth**: it opens at once even while downloads run (it took a couple of seconds on a big library), the library's files no longer flash on it - they leave and the trash's appear one after another, as anywhere in the media - and files deleted for good disappear as they go, not all at the end.
- **Roomier download queue**: each download is one line - the search, how far it is and how fast - with a short summary under its bar. Tap it for all its numbers with their names, the time left, and its actions. Removing a download from the queue asks first.
- **Tags keep up during big downloads**: looking up tag types and refreshing older files' tags no longer waits for a site's downloads to finish (a big first check could hold them up for days). It runs alongside, slowly, and only takes the site's turn when no download or check needs it.
- **More files at once where it pays**: a site plugin can let Vitrine try more simultaneous downloads; it keeps an extra connection only while it adds at least a tenth to the total speed, and backs off at the first "too many requests". Plugins can also say which Vitrine version they need, so older versions aren't offered an update they can't read.
- **A health check of your library**: every week Vitrine checks its database and that every file is still on the disk, and every day it reads back a share of your files to check they haven't been damaged (all of them about every four weeks, slowly). Anything it finds is a notice, and Settings > Stats shows how it's going.
- **Every backup is checked**: right after it's made, Vitrine reads it back as a restore would. A backup that couldn't be restored is reported (a notice for the daily one) and never replaces the older, good ones.
- **Warnings are kept for weeks**: warnings and errors also go to a log of their own, so `vitrine logs -level warn` and the problem report reach back far beyond the last few hours of a busy downloader.
- **Settings > Stats opens at once on a big library** (it took several seconds with hundreds of thousands of files).
- **Less downloading twice**: when a site lists an out-of-date checksum for a file it has since replaced (common on rule34's older videos), Vitrine checks two small pieces of it again instead of downloading the whole file a second time. The log says so once per site, not for every file.
- **Reorder the download that's running**: drag it below another (a big first check, say) and the one now ahead takes over after the file in flight; the one moved down carries on later from where it was.
- **Update during setup**: when a newer Vitrine is out, the first steps offer it at the top; after the update, setup starts over on the new version. Otherwise a Check for updates link sits beside the step count.

### Fixed

- **Big disks in TB** in the first steps ("5.4 TB", not "5,538 GB").
- **No false "Updated" for a plugin**: when the plugin list offers a version its file doesn't have yet, updating says so instead of reporting success and offering the same update again.

## 1.0.5 - 2026-10-26

### New

- **Erase every trace of a removed site**: after removing a site, Vitrine offers to wipe it from the server completely - the files only it gave, its tags and links on the rest, its downloads, subscriptions, history, deletion records, notices, login and log lines, and its entries in menus, filters and saved searches. Files another site also gave stay. Older backups go too if you tick the box. Deletion records from before this version can't be traced to a site and stay.

### Improved

- **Site filter** (Sort and filters > From): only the sites your files came from are listed, and comic sites no longer show in the media's.
- **No more "Tap for sound" button**: when the browser won't play a video with sound (an iPhone, after a swipe), it plays muted without a button over it - the speaker in the controls shows it and turns the sound on.

### Fixed

- **Checking for updates** works behind a VPN or another shared internet address: when GitHub refuses ("403 Forbidden" - its hourly limit, used up by others on the same address), Vitrine asks GitHub's website instead.

## 1.0.4 - 2026-10-26

### Improved

- **Videos play with their sound** from the start (Start videos muted, in Settings > Viewing, is now off unless you turn it on). Where the browser doesn't allow it, the video plays muted with Tap for sound, as before.
- **The app's own scrollbar everywhere**: menus, sheets, dropdowns, dialogs and the comic reader have the thin floating scrollbar, on phones too (the details sheet on a phone had the browser's own).

### Fixed

- **Video sound**: the mute button can always be pressed (at a high volume the hidden volume slider covered part of it), and a video paused right away stays paused when the browser refuses its sound.
- **Search chips**: a tag picked from the suggestions slides into the search bar smoothly (its first frames came out broken, so it popped in).
- **Details on phones**: with the details sheet up, the picture moves into the room above it instead of staying hidden behind the sheet.

## 1.0.3-hotfix - 2026-10-26

### Fixed

- **Windows installer**: Vitrine opens after installing (1.0.3's installer put the command-line program in its place, which closed at once).

## 1.0.3 - 2026-10-26

### New

- **Windows installer**: `Vitrine-Setup-<version>-x64.exe` installs Vitrine for you (no administrator needed) with ffmpeg and libvips included. It asks where to keep the library, the original files and the backups, the port, and whether phones on your Wi-Fi may open it; it can start Vitrine when you sign in, add the `vitrine` command to PATH and put Vitrine in the Start menu (and on the desktop). Vitrine then lives in the notification area: click it to open Vitrine, or use its menu - the address for your phone, your files, the log, Start with Windows, Restart, Quit. Update in Settings installs new versions by itself and brings Vitrine back. Uninstalling keeps your library unless you say otherwise.
- **Portable**: a `config.toml` next to the program makes Vitrine portable - with `data_dir = "data"` everything lives in that folder (move or copy it), a double-click starts the server, and ffmpeg/libvips placed beside it are used. Relative paths in a config file now count from the file's folder, not from where Vitrine was started.
- **Settings > Library** (was Media): a card each for Media, Comics, Browse and the trash. Comics and Browse each have a default search and sort, and open with them (Browse's sort can be one of the site's own lists, like Popular).
- **New filters**: New (the files marked NEW) and Animated (videos and animated pictures), in Sort and filters - or type `system:new`, `system:animated`.
- **Downloads are checked**: every file is compared with the md5 its site gives for it; one that came out wrong is downloaded again (a site whose md5 doesn't match its own file still gets its file).
- **Comics > Browse**: galleries with a blacklisted tag are hidden, as they are from downloads.

### Improved

- **Notifications about blacklisted tags** (the bell's and push) take you to Tags > Rules, its Delete them button in view and lit.
- **Slow connections re-rolled**: a file connection far slower than the site usually gives (a server trickling at 100 KB/s where a fresh connection gets 30 MB/s) is carried on on a fresh connection within 20 seconds.
- **Comics > Browse**: a gallery's pages show in the details panel as they come (each in its place at once, filling in sheet by sheet), the next ones fetched ahead; Download there is purple; a new search clears the last one's galleries at once (a spinner until the new ones come).
- **Viewer on phones**: the controls split like a phone's photo app - close, where you are and the details at the top, favorite, I came and full screen along the bottom in reach of your thumb; the size shows a moment while you zoom. Large libraries no longer push the buttons off the screen.
- **Top bar**: always there - it no longer slides away when you scroll down.
- **Duplicates**: a big picture still loading shows a spinner on its side until it's there (only the blurry thumbnail showed); the spinner shows on bright pictures too.
- **Trash**: Empty trash spins until it's done.

### Fixed

- **Files arriving in any order**: sorted other than newest first (oldest first, by size, random...), downloads coming in no longer change the file you're viewing, shift what's on screen or make a tap open the neighboring file.
- **The Downloads menu and the bell** no longer show a scrollbar for a moment when a download or notification comes in while they're open.
- **Long names that scroll** (a download's search in the Downloads menu, a comic's title on hover): the soft edge shows only where text is out of view, so the first and last letters are no longer faded.
- **Removing a running download** no longer, now and then, starts it again in the background and holds up its site's other downloads for up to a minute.
- Pressing "I came" or the heart on a video no longer freezes its picture while the sound plays on.
- **Tiles keep their insides**: as a cover zooms, a row resizes or a collection opens, the picture keeps its proportions and the title, chips, marks and badges keep their size and place (they shrank and grew back, the pages icon drifted, and the title vanished as a comic's cover zoomed); a comic's title, chips and gradient stay sharp and still while its cover zooms.
- **Comics downloads count pages**: the Downloads menu, the bell and notifications say how many pages a gallery brought ("40 new pages"), not "1 new file".
- **Comics > Browse**: covers that fail in a bad moment are fetched again (a burst of the site's failures left them broken), a cover that never comes stops spinning, and a new search never shows the last one's covers (Chrome kept them, titles and all).
- **Trash**: going there no longer shows the library's files for a moment first.
- **Taps after a swipe**: a tap right after a quick swipe - to the next picture, or down to close it - works at once anywhere in the app (the browser ignored it, so it took a second tap).

## 1.0.2 - 2026-10-25

### New

- **Updates**: a page of its own (Settings > Updates, in place of What’s new) - Vitrine tells you when a new
  version is out (and the bell does; it checks GitHub once a day and can be told not to) and shows what’s new in
  it above the changes so far. With Watchtower set up (see the example compose file), Update installs
  it from there: a backup first, then a full-screen updater follows each step and fades the app back in on the new
  version.
- **Trackpad gestures in the viewer**: swipe with two fingers to go to the next or previous file (it follows your fingers), and move a zoomed picture with two fingers; pinch zooms.
- **The menu shows what's in the trash**: deleted files and comics together on Trash, each on its own under it.

### Improved

- **Notifications**: pop-ups stack by kind like the Downloads page (newest in front, the rest peeking out behind;
  point at or tap a stack to fan it out), sit just above the bottom of the screen and come and go smoothly; the bell
  stacks the same way. A notice about one file or comic names it (“Deleted file #1234”); several just say how many.
  Push notifications no longer show site names, tags or searches - only what happened and how many.
- **Downloads**: a download's line is shorter - "57,734/72,588" (posts gone through, out of the limit you picked or
  the site's count), then icons with counts for new, had, blacklisted and the rest. A limit now counts every post
  gone through, blacklisted ones too, like the line does.
- **Downloads button**: in the top bar on every page, not only the library.
- **Phones**: every dropdown opens as a sheet from the bottom, like the other menus.
- **Pixel-perfect motion**: the details panel opens and closes without the picture snapping aside at the end; bottom sheets no longer leave a dark band behind as they close; search suggestions fade in and out; Settings and Tags sections cross-fade instead of the old one vanishing; a picture fades from its thumbnail to full sharpness; a comic's title fades in as the reader closes, its cover rounding its corners on the way back; pages cross-fade when you go from one to another; Duplicates' next pair fades in; the comic reader closed with Back flies home like its close button.
- **Smoother motion**: stacks unfold from under their front card and fan out a little when pointed at; menus settle without a last-moment jump; Downloads tabs and Add site's kinds switch cleanly (nothing lingering or jumping). The Subscriptions tab is "Subs".
- **New download**: the limit is labeled Posts (it counts every post gone through - new, had or blacklisted).
- **Top bar**: the downloads and bell icons are a little smaller, their counters too.
- **The bell and the Downloads menu** open with their stacks folded.

### Fixed

- A file that finished just as its download was stopped is counted as downloaded (it showed as "had" after resuming).
- A site behind Cloudflare no longer loses its fresh clearance when several requests are challenged at once.
- **Hover previews**: a thumbnail zooms into its card smoothly - no more shaking as it grows or shrinks back.
- **Duplicates**: the last pair fades away cleanly (it shrank first).

## 1.0.1 - 2026-10-24

### New

- **Adding sites**: the list of sites to add has a search, filters by kind (image boards, galleries, social), shows
  the first few with "Show all", and slides open, filters and installs smoothly.
- **Site logins**: entered in a dialog (a site’s ⋯ menu, or the line on its card) with a Test button that tries
  what you typed before saving it.
- **Downloads menu**: the download button in the top bar shows how many downloads are running or waiting, and opens
  a list of them with their progress, plus New.
- **Tags in the details**: press and hold a tag to pick several, then Search, Download or Blacklist them. A tag’s menu can add it to the download blacklist (or take it off), and blacklisted tags are
  marked with a crossed-out eye.
- **The example compose file** includes FlareSolverr (optional) for sites behind Cloudflare.

### Improved

- **A new look**: the Plus Jakarta Sans font (bundled - nothing is fetched from elsewhere) and a fuller, consistent
  icon set (Lucide).
- **Smoother everywhere**: things slide, fade and move into place instead of popping in and out - dialogs, menus,
  lists, chips, panels - and text no longer shifts as an animation ends; buttons and chips that come and go let what’s
  beside them glide, and the details panel changes smoothly as you move to the next file.
- **Default sort**: newest uploaded first (unless you picked another).
- **Default layout**: Rows (each thumbnail in its file’s shape) on a device that hasn’t picked one.
- **A slimmer top bar on phones**: just the logo’s V, shorter file counts (159K files); the button that marks new
  files as seen is now an eye beside the search bar.
- **Searching any of the picked tags** has its own icon (a tilde, the search’s OR) instead of a plus.
- **Push notifications** for new files say "across N subscriptions" every time.

### Fixed

- **Blacklist**: an entry with a namespace (“creator:x”) now also stops downloads of posts tagged x - sites send
  bare tags, so such entries let files through before; adding from the details adds the bare tag.
- **Scrolling the library** no longer stalls while downloads run (the downloads menu reads much less, less often).
- **Deleting files elsewhere** (the blacklist’s clean-up, Duplicates) updates the media page at once, no reload.
- **Duplicates**: the picture no longer starts small and grows when the page opens, or shrinks after you keep one; the “still looking” progress is a thin bar under the heading.
- **iPhone**: the library’s last row of thumbnails keeps clear of the home bar and the curved corners; pages, sheets
  and the menu keep clear of the screen’s rounded corners at the bottom.
- **Android**: the home-screen icon’s logo is smaller (it was too big in round icons - add the app again to get it),
  the start-up screen goes straight to the app (no second one with a logo of another size), comic pages turn and close with a swipe again, and no
  thin line shows under the top bar while scrolling.

## 1.0.0 - 2026-10-23

The first public release.

- **A media library** for images, GIFs and videos: a fast tag search with chips, sort and filters, saved searches,
  favorites, a viewer with gestures and video controls, a details panel, trash with undo, and duplicates to compare.
- **A built-in downloader**: site plugins (Gelbooru, Danbooru, Shimmie, e-hentai and Mastodon kinds), one-off
  downloads and subscriptions with groups, a queue per site, retries, a blacklist and file size limits.
- **Comics**: galleries as comics with a reader, collections and progress.
- **Tags**: a tag browser, namespaces, parents and siblings, and tags read again from their sites now and then.
- **One web app** for desktop and phones (installable on the home screen), with push notifications, backups and
  stats; Docker images and programs for Linux, Windows and macOS.
