// The current search's results. The server keeps the whole sorted list
// under a result ID; we fetch slices of IDs by position and file details in
// batches, only around what is on screen - memory stays flat at any size,
// and there is no "page" to run off the end of.

import { api, ApiError, type Comic, type FileInfo, type Scope } from './api';
import { settings } from './settings.svelte';
import { decodeShapes } from './masonry';
import { t, i18n } from './i18n.svelte';

const SLICE = 500; // IDs per slice request
const FIRST = 500; // IDs sent with the search itself

/** Lets a caller take part in replacing the list (see merge). */
interface SearchHook {
  prepare?: (res: { result: string; total: number; ids: number[]; found?: Record<string, number> }) => Promise<[number, number[]][]>;
  commit?: () => void;
  /** Files whose places in the new list the hook wants (the search's find=). */
  find?: number[];
}

export class Results {
  /** The index searched: '' the library's files, 'comics' the Comics page's comics (shown as files: see comicFile). */
  readonly index: Scope;
  constructor(index: Scope = '') {
    this.index = index;
  }
  query = $state('');
  /** A saved search or group tag opened from the menu: its files since it was last opened
   *  are the NEW ones (instead of since "Mark all as seen"); null = the usual. */
  sinceOverride: number | null = null;
  /** The page the search runs on: '' the library, 'favorites' only favorites (not shown as a chip), 'trash' the
   *  deleted files (the trash's own index on the server). */
  scope = $state<'' | 'favorites' | 'trash'>('');
  /** The index the server searches: the trash's on the Trash page. */
  get searched(): Scope {
    return this.scope === 'trash' ? (this.index === 'comics' ? 'comics_trash' : 'trash') : this.index;
  }
  /** Comics: a collection's own page (its comics only; else a collection's comics fold into its tile). */
  collection = $state(0);
  /** Comics: the next list opens a collection's tile (its comics fly out of it) or closes into it (id: the tile's);
   *  the grid takes it with that list. */
  morph: { kind: 'open' | 'close'; id: number } | null = null;
  /** The search as the server runs it (with the page's limit). */
  full(q = this.query) {
    return this.scope === 'favorites' ? ('system:favorite ' + q).trim() : q;
  }
  /** The result order ("imported", "uploaded_asc", ...; '' = the server's default), kept apart from the query. */
  sort = $state('');
  /** Whether a search is running. */
  busy = $state(false);
  error = $state('');
  total = $state(0);
  /** The Comics page: the comics found, each of a collection's counted (total: the tiles). */
  comics = $state(0);
  tookMs = $state(0);
  unknown = $state<string[]>([]);
  /** Evenly spaced samples of the result's dates (for the fast scroller), or
   *  null when the order isn't by date or they haven't arrived. */
  dates = $state<number[] | null>(null);
  /** Files matching the search imported since it ran (the "N new files" button). */
  arrived = $state(0);
  /** Files in the result imported since "Mark all as seen", and how many of
   *  them are first (the New section; 0 when it's folded). */
  fresh = $state(0);
  pinned = $state(0);
  /** The masonry grid's shapes (every file's width/height x 1024, in result order), when it is on. */
  shapes = $state.raw<Uint16Array | null>(null);
  /** Bumped by every finished search (a new list, even with the same length). */
  searches = $state(0);
  private snapshotAt = 0;
  /** Bumped whenever more IDs or file details arrive (cheap re-render signal). */
  version = $state(0);

  private resultId = '';
  private slices = new Map<number, number[]>(); // slice number -> IDs
  private pendingSlices = new Map<number, Promise<void>>();
  private files = new Map<number, FileInfo>();
  private pendingFiles = new Set<number>();
  private gen = 0; // search generation: replies for older searches are dropped
  private abort?: AbortController;

  async search(q: string, sort = this.sort, hook?: SearchHook) {
    const gen = ++this.gen;
    this.abort?.abort();
    this.abort = new AbortController();
    this.busy = true;
    this.error = '';
    try {
      const full = this.full(q);
      const res = await api.search(sort ? `${full} sort:${sort}` : full, FIRST, this.abort.signal, {
        since: this.searched ? 0 : (this.sinceOverride ?? settings.seenAt), // (NEW files: the library's only)
        pin: true,
        order: settings.newOrder,
        bulk: settings.newFromBulk,
      }, settings.gridLayout === 'masonry' || this.index === 'comics', this.searched, this.index === 'comics' ? this.collection : 0, hook?.find ?? []);
      if (gen !== this.gen) return;
      const extra = (await (hook?.prepare ?? ((res) => this.focusSlices(res)))(res)) ?? [];
      if (gen !== this.gen) return;
      this.query = q;
      this.sort = sort;
      this.resultId = res.result;
      this.total = res.total;
      this.comics = res.comics ?? res.total;
      this.tookMs = res.took_ms;
      this.seed = res.seed;
      this.unknown = res.unknown;
      this.slices = new Map([[0, res.ids], ...extra]);
      this.pendingSlices.clear();
      this.snapshotAt = res.now;
      this.searches++;
      this.arrived = 0;
      this.fresh = res.new ?? 0;
      this.pinned = res.pinned ?? 0;
      this.shapes = res.shapes !== undefined ? decodeShapes(res.shapes) : null;
      hook?.commit?.(); // in the same step as the new list: nothing renders in between
      this.version++;
      this.loadDates(gen);
    } catch (e) {
      if (gen !== this.gen || (e as Error).name === 'AbortError') return;
      this.error = (e as Error).message;
    } finally {
      if (gen === this.gen) this.busy = false;
    }
  }

  /** The fast scroller's date labels: by upload or import time, following the order. */
  private async loadDates(gen: number) {
    this.dates = null;
    const s = this.sort || 'imported';
    const field = s.startsWith('uploaded') ? 'uploaded' : s.startsWith('imported') ? 'imported' : null;
    if (!field || this.total < 2) return;
    try {
      const res = await api.resultDates(this.resultId, field, 200, this.searched);
      if (gen === this.gen) this.dates = res.dates;
    } catch {
      /* positions instead of dates */
    }
  }

  /** Asks whether files matching the search arrived since it ran. The list
   *  itself never changes under the user: the pill offers to refresh. */
  async checkNew() {
    if (!this.snapshotAt || this.busy) return;
    const gen = this.gen;
    try {
      const { new: n } = await api.newCount(this.full(), this.snapshotAt, true, this.searched);
      if (gen === this.gen) this.arrived = n;
    } catch {
      /* next time */
    }
  }

  /** The server's time of the current result. */
  get snapshot() {
    return this.snapshotAt;
  }

  /** The positions on screen [from, to) (the grid keeps it up to date): a
   *  refresh brings their IDs along with the new list, so nothing there goes
   *  blank for a moment (and the grid can animate what changed). */
  focus = { from: 0, to: 0 };

  /** Files were deleted or brought back somewhere else (the blacklist's clean-up, Duplicates): the search runs again
   *  if it has run - the library stays loaded behind other pages, and showed the gone files until a reload. */
  changedElsewhere() {
    if (this.searches > 0) return this.refresh();
  }

  /** Re-runs the current search (e.g. after a delete), keeping positions where possible. */
  refresh() {
    // Random: the same shuffle again (its seed), not a new one.
    const sort = this.sort === 'random' && this.seed ? `random_${this.seed}` : this.sort;
    return this.search(this.query, sort);
  }

  /** A new list's slices covering the positions on screen (beyond the first, which comes with it). */
  private async focusSlices(res: { result: string; total: number }): Promise<[number, number[]][]> {
    const { from, to } = this.focus;
    const want: number[] = [];
    for (let n = Math.floor(from / SLICE); n <= Math.floor((Math.min(to, res.total) - 1) / SLICE); n++) if (n > 0) want.push(n);
    try {
      return await Promise.all(want.map(async (n) => [n, (await api.slice(res.result, n * SLICE, SLICE)).ids] as [number, number[]]));
    } catch {
      return [];
    }
  }
  private seed = 0;

  /**
   * Takes in files that arrived since the search ran (it runs again) without moving anything: by the files
   * themselves, wherever new ones land - at the top (newest first, or pinned as new) or anywhere in another order
   * (oldest first, uploaded, size, random...: the first file stayed where it was, so "how far did the first move"
   * said nothing moved while everything after the new ones had - the open file changed by itself, the user).
   * keep: the open file's position; anchor: the first on screen's. onShift gets how many came in ahead of the
   * anchor (rows for the grid to add above) and where the open file is now (-1: gone).
   */
  async merge(keep: number | null = null, onShift: (shift: number, kept: number) => void = () => {}, anchor: number | null = null): Promise<number> {
    const first = this.idAt(0);
    const before = this.total;
    const anchorAt = anchor ?? 0;
    const anchorId = this.idAt(anchorAt) ?? first;
    const keepId = keep !== null ? this.idAt(keep) : undefined;
    let shift = 0;
    let kept = keep ?? -1;
    const sort = this.sort === 'random' && this.seed ? `random_${this.seed}` : this.sort;
    await this.search(this.query, sort, {
      find: [anchorId, keepId].filter((x): x is number => x !== undefined),
      // Before the new list is shown: how far the anchor moved, and the IDs around where the open file is now - so
      // the viewer never shows a blank or another file for a moment.
      prepare: async (res) => {
        if (anchorId === undefined) return [];
        const at = res.found?.[anchorId] ?? res.ids.indexOf(anchorId);
        shift = at >= 0 ? at - (anchor === null ? 0 : anchorAt) : Math.max(0, res.total - before);
        if (keep === null) return [];
        kept = keepId !== undefined ? (res.found?.[keepId] ?? -1) : -1;
        const want = kept >= 0 ? kept : keep + shift;
        const n = Math.floor(want / SLICE);
        if (n === 0 || want >= res.total) return [];
        try {
          return [[n, (await api.slice(res.result, n * SLICE, SLICE)).ids]];
        } catch {
          return [];
        }
      },
      commit: () => onShift(shift, kept),
    });
    return shift;
  }

  /** A file's position in the loaded part of the list (-1 if not loaded). */
  indexOf(id: number): number {
    for (const [n, ids] of this.slices) {
      const k = ids.indexOf(id);
      if (k >= 0) return n * SLICE + k;
    }
    return -1;
  }

  /** The file ID at a position, if loaded. */
  idAt(i: number): number | undefined {
    if (i < 0 || i >= this.total) return undefined;
    return this.slices.get(Math.floor(i / SLICE))?.[i % SLICE];
  }

  file(id: number | undefined): FileInfo | undefined {
    return id === undefined ? undefined : this.files.get(id);
  }

  fileAt(i: number): FileInfo | undefined {
    return this.file(this.idAt(i));
  }

  /** Loads the ID slices covering positions [from, to) (waiting for ones
   *  already on their way). */
  async ensureIDs(from: number, to: number) {
    from = Math.max(0, from);
    to = Math.min(this.total, to);
    if (from >= to) return;
    const gen = this.gen;
    const resultId = this.resultId;
    const waits: Promise<void>[] = [];
    for (let s = Math.floor(from / SLICE); s <= Math.floor((to - 1) / SLICE); s++) {
      if (this.slices.has(s)) continue;
      let p = this.pendingSlices.get(s);
      if (!p) {
        p = (async () => {
          try {
            const res = await api.slice(resultId, s * SLICE, SLICE);
            if (gen !== this.gen) return;
            this.slices.set(s, res.ids);
            this.version++;
          } catch (e) {
            if (gen !== this.gen) return;
            if (e instanceof ApiError && e.status === 404) {
              this.refresh(); // the server dropped our result list: search again
            }
          } finally {
            this.pendingSlices.delete(s);
          }
        })();
        this.pendingSlices.set(s, p);
      }
      waits.push(p);
    }
    await Promise.all(waits);
  }

  /** Loads IDs and file details for positions [from, to). */
  async ensure(from: number, to: number) {
    from = Math.max(0, from);
    to = Math.min(this.total, to);
    if (from >= to) return;
    const gen = this.gen;

    // 1. ID slices.
    await this.ensureIDs(from, to);
    if (gen !== this.gen) return;

    // 2. File details for the IDs we now have.
    const ids: number[] = [];
    for (let i = from; i < to; i++) {
      const id = this.idAt(i);
      if (id !== undefined && !this.files.has(id) && !this.pendingFiles.has(id)) ids.push(id);
    }
    for (let k = 0; k < ids.length; k += 1000) {
      const batch = ids.slice(k, k + 1000);
      batch.forEach((id) => this.pendingFiles.add(id));
      try {
        const files = await this.fetchFiles(batch);
        for (const f of files) this.files.set(f.id, f);
        this.version++;
      } catch {
        // shown as empty tiles; the next scroll tries again
      } finally {
        batch.forEach((id) => this.pendingFiles.delete(id));
      }
    }
    this.watchThumbnails(from, to);
  }

  /** Files still being processed (just downloaded) have no thumbnail yet: check again shortly. */
  private thumbTimer?: number;
  private watchThumbnails(from: number, to: number) {
    const missing: number[] = [];
    for (let i = from; i < to; i++) {
      const f = this.fileAt(i);
      if (f && !f.thumb) missing.push(f.id);
    }
    if (!missing.length || this.thumbTimer) return;
    this.thumbTimer = window.setTimeout(async () => {
      this.thumbTimer = undefined;
      try {
        const files = await this.fetchFiles(missing.slice(0, 1000));
        for (const f of files) this.files.set(f.id, f);
        this.version++;
      } catch {
        /* next time */
      }
      this.watchThumbnails(from, to);
    }, 3000);
  }

  /** Details for IDs: files, or comics shown as files. */
  private async fetchFiles(ids: number[]): Promise<FileInfo[]> {
    if (this.index === 'comics') return (await api.comicInfo(ids)).comics.map(comicFile);
    return (await api.files(ids)).files;
  }

  /** Deletes (undo with restore), and favorites, for this index's items. */
  remove(ids: number[]) {
    return this.index === 'comics' ? api.deleteComics(ids) : api.remove(ids);
  }
  restore(ids: number[]) {
    return this.index === 'comics' ? api.undeleteComics(ids) : api.restore(ids);
  }
  favorite(ids: number[], on: boolean) {
    return this.index === 'comics' ? api.comicsFavorite(ids, on) : api.favorite(ids, on);
  }

  /** The IDs at positions [from, to], loading the ID slices they need (but not details) - for selecting ranges. */
  async idsInRange(from: number, to: number): Promise<number[]> {
    if (from > to) [from, to] = [to, from];
    from = Math.max(0, from);
    to = Math.min(this.total - 1, to);
    await this.ensureIDs(from, to + 1);
    const out: number[] = [];
    for (let i = from; i <= to; i++) {
      const id = this.idAt(i);
      if (id !== undefined) out.push(id);
    }
    return out;
  }

  /** Changes a cached file's details in place (e.g. a favorite toggled): the grid and viewer follow. */
  patch(id: number, change: Partial<FileInfo>) {
    const f = this.files.get(id);
    if (!f) return;
    this.files.set(id, { ...f, ...change });
    this.version++;
  }

  /** Updates a cached file's details (after an edit). */
  forget(id: number) {
    this.files.delete(id);
  }

  /** Loads the cached details again, in place (nothing goes blank meanwhile): e.g. comics whose pages came in. */
  async reloadDetails() {
    const ids = [...this.files.keys()];
    for (let k = 0; k < ids.length; k += 500) {
      try {
        const files = await this.fetchFiles(ids.slice(k, k + 500));
        for (const f of files) this.files.set(f.id, f);
        this.version++;
      } catch {
        return; // next time
      }
    }
  }
}

export const results = new Results();

/** The 4x4 color preview as a tiny data URL (cached; the browser blurs it when scaling up). */
const placeholderCache = new Map<string, string>();
export function placeholderURL(b64: string | undefined): string {
  if (!b64) return '';
  const hit = placeholderCache.get(b64);
  if (hit) return hit;
  const bytes = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
  const c = document.createElement('canvas');
  c.width = c.height = 4;
  const ctx = c.getContext('2d')!;
  const img = ctx.createImageData(4, 4);
  for (let i = 0; i < 16; i++) img.data.set([bytes[i * 3], bytes[i * 3 + 1], bytes[i * 3 + 2], 255], i * 4);
  ctx.putImageData(img, 0, 0);
  const url = c.toDataURL();
  placeholderCache.set(b64, url);
  return url;
}

export function formatDuration(ms: number): string {
  const s = Math.round(ms / 1000);
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = String(s % 60).padStart(2, '0');
  return h ? `${h}:${String(m).padStart(2, '0')}:${sec}` : `${m}:${sec}`;
}

export function formatSize(n: number): string {
  const fixed = (v: number, digits: number) =>
    new Intl.NumberFormat(i18n.lang, { minimumFractionDigits: digits, maximumFractionDigits: digits }).format(v);
  if (n >= 2 ** 40) return t('unit.size.TB', { x: fixed(n / 2 ** 40, 2) });
  if (n >= 1 << 30) return t('unit.size.GB', { x: fixed(n / (1 << 30), 1) });
  if (n >= 1 << 20) return t('unit.size.MB', { x: fixed(n / (1 << 20), 1) });
  if (n >= 1 << 10) return t('unit.size.KB', { x: fixed(Math.round(n / (1 << 10)), 0) });
  return t('unit.size.B', { n });
}

// Phones view huge images as resized copies: decoding a very large original
// can crash Safari (iOS is a first-class target). Desktops get copies only for
// truly enormous images.
const touchScreen = typeof matchMedia === 'function' && matchMedia('(pointer: coarse)').matches;
function sized(f: FileInfo, limit: number, extra = ''): string {
  if (!f.mime.startsWith('image/') || f.type === 'gif' || !f.width) return f.url;
  if (Math.max(f.width, f.height) <= limit) return f.url;
  return f.url + (f.url.includes('?') ? '&' : '?') + 'size=' + limit + extra; // file URLs carry a signature (?k=)
}

// The screen's long side in device pixels, snapped to the server's sizes:
// big enough to look sharp, small enough to decode and move quickly.
const SIZES = [1600, 2048, 2560, 3072, 4096, 8192];
const screenPx = typeof screen === 'undefined' ? 2048 : Math.ceil(Math.max(screen.width, screen.height) * (devicePixelRatio || 1));
const viewLimit = Math.min(SIZES.find((s) => s >= screenPx) ?? 4096, touchScreen ? 4096 : 8192);

/** The image to show and swipe through: about the screen's size. */
/** A picture's mid-size copy (the grid's hover card). */
export function previewURL(f: FileInfo): string {
  return f.type === 'gif' ? f.url : sized(f, 1600);
}

export function viewURL(f: FileInfo): string {
  return sized(f, viewLimit, '&for=screen'); // the server makes this size ahead for new files
}

/** The image for zooming in: the original, or (huge ones) a big copy. */
export function fullURL(f: FileInfo): string {
  const limit = touchScreen ? 4096 : 8192;
  return limit === viewLimit ? viewURL(f) : sized(f, limit);
}


/** A comic as a grid tile: its cover's shape and thumbnail, the comic itself in .comic. */
function comicFile(c: Comic): FileInfo {
  return {
    id: c.id,
    sha256: '',
    type: 'comic',
    mime: c.cover_mime ?? 'image/jpeg',
    size: 0,
    // (the shape shown: the crop's, if the cover is cropped)
    width: Math.round((c.cover_w ?? 0) * (c.crop?.[2] ?? 1)),
    height: Math.round((c.cover_h ?? 0) * (c.crop?.[3] ?? 1)),
    duration_ms: 0,
    has_audio: false,
    favorite: c.favorite,
    imported_at: c.created_at,
    modified_at: c.updated_at,
    posted_at: c.posted_at,
    placeholder: c.placeholder,
    url: c.cover_url ?? '',
    thumb: c.thumb,
    comic: c,
  };
}
