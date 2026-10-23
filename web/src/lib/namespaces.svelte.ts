// Tag namespaces (creator, character, series, meta, ...): each has its own
// color, and a file's tags are listed grouped by namespace. Only a prefix
// the library knows is a namespace - "9:16" is a plain tag - so the list
// comes from the server.
import { api } from './api';
import { toasts } from './toast.svelte';
import { t } from './i18n.svelte';

class Namespaces {
  names = $state<string[]>([]);
  /** The comics' own namespaces (female, language, ... from a gallery site): namespaces on the Comics page. */
  comics = $state<string[]>([]);

  // A site's namespaces seen while browsing it (Comics > Browse), before any comic of it is in the library.
  private seen = new Set<string>();

  async loadComics() {
    try {
      const list = (await api.namespaces('comics')).namespaces;
      this.comics = [...list, ...[...this.seen].filter((ns) => !list.includes(ns))];
    } catch {
      // offline: uncolored until next time
    }
  }

  /** A gallery site's namespaces (Browse): its tags grouped as a downloaded comic's will be. */
  addComics(list: string[]) {
    const fresh = list.filter((ns) => ns && !this.seen.has(ns) && !this.names.includes(ns) && !this.comics.includes(ns));
    for (const ns of list) if (ns) this.seen.add(ns);
    if (fresh.length) this.comics = [...this.comics, ...fresh];
  }

  async load() {
    try {
      this.names = (await api.namespaces()).namespaces;
    } catch {
      // Not logged in yet, or offline: tags show uncolored until next time.
    }
  }
}

export const namespaces = new Namespaces();

/** Splits "creator:some_artist" into its namespace and name ("" = no namespace). */
export function splitTag(tag: string): { ns: string; name: string } {
  const i = tag.indexOf(':');
  if (i > 0) {
    const ns = tag.slice(0, i).toLowerCase();
    if (namespaces.names.includes(ns) || namespaces.comics.includes(ns)) return { ns, name: tag.slice(i + 1) };
  }
  return { ns: '', name: tag };
}

// Booru order: who made it, what it's from, who's in it, then the rest; meta last.
const order = ['creator', 'series', 'character'];
const known: Record<string, string> = {
  '': 'var(--tag-general)',
  creator: 'var(--tag-creator)',
  character: 'var(--tag-character)',
  series: 'var(--tag-series)',
  meta: 'var(--tag-meta)',
  // A gallery site's (comics):
  female: 'var(--pink)',
  male: 'var(--sapphire)',
  language: 'var(--teal)',
  category: 'var(--yellow)',
};
const spare = ['var(--teal)', 'var(--yellow)', 'var(--sky)', 'var(--flamingo)', 'var(--lavender)', 'var(--maroon)'];

/** The color of a namespace (other namespaces get a stable spare color). */
export function nsColor(ns: string): string {
  if (ns in known) return known[ns];
  let h = 0;
  for (const c of ns) h = (h * 31 + c.charCodeAt(0)) >>> 0;
  return spare[h % spare.length];
}

/** The color of a tag as written ("creator:some_artist", "-blue_sky"). */
export function tagColor(tag: string): string {
  return nsColor(splitTag(tag.replace(/^-/, '')).ns);
}

/** Sort key of a namespace: creator, series, character, others A-Z, general, meta. */
export function nsRank(ns: string): string {
  const i = order.indexOf(ns);
  if (i >= 0) return `0${i}`;
  if (ns === '') return '2';
  if (ns === 'meta') return '3';
  return `1${ns}`;
}

/** The namespaces a tag can be put in: the usual ones and the library's own, in booru order. */
export function nsChoices(): string[] {
  return [...new Set(['creator', 'series', 'character', 'meta', '', ...namespaces.names])].sort((a, b) => nsRank(a).localeCompare(nsRank(b)));
}

/** Moves a tag to another namespace (on every file); says so. The new tag, or null. */
export async function moveToNamespace(tag: string, ns: string): Promise<string | null> {
  try {
    const res = await api.setTagNamespace(tag, ns);
    await namespaces.load(); // (a namespace no file had before)
    toasts.show(t('namespace.moved', { tag: splitTag(tag).name, ns: nsLabel(ns).toLowerCase(), n: res.files }), { cat: 'tagEdits' });
    return res.tag;
  } catch (e) {
    toasts.error((e as Error).message);
    return null;
  }
}

/** "Creator", "General", ... */
export function nsLabel(ns: string): string {
  if (!ns) return t('namespace.general');
  if (ns === 'creator' || ns === 'series' || ns === 'character' || ns === 'meta') return t(`namespace.${ns}`);
  return ns[0].toUpperCase() + ns.slice(1).replace(/_/g, ' ');
}
