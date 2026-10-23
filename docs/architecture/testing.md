# Testing

Go tests and the Playwright end-to-end tests (projects, the per-worker test servers and library, how to run them).

## End-to-end tests (as built, 2026-10-19)

`web/`: `npm run e2e` (Playwright; projects: an emulated iPhone on WebKit, desktop WebKit, desktop Chromium).
`e2e/setup.ts` (global setup) builds the web app and Vitrine once, generates the test media (kept between runs
in the temp folder), imports it into a template library with the test site plugins and a local plugin list, then
starts one `vitrine serve` per worker on its own copy of that library (port 8100 + worker; logs `w<n>.log` in the
temp folder). `e2e/fixtures.ts` points each test at its worker's server, so tests run fully in parallel without
sharing files, tags or settings - and they test the built app, the way it ships (not the dev server). Workers:
half as many as the cores to use, at most 3/4 of the machine (6 on 16 cores); `E2E_WORKERS` overrides. A full run
takes about 2 minutes (it was 10 one at a time). The parallel runs found two real timing bugs: the scrub bar gave
up waiting for a slow seek after 400 ms and started another on top of it (now it waits while the video is still
seeking), and the duplicates page left newly shown videos paused when switching flip / side by side after they
had started.

## As built (web client, from 2026-10-14 on)
- **E2E now also runs on desktop Chromium** (Chrome, Brave, Edge): `desktop-chromium` project.
  It found a real bug: the viewer's wheel throttle started at 0, so a wheel in the first 300 ms after the page loaded
  was ignored. Tests that change shared server state run once (desktop-webkit).
- **Shared server state between tests (2026-10-22)**: the Settings > Sites test updates `test-down` to 1.1.0 from the
  local plugin list mid-run; that version had no OR search, so every OR test that ran after it failed (the old
  "flaky" chip / details / OR-download tests). The 1.1.0 fixture keeps `or: "( {tags} )"` now. A test that changes
  a site for good must leave what other tests rely on as it was.
- **Counting grid tiles (2026-10-22)**: the grid draws only the rows near the screen, so a count of `.tile`s
  scrolled down can be short of what's loaded (the Browse test's "23 tiles" failed now and then with all 23 there).
  Check for the item itself (its title) after scrolling to it, or the request that loads it.
- **Nothing left behind (2026-10-23, the user: "what can you do about the broken tests")**: each worker has its own
  server, so tests never clash at the same moment - they clash through what one leaves for the next on that server.
  Found by running the whole suite twice over without retries (`--repeat-each 2 --retries=0`): Media switched off
  (the sections test's save left as the page closed, past its routes - every later test opened on Comics), a bulk tag,
  a renamed tag, a gallery subscription, saved searches, a blacklist delete. The rules now: a test removes what it
  made (subscriptions, tags, saved searches, collections - `afterEach` for those that can fail halfway), a test that
  routes the settings answers their reads from the same record and waits for a save before leaving the page, and
  tests look only at their own data (unique names, filters) rather than counting everything. The blacklist test
  answers the blacklist and the delete itself (it deleted two real files for a moment).
- **Timing (2026-10-23)**: a short-lived state (the hover card's 220 ms shrink) is noted by a MutationObserver as it
  happens, not polled for; a state the app reaches eventually (two videos in step after a flip) is polled for until it
  holds, not read once after a fixed wait. Waits are 10 s for something to show, 45 s per test (`playwright.config.ts`):
  six browsers at once made the 5 s default run out now and then.
