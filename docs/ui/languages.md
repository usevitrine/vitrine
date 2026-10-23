# Languages

UI texts in locales/ (en-US main, en-GB only what differs), how they're loaded and checked.

## Languages as built (2026-10-20)
- Every UI text is in `locales/<tag>.json` (top level: the Go package `locales` embeds them; the web app bundles them
  with `import.meta.glob`). **en-US is the main file** (every text, American spelling); **en-GB** lists only what
  differs (favourite, colour, cancelled, any more). Adding a language = copy, translate, pull request - see
  `CONTRIBUTING.md`. `npm run i18n:check` (web/scripts/i18n-check.mjs): texts the code asks for that en-US lacks,
  other languages' keys/plural forms/placeholders that don't match en-US, unused texts (keys built from a prefix
  count via a template `` `prefix.${x}` `` or an `// i18n-keys: prefix.*` comment).
- Web: `lib/i18n.svelte.ts` - `t(key, vars)` ({name} placeholders; plural objects picked with `Intl.PluralRules` by
  `vars.n`; numbers in placeholders formatted for the language), `num`/`date`/`time`/`dateTime`, `rich()` +
  `components/Rich.svelte` for `<b>`/`<c>` parts (never HTML). Static label tables became getters or `$derived`
  (`choice()` in lib/system, `section()` in lib/settingsSections) so a language change re-renders them; a loop
  variable or parameter named `t` was renamed where it would hide `t()`. Styles for bold parts inside `Rich` use
  `:global(strong)` (scoped styles don't reach into it). `<html lang>` follows the language.
- Which language: Settings > Viewing > Language (shared setting `language`: `auto` or a tag) > the browser's
  languages (exact, then the base language) > the server's default (`language` / `VITRINE_LANGUAGE`, sent in
  `/api/session`, kept on the device) > en-US. **English is US English by default** (2026-10-23, the user: "make sure the
  default language is english us (of course the user picks their own)"): a browser in any English (en-GB, en-AU, a
  plain en in London) gets en-US; English (UK) only when picked in Settings. (Before, en-GB browsers and plain "en" in
  British-English time zones got en-GB.)
- Push notifications: `internal/i18n` reads the same files; each push device stores its language
  (`push_subscriptions.language`, migration 0025; the app re-sends it when its language changes), and the notifier
  composes each kind once per language. Server plurals are one/other only (enough for English; CLDR rules can come
  with the first language that needs more). Server error messages stay English.
- The e2e tests run with an en-US browser (the main file); one test picks English (UK) in Settings and checks the
  British spelling, then goes back to Automatic.
