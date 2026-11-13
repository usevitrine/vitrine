# Rules for working on Vitrine

The rules every change follows: the repo will be public, so nothing personal goes in; how texts, spelling and
examples are written; how decisions are recorded.

## Public-project rules
- Nothing specific to anyone's setup in code or defaults (no hostnames, IPs, paths, Proxmox assumptions) - config.
- No secrets in the repo or plugins; credentials/API keys/Web Push keys live in the user's data dir.
- Don't copy code from Hydrus, hydrus-web, gallery-dl, hydownloader, Stash or monbooru unless its license is
  compatible with AGPL-3.0 (e.g. GPL-2.0-only is not) - otherwise implement from APIs/docs
  (`docs/project/background.md`).
- NSFW site plugins only in the plugins repo; the core repo ships a neutral example plugin.
- Respect site rate limits by default, identifiable User-Agent with a contact URL, no telemetry.
- README, plugin-author docs, tests and benchmarks; CI only from the first public release (run tests locally until
  then).
- Personal or deployment details go in `CLAUDE.local.md` (git-ignored), never in tracked files.
- Examples in code, tests and docs use neutral tags, artists and sites (`blue_sky`, `some_artist`,
  `favorite_artists`, safebooru.org) - never real personal searches or tags.

## Words
- **American spelling** in code, comments, tests and docs (color, favorite, center, normalize).
- UI text lives in `locales/` (en-US main, en-GB lists only what differs; `docs/ui/languages.md`): new texts go in
  en-US.json (+ en-GB.json when British spelling differs) and `npm run i18n:check` must pass.
- UI hints only where a label could mislead, in a few words (`docs/ui/design-language.md`).

## Working rules
- **Never print secrets** (API keys, site credentials, tokens, Web Push keys), not even partially - mask them.
- Before using a library API, check current docs (versions move, e.g. Svelte 5.x).
- Explain things to the user in plain terms.
- UI work is checked on a phone and a desktop size (screenshots) before it's called done; iOS gets the same care.
- **A fix to what's already in a library is a fix in the fixes menu** (the wrench: offered with what it does, Run or
  Skip, progress, OK when done - `docs/ui/notifications.md`, "Fixes from updates"), never a silent migration or start-up
  pass (2026-11-13, the user: "whenever we do a database/library fix like this it should be a fixes menu thing").
  Migrations change the schema; data that was saved wrong is put right by a fix.
- Tests: `go test ./...`; in `web/`: `npm run check`, `npm run i18n:check`, `npm run build`, `npm run e2e`
  (`docs/architecture/testing.md`).

## Docs
- The docs are split by topic so only what a task needs is read; `docs/README.md` maps them.
- When a decision is made or changed, update the matching topic doc (and `docs/project/overview.md` if it's a key
  decision). New built behavior goes in the topic's "as built" part, dated.
- A topic doc that grows past ~300 lines is split further (and `docs/README.md` updated).
- **`CHANGELOG.md`** (shown in the app: Settings > Updates, built in at build time): every change a user would
  notice gets a line under `## Unreleased`, in plain words (what it does for them, not how), in its section -
  `### Action needed` (what a user must do: see "Versions"), `### New` (features), `### Improved` (better at what it
  did), `### Fixed` (bugs) - in that order, a section only
  when it has lines (the user, 2026-10-25: "i want the changes sorted into different section such as bug fixes new
  features"). An item mixing kinds is split. At a release that heading becomes `## <version> - <date>` in the same
  commit the tag goes on, and a new `## Unreleased` starts empty (a hotfix: `## X.Y.Z-hotfix - <date>`, the tag
  `vX.Y.Z-hotfix`). The app shows the sections as headings
  (`lib/changelog`'s groups) and `` `code` `` as code; a version before 1.0.3 reads them as plain lines.

## Versions
Decided 2026-11-13 (the user: "we need to figure out strict criteria for what a 1.0.x, 1.x.0 and an x.0.0 release is").
Every release gets the image tags `X.Y.Z`, `X.Y` and `X`: someone on `X.Y` expects fixes only, someone on `X`
nothing that needs them to act. A release is the highest kind anything in it calls for:
- **Patch (X.Y.Z+1)**: only `### Fixed` and `### Improved` - what was there works better or looks right. No new
  setting, screen, API endpoint, CLI command or plugin manifest field, and **no new database migration** (an older
  version refuses a newer database, so a patch must always be safe to go back from).
- **Minor (X.Y+1.0)**: anything `### New` - a feature, setting, screen, API endpoint, CLI command, plugin engine
  feature - and any migration, as long as it only adds: everything that worked before works the same way.
- **Major (X+1.0.0)**: anything under `### Action needed` - a feature, setting, CLI command, API endpoint, config key
  or environment variable removed or renamed; a plugin format change that breaks existing plugins; a platform dropped;
  compose, NixOS options or the install needing edits; data lost or logins to enter again. Each entry says exactly what
  to do.
- **Hotfix (vX.Y.Z-hotfix)**: the same version again, for the installer or the programs only (unchanged).
- Enforced by the release script (`deploy.local.sh`, git-ignored): it works the kind out from CHANGELOG.md's top section
  and the migrations added since the last tag, and tags nothing unless that section's heading is that version.
  `./deploy.local.sh release check` says which version the next one is. The 1.0.x releases before this rule had
  features in them too; it applies from 1.1.0 on.
