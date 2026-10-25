# Changelog

What changed in each version of Vitrine, newest first. The app shows this too (Settings > What’s new).

## Unreleased

- **Settings > Library** (was Media): a card each for Media, Comics and the trash. Comics has its own default order (the page opens in it) and Browse search, the search - or one of the site's own lists, like Popular - Comics > Browse opens with, set in Browse's own Sort and filters menu.
- **Windows installer**: `Vitrine-Setup-<version>-x64.exe` installs Vitrine for you (no administrator needed) with ffmpeg and libvips included. It asks where to keep the library, the original files and the backups, the port, and whether phones on your Wi-Fi may open it; it can start Vitrine when you sign in, add the `vitrine` command to PATH and put Vitrine in the Start menu (and on the desktop). Vitrine then lives in the notification area: click it to open Vitrine, or use its menu - the address for your phone, your files, the log, Start with Windows, Restart, Quit. Update in Settings installs new versions by itself and brings Vitrine back. Uninstalling keeps your library unless you say otherwise.
- **Portable**: a `config.toml` next to the program makes Vitrine portable - with `data_dir = "data"` everything lives in that folder (move or copy it), a double-click starts the server, and ffmpeg/libvips placed beside it are used. Relative paths in a config file now count from the file's folder, not from where Vitrine was started.
- **Tiles keep their insides**: as a cover zooms, a row resizes or a collection opens, the picture keeps its proportions and the title, chips, marks and badges keep their size and place (they shrank and grew back, the pages icon drifted, and the title vanished as a comic's cover zoomed); a comic's title, chips and gradient stay sharp and still while its cover zooms.
- **Comics downloads count pages**: the Downloads menu, the bell and notifications say how many pages a gallery brought ("40 new pages"), not "1 new file".
- **Comics > Browse**: galleries with a blacklisted tag are hidden; a gallery's pages show in the details panel as they come (each in its place at once, filling in sheet by sheet), the next ones fetched ahead; Download there is purple; a new search clears the last one's galleries at once (a spinner until the new ones come); covers that fail in a bad moment are fetched again (a burst of the site's failures left them broken), a cover that never comes stops spinning, and a new search never shows the last one's covers (Chrome kept them, titles and all).
- **Trash**: going there no longer shows the library's files for a moment first, and Empty trash spins until it's done.
- **New filters**: New (the files marked NEW) and Animated (videos and animated pictures), in Sort and filters - or type `system:new`, `system:animated`.
- **Downloads are checked**: every file is compared with the md5 its site gives for it; one that came out wrong is downloaded again (a site whose md5 doesn't match its own file still gets its file).
- **Duplicates**: a big picture still loading shows a spinner on its side until it's there (only the blurry thumbnail showed); the spinner shows on bright pictures too.
- **Top bar**: always there - it no longer slides away when you scroll down.
- **Viewer on phones**: the controls split like a phone's photo app - close, where you are and the details at the top, favorite, I came and full screen along the bottom in reach of your thumb; the size shows a moment while you zoom. Large libraries no longer push the buttons off the screen.
- **Taps after a swipe**: a tap right after a quick swipe - to the next picture, or down to close it - works at once anywhere in the app (the browser ignored it, so it took a second tap).

## 1.0.2 - 2026-10-25

- **Fixes**: a file that finished just as its download was stopped is counted as downloaded (it showed as "had" after resuming); a site behind Cloudflare no longer loses its fresh clearance when several requests are challenged at once.
- **Phones**: every dropdown opens as a sheet from the bottom, like the other menus.
- **Pixel-perfect motion**: the details panel opens and closes without the picture snapping aside at the end; bottom sheets no longer leave a dark band behind as they close; search suggestions fade in and out; Settings and Tags sections cross-fade instead of the old one vanishing; a picture fades from its thumbnail to full sharpness; a comic's title fades in as the reader closes, its cover rounding its corners on the way back; pages cross-fade when you go from one to another; Duplicates' next pair fades in; the comic reader closed with Back flies home like its close button.
- **New download**: the limit is labeled Posts (it counts every post gone through - new, had or blacklisted).
- **The menu shows what's in the trash**: deleted files and comics together on Trash, each on its own under it.
- **Duplicates**: the last pair fades away cleanly (it shrank first).
- **Top bar**: the downloads and bell icons are a little smaller, their counters too.
- **Trackpad gestures in the viewer**: swipe with two fingers to go to the next or previous file (it follows your fingers), and move a zoomed picture with two fingers; pinch zooms.
- **The bell and the Downloads menu** open with their stacks folded.
- **Downloads button**: in the top bar on every page, not only the library.
- **Smoother motion**: stacks unfold from under their front card and fan out a little when pointed at; menus settle without a last-moment jump; Downloads tabs and Add site's kinds switch cleanly (nothing lingering or jumping). The Subscriptions tab is "Subs".
- **Hover previews**: a thumbnail zooms into its card smoothly - no more shaking as it grows or shrinks back.
- **Updates**: a page of its own (Settings > Updates, in place of What’s new) - Vitrine tells you when a new
  version is out (and the bell does; it checks GitHub once a day and can be told not to) and shows what’s new in
  it above the changes so far. With Watchtower set up (see the example compose file), Update installs
  it from there: a backup first, then a full-screen updater follows each step and fades the app back in on the new
  version.
- **Notifications**: pop-ups stack by kind like the Downloads page (newest in front, the rest peeking out behind;
  point at or tap a stack to fan it out), sit just above the bottom of the screen and come and go smoothly; the bell
  stacks the same way. A notice about one file or comic names it (“Deleted file #1234”); several just say how many.
  Push notifications no longer show site names, tags or searches - only what happened and how many.
- **Downloads**: a download's line is shorter - "57,734/72,588" (posts gone through, out of the limit you picked or
  the site's count), then icons with counts for new, had, blacklisted and the rest. A limit now counts every post
  gone through, blacklisted ones too, like the line does.

## 1.0.1 - 2026-10-24

- **Adding sites**: the list of sites to add has a search, filters by kind (image boards, galleries, social), shows
  the first few with "Show all", and slides open, filters and installs smoothly.
- **Site logins**: entered in a dialog (a site’s ⋯ menu, or the line on its card) with a Test button that tries
  what you typed before saving it.
- **Downloads menu**: the download button in the top bar shows how many downloads are running or waiting, and opens
  a list of them with their progress, plus New.
- **Default sort**: newest uploaded first (unless you picked another).
- **A new look**: the Plus Jakarta Sans font (bundled - nothing is fetched from elsewhere) and a fuller, consistent
  icon set (Lucide).
- **Default layout**: Rows (each thumbnail in its file’s shape) on a device that hasn’t picked one.
- **iPhone**: the library’s last row of thumbnails keeps clear of the home bar and the curved corners.
- **Searching any of the picked tags** has its own icon (a tilde, the search’s OR) instead of a plus.
- **A slimmer top bar on phones**: just the logo’s V, shorter file counts (159K files); the button that marks new
  files as seen is now an eye beside the search bar.
- **Smoother everywhere**: things slide, fade and move into place instead of popping in and out - dialogs, menus,
  lists, chips, panels - and text no longer shifts as an animation ends; buttons and chips that come and go let what’s
  beside them glide, and the details panel changes smoothly as you move to the next file.
- **iPhone**: pages, sheets and the menu keep clear of the screen’s rounded corners at the bottom.
- **Tags in the details**: press and hold a tag to pick several, then Search, Download or Blacklist them. A tag’s menu can add it to the download blacklist (or take it off), and blacklisted tags are
  marked with a crossed-out eye.
- **Blacklist**: an entry with a namespace (“creator:x”) now also stops downloads of posts tagged x - sites send
  bare tags, so such entries let files through before; adding from the details adds the bare tag.
- **Scrolling the library** no longer stalls while downloads run (the downloads menu reads much less, less often).
- **Deleting files elsewhere** (the blacklist’s clean-up, Duplicates) updates the media page at once, no reload.
- **Push notifications** for new files say "across N subscriptions" every time.
- **Duplicates**: the picture no longer starts small and grows when the page opens, or shrinks after you keep one; the “still looking” progress is a thin bar under the heading.
- **Android**: the home-screen icon’s logo is smaller (it was too big in round icons - add the app again to get it),
  the start-up screen goes straight to the app (no second one with a logo of another size), comic pages turn and close with a swipe again, and no
  thin line shows under the top bar while scrolling.
- **The example compose file** includes FlareSolverr (optional) for sites behind Cloudflare.

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
