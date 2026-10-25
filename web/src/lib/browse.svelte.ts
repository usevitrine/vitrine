// Comics > Browse (2026-10-22): a gallery site's search, shown by the library's own grid and search bar (the user: it
// must look like the other libraries). The results are the site's galleries, page after page as the grid nears the
// end, each dressed as a comic (cover, title, pages; marked when the library has it or its download waits), so the
// grid draws them as it draws Comics - hover zoom, rows, the shared motion.
import { api, type BrowseGallery, type FileInfo } from './api';
import { Results } from './results.svelte';
import { tagQuery } from './sites.svelte';
import { router } from './router.svelte';

const SHAPE = Math.round((7 / 10) * 1024);

/** Pictures loaded (into the browser's cache: they're sent as unchanging, so the tiles then show them at once), or
 *  the time up - whichever first; called off with the search (its requests too: a newer search's covers don't wait
 *  behind them). */
function preload(urls: string[], ms: number, signal: AbortSignal): Promise<void> {
  const all = Promise.all(
    urls.filter(Boolean).map((u) =>
      fetch(u, { signal })
        .then((r) => r.blob())
        .catch(() => {}),
    ),
  );
  return Promise.race([all.then(() => {}), new Promise<void>((r) => setTimeout(r, ms))]);
} // (covers: about 7 wide to 10 tall - the site doesn't say each one's)

export class BrowseResults extends Results {
  /** The site browsed (a plugin ID). */
  site = $state('');
  /** The galleries as the site described them (the preview's details), by ID. */
  readonly galleries = new Map<number, BrowseGallery>();
  private order: number[] = [];
  private items = new Map<number, FileInfo>();
  private next = 0; // the site's cursor for more (0: no more)
  /** The site's next page is on its way (the grid's foot shows it). */
  loadingMore = $state(false);
  private run = 0; // replies for an older search are dropped

  constructor() {
    super('comics');
  }

  /** The site's other order (one of its own lists - popular, a toplist; no search), or '' for its newest first. */
  sort = $state('');

  /** The search as the site writes it: each chip's tag in its syntax (a namespace, a minus kept), title words
   *  ("/words") plain, and the filter menu's filters as the server's filter terms (category:, rating:, pages:,
   *  filter:expunged, filter:torrent - see the plugin's ehSearch). */
  siteQuery(q = this.query): string {
    let from = 0;
    let to = 0;
    const out: string[] = [];
    for (const w of q.split(/\s+/)) {
      if (!w || w === '(' || w === ')' || w === '~') continue;
      const neg = w.startsWith('-') ? '-' : '';
      const body = neg ? w.slice(1) : w;
      const low = body.toLowerCase();
      const pages = /^system:pages(<=|>=|<|>|=)(\d+)$/.exec(low);
      const stars = /^system:stars>=([2-5])$/.exec(low);
      if (body.startsWith('/')) out.push(neg + body.slice(1).replaceAll('_', ' '));
      else if (low.startsWith('title:')) out.push(neg + body.slice(6).replaceAll('_', ' '));
      else if (low.startsWith('category:')) out.push(low); // (the site's category filter: the server turns it into that)
      else if (pages && !neg) {
        const n = Number(pages[2]);
        const op = pages[1];
        if (op === '>' || op === '>=' || op === '=') from = Math.max(from, op === '>' ? n + 1 : n);
        if (op === '<' || op === '<=' || op === '=') {
          const most = op === '<' ? n - 1 : n;
          to = to ? Math.min(to, most) : most;
        }
      } else if (stars && !neg) out.push('rating:' + stars[1]);
      else if ((low === 'system:expunged' || low === 'system:torrent') && !neg) out.push('filter:' + low.slice(7));
      else if (low.startsWith('system:')) continue; // (a library filter the site can't do)
      else out.push(neg + tagQuery(this.site, body));
    }
    if (from || to > 0) out.push(`pages:${from || ''}-${to > 0 ? to : ''}`);
    return out.join(' ');
  }

  /** The search on its way (and its next page, its covers): a new one calls it off - a new tag needn't wait for it. */
  private inflight: AbortController | null = null;

  override async search(q: string) {
    if (!this.site) return;
    const my = ++this.run;
    this.inflight?.abort();
    const abort = (this.inflight = new AbortController());
    this.query = q;
    this.next = 0; // (the old search's cursor: never for this one)
    this.loadingMore = false;
    this.busy = true;
    this.error = '';
    // The last search's galleries go at once (the user: "when entering a new tag it should clear the results") - the
    // page's spinner shows until the new ones come (the site takes a second or two).
    this.order = [];
    this.items.clear();
    this.galleries.clear();
    this.settle();
    try {
      const res = await api.browse(this.site, this.siteQuery(q), 0, this.sort, abort.signal);
      if (my !== this.run) return;
      // The first screenful's covers before the list shows (at most a moment): they come in together, not cover by
      // cover.
      await preload(res.galleries.slice(0, 24).map((g) => this.image(g.cover)), 1200, abort.signal);
      if (my !== this.run) return;
      this.take(res.galleries);
      this.next = res.next;
      this.comics = res.total >= 0 ? res.total : this.order.length;
      this.settle();
      this.searches++;
    } catch (e) {
      if (my === this.run && !abort.signal.aborted) this.error = (e as Error).message;
    } finally {
      if (my === this.run) this.busy = false;
    }
  }

  /** The site's next page, appended (the grid neared the end). */
  private async more() {
    if (this.loadingMore || !this.next || !this.site) return;
    const my = this.run;
    this.loadingMore = true;
    try {
      const res = await api.browse(this.site, this.siteQuery(), this.next, this.sort, this.inflight?.signal);
      if (my !== this.run) return;
      this.take(res.galleries);
      this.next = res.next;
      this.settle();
    } catch {
      /* the next time the end comes near */
    } finally {
      if (my === this.run) this.loadingMore = false;
    }
  }

  private take(list: BrowseGallery[]) {
    for (const g of list) {
      if (this.galleries.has(g.id)) continue;
      this.galleries.set(g.id, g);
      this.order.push(g.id);
      this.items.set(g.id, this.dress(g));
    }
  }

  private settle() {
    this.total = this.order.length;
    this.shapes = new Uint16Array(this.total).fill(SHAPE);
    this.version++;
    this.checkNear(); // (the end may be on screen already - or a page asked for meanwhile was dropped)
  }

  /** A cover or thumbnail picture of the site's, through the server (ExHentai's need the login). */
  image(u?: string): string {
    return u ? `/api/browse/image?site=${encodeURIComponent(this.site)}&u=${encodeURIComponent(u)}` : '';
  }

  /** A gallery dressed as a comic: what the grid draws. */
  private dress(g: BrowseGallery): FileInfo {
    const cover = this.image(g.cover);
    return {
      id: g.id,
      sha256: '',
      type: 'comic',
      mime: 'image/webp',
      size: 0,
      width: 700,
      height: 1000,
      duration_ms: 0,
      has_audio: false,
      imported_at: 0,
      modified_at: 0,
      posted_at: g.posted,
      url: cover,
      thumb: cover,
      comic: {
        id: g.id,
        title: g.title,
        title_alt: g.title_alt,
        site: this.site,
        url: g.url,
        category: g.category,
        pages: g.pages,
        have: g.pages,
        created_at: 0,
        updated_at: 0,
        read_page: 0,
        tags: g.tags.map((t) => t.name),
        thumb: cover,
        mark: g.comic ? 'have' : g.queued ? 'queued' : undefined,
      },
    };
  }

  /** A gallery's download started from the preview: marked. */
  queued(id: number) {
    const g = this.galleries.get(id);
    const f = this.items.get(id);
    if (!g || !f?.comic) return;
    g.queued = true;
    this.items.set(id, { ...f, comic: { ...f.comic, mark: g.comic ? 'have' : 'queued' } });
    this.version++;
  }

  override refresh() {
    return this.search(this.query);
  }
  override async merge() {
    return 0; // (nothing arrives on its own: the site's list is what a search brings)
  }
  override async checkNew() {}
  override indexOf(id: number) {
    return this.order.indexOf(id);
  }
  override idAt(i: number) {
    return i >= 0 && i < this.total ? this.order[i] : undefined;
  }
  override file(id: number | undefined) {
    return id === undefined ? undefined : this.items.get(id);
  }
  override fileAt(i: number) {
    return this.file(this.idAt(i));
  }
  override async ensureIDs() {}
  override async ensure() {
    // The site's next page only as the end of what's on screen comes near (not the grid's rows kept ready further
    // on: each page asked for the next - the whole site, a page a second, while nothing moved), on this page only.
    // (A moment later: the grid notes what's on screen just after asking.)
    this.checkNear();
  }
  private checkNear() {
    clearTimeout(this.nearTimer);
    this.nearTimer = window.setTimeout(() => {
      // (nothing to show yet - a page all blacklisted: the next one, until there is or the site has no more)
      if (router.screen === 'browse' && this.searches > 0 && (this.total === 0 || this.focus.to >= this.total - 12)) this.more();
    }, 50);
  }
  private nearTimer = 0;
  override async idsInRange(from: number, to: number) {
    if (from > to) [from, to] = [to, from];
    return this.order.slice(Math.max(0, from), to + 1);
  }
  override patch() {}
  override forget() {}
  override async reloadDetails() {}
}
