# Contributing

Thanks for helping. Vitrine is one Go program with its web app (Svelte 5, in
`web/`) built into it. Run the tests before sending a change:

```sh
go test ./...
cd web && npm run check && npm run i18n:check && npm run build && npm run e2e
```

## Dependencies and licenses

Vitrine is AGPL-3.0-or-later; anything it depends on must be compatible with that (MIT, BSD, Apache-2.0, ISC are).
The notices of everything built into the program are served at `/licenses`: the web app's come from the build by
themselves, the Go side's are generated - after adding or updating a Go dependency, run `npm run notices` in
`web/` and commit `internal/server/licenses/go.txt` (a test fails until then).

## Translations

Every text the app shows lives in `locales/`, one JSON file per language,
named by its language tag (`en-US.json`, `en-GB.json`, `de-DE.json`, ...).
The web app bundles these files when it's built, and the server embeds the
same files for the push notifications it sends.

- **`en-US.json` is the main file**: it has every text.
- **Every other file lists only what differs** from it. Anything it leaves out
  comes from en-US, so a half-done translation still works.
- `"_name"` is the language's name in its own language, as the picker in
  Settings > Viewing shows it (`"English (UK)"`, `"Deutsch"`).
- `"_group"` (optional) is the thousands separator the server uses in push
  notifications (`","` by default); the web app formats numbers by itself.

### Add a language (no code needed)

1. Copy `locales/en-US.json` to `locales/<tag>.json` (for example
   `locales/de-DE.json`) and set `"_name"`.
2. Translate the texts. Delete any you leave as they are - they fall back to
   en-US.
3. Run `npm run i18n:check` in `web/` (it says what's wrong, if anything).
4. Send a pull request.

### The rules of a text

- **Placeholders** in braces are filled in by the app: `"Deleted {n} files"`.
  Keep them (you can move them); don't add new ones.
- **Plurals**: a text that depends on a number is an object of plural forms,
  picked by `{n}` - `{ "one": "{n} file", "other": "{n} files" }`. Use the
  forms your language has (`zero`, `one`, `two`, `few`, `many`, `other`, as
  [Unicode CLDR](https://cldr.unicode.org/index/cldr-spec/plural-rules) names
  them); `other` is always needed.
- **Bold and code**: `<b>...</b>` and `<c>...</c>` mark words shown in bold or
  as code. Keep the markers around the words they belong to - no other HTML
  is allowed (it's shown as text).
- **Never translated**: tags, site names, search syntax (`system:`, `fav:me`,
  `blue_sky`), file types (GIF, MP4) and units like MB.

### Which language a device gets

Settings > Viewing > Language (shared by every device; *Automatic* by
default), else the browser's languages (`en-AU` finds `en-GB`, a plain `de`
finds `de-DE`; a plain `en` picks British or American English by the time
zone), else the server's `language` setting (`VITRINE_LANGUAGE`), else en-US.

### For developers

In the web app, `t('key', { n })` from `web/src/lib/i18n.svelte.ts` gives a
text (`num()`, `date()`, `time()` format numbers and dates the language's way;
`<Rich text={...} />` shows `<b>`/`<c>` parts). New texts go into en-US.json
(and en-GB.json if British spelling differs). `npm run i18n:check` reports
texts the code asks for that en-US.json doesn't have, and texts nothing uses.
Error messages from the server are English for now.
