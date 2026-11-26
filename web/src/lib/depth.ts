// Check depth: how many posts a subscription's check goes on past the newest
// post the last check saw (posts tagged later turn up further down a search).
// Set on a subscription, else its group, else its site, else for all.
import type { MenuChoice } from '../components/ActionMenu.svelte';
import { settings } from './settings.svelte';
import { t } from './i18n.svelte';

const steps = [0, 25, 50, 100, 200, 500, 1000];

export function depthLabel(n: number): string {
  return n === 0 ? t('depth.none') : t('depth.posts', { n });
}

/** What a site's subscriptions get unless their group or they set one. */
export function siteDepth(site: string): number {
  return settings.siteCheckDepths[site] ?? settings.checkDepth;
}

/** The menu page to pick a depth: "Default (what applies otherwise)" first. */
export function depthChoice(own: number | undefined, inherited: number, pick: (n: number) => void): MenuChoice {
  const options = [{ value: '-1', label: inherited === 0 ? t('depth.defaultNone') : t('depth.defaultPosts', { n: inherited }) }];
  for (const n of own !== undefined && !steps.includes(own) ? [...steps, own].sort((a, b) => a - b) : steps) {
    options.push({ value: String(n), label: depthLabel(n) });
  }
  return { heading: t('depth.label'), value: String(own ?? -1), options, pick: (v) => pick(+v) };
}
