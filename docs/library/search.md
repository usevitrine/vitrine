# Search

What search must do (OR groups, editable chips) and the search syntax as built (tags, wildcards, system: filters, sorts, limits).

## Search
- Hydrus-level: tags with AND / OR / NOT, wildcards (`*sky*`), namespace searches, parent tags; system filters:
  import/modified time, has audio, filetype, duration, dimensions, size, site rating, tag count, limit; many sort
  orders. Autocomplete with counts, saved searches, favorite tags (incl. OR groups), a default search.
- **Two ways to enter a search, both producing chips**: type booru-style text (`tag -excluded ( a ~ b )`) - it turns
  into chips - or build chips with autocomplete and `system:` filter pickers.
- **OR groups** (like hydrus-web's "OR Search"): an OR button opens a picker with its own autocomplete to build a list
  of alternatives; the group is one chip shown as `a OR b OR c`; it can include negated tags and system filters and
  can be saved as a favorite. Typing `( a ~ b )` creates the same chip.
- **Every chip is editable in place** (hydrus-web can only remove and re-add): tap a tag chip to change its text (with
  autocomplete) or toggle exclude; "add alternative" turns a tag chip into an OR group; tap an OR chip to
  add/remove/edit members, negate one, or split it back into separate chips; drag a chip onto another to OR them
  (mouse and touch); tap a `system:` chip to change its value ("imported in the last 1 day" -> "7 days"). Edits re-run
  the search instantly; favorites are edited the same way.
- Typical daily use to optimize for: browse recent imports newest-first; quick one-tap filters (has audio, imported
  in the last hour/day); group tags; site tags; wildcard tags.

**Plain site tags and comics** (2026-10-21): a plain site tag (no tag types: "3d" from baraag) lands on the one
namespaced tag of that name (meta:3d) - not counting tags only comics have (a gallery site's other:3d made the name look
ambiguous: a plain 3d was made, every re-synced file moved over to it and counted as modified, so old files came back
to the top of "modified recently"). Migration 0029 merged the three split tags back (files' modified times untouched).

**Suggestions keep to the sites picked** (2026-10-21): with `system:site=` chips in the search (not negated), the
search box suggests only tags of files from those sites, counted on those files; New download suggests the chosen
site's (`/api/autocomplete?sites=a,b`; by the files' sites - the index keeps no origin per tag). A site with no files
in the library yet (not downloaded from): every tag.

**Suggestions: none already there; marked ones go in together** (2026-10-23, the user: "if a tag is already in the
search bar it shouldnt be in the autocomplete list", "mark tags in autocomplete to add to search in an AND or OR
search", "on pc ... arrow keys to navigate and space bar to select"): `TagInput` leaves out the search's chips
(`exclude`) and the box's other words (18 asked for, 12 shown). With `anyOf` (the search bar; New download's search: the
site's OR search, none where it has none) each row has a round mark at its end (tap it; Ctrl/Cmd+click; arrows then
Space); marks last while the box has the focus, across words typed one after another, and a bar at the list's bottom
shows them (tap one to unmark) with **All of them** (each as a chip; Enter) and **Any of them** (one OR chip; Shift+Enter)
- phones: the tags on one line, the two buttons under them, full width. The list scrolls under the app's floating
scrollbar (`overlayScroll` on its inner `ul.scroll`; the box around it is what moves to the page's top level).

## Search syntax as built (step 1, 2026-10-14)
The text form (`internal/search`; every chip maps to one term, so text and chips convert both ways losslessly):
- `tag` - a plain name matches that tag **in every namespace** (`some_artist` finds `creator:some_artist`), like Hydrus;
  `ns:tag` only that namespace. A prefix is a namespace only if the library has one by that name, so verbatim site
  tags like `9:16` or `rating:safe` stay literal. Matching is case-insensitive; tags the user types are trimmed and
  lower-cased (site tags are stored verbatim).
- `-tag` excludes; `*` is a wildcard anywhere (`*sky*`, `blue*`, `creator:*`).
- `( a ~ -b ~ system:has_audio )` - OR group (the syntax of gelbooru-style sites); members can be negated tags, wildcards or filters.
  Parentheses and `~` are only syntax as separate words, so tags like `foo_(artist)` work.
- `system:` filters, no spaces inside: `imported`, `modified` (age `<5d` = less than 5 days ago - units s, m, h, d,
  w, mo, y - or a date `>=2026-01-31` / `=2026-01-31T15:04` in local time, `=` meaning that whole day/minute),
  `size` (`>1.5GB`, `<100KB`; 1 KB = 1024 B), `width`, `height`, `duration` (`>30s`, `<=1m30s`; a bare number is
  seconds), `tags` (tag count), `has_audio` (no value; `-system:has_audio` for silent), `type` (`=video`,
  `=image`, `!=jpg,mp4`, any format name or extension), `rating` (`=general|sensitive|questionable|explicit`,
  `safe` = general, `!=` too). Operators: `< <= > >= = !=`.
- `sort:` `imported` (default, newest first), `modified`, `size`, `width`, `height`, `duration`, `tags`, `random`
  (`random_<seed>` repeats an order; each file's place is a hash of seed and ID, so files added later land in the
  order without reshuffling the others - 4.7 ms for 100k files); add `_asc` for ascending. `limit:N` keeps the first
  N in sort order.
- Unknown tags match nothing and are reported back (so the UI can flag a typo).
- **Modified time** = the last time the file's tags actually changed by the user's own edits (adding, removing,
  merging duplicates) or by a site new to the file (the same file from another site), or its import time. A tag
  refresh of a site the file already has doesn't count (2026-10-21; before, "modified recently" filled up with
  files whose only change was a site's retagging - those earlier times stay).
- Site ratings are stored as a number: 1 general/safe, 2 sensitive, 3 questionable, 4 explicit (plugins map their
  site's letters - on gelbooru-style sites `s` means safe, on Danbooru it means sensitive).
- **system:found_by=<subscription ID> (2026-10-22)**: the files a subscription downloaded (the index keeps a bitmap
  per subscription from `file_sources`, added to as files come in; a merge of duplicates shows on the next start).
  Its chip reads "found by “<the subscription's name>”" (`lib/subNames`). The Downloads page's "+N" opens it.
- **No "No tag …" line (2026-10-22)**: the user - an empty result says enough; unknown tags still match nothing.
