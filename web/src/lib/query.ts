// Search chips <-> query text. The server parses the text (internal/search);
// this only splits it into chips so each can be shown and edited on its own.
//
//   blue_sky -cloud ( a ~ -b ) system:imported<5d sort:size
//   -> [blue_sky] [-cloud] [a OR -b] [imported<5d] [sort: size]
//
// Comics: "/some_title" searches the titles only (typed with spaces: "/some title").

import { siteLabel } from './sites.svelte';
import { splitTag } from './namespaces.svelte';
import { describe, isFavorite } from './system';
import { t } from './i18n.svelte';

export interface Atom {
  neg: boolean;
  text: string; // without the leading '-'
}

/** One chip: a single atom, or an OR group of several. */
export interface Chip {
  atoms: Atom[];
}

export type Kind = 'tag' | 'wildcard' | 'system' | 'sort' | 'limit' | 'title';

export function kindOf(text: string): Kind {
  const t = text.toLowerCase();
  if (t.startsWith('system:limit=') || t.startsWith('limit:')) return 'limit';
  if (t.startsWith('system:')) return 'system';
  if (t.startsWith('sort:') || t.startsWith('order:')) return 'sort';
  if (t.length > 1 && t.startsWith('/')) return 'title';
  if (t.includes('*')) return 'wildcard';
  return 'tag';
}

/** Whether typed text is a title search ("/words", "-/words"): its words can have spaces. */
export function isTitleText(text: string): boolean {
  return /^-?\/./.test(text.trimStart());
}

/** A title search as one term: "/some title" -> "/some_title" (other text as it is). */
export function titleTerm(text: string): string {
  const m = /^(-?)\/(.*)$/s.exec(text.trim());
  return m && m[2].trim() ? m[1] + '/' + m[2].trim().replace(/\s+/g, '_') : text.trim();
}

function atom(token: string): Atom {
  if (token.length > 1 && token.startsWith('-')) return { neg: true, text: token.slice(1) };
  return { neg: false, text: token };
}

/** Splits query text into chips. Lenient: an unclosed group takes the rest. */
export function parse(text: string): Chip[] {
  const toks = text.split(/\s+/).filter(Boolean);
  const chips: Chip[] = [];
  for (let i = 0; i < toks.length; i++) {
    const t = toks[i];
    if (t === '(') {
      const atoms: Atom[] = [];
      for (i++; i < toks.length && toks[i] !== ')'; i++) {
        if (toks[i] !== '~' && toks[i] !== '(') atoms.push(atom(toks[i]));
      }
      if (atoms.length) chips.push({ atoms });
    } else if (t !== ')' && t !== '~') {
      chips.push({ atoms: [atom(t)] });
    }
  }
  return chips;
}

export function atomText(a: Atom): string {
  return (a.neg ? '-' : '') + a.text;
}

export function chipText(c: Chip): string {
  if (c.atoms.length === 1) return atomText(c.atoms[0]);
  return '( ' + c.atoms.map(atomText).join(' ~ ') + ' )';
}

export function format(chips: Chip[]): string {
  return chips.map(chipText).join(' ');
}

/** A short label for a chip's atom ("imported<5d" instead of "system:imported<5d"). */
export function label(a: Atom, comics = false): string {
  const k = kindOf(a.text);
  let text = a.text;
  const site = /^system:site(!?=)(.+)$/i.exec(text);
  if (site) {
    const names = site[2].toLowerCase().split(',').filter(Boolean).map(siteLabel);
    return t(site[1] === '!=' ? 'chipLabel.notFrom' : 'chipLabel.from', { sites: names.join(t('describe.or')) });
  }
  if (a.neg && text.toLowerCase() === 'system:has_audio') return t('chipLabel.noAudio');
  if (a.neg && isFavorite(text)) return t('chipLabel.notFavorite');
  if (k === 'system') {
    const words = describe(text, comics);
    if (words) return words;
    text = text.slice('system:'.length).replace(/_/g, ' ');
  }
  // A tag type (creator:, character:, ...) shows as the chip's color, not as text.
  if (k === 'tag') text = splitTag(text).name;
  if (k === 'sort') text = t('chipLabel.sort', { sort: text.slice(text.indexOf(':') + 1).replace('_asc', ' ↑') });
  if (k === 'limit') text = t('chipLabel.first', { n: text.slice(text.search(/[:=]/) + 1) });
  if (k === 'title' && comics) text = t('chipLabel.title', { words: text.slice(1).replace(/_/g, ' ') });
  return text;
}

/** Whether parentheses in text are balanced (so it can be turned into chips). */
export function balanced(text: string): boolean {
  let depth = 0;
  for (const t of text.split(/\s+/)) {
    if (t === '(') depth++;
    if (t === ')') depth--;
  }
  return depth <= 0;
}

/**
 * Splits sort words (sort:x / order:x) out of typed text: the sort is kept
 * apart from the search (in the sort menu and the URL), never as a chip.
 * Returns the text without them and the last sort found ('' if none),
 * normalized: "imported", "imported_asc", "uploaded", "uploaded_asc", or
 * another field as typed (e.g. "size_asc").
 */
export function extractSort(text: string): { q: string; sort: string } {
  let sort = '';
  const kept: string[] = [];
  for (const tok of text.split(/\s+/).filter(Boolean)) {
    const m = /^(?:sort|order):(.+)$/i.exec(tok);
    if (m) sort = normalizeSort(m[1]);
    else kept.push(tok);
  }
  return { q: kept.join(' '), sort };
}

function normalizeSort(s: string): string {
  return s
    .toLowerCase()
    .replace(/_desc$/, '')
    .replace(/^(posted|upload|source_time)(?=$|_)/, 'uploaded')
    .replace(/^import(?=$|_)/, 'imported')
    .replace(/^(favou?rites?|fav|favou?rited)(?=$|_)/, 'favorite');
}
