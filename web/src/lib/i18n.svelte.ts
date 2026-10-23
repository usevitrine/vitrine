// The app's texts in the user's language. The language files live in the
// repo's top-level locales/ folder (shared with the server, which writes push
// notifications from them): en-US.json has every text, the others only what
// differs (anything missing comes from en-US). A text is a string with {name}
// placeholders, or an object of plural forms picked by the number {n}
// ({"one": "{n} file", "other": "{n} files"}, the forms Intl.PluralRules
// names). Numbers in placeholders are written the language's way.
//
// Which language: Settings > Viewing (shared by every device; "auto" by
// default), else the browser's languages, else the server's default
// (VITRINE_LANGUAGE), else en-US. English in any variant is en-US unless
// English (UK) is picked in Settings (2026-10-23: US English is the default).
import { settings } from './settings.svelte';

type Text = string | Partial<Record<Intl.LDMLPluralRule, string>>;
type Tree = { [key: string]: Text | Tree };

const MAIN = 'en-US';

const files = import.meta.glob('../../../locales/*.json', { eager: true, import: 'default' }) as Record<string, Tree>;

const forms = new Set(['zero', 'one', 'two', 'few', 'many', 'other']);
const isPlural = (v: object) => 'other' in v && Object.entries(v).every(([k, x]) => forms.has(k) && typeof x === 'string');

function flatten(tree: Tree, prefix = '', out: Record<string, Text> = {}): Record<string, Text> {
  for (const [k, v] of Object.entries(tree)) {
    const key = prefix ? `${prefix}.${k}` : k;
    if (typeof v === 'string' || isPlural(v)) out[key] = v as Text;
    else flatten(v as Tree, key, out);
  }
  return out;
}

const texts: Record<string, Record<string, Text>> = {};
for (const [path, tree] of Object.entries(files)) {
  texts[path.replace(/^.*\/|\.json$/g, '')] = flatten(tree);
}

/** The languages there are files for, by name ("English (UK)"). */
export const languages = Object.keys(texts)
  .map((code) => ({ code, name: String(texts[code]._name ?? code) }))
  .sort((a, b) => a.name.localeCompare(b.name));

/** The best file for a browser language ("de-AT" -> de-DE if there's one; English of any kind -> en-US). */
function match(want: string): string | null {
  const base = want.split('-')[0].toLowerCase();
  if (base === 'en') return MAIN; // (English (UK) only when picked in Settings)
  const exact = Object.keys(texts).find((c) => c.toLowerCase() === want.toLowerCase());
  if (exact) return exact;
  const same = Object.keys(texts).filter((c) => c.split('-')[0].toLowerCase() === base);
  return same.length ? same.sort()[0] : null;
}

class I18n {
  /** The server's default (VITRINE_LANGUAGE), from the session. */
  serverDefault = $state(load());
  lang = $derived.by(() => {
    if (settings.language !== 'auto' && texts[settings.language]) return settings.language;
    const browser = typeof navigator === 'undefined' ? [] : navigator.languages?.length ? navigator.languages : [navigator.language];
    for (const l of browser) {
      const m = l && match(l);
      if (m) return m;
    }
    return texts[this.serverDefault] ? this.serverDefault : MAIN;
  });
  plurals = $derived(new Intl.PluralRules(this.lang));
  numbers = $derived(new Intl.NumberFormat(this.lang));

  setServerDefault(tag: string | undefined) {
    if (!tag || tag === this.serverDefault) return;
    this.serverDefault = tag;
    try {
      localStorage.setItem('vitrine.serverLanguage', tag);
    } catch {
      /* asked again next time */
    }
  }
}

function load(): string {
  try {
    return localStorage.getItem('vitrine.serverLanguage') ?? MAIN;
  } catch {
    return MAIN;
  }
}

export const i18n = new I18n();

/** A number written the language's way (1,234 / 1.234). */
export function num(n: number): string {
  return i18n.numbers.format(n);
}

/** A date written the language's way. */
export function date(d: Date | number, opts: Intl.DateTimeFormatOptions): string {
  return new Date(d).toLocaleDateString(i18n.lang, opts);
}

/** A time of day written the language's way. */
export function time(d: Date | number, opts: Intl.DateTimeFormatOptions = { hour: '2-digit', minute: '2-digit' }): string {
  return new Date(d).toLocaleTimeString(i18n.lang, opts);
}

/** A date and time written the language's way. */
export function dateTime(d: Date | number, opts: Intl.DateTimeFormatOptions): string {
  return new Date(d).toLocaleString(i18n.lang, opts);
}

/**
 * A text in the current language, its {placeholders} filled in from vars. A
 * plural text picks its form by vars.n. A missing text shows its key (and
 * `npm run i18n:check` catches it before that).
 */
export function t(key: string, vars?: Record<string, string | number>): string {
  const lang = i18n.lang;
  let v = texts[lang]?.[key] ?? texts[MAIN][key];
  if (v === undefined) {
    if (import.meta.env.DEV) console.warn('i18n: no text for', key);
    return key;
  }
  if (typeof v !== 'string') {
    const n = Number(vars?.n ?? 0);
    v = v[i18n.plurals.select(n)] ?? v.other ?? '';
  }
  if (!vars) return v;
  return v.replace(/\{(\w+)\}/g, (m, name: string) => {
    const x = vars[name];
    if (x === undefined) return m;
    return typeof x === 'number' ? num(x) : x;
  });
}

/**
 * A text with <b>bold</b> and <c>code</c> parts, split for showing
 * (components/Rich). Lets a translation put them where its grammar needs
 * them; no HTML from the language files is ever rendered.
 */
export function rich(text: string): { text: string; as: '' | 'b' | 'c' }[] {
  const out: { text: string; as: '' | 'b' | 'c' }[] = [];
  let last = 0;
  for (const m of text.matchAll(/<(b|c)>(.*?)<\/\1>/g)) {
    if (m.index > last) out.push({ text: text.slice(last, m.index), as: '' });
    out.push({ text: m[2], as: m[1] as 'b' | 'c' });
    last = m.index + m[0].length;
  }
  if (last < text.length) out.push({ text: text.slice(last), as: '' });
  return out;
}
