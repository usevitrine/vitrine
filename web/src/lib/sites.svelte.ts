// How sites are shown everywhere: by their web address ("safebooru.org"), not
// the plugin ID ("safebooru"). Loaded once after logging in.
import { api } from './api';
import { splitTag } from './namespaces.svelte';
import { t } from './i18n.svelte';
import { noticeSites } from './notices.svelte';

class Sites {
  names = $state<Record<string, string>>({});
  /** The new-download box's hint for sites with their own search syntax. */
  hints = $state<Record<string, string>>({});
  /** Each site's engine (the site picker groups by it when there are many). */
  engines = $state<Record<string, string>>({});
  /** How a tag becomes each site's search (see tagQuery). */
  tagQueries = $state<Record<string, Record<string, string>>>({});
  /** How each site writes an OR search (only sites that can). */
  orQueries = $state<Record<string, { tmpl: string; sep: string }>>({});
  /** Each site's host (its icon), and the sites switched off. */
  hosts = $state<Record<string, string>>({});
  /** Whether fav:me (your favorites on the site) works: its credentials say who you are. */
  favMe = $state<Record<string, boolean>>({});
  off = $state<string[]>([]);
  /** Loaded once (or failed): what a tag becomes on each site is known from then on. */
  loaded = $state(false);
  private ready: () => void = () => {};
  private readyP = new Promise<void>((r) => (this.ready = r));
  /** Once loaded (at once if it is). */
  whenLoaded(): Promise<void> {
    return this.loaded ? Promise.resolve() : this.readyP;
  }

  async load() {
    try {
      const list = (await api.sites()).sites;
      this.names = Object.fromEntries(list.map((s) => [s.id, s.site]));
      this.hints = Object.fromEntries(list.filter((s) => s.search_hint).map((s) => [s.id, s.search_hint!]));
      this.engines = Object.fromEntries(list.map((s) => [s.id, s.engine]));
      this.tagQueries = Object.fromEntries(list.map((s) => [s.id, s.tag_query ?? {}]));
      this.hosts = Object.fromEntries(list.map((s) => [s.id, s.host]));
      this.favMe = Object.fromEntries(list.map((s) => [s.id, !!s.fav_me]));
      this.off = list.filter((s) => s.off).map((s) => s.id);
      this.orQueries = Object.fromEntries(list.filter((s) => s.or_query).map((s) => [s.id, { tmpl: s.or_query!, sep: s.or_sep || ' ~ ' }]));
    } catch {
      // No downloader (or offline): plugin IDs are shown instead.
    }
    this.loaded = true;
    this.ready();
  }
}

export const sites = new Sites();

/** A site's address for display ("safebooru" -> "safebooru.org"). */
export const siteLabel = (id: string) => sites.names[id] ?? id;
noticeSites.label = siteLabel; // (a plugin update's notice names its site)

/** A library tag as a site's search for it (a tag's "New download"): each
 *  site says how in its plugin - e.g. a Mastodon server searches #tag, and
 *  @name for a creator; a booru the plain tag, without its namespace. */
export function tagQuery(site: string, tag: string): string {
  const { ns, name } = splitTag(tag);
  const q = sites.tagQueries[site] ?? {};
  // {tag}: the name; {name}: with spaces for underscores; {ns}: its namespace.
  return (q[ns] ?? q['*'] ?? '{tag}').replaceAll('{tag}', name).replaceAll('{name}', name.replace(/_/g, ' ')).replaceAll('{ns}', ns);
}

/** An engine's name for people ("gelbooru-v02" -> "Gelbooru 0.2"). */
export function engineLabel(engine: string): string {
  const known: Record<string, string> = {
    'gelbooru-v02': 'Gelbooru 0.2',
    'gelbooru-v02-html': t('sites.engine.webPages', { engine: 'Gelbooru 0.2' }),
    shimmie2: 'Shimmie2',
    mastodon: 'Mastodon',
    danbooru: 'Danbooru',
    moebooru: 'Moebooru',
  };
  return known[engine] ?? (engine ? engine[0].toUpperCase() + engine.slice(1) : t('sites.engine.other'));
}

/** Whether the site can search for any of these tags at once: it has an OR search, and
 *  none of them is written as a profile there (@name: a Mastodon server ORs hashtags only). */
export function canOr(site: string, tags: string[]): boolean {
  return !!sites.orQueries[site] && tags.length > 1 && tags.every((t) => !tagQuery(site, t).startsWith('@'));
}

/** Several library tags as one search on a site: all of them, or (any) an OR
 *  search where the site has one - else all of them. */
export function tagsQuery(site: string, tags: string[], any = false): string {
  const each = tags.map((t) => tagQuery(site, t));
  const or = sites.orQueries[site];
  if (any && canOr(site, tags)) return or.tmpl.replaceAll('{tags}', each.join(or.sep));
  return each.join(' ');
}
