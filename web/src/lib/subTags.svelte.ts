// Which tags the subscriptions follow (2026-10-30, the user: "wherever a tag is displayed, i want an indicator that a
// tag is present in a subscription"): a bell on the tag - filled when a subscription's search is that tag alone
// (excluded tags beside it allowed: "some_artist -ai_generated"), an outline when it's one of several ("blue_sky
// rating:safe"), dimmed when every one is paused. Asked for once a tag needs it (GET /api/subscriptions/tags, small),
// again a moment after the downloads change (live "downloads" event); the Downloads page hands over its list too.
import { api } from './api';
import { live } from './live.svelte';
import { router } from './router.svelte';
import { splitTag } from './namespaces.svelte';

export interface SubOfTag {
  id: number;
  site: string;
  query: string;
  paused?: boolean;
}

export interface TagSubs {
  own: boolean; // a subscription is this tag alone
  paused: boolean; // every one is paused
  subs: SubOfTag[];
}

/** The tags a subscription's search names: excluded ones, wildcards and an OR group's marks left out; a site's
 *  own marks taken off (#tag, @name). Lowercase. */
export function searchTags(query: string): string[] {
  return query
    .toLowerCase()
    .split(/\s+/)
    .filter((w) => w && !w.startsWith('-') && !w.includes('*') && !/^[()~]+$/.test(w) && !/^https?:/.test(w))
    .map((w) => w.replace(/^[#@]/, ''))
    .filter(Boolean);
}

class SubTags {
  private list = $state<SubOfTag[]>([]);
  private loaded = false;
  private listening = false;
  private timer = 0;

  /** tag (lowercase, as the search has it) -> its subscriptions, own ones first. */
  private index = $derived.by(() => {
    const m = new Map<string, { own: SubOfTag[]; part: SubOfTag[] }>();
    for (const s of this.list) {
      const tags = searchTags(s.query);
      for (const tag of new Set(tags)) {
        let e = m.get(tag);
        if (!e) m.set(tag, (e = { own: [], part: [] }));
        (tags.length === 1 ? e.own : e.part).push(s);
      }
    }
    return m;
  });

  set(subs: SubOfTag[]) {
    this.list = subs.map(({ id, site, query, paused }) => ({ id, site, query, paused }));
    this.loaded = true;
  }

  private ask() {
    if (this.listening) return;
    this.listening = true;
    if (!this.loaded) this.load();
    // (the downloads changed: a subscription added, paused, removed - a moment later, once for a burst)
    live.onDownloads(() => {
      clearTimeout(this.timer);
      this.timer = window.setTimeout(() => this.load(), 1000);
    });
  }

  private load() {
    api
      .subscriptionTags()
      .then((r) => this.set(r.subscriptions))
      .catch(() => {});
  }

  /** The subscriptions that follow a tag (written as the library has it: "creator:some_artist"), or null. A
   *  subscription on the bare name counts for the namespaced tag. */
  of(tag: string): TagSubs | null {
    this.ask();
    const full = tag.replace(/^-/, '').toLowerCase();
    const bare = splitTag(full).name;
    const a = this.index.get(full);
    const b = bare !== full ? this.index.get(bare) : undefined;
    if (!a && !b) return null;
    const own = [...(a?.own ?? []), ...(b?.own ?? [])];
    const subs = [...new Map([...own, ...(a?.part ?? []), ...(b?.part ?? [])].map((s) => [s.id, s])).values()];
    return { own: own.length > 0, paused: subs.every((s) => s.paused), subs };
  }

  /** On the Downloads page: one subscription in view (flashed), several shown by the filter. */
  open(tag: string, subs: SubOfTag[]) {
    router.focus = subs.length === 1 ? 'sub:' + subs[0].id : 'subs:' + splitTag(tag.replace(/^-/, '')).name;
    router.go('downloads');
  }
}

export const subTags = new SubTags();
