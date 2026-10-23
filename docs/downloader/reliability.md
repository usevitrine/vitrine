# When sites misbehave

Cloudflare and FlareSolverr, stalled and trickling downloads, and the automatic pause of a site that stops answering.

## Cloudflare and FlareSolverr (2026-10-16)
- Every site request goes through `plugin.Client.getWith`. A reply that is a Cloudflare challenge (403/503 with
  `cf-mitigated: challenge`, or `Server: cloudflare` and a challenge page - `cf-chl` markup or a "Just a moment" title;
  not Cloudflare's challenge-platform script alone, which it puts in a site's own error pages too: a site down behind
  Cloudflare is an error saying its page's title, e.g. "answered 503 Service Unavailable (its page says: Temporarily
  down)", 2026-10-21) is passed with **FlareSolverr** when its
  address is set (Settings > Sites > Cloudflare; stored as the setting `flaresolverrURL`, applied at once and at start):
  `plugin.Solver` asks FlareSolverr (`POST /v1 request.get`, one visit per host at a time, others wait for it) and keeps
  the clearance (cookies + the browser's User-Agent, which must go together) per cookie domain until `cf_clearance`
  expires (30 min without a date). The request is sent again with it; later requests carry it from the start.
- FlareSolverr visits the URL **without credentials** (the clearance is for the whole site). If the site still refuses
  cleared requests (it checks more than cookies), a page request uses the page FlareSolverr's browser got (only when
  that was the same URL); otherwise a clear error. Files never go through FlareSolverr.
- Without FlareSolverr a challenge is `CloudflareError` ("blocked by Cloudflare - set up FlareSolverr in Settings >
  Sites"). FlareSolverr must reach sites from the same IP as Vitrine (behind a VPN: share its network namespace).
- While a clearance is in use, requests carry the browser's User-Agent instead of Vitrine's (Cloudflare ties the
  clearance to it).

## Stalls and logs (2026-10-20)
- A file download with no data for a minute (no answer, or a stall mid-file) is given up (`plugin.ErrStalled`; tried
  again like any connection trouble): a hung connection used to hold its download slot - and a manual Retry - for good.
- The log file (DataDir/logs, two files of 16 MB) takes debug detail whatever the terminal's level: each search's
  start (resumed or not) and end, each page (posts, total, time), each post (outcome, file, time, error), each file
  attempt (bytes, time, error), each site request (host and path - never the query - status, time), manual retries,
  thumbnail batches. Every web request is logged only with VITRINE_LOG_LEVEL=debug (it's every thumbnail too).

## A site that stops answering pauses by itself (2026-10-21)
- `plugin.Client` counts each site's **connection failures in a row** (no answer, a 5xx, a Cloudflare block it can't
  pass; any other answer - a 404 too - resets the count; canceled requests don't count). After `pauseAfter` of them
  (setting, Settings > Sites "When a site stops answering": 3 / 5 (default) / 10 / 20 / never = 0; also
  `vitrine setting pauseAfter <n>`) the site is **paused** (`breaker.go`): its requests fail at once with
  `plugin.ErrPaused` for 2 min, then one request tries again (the others still wait); a failed try pauses it longer
  (5, 10, 20, 30 min, then hourly), an answer ends the pause. Try now (Downloads) ends the wait.
- The downloader treats a paused site as **waiting, not failing**: a search page that got no answer is asked again a
  few times first (3 s, 6 s, ...: a briefly unwell site carries on, a down one pauses on the way); a search or a
  subscription check on a paused site stays running where it was (resumed once it answers), a post whose file failed
  because of it isn't recorded as failed, the site's worker sleeps until the next try, background work (tag types,
  tag refresh) waits until its next try is due (then the next lookup or refresh is that try). **Tag refreshes still
  queue up** while it's paused: a selection's Refresh tags marks its posts due as always, and one file's Refresh tags
  (details) queues a paused site's post instead of asking (`{found, queued}`); the Tag refresh card shows the site
  Paused; the queue is worked through once it answers. Only connection failures count: a site answering nonsense still fails the search as before.
- Told **once each way**: the events `paused` (with the last failure) and `back` - a bell notice and a push
  notification (under the "failed" push setting), not one per try or per subscription. The Downloads page shows a
  Paused chip by the site and a line under its queue (why, when it tries again, Try now); its running search shows as
  Waiting. `GET /api/downloads` has `sites_paused` (site -> {since, until, reason, tries}), `GET /api/sites` each
  site's `paused`; `POST /api/sites/{id}/try`.
- The e2e test library sets `pauseAfter` to 0 (its test sites never answer); the pause test turns it on for itself.
- **Kept over restarts** (2026-10-21): each change of a site's pause is saved (`site_pauses`, migration 0032;
  `library.SetSitePause`), and a site's client starts with it (`Client.RestorePause`, also after a plugin update):
  still paused, its next try and its tries as they were - a restart doesn't start the waits over or tell the user
  again. Answering again deletes it.
