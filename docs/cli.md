# The vitrine command

Everything the `vitrine` program does from a terminal: run the server, look at and steer a running Vitrine (its
queue, subscriptions, notices, log), and work on the library. `vitrine help` lists the commands; `vitrine <command>
-h` shows one command's flags and examples.

## Talking to a running Vitrine

While `vitrine serve` runs, it also listens on a **local socket**: a file in its data folder,
`<data_dir>/vitrine.sock`. The commands below talk to the server through it, using the same API the web app uses -
so what they change shows in the open web app at once, and the server's search index never goes stale.

- **No login**: whoever can open the socket file acts as the account. Only programs on the same machine can reach it
  (in Docker: only inside the container), and on Linux and macOS the file is readable by its owner only - the same
  people who can already read the library's database.
- **In Docker**: run the commands inside the container, e.g. `docker exec vitrine vitrine status` (the container
  is named `vitrine` in [`compose.example.yaml`](../compose.example.yaml)). The container's settings
  (`VITRINE_DATA_DIR` and so on) apply, so the command finds the socket by itself.
- **Another place**: `socket = "/run/vitrine.sock"` in the config file (or `VITRINE_SOCKET`); `socket = "off"` turns
  it off. Commands use the same setting, so give them the same config file or `-data-dir`.
- **Shared flags** (every command): `-config <file>`, `-data-dir <dir>`, `-files-dir <dir>`. The commands that only
  work with a running Vitrine also take `-json`: the server's answer as it is, for scripts.
- Flags may come before or after the other words: `vitrine queue add safebooru blue_sky -max 50`. A word that only
  looks like a flag stays part of the search (`-cloud` in `vitrine queue add safebooru blue_sky -cloud`).
- **Sites** are named by their plugin ID, as `vitrine sites` lists them. Nothing about a site is built into the
  commands: a newly installed plugin works with them at once.

### status

```sh
vitrine status
```

The version and how long it has been running, the library (files, size, tags, favorites), free disk space, the
queue (running, waiting, paused, failed), subscriptions (paused, failing), sites that are switched off or paused
after failing to answer, and unread notices.

### queue - the download queue

```sh
vitrine queue                       # what's downloading and waiting, per site
vitrine queue -a                    # finished downloads too (Downloads > History)
vitrine queue -w                    # keep the list up to date (Ctrl+C stops)
vitrine queue add safebooru "blue_sky -cloud" -max 50   # download a search once (-max: at most N files)
vitrine queue pause 12              # pause, resume or remove downloads by ID (the first column)
vitrine queue resume 12 13
vitrine queue remove 12
vitrine queue clear                 # remove the finished ones
```

Each line shows the download's ID, site, state and search, then its progress (posts gone through, of the site's
total) and what came of them (new files, ones the library had, deleted before, blacklisted, skipped, failed). A
subscription's check shows as "check of subscription #7" ("first check" for the one that takes everything); a site
that is paused or switched off is said under the list. `-label` gives a download a name in the lists.

### subs - subscriptions

```sh
vitrine subs                        # every subscription: state, search, group, files, last and next check
vitrine subs -site safebooru        # one site's
vitrine subs add safebooru blue_sky -every 1d -group Landscapes -first 100
vitrine subs check 7 8              # check now
vitrine subs pause 7                # pause, resume, remove by ID
vitrine subs every 7 12h            # a new schedule: 6h, 1d, 3d, 1w, 90m, ...
```

`add` takes `-every` (default 1d), `-first` (the first check gets at most this many files; 0: all) and `-group`
(the group is made if the site has none of that name).

### failed - posts whose file failed

```sh
vitrine failed                      # site, post, when, why (and the post's link)
vitrine failed retry safebooru 1234567 1234568
vitrine failed retry all
vitrine failed dismiss safebooru 1234567   # or: dismiss all
```

A retry runs in its site's queue (`vitrine queue` shows it).

### notices

```sh
vitrine notices                     # newest first, unread ones marked *  (-n: how many, default 30)
vitrine notices read                # mark all read - on every device
vitrine notices clear
```

The notices behind the web app's bell: downloads that finished or failed, new files from subscriptions, blacklisted
posts found, sites paused, and so on.

### logs

```sh
vitrine logs                        # the last 100 records, oldest first
vitrine logs -n 500 -level warn     # only warnings and errors
vitrine logs -f                     # keep printing new records (Ctrl+C stops)
```

The server keeps its recent log across restarts (in the data folder). Unlike the problem report (Settings > Stats),
site names aren't hidden here.

### api - any API call

```sh
vitrine api /api/stats
vitrine api "/api/search?q=blue_sky&limit=5"
vitrine api POST /api/favorite '{"ids": [12], "on": true}'
```

For anything the other commands don't cover, for scripts and for debugging: prints the server's JSON answer. The
endpoints are listed in [architecture/server.md](architecture/server.md); the web app's
`web/src/lib/api.ts` uses every one of them.

## The library

These work through the running Vitrine when there is one (the change applies at once), and on the library's files
directly when it isn't running.

| Command | What it does |
|---|---|
| `vitrine search <search>` | Search: `vitrine search blue_sky -cloud system:imported<5d sort:size` (`-n` how many, `-tags` each file's tags) |
| `vitrine tag -add <tag> -remove <tag> <file ID>...` | Add or remove tags (`-add`/`-remove` repeat) |
| `vitrine blacklist [list \| add <tag>... \| remove <tag>... \| matches]` | Tags never downloaded (`*` wildcards); `matches`: library files that have them |
| `vitrine setting [<name> [<value>]]` | The settings shared by every device (the web app's Settings); a value is JSON: `vitrine setting pauseAfter 10` |
| `vitrine sites [on \| off \| try <site>]` | The site plugins - running: switched off, paused, credentials, subscriptions; `on`/`off` switch one, `try` tries a paused one now |
| `vitrine credentials <site>` | A site's account or API key (asked for; typed values aren't shown). Running: used at once |

For these, flags come first (a search's `-cloud` is a search word): `vitrine search -n 5 blue_sky -cloud`.

## Without the server

| Command | What it does |
|---|---|
| `vitrine serve` | Run the web server (the web app, its API and the local socket); `-listen` sets the address |
| `vitrine passwd <name>` | Set the password (creating the account if needed); ends every session |
| `vitrine import <file or folder>...` | Copy images and videos into the library (originals stay where they are) |
| `vitrine import-comic <folder or zip>` | Add a comic to the Comics page |
| `vitrine download -max 20 <site> <search>` | Download a search without the server (Ctrl+C and run again to carry on) |
| `vitrine process` | Make missing thumbnails (and read videos' length, size, audio) |
| `vitrine backup` / `vitrine restore <file>` | Back up the library database (safe while it runs) / restore one (in place at the next start) |
| `vitrine config` | The settings in effect: where the library lives, which tools were found |
| `vitrine version` | The version |

`import`, `import-comic` and `download` open the library themselves: the files they add show in a running Vitrine
after it restarts (to download through the running one, use `vitrine queue add`).
