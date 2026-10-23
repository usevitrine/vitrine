# Vitrine - notes for Claude

Vitrine is a fast, self-hosted media library with a built-in downloader and its own web client: one Go program
(`cmd/vitrine`, `internal/`) with a Svelte 5 web app (`web/`) embedded, site plugins in the separate
`usevitrine/vitrine-plugins` repo.

## Read only what the task needs
The docs are split by topic in `docs/`; **`docs/README.md` maps them** (one line per file). Open that map, then
only the files the task touches - e.g. a Downloads page change: `docs/ui/design-language.md` +
`docs/ui/downloads-page.md`; an engine fix: `docs/downloader/engines.md`. Start with `docs/project/overview.md`
when you need the big picture, and always follow `docs/project/rules.md`.

Also read **`CLAUDE.local.md`** if it exists: the author's own setup, deployment and measurements (git-ignored -
the repo will be public).

## Always
- Never print secrets (API keys, credentials, tokens, Web Push keys), not even partially.
- Nothing personal or setup-specific in tracked files; neutral examples (`blue_sky`, `some_artist`, safebooru.org).
- American spelling; UI texts in `locales/en-US.json` (+ en-GB when it differs), `npm run i18n:check` must pass.
- When a decision is made or changed, update the matching doc in `docs/` (see `docs/project/rules.md`, "Docs").
