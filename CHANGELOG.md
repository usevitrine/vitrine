# Changelog

What changed in each version of Vitrine, newest first. The app shows this too (Settings > What’s new).

## Unreleased

- **Adding sites**: the list of sites to add has a search, filters by kind (image boards, galleries, social), shows
  the first few with "Show all", and slides open, filters and installs smoothly.
- **Site logins**: entered in a dialog (a site’s ⋯ menu, or the line on its card) with a Test button that tries
  what you typed before saving it.
- **Downloads menu**: the download button in the top bar shows how many downloads are running or waiting, and opens
  a list of them with their progress, plus New.
- **Default sort**: newest uploaded first (unless you picked another).
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
