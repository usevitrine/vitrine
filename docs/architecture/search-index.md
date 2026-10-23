# The search index

The in-memory index (roaring bitmaps, column arrays) as built: how searches, sorts and autocomplete run, and the measured speed.

## Search index as built (step 1 item 4, 2026-10-14)
- `internal/index`: per-tag roaring bitmaps (`raw` = files that have the tag; `eff` = files that have it, a sibling
  or a descendant - the same bitmap for tags without relations), column arrays per file, sorted tag names
  (prefix autocomplete), trigram bitmaps (wildcards). Only live files are indexed (delete removes, undo re-adds),
  so counts need no extra filtering. One RWMutex: searches share it, updates are microseconds (~140 ns per tag
  change).
- Parents/siblings: the whole relation set is recomputed on every relation change (cheap - only tags with relations
  get their own bitmap); loops and two-ideal conflicts are rejected before anything is saved.
- Filters: a lone filter (e.g. `system:modified<5d`) scans its column in one tight loop; filters next to tag terms
  only check the tag matches. Sorting: small results are sorted directly; big ones walk a presorted list of all
  files, testing membership in a dense bitset (the fix the spike suggested). Presorted lists are rebuilt lazily
  after changes. Ties sort by ID so all paths give the same order.
- `internal/library`: every write is one DB transaction, then the index update, under one lock (changes reach the
  index in commit order). Tests rebuild the index from the DB after each scenario and compare it with the live one.
- **Measured** (an 8-core desktop CPU, synthetic Zipf data, `go test -bench`):

  | | 100k files / 85k tags | 1M files / 300k tags |
  |---|---|---|
  | Tag searches (one, two, NOT, OR group), first 1000 | 6-19 µs | 18-41 µs |
  | Group tag + has_audio + imported<5d | 0.12 ms | 0.95 ms |
  | `system:modified<5d` (the daily default) | 0.2 ms | 1.1 ms |
  | Wildcard `*senra*` | 0.1 ms | 0.55 ms |
  | Popular tag, whole result sorted by size | 0.34 ms (65k) | 6.9 ms (622k) |
  | Autocomplete, 1 letter / `creator:` (worst cases) | 0.4 / 0.6 ms | 2.3 / 5.2 ms |

  All within the targets (search < 20 ms, autocomplete < 10 ms at 1M). **Start-up**: building the index from
  meta.db at ~90k files / 4.4M links takes **1.2 s** - acceptable; the planned snapshot (load bitmaps from a file
  checked against `meta.generation`) can cut it to tens of ms later.
