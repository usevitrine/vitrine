# Planned features

Library
1. Content-addressed file store; files come from the downloader (site searches and subscriptions). **No web upload or
   watched folders** (decided 2026-10-16: Vitrine is for libraries built from imageboards/boorus, not for adding your
   own files); the CLI `vitrine import` stays for testing.
2. Tags with namespaces (only where a plugin declares them); **tag parents and siblings** (editable, applied in
   search/display, cycle checks); tag origin per file (site or user).
3. Post URL and **source** links kept apart; import/modified times.
4. Delete with a 7-day undo window; tombstones so deleted files are never downloaded again.
5. Periodic **tag refresh** of existing posts.
6. Not planned: user ratings (favorites instead: a heart, 2026-10-19; an opt-in "I came" counter with Rankings, 2026-10-20), inbox/archive, PTR, Hydrus import, adding your own files (web upload, watched folders). Low priority: notes, view counts.

Search
7. Tags with AND / OR / NOT, wildcards, namespace searches; parent tags find their children's files.
8. System filters: import/modified time, has audio, filetype, duration, dimensions, size, site rating, tag count,
   limit; many sort orders.
9. Booru-style typing and chips; **OR groups**; **every chip editable in place**; autocomplete with counts, saved
   searches, favorite tags, a default search.

Media
10. Thumbnails for images/video/animations; video duration/audio/codec detection; preview-frame sprite sheets.
11. Resized renditions for huge images; transcodes/fast-start copies when a device can't play a video well.
12. Duplicate detection (perceptual hashes) with a review screen - **done 2026-10-16** (see `docs/library/duplicates.md`); automatic handling of exact matches: later, if wanted.

Web client
13. Mobile-first PWA, equally polished on desktop (shortcuts, drag-select, side panels, **bulk tag editing** - the
    selection's tag editor with per-tag counts, built 2026-10-16).
14. Own video player; never-ending browsing with grid/viewer in sync (see `docs/ui/grid-and-search.md`, `docs/ui/viewer-and-video.md`).
15. Settings stored server-side; push notifications (Web Push).

Downloader (see `docs/downloader/`)
16. Straight into the library, file by file; exact resume; stops only between files; md5 check before download.
17. A worker per site; ordered queue with switching at file boundaries; per-site limits; ETA.
18. Blacklist on post tags before download; file rules (size/megapixels, mp4 vs webm).
19. Searches -> subscriptions without overlap; subscription groups; history kept when moving; accurate counts.
20. One-off clean-ups: delete by tags, purge newly blacklisted tags.
21. Plugins: manifests + sandboxed JS, separate plugins repo/index, self-tests, update checks.

Platform
22. Built-in login (single user); optional egress/VPN check.
23. Docker image and native binaries, both first-class.
24. Backup command (consistent DB snapshot); everything else regenerable or plain files. **Built 2026-10-16**: daily
    backups, Settings > Backups (back up now, download, restore, restore from a file), `vitrine backup` / `restore`.
