// A new download: the New download dialog (NewDownloadDialog.svelte, shown by App) - from the Library's download
// button (the search in the bar, or another), or from library tags (a tag's menu, a search chip, the picked tags in
// the details: straight to the site, where the tags came from first - most tags, then most files).
import { api } from './api';

export interface SiteChoice {
  site: string;
  files: number; // files that got the tag(s) from there
  tags: number; // how many of the tags it gave
}

export interface NewDownloadRequest {
  query: string; // a library search
  fromTags: boolean; // its tags were picked: no "this search or another" step
  fallback?: string; // the site to offer first when the tags came from none (e.g. the file's)
  // A search on a site already (a gallery in Comics > Browse): straight to "once or subscribe".
  site?: string;
  text?: string;
  label?: string; // shown instead of the search (the gallery's title)
  subscribe?: boolean; // Subscribe picked to start with (a tag's menu: "Subscribe…")
  ondone?: (subscribed: boolean) => void;
}

class Dialog {
  current = $state<NewDownloadRequest | null>(null);
}
export const newDownloadDialog = new Dialog();

/** The dialog for a library search (the Library's download button). */
export function openNewDownload(query: string) {
  newDownloadDialog.current = { query, fromTags: false };
}

/** The dialog for one search on a site (a gallery in Comics > Browse: its link) - once, or a subscription. */
export function openSiteDownload(site: string, text: string, label: string, ondone?: (subscribed: boolean) => void) {
  newDownloadDialog.current = { query: '', fromTags: false, site, text, label, ondone };
}

/** The dialog for library tags (any: an OR group - posts with any of them; a site without an OR search: one search each). */
export function newDownload(tags: string[], { any = false, fallback, subscribe }: { any?: boolean; fallback?: string; subscribe?: boolean } = {}) {
  if (!tags.length) return;
  const or = any && tags.length > 1;
  newDownloadDialog.current = { query: or ? `( ${tags.join(' ~ ')} )` : tags.join(' '), fromTags: true, fallback, subscribe };
}

/** The sites that gave the tags, best first (only sites that can download them). */
export async function tagOrigins(tags: string[], usable: (site: string) => boolean): Promise<SiteChoice[]> {
  let from: Record<string, { site: string; count: number }[]> = {};
  try {
    from = (await api.tagSites(tags)).tags;
  } catch {
    return [];
  }
  const by = new Map<string, SiteChoice>();
  for (const list of Object.values(from)) {
    for (const { site, count } of list) {
      const c = by.get(site) ?? { site, files: 0, tags: 0 };
      c.files += count;
      c.tags++;
      by.set(site, c);
    }
  }
  return [...by.values()].filter((c) => usable(c.site)).sort((a, b) => b.tags - a.tags || b.files - a.files || a.site.localeCompare(b.site));
}
