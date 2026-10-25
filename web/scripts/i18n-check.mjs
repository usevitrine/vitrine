// Checks the language files (../locales) against each other and the code:
//   - every text the code asks for (t('key') in web/src, .T("key") in Go) is in en-US.json;
//   - the other languages only have keys en-US has, plural texts stay plural, and
//     their {placeholders} are ones the en-US text has;
//   - en-US texts nothing uses (a warning: maybe the code builds the key).
// Run: npm run i18n:check (exits 1 on errors).
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = join(dirname(fileURLToPath(import.meta.url)), '..', '..');
const forms = new Set(['zero', 'one', 'two', 'few', 'many', 'other']);
const isPlural = (v) => v && typeof v === 'object' && 'other' in v && Object.entries(v).every(([k, x]) => forms.has(k) && typeof x === 'string');

function flatten(tree, prefix = '', out = {}) {
  for (const [k, v] of Object.entries(tree)) {
    const key = prefix ? `${prefix}.${k}` : k;
    if (typeof v === 'string' || isPlural(v)) out[key] = v;
    else if (v && typeof v === 'object') flatten(v, key, out);
    else out[key] = v;
  }
  return out;
}
const holes = (v) => new Set([...(typeof v === 'string' ? [v] : Object.values(v)).join(' ').matchAll(/\{(\w+)\}/g)].map((m) => m[1]));

const errors = [];
const warnings = [];
const dir = join(root, 'locales');
const langs = {};
for (const f of readdirSync(dir).filter((f) => f.endsWith('.json'))) {
  try {
    langs[f.replace(/\.json$/, '')] = flatten(JSON.parse(readFileSync(join(dir, f), 'utf8')));
  } catch (e) {
    errors.push(`${f}: ${e.message}`);
  }
}
const main = langs['en-US'];
if (!main) {
  console.error('locales/en-US.json is missing');
  process.exit(1);
}
for (const [k, v] of Object.entries(main)) {
  if (typeof v !== 'string' && !isPlural(v)) errors.push(`en-US: ${k} is neither a text nor plural forms`);
}
for (const [lang, texts] of Object.entries(langs)) {
  if (lang === 'en-US') continue;
  if (!texts._name) errors.push(`${lang}: no "_name" (the language's name in the picker)`);
  for (const [k, v] of Object.entries(texts)) {
    if (k.startsWith('_')) continue;
    if (!(k in main)) {
      errors.push(`${lang}: ${k} isn't in en-US.json`);
      continue;
    }
    if (isPlural(main[k]) !== isPlural(v) && typeof v !== 'string') errors.push(`${lang}: ${k} should be ${isPlural(main[k]) ? 'plural forms' : 'a text'}`);
    if (typeof v !== 'string' && !isPlural(v)) {
      errors.push(`${lang}: ${k} is neither a text nor plural forms`);
      continue;
    }
    const allowed = holes(main[k]);
    for (const h of holes(v)) if (!allowed.has(h)) errors.push(`${lang}: ${k} has {${h}}, which en-US's text doesn't`);
  }
}

// The keys the code uses.
const used = new Set();
const prefixes = new Set();
function walk(d, exts, out = []) {
  for (const f of readdirSync(d)) {
    if (f === 'node_modules' || f.startsWith('.')) continue;
    const p = join(d, f);
    if (statSync(p).isDirectory()) walk(p, exts, out);
    else if (exts.some((e) => f.endsWith(e)) && !f.endsWith('_test.go')) out.push(p);
  }
  return out;
}
const sources = [...walk(join(root, 'web', 'src'), ['.svelte', '.ts']), ...walk(join(root, 'internal'), ['.go']), ...walk(join(root, 'cmd'), ['.go'])];
for (const file of sources) {
  const src = readFileSync(file, 'utf8');
  for (const m of src.matchAll(/(?<![\w.])t\(\s*'([\w.-]+)'|(?<![\w.])t\(\s*"([\w.-]+)"|\.T\(\s*"([\w.-]+)"/g)) {
    const key = m[1] ?? m[2] ?? m[3];
    used.add(key);
    if (!(key in main)) errors.push(`${file.slice(root.length + 1)}: no en-US text for ${key}`);
  }
  // Built keys: t(`downloads.state.${s}`), choice(u, `unit.name.${u}`) - everything under the prefix counts as used.
  for (const m of src.matchAll(/`([\w-]+\.[\w.-]*)\$\{/g)) prefixes.add(m[1]);
  // Keys built from a variable prefix, named in a comment: // i18n-keys: settings.section.*
  for (const m of src.matchAll(/i18n-keys:([ \w.*-]+)/g)) for (const p of m[1].trim().split(/\s+/)) prefixes.add(p.replace(/\*$/, ''));
  // Keys passed around before t() reads them (choice(value, 'key'), ...): any string that is a key.
  for (const m of src.matchAll(/['"]([\w-]+\.[\w.-]+)['"]/g)) if (m[1] in main) used.add(m[1]);
}
for (const k of Object.keys(main)) {
  if (k.startsWith('_') || used.has(k) || [...prefixes].some((p) => p && k.startsWith(p))) continue;
  warnings.push(`unused: ${k}`);
}

for (const w of warnings) console.warn('warning:', w);
for (const e of errors) console.error('error:', e);
console.log(`${Object.keys(main).length} texts in en-US; ${Object.keys(langs).length} languages; ${errors.length} errors, ${warnings.length} warnings`);
process.exit(errors.length ? 1 : 0);
