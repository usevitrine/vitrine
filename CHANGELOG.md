# Changelog

What changed in each version of Vitrine, newest first. The app shows this too (Settings > What’s new).

## Unreleased

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
