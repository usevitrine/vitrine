// A library search as a search on a site (the New download dialog's "This search"): its tags written the site's
// way, an OR group as the site's OR search - where the site has none, split: one search for each combination
// (2026-10-22, the user: "separating the or's into separate queries to get all combinations"); what the site can't
// search - filters, wildcards, an OR group with an excluded tag, more combinations than MAX_SEARCHES - left out (and said).
import { parse, label, kindOf, type Chip } from './query';
import { tagQuery, tagsQuery, canOr } from './sites.svelte';
import { t } from './i18n.svelte';

const tagChip = (c: Chip) => c.atoms.every((a) => a.text.trim() !== '' && kindOf(a.text) === 'tag');

/** The tags a library search names (OR groups too; excluded ones as well - they say where the search came from). */
export function searchTags(q: string): string[] {
  return parse(q)
    .filter(tagChip)
    .flatMap((c) => c.atoms.map((a) => a.text));
}

/** A search in words, as its chips show them ("blue_sky · landscape or portrait · images"). */
export function searchWords(chips: Chip[]): string {
  return chips
    .map((c) => c.atoms.map((a) => (a.neg && kindOf(a.text) === 'tag' ? '-' : '') + label(a)).join(t('common.orSep')))
    .join(' · ');
}

/** The most searches one library search splits into (its OR groups' combinations). */
const MAX_SEARCHES = 24;

/** The search on the site, the OR groups it has no OR search for (split), and the chips left out. */
export function siteSearch(site: string, q: string): { query: string; split: Chip[]; left: Chip[] } {
  const out: string[] = [];
  const split: Chip[] = [];
  const left: Chip[] = [];
  let n = 1; // (the combinations so far)
  for (const c of parse(q)) {
    if (!tagChip(c)) {
      left.push(c);
    } else if (c.atoms.length === 1) {
      const a = c.atoms[0];
      out.push((a.neg ? '-' : '') + tagQuery(site, a.text));
    } else {
      const tags = c.atoms.map((a) => a.text);
      if (c.atoms.some((a) => a.neg)) left.push(c);
      else if (canOr(site, tags)) out.push(tagsQuery(site, tags, true));
      else if (n * tags.length <= MAX_SEARCHES) {
        split.push(c);
        n *= tags.length;
      } else left.push(c);
    }
  }
  return { query: out.join(' '), split, left };
}

/** A search with its split OR groups: one search for each combination - (a ~ b) (c ~ d) x: a c x, a d x, b c x, b d x. */
export function splitSearches(site: string, base: string, split: Chip[]): string[] {
  let all = [base.trim()];
  for (const c of split) all = all.flatMap((q) => c.atoms.map((a) => [q, tagQuery(site, a.text)].filter(Boolean).join(' ')));
  return all;
}
