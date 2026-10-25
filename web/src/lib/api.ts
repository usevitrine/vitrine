// Typed client for Vitrine's JSON API (see internal/server).
import { connection } from './offline.svelte';
import { t } from './i18n.svelte';

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
  }
}

/** Called when a request finds the session gone (to show the login). */
let onLoggedOut: () => void = () => {};
export function setLoggedOutHandler(fn: () => void) {
  onLoggedOut = fn;
}

async function request<T>(method: string, path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  const init: RequestInit = { method, signal, headers: {} };
  if (body !== undefined) {
    (init.headers as Record<string, string>)['Content-Type'] = 'application/json';
    init.body = JSON.stringify(body);
  }
  const resp = await fetch(path, init);
  if (method === 'GET') connection.offline = resp.headers.get('X-Vitrine-Offline') === '1'; // the service worker's saved copy
  const data = await resp.json().catch(() => ({}));
  if (!resp.ok) {
    if (resp.status === 401 && !path.startsWith('/api/login') && !path.startsWith('/api/setup')) {
      onLoggedOut();
    }
    throw new ApiError(data.error ?? resp.statusText, resp.status);
  }
  return data as T;
}

const get = <T>(path: string, signal?: AbortSignal) => request<T>('GET', path, undefined, signal);
const post = <T>(path: string, body: unknown) => request<T>('POST', path, body);

// ---- types

export interface PluginInfo {
  id: string;
  name: string;
  site: string;
  version: string;
  description?: string;
  kind?: 'booru' | 'gallery' | 'social' | string; // what sort of site (Add site's filters); others are only under All
  nsfw?: boolean;
  installed: boolean; // a plugin with this ID is installed
  installed_version?: string;
  update: boolean; // the list has a newer version
}

export interface Installed {
  id: string;
  site: string;
  version: string;
  old_version?: string;
  new: boolean;
}

export interface PushDevice {
  endpoint: string;
  label: string;
  created_at: number;
  last_ok_at?: number;
}

export interface Session {
  setup_needed: boolean;
  logged_in: boolean;
  user?: string;
  language?: string; // the server's default language (VITRINE_LANGUAGE)
  anonymous?: boolean; // signed out: the plain sign-in page (/signin), not the app's own
}

/** Anonymous sign-in (the setting anonymousLogin): signed out, off to the server's plain sign-in page - the app's own
 *  login screen (its name and logo) never shows. /signin is past the service worker (it never keeps that page). */
export function toPlainSignIn() {
  location.replace('/signin');
}

/** Which index a search runs on: '' the library, 'comics' the Comics page. */
export type Scope = '' | 'comics' | 'trash' | 'comics_trash';
const sc = (scope: Scope) => (scope ? '&scope=' + scope : '');

export interface SearchResult {
  result: string; // server-side result list ID
  total: number;
  matched: number;
  ids: number[]; // the first slice
  unknown: string[];
  seed: number;
  took_ms: number;
  query: string; // the query as the server understood it
  now: number; // the server's time of the result (ms), for newCount
  new?: number; // files in the result imported after new_since
  pinned?: number; // of them, how many are first (the New section)
  comics?: number; // the Comics page: the comics found (total counts a collection as one tile)
  shapes?: string; // with shapes=1: every file's width/height x 1024, in order (base64 of uint16s, little-endian)
  found?: Record<string, number>; // with find=: where those files are in the list
}

export interface FileInfo {
  id: number;
  sha256: string;
  type: string;
  mime: string;
  size: number;
  width: number;
  height: number;
  duration_ms: number;
  has_audio: boolean;
  favorite?: boolean;
  came?: number; // times counted with the "I came" button
  imported_at: number;
  modified_at: number;
  posted_at?: number; // when posted on its site (upload time)
  placeholder?: string; // base64 of 4x4 RGB
  url: string;
  thumb?: string;
  /** On the Comics page (a comic shown as a tile): the comic. */
  comic?: Comic;
}

/** A comic (the Comics page; its pages are files, never in the library's grid). */
export interface Comic {
  id: number;
  title: string;
  title_alt?: string;
  site?: string; // plugin ID ("" = imported)
  gallery?: string;
  url?: string;
  category?: string;
  pages: number; // how many it has
  have: number; // how many are downloaded
  cover?: number;
  posted_at?: number;
  created_at: number;
  updated_at: number;
  read_page: number; // where reading stopped (0-based)
  read_at?: number;
  tags: string[];
  thumb?: string; // the cover's thumbnail
  /** Comics > Browse: the library has it, or its download waits. */
  mark?: 'have' | 'queued';
  placeholder?: string;
  cover_w?: number;
  cover_h?: number;
  cover_url?: string;
  cover_mime?: string;
  favorite?: boolean;
  site_title?: string; // the site's title, when the user gave the comic their own (title)
  crop?: number[]; // the part of the cover shown: x, y, width, height (fractions)
  collection?: number; // the collection it's in
  /** This entry is a collection's tile (its comics folded into it): the rest is its cover comic's. */
  stack?: Collection;
  mosaic?: { thumb: string; crop?: number[]; w?: number; h?: number }[]; // (a collection's tile: its first comics' covers)
}

/** Comics that belong together (a series), named by the user. */
export interface Collection {
  id: number;
  name: string;
  count: number;
  read: number; // of them, read to the end
  cover: number; // the comic whose cover it shows
}

/** A file on the duplicates screen: with its tag count and sites. */
export interface DupeFile extends FileInfo {
  tags: number;
  sites: string[] | null;
}

export interface DupePair {
  a: DupeFile;
  b: DupeFile;
  distance: number;
}

export interface DupeList {
  total: number;
  pairs: DupePair[];
  progress: { checked: number; total: number };
}

export interface FileTag {
  tag: string;
  origins: string[];
}

export interface SpriteInfo {
  url: string;
  frames: number;
  cols: number;
  tile_w: number;
  tile_h: number;
  interval_ms: number; // frame k shows the video at k * interval_ms
}

export interface FileSource {
  site: string;
  query: string;
  subscription_id?: number; // the subscription for this search, if there is one now
  downloaded: boolean; // this search downloaded it (else it was in the library already)
  first_at: number;
}

export interface FileLink {
  kind: 'post' | 'source' | 'file';
  site: string;
  url: string;
}

/** A backup of the library database (Settings > Backups). */
export interface UpdateState {
  current: string; // this program's version
  latest: string; // the newest release ("" = not asked yet)
  page: string; // its page on GitHub
  newer: boolean;
  checked_at: number; // Unix ms (0 = never)
  error: string; // why the last check failed
  check: boolean; // the daily check is on (setting updateCheck)
  can_update: boolean; // Watchtower is set up, or installed with the Windows installer: the Update button
  updating: '' | 'backup' | 'download'; // an update under way: its step on the server
  update_error: string; // why the last update failed
  changelog: string; // a newer release's CHANGELOG.md ("" = none, or it couldn't be read)
}

export interface BackupInfo {
  name: string;
  size: number;
  created_at: number;
}

export interface Suggestion {
  tag: string;
  count: number;
  ideal?: string;
}

export interface Relation {
  from: string;
  to: string;
}

export interface DownloadSearch {
  id: number;
  site: string;
  query: string;
  label?: string; // its name where the query says little (a gallery's title)
  pages?: number; // pages it brought in for galleries
  state: 'queued' | 'running' | 'paused' | 'done' | 'failed';
  max_posts: number;
  site_total: number;
  handled: number;
  downloaded: number;
  already: number;
  deleted: number;
  skipped: number;
  blacklisted: number;
  position: number;
  failed: number;
  attempts?: number; // failures in a row
  retry_at?: number; // when it is tried again automatically (ms; absent = not)
  error?: string;
  created_at: number;
  finished_at?: number;
  subscription_id?: number; // set on a subscription's check
  first_check?: boolean; // its first check: a bulk download (it doesn't go first)
}

export interface BrowsedTag {
  tag: string; // "namespace:name" or "name"
  namespace: string;
  count: number; // live files that have it
  ideal?: string; // a sibling: the tag it stands for
  from_sites: boolean; // some file has it from a site
}

/** A site paused by itself: it kept failing to answer; its downloads wait until it does (tried again now and then). */
export interface SitePause {
  since: string;
  until: string; // the next try
  reason: string; // the last failure
  tries: number; // tries that failed since
}

export interface SiteInfo {
  id: string;
  name: string;
  site: string; // its web address, how it's shown ("safebooru.org")
  version: string;
  engine: string;
  host: string;
  built_in: boolean;
  tag_types: boolean;
  fields: string[]; // credential fields
  required: boolean;
  help?: string;
  search_hint?: string; // the new-download box's hint (the site's own search syntax)
  tag_query?: Record<string, string>; // how a tag becomes its search: namespace (or "*") -> template with {tag}
  or_query?: string; // how the site writes an OR search ({tags}), if it can
  or_sep?: string;
  fav_by?: 'id' | 'name' | 'me'; // a user's favorites: fav:<user ID> / fav:<user name> / only your own
  fav_me?: boolean; // fav:me works (its credentials say who you are)
  set: Record<string, boolean>; // which fields have a value (never the values)
  removable: boolean; // always (a built-in one is remembered as removed)
  overrides: boolean; // replaces a built-in plugin (removing goes back to it)
  off: boolean; // switched off: nothing runs, its queue and subscriptions wait
  paused?: SitePause;
  subscriptions: number;
  downloads: number; // not done yet
}

export interface SiteTest {
  ok: boolean;
  posts: number;
  message: string;
  detail?: string;
}

export interface SavedSearch {
  id: number;
  name: string;
  query: string;
  sort: string; // '' = the default order
  count: number; // files it finds now
}

export interface RefreshStatus {
  site: string;
  posts: number; // posts with a file in the library
  due: number; // due for a tag refresh now
  paused?: boolean; // its site isn't answering: what's due waits
}

/** What the Downloads page shows of a subscription besides its numbers (GET /api/subscriptions/looks). */
export interface SubPic {
  id: number;
  thumb: string;
  w: number;
  h: number;
}
export interface SubLook {
  recent?: SubPic[]; // the newest files it downloaded
  comic?: number; // a gallery's comic in the library
  cover?: SubPic; // ... its cover
  pages?: number; // ... its pages
  fresh?: number; // files since the time asked for
}

export interface Subscription {
  id: number;
  site: string;
  query: string;
  label?: string; // its name where the query says little (a gallery's title)
  pages?: number; // pages its checks brought in for galleries
  interval: number; // seconds between checks
  paused: boolean;
  initial_max: number;
  checks: number;
  downloaded: number;
  last_new: number;
  error?: string;
  created_at: number;
  last_check_at?: number;
  next_check_at: number;
  check?: DownloadSearch; // the check in progress (or paused or failed)
  group_id?: number; // its group
  check_depth?: number; // its own check depth (else its group's, site's or the setting for all)
  depth: number; // the check depth used
  fav_gone?: number; // a favorites subscription: files no longer in the site's favorites (delete or keep?)
  last_found_at?: number; // when it last brought in a new file (Unix ms)
}

/** A named group of subscriptions on one site. */
export interface SubGroup {
  id: number;
  site: string;
  name: string;
  interval?: number; // its usual schedule (seconds)
  check_depth?: number; // its own check depth (else its site's or the setting for all)
  depth: number; // what its subscriptions get unless they set one
  created_at: number;
}

export interface BlacklistMatches {
  total: number;
  size: number;
  tags: { tag: string; files: number }[];
}

// ---- calls

export const api = {
  flaresolverr: () => get<{ url: string }>('/api/flaresolverr'),
  setFlaresolverr: (url: string) => post<{ url: string; version?: string; error?: string }>('/api/flaresolverr', { url }),
  dupes: (max: number, limit: number) => get<DupeList>(`/api/dupes?max=${max}&limit=${limit}`),
  decideDupe: (a: number, b: number, keep: 'a' | 'b' | 'both') => post<{ ok: boolean }>('/api/dupes/decide', { a, b, keep }),
  undoDupe: (a: number, b: number) => post<{ ok: boolean }>('/api/dupes/undo', { a, b }),
  plugins: (refresh = false) => get<{ plugins: PluginInfo[]; error?: string }>('/api/plugins' + (refresh ? '?refresh=1' : '')),
  installPlugin: (id: string) => post<{ installed: Installed; sites: SiteInfo[] }>('/api/plugins/install', { id }),
  removePlugin: (id: string) =>
    post<{ removed: { id: string; site: string; restored: boolean }; sites: SiteInfo[] }>(`/api/plugins/${encodeURIComponent(id)}/remove`, {}),
  uploadPlugin: (yaml: string) => post<{ installed: Installed; sites: SiteInfo[] }>('/api/plugins/upload', { yaml }),
  push: () => get<{ key: string; devices: PushDevice[] }>('/api/push'),
  pushSubscribe: (subscription: PushSubscriptionJSON, label: string, language: string) =>
    post<{ key: string; devices: PushDevice[] }>('/api/push/subscribe', { subscription, label, language }),
  pushUnsubscribe: (endpoint: string) =>
    post<{ key: string; devices: PushDevice[] }>('/api/push/unsubscribe', { endpoint }),
  pushTest: (endpoint: string) => post<{ sent: boolean }>('/api/push/test', { endpoint }),
  session: () => get<Session>('/api/session'),
  comics: (q: string, sort: string, offset = 0, limit = 200) =>
    get<{ comics: Comic[]; total: number }>(`/api/comics?q=${encodeURIComponent(q)}&sort=${sort}&offset=${offset}&limit=${limit}`),
  comic: (id: number) => get<{ comic: Comic; pages: FileInfo[]; tags: FileTag[] }>(`/api/comics/${id}`),
  updateComic: (id: number, change: { read_page?: number; title?: string; crop?: number[]; tags?: string[]; add_tags?: string[] }) => post<{ ok: boolean }>(`/api/comics/${id}`, change),
  deleteComics: (ids: number[]) => post<{ done: number }>('/api/comics/delete', { ids }),
  undeleteComics: (ids: number[]) => post<{ done: number }>('/api/comics/undelete', { ids }),
  /** Details for the IDs a comics search gave, in that order. */
  comicInfo: (ids: number[], signal?: AbortSignal) => get<{ comics: Comic[] }>(`/api/comics/info?ids=${ids.join(',')}`, signal),
  comicsFavorite: (ids: number[], on: boolean) => post<{ changed: number }>('/api/comics/do/favorite', { ids, on }),
  comicsRead: (ids: number[], read: boolean) => post<{ changed: number }>('/api/comics/do/read', { ids, read }),
  collections: () => get<{ collections: Collection[] }>('/api/comics/collections'),
  createCollection: (name: string, ids: number[]) => post<{ id: number }>('/api/comics/collections', { name, ids }),
  renameCollection: (id: number, name: string) => post<{ ok: boolean }>(`/api/comics/collections/${id}`, { name }),
  deleteCollection: (id: number) => post<{ ok: boolean }>(`/api/comics/collections/${id}/remove`, {}),
  orderCollection: (id: number, ids: number[]) => post<{ ok: boolean }>(`/api/comics/collections/${id}/order`, { ids }),
  deleteCollectionComics: (id: number) => post<{ ids: number[] }>(`/api/comics/collections/${id}/delete`, {}),
  /** Puts comics in a collection (0: out of theirs). */
  setCollection: (ids: number[], collection: number) => post<{ changed: number }>('/api/comics/do/collection', { ids, collection }),
  comicsTags: (ids: number[], add: string[], remove: string[]) => post<{ changed: number }>('/api/comics/do/tags', { ids, add, remove }),
  comicsTagsOf: (ids: number[]) => post<{ files: number; tags: { tag: string; count: number }[] }>('/api/comics/do/tags-of', { ids }),
  comicsRefresh: (ids: number[]) => post<{ found: number }>('/api/comics/do/refresh', { ids }),
  /** Their pages cleared and fetched again from the site (a download each). */
  comicsRedownload: (ids: number[]) => post<{ queued: number }>('/api/comics/do/redownload', { ids }),
  setup: (name: string, password: string) => post<Session>('/api/setup', { name, password }),
  login: (name: string, password: string) => post<Session>('/api/login', { name, password }),
  logout: () => post<Session>('/api/logout', {}),

  siteCounts: (q: string, scope: Scope = '') =>
    get<{ total: number; sites: { site: string; count: number }[] }>(`/api/search/sites?q=${encodeURIComponent(q)}${sc(scope)}`),
  newCount: (q: string, since: number, bulk = true, scope: Scope = '') =>
    get<{ new: number }>(`/api/new?since=${since}&q=${encodeURIComponent(q)}${bulk ? '' : '&new_bulk=0'}${sc(scope)}`),
  /** collection (comics): that collection's comics only; else a collection's comics fold into its tile. */
  search: (q: string, limit: number, signal?: AbortSignal, fresh?: { since: number; pin: boolean; order: string; bulk: boolean }, shapes = false, scope: Scope = '', collection = 0, find: number[] = []) =>
    get<SearchResult>(
      `/api/search?limit=${limit}&q=${encodeURIComponent(q)}${shapes ? '&shapes=1' : ''}${sc(scope)}${collection ? '&collection=' + collection : ''}${find.length ? '&find=' + find.join(',') : ''}` +
        (fresh?.since ? `&new_since=${fresh.since}&pin=${fresh.pin ? 1 : 0}&new_sort=${encodeURIComponent(fresh.order)}${fresh.bulk ? '' : '&new_bulk=0'}` : ''),
      signal,
    ),
  resultDates: (id: string, field: 'imported' | 'uploaded', n = 200, scope: Scope = '') =>
    get<{ total: number; dates: number[] }>(`/api/results/${id}/dates?field=${field}&n=${n}${sc(scope)}`),
  slice: (result: string, offset: number, limit: number, signal?: AbortSignal) =>
    get<{ total: number; offset: number; ids: number[] }>(
      `/api/results/${result}?offset=${offset}&limit=${limit}`,
      signal,
    ),
  files: (ids: number[], signal?: AbortSignal) => get<{ files: FileInfo[] }>(`/api/files?ids=${ids.join(',')}`, signal),
  fileTags: (id: number) => get<{ tags: FileTag[]; urls: FileLink[]; sources: FileSource[] }>(`/api/files/${id}/tags`),
  tagsOf: (ids: number[]) => post<{ files: number; tags: { tag: string; count: number }[] }>('/api/tags/of', { ids }),
  editTags: (ids: number[], add: string[], remove: string[]) =>
    post<{ updated: number }>('/api/tags', { ids, add, remove }),
  remove: (ids: number[]) => post<{ deleted: number }>('/api/delete', { ids }),
  favorite: (ids: number[], on: boolean) => post<{ changed: number }>('/api/favorite', { ids, on }),
  restore: (ids: number[]) => post<{ restored: number }>('/api/undelete', { ids }),
  /** The sections: what each holds; switching one off (its subscriptions and downloads go; delete: all of it, for good). */
  sections: () => get<Record<'media' | 'comics', { items: number; bytes: number; subscriptions: number; downloads: number }>>('/api/sections'),
  sectionOff: (section: 'media' | 'comics', del: boolean) =>
    post<{ subscriptions: number; downloads: number; deleting: boolean }>(`/api/sections/${section}/off`, { delete: del }),
  /** The trash: what it holds, and after how many days it empties itself (0 = never). */
  trashInfo: () => get<{ files: number; bytes: number; comics: number; comic_bytes: number; days: number }>('/api/trash'),
  purgeTrash: (ids: number[]) => post<{ purged: number }>('/api/trash/purge', { ids }),
  emptyTrash: () => post<{ purged: number }>('/api/trash/empty', {}),
  purgeComicsTrash: (ids: number[]) => post<{ purged: number }>('/api/trash/purge-comics', { ids }),
  emptyComicsTrash: () => post<{ purged: number }>('/api/trash/empty-comics', {}),
  /** A favorites subscription's files no longer in the site's favorites, and what to do with them. */
  favGone: (sub: number) => get<{ files: number[] }>(`/api/subscriptions/${sub}/fav-gone`),
  favGoneDelete: (sub: number) => post<{ deleted: number }>(`/api/subscriptions/${sub}/fav-gone-delete`, {}),
  favGoneKeep: (sub: number) => post<{ ok: boolean }>(`/api/subscriptions/${sub}/fav-gone-keep`, {}),
  /** sites: only the tags of files from those sites (comma-separated, as in system:site=). */
  autocomplete: (q: string, limit = 12, signal?: AbortSignal, scope: Scope = '', sites = '') =>
    get<{ suggestions: Suggestion[]; took_ms: number }>(
      `/api/autocomplete?limit=${limit}&q=${encodeURIComponent(q)}${sc(scope)}${sites ? '&sites=' + encodeURIComponent(sites) : ''}`,
      signal,
    ),
  /** The comics' tags (never the library's); site: only that site's galleries'. */
  comicTags: (q: string, site = '', limit = 12, signal?: AbortSignal) =>
    get<{ suggestions: Suggestion[] }>(`/api/comics/tags?limit=${limit}&site=${encodeURIComponent(site)}&q=${encodeURIComponent(q)}`, signal),
  relations: () => get<{ parents: Relation[]; siblings: Relation[] }>('/api/relations'),
  editRelation: (kind: 'parent' | 'sibling', action: 'add' | 'remove', from: string, to = '') =>
    post<{ parents: Relation[]; siblings: Relation[] }>('/api/relations', { kind, action, from, to }),
  namespaces: (scope: Scope = '') => get<{ namespaces: string[] }>('/api/namespaces?x' + sc(scope)),
  /** The problem report (plain text, nothing private in it). */
  report: async () => {
    const r = await fetch('/api/report', { cache: 'no-store' });
    if (!r.ok) throw new ApiError(t('report.failed', { status: String(r.status) }), r.status);
    return r.text();
  },
  /** The saved searches menu's NEW pills: files imported since each saved search / group tag was last opened. */
  news: (bulk: boolean) => get<News>('/api/news' + (bulk ? '' : '?new_bulk=0')),
  /** A saved search ("saved:<id>") or group tag ("tag:<tag>") opened from the menu: when it was opened before. */
  seen: (key: string) => post<{ previous: number }>('/api/seen', { key }),
  /** Posts whose file couldn't be brought in (newest first), and one tried again now. */
  /** The downloader's recent events for the bell (see notices.svelte.ts). */
  notices: () => get<{ boot: string; notices: import('./notices.svelte').ServerNotice[]; read?: number }>('/api/notices'),
  addNotice: (text: string, tone: string, category: string) => post<{ id: number }>('/api/notices', { text, tone, category }),
  markNotices: (upTo: number, clear: boolean) => post<{ read: number; cleared: number }>(clear ? '/api/notices/clear' : '/api/notices/read', { up_to: upTo }),
  browseSites: () => get<{ sites: string[]; sorts: Record<string, string[]> }>('/api/browse/sites'),
  browse: (site: string, q: string, before = 0, sort = '', signal?: AbortSignal) =>
    get<{ galleries: BrowseGallery[]; total: number; next: number }>(
      `/api/browse?site=${encodeURIComponent(site)}&q=${encodeURIComponent(q)}&before=${before}${sort ? '&sort=' + encodeURIComponent(sort) : ''}`,
      signal,
    ),
  browseTags: (site: string, q: string, signal?: AbortSignal) =>
    get<{ suggestions: Suggestion[] }>(`/api/browse/tags?site=${encodeURIComponent(site)}&q=${encodeURIComponent(q)}`, signal),
  browseThumbs: (site: string, id: number, token: string, p = 0) =>
    get<{ thumbs: BrowseThumb[]; pages: number }>(`/api/browse/gallery?site=${encodeURIComponent(site)}&id=${id}&token=${token}&p=${p}`),
  failedPosts: () => get<{ posts: FailedPost[] }>('/api/failed-posts'),
  /** Tries a failed post again in the background (it shows in its site's queue: see RetryJob). */
  dismissPosts: (posts: { site: string; post: string }[]) => post<{ dismissed: number }>('/api/failed-posts/dismiss', { posts }),
  retryPost: (site: string, id: string) => post<{ started: boolean }>('/api/failed-posts/retry', { site, post: id }),
  climax: (file: number) => post<{ id: number; count: number }>('/api/climax', { file }),
  /** A press in the comic reader, on a page (0-based): that page's count. */
  climaxPage: (comic: number, page: number) => post<{ id: number; count: number }>('/api/climax', { comic, page }),
  removeClimaxes: (file: number, all: boolean) => post<{ count: number }>('/api/climax/remove', { file, all }),
  clearClimaxes: () => post<{ removed: number }>('/api/climax/clear', {}),
  undoClimax: (id: number) => post<{ file: number; count: number }>('/api/climax/undo', { id }),
  rankings: (period: string) => get<Rankings>(`/api/rankings?period=${period}&tz=${-new Date().getTimezoneOffset()}`),
  stats: (days = 0) => get<LibraryStats>(`/api/stats?days=${days}&tz=${-new Date().getTimezoneOffset()}`),
  downloads: () =>
    get<{ sites: string[]; sites_off?: string[]; sites_paused?: Record<string, SitePause>; searches: DownloadSearch[]; subscriptions: Subscription[]; groups: SubGroup[]; refresh: RefreshStatus[]; retries?: RetryJob[] }>(
      '/api/downloads',
    ),
  /** Only what's going on now (running, waiting, paused) and the paused sites: the top bar's downloads menu. */
  activeDownloads: () => get<{ searches: DownloadSearch[]; sites_paused?: Record<string, SitePause> }>('/api/downloads?active=1'),
  /** favorite: the files it brings in become favorites too (e.g. your own favorites on the site). */
  addDownload: (site: string, query: string, max_posts: number, favorite = false, label = '') =>
    post<DownloadSearch>('/api/downloads', { site, query, max_posts, favorite, label }),
  pauseDownload: (id: number) => post<{ ok: boolean }>(`/api/downloads/${id}/pause`, {}),
  resumeDownload: (id: number) => post<{ ok: boolean }>(`/api/downloads/${id}/resume`, {}),
  backups: () => get<{ backups: BackupInfo[]; dir: string; auto: boolean; keep: number; pending: boolean; last_error: string }>('/api/backups'),
  createBackup: () => post<{ backup: BackupInfo }>('/api/backups', {}),
  /** Vitrine's updates: the latest release (asked for once a day), and the Update button (with Watchtower). */
  updateState: () => get<UpdateState>('/api/update'),
  checkForUpdate: () => post<UpdateState>('/api/update/check', {}),
  startUpdate: () => post<{ started: boolean; backup: string }>('/api/update', {}),
  deleteBackup: (name: string) => post<{ ok: boolean }>(`/api/backups/${encodeURIComponent(name)}/delete`, {}),
  restoreBackup: (name: string) => post<{ ok: boolean; pending: boolean }>(`/api/backups/${encodeURIComponent(name)}/restore`, {}),
  /** Stages a backup file (a .db.gz from Download, or a plain meta.db). */
  uploadBackup: async (file: Blob) => {
    const resp = await fetch('/api/backups/upload', { method: 'POST', body: file, headers: { 'Content-Type': 'application/octet-stream', 'X-Vitrine-Upload': '1' } });
    const data = await resp.json().catch(() => ({}));
    if (!resp.ok) throw new ApiError(data.error ?? resp.statusText, resp.status);
    return data as { ok: boolean; pending: boolean };
  },
  cancelRestore: () => post<{ ok: boolean }>('/api/backups/cancel-restore', {}),
  restart: () => post<{ ok: boolean }>('/api/restart', {}),
  removeDownload: (id: number) => post<{ ok: boolean }>(`/api/downloads/${id}/remove`, {}),
  clearDownloads: () => post<{ removed: number }>('/api/downloads/clear', {}),
  reorderDownloads: (site: string, ids: number[]) => post<{ ok: boolean }>('/api/downloads/reorder', { site, ids }),
  tagList: (q: string, ns: string, sort: 'count' | 'name', offset: number, limit: number) =>
    get<{ total: number; tags: BrowsedTag[] }>(
      `/api/tags?q=${encodeURIComponent(q)}&ns=${encodeURIComponent(ns)}&sort=${sort}&offset=${offset}&limit=${limit}`,
    ),
  renameTag: (from: string, to: string) => post<{ files: number }>('/api/tags/rename', { from, to }),
  // A tag into another namespace ("" = general), on every file; merged into a tag of that name there.
  setTagNamespace: (tag: string, namespace: string) => post<{ tag: string; files: number }>('/api/tags/namespace', { tag, namespace }),
  settings: () => get<Record<string, unknown>>('/api/settings'),
  setSettings: (values: Record<string, unknown>) => post<Record<string, unknown>>('/api/settings', values),
  sites: () => get<{ sites: SiteInfo[] }>('/api/sites'),
  /** Which sites gave each tag (most files first). */
  tagSites: (tags: string[]) =>
    get<{ tags: Record<string, { site: string; count: number }[]> }>('/api/tags/sites?' + tags.map((t) => 't=' + encodeURIComponent(t)).join('&')),
  setCredentials: (id: string, values: Record<string, string>) =>
    post<{ sites: SiteInfo[] }>(`/api/sites/${id}/credentials`, { values }),
  /** values: typed credentials to try (not saved); none = the saved ones. */
  testSite: (id: string, values?: Record<string, string>) => post<SiteTest>(`/api/sites/${id}/test`, values ? { values } : {}),
  trySite: (id: string) => post<{ sites: SiteInfo[] }>(`/api/sites/${encodeURIComponent(id)}/try`, {}),
  setSiteOff: (id: string, off: boolean) => post<{ sites: SiteInfo[] }>(`/api/sites/${encodeURIComponent(id)}/off`, { off }),
  saved: (scope: Scope = '') => get<{ saved: SavedSearch[] }>('/api/saved?x' + sc(scope)),
  addSaved: (name: string, query: string, sort: string, scope: Scope = '') =>
    post<{ id: number; saved: SavedSearch[] }>('/api/saved?x' + sc(scope), { name, query, sort }),
  updateSaved: (id: number, change: { name?: string; query?: string; sort?: string }, scope: Scope = '') =>
    post<{ saved: SavedSearch[] }>(`/api/saved/${id}?x${sc(scope)}`, change),
  removeSaved: (id: number, scope: Scope = '') => post<{ saved: SavedSearch[] }>(`/api/saved/${id}/remove?x${sc(scope)}`, {}),
  reorderSaved: (ids: number[], scope: Scope = '') => post<{ saved: SavedSearch[] }>('/api/saved/reorder?x' + sc(scope), { ids }),
  scheduleRefresh: (ids: number[]) => post<{ posts: number }>('/api/refresh', { ids }),
  sprite: (id: number) => get<SpriteInfo>(`/api/files/${id}/sprite`),
  refreshFile: (id: number) => post<{ found: number; queued?: number }>(`/api/files/${id}/refresh`, {}),
  subscribe: (site: string, query: string, interval: number, initial_max: number, group: { group_id?: number; group_name?: string } = {}, favorite = false, label = '') =>
    post<Subscription>('/api/subscriptions', { site, query, interval, initial_max, ...group, favorite, label }),
  addGroup: (site: string, name: string) => post<SubGroup>('/api/sub-groups', { site, name }),
  updateGroup: (id: number, change: { name?: string; interval?: number; check_depth?: number }) => post<SubGroup>(`/api/sub-groups/${id}`, change),
  groupAction: (id: number, action: 'check' | 'pause' | 'resume' | 'remove') => post<{ ok: boolean }>(`/api/sub-groups/${id}/${action}`, {}),
  subscriptionLooks: (since: number) => get<{ looks: Record<string, SubLook> }>(`/api/subscriptions/looks?since=${since}`),
  subscribeSearch: (search_id: number, interval: number) => post<Subscription>('/api/subscriptions', { search_id, interval }),
  updateSubscription: (id: number, change: { interval?: number; paused?: boolean; group_id?: number; check_depth?: number }) =>
    post<Subscription>(`/api/subscriptions/${id}`, change),
  checkSubscription: (id: number) => post<{ ok: boolean }>(`/api/subscriptions/${id}/check`, {}),
  removeSubscription: (id: number) => post<{ ok: boolean }>(`/api/subscriptions/${id}/remove`, {}),
  blacklist: () => get<{ tags: string[] }>('/api/blacklist'),
  setBlacklist: (tags: string[]) => post<{ tags: string[] }>('/api/blacklist', { tags }),
  blacklistMatches: () => get<BlacklistMatches>('/api/blacklist/matches'),
  deleteBlacklisted: (tags: string[] = []) =>
    post<{ deleted: number; ids: number[] }>('/api/blacklist/delete', { tags }),
};

/** Settings > Stats: the library, downloads, storage and the server at a glance. */
export interface LibraryStats {
  files: number;
  bytes: number;
  kinds: { kind: 'image' | 'gif' | 'video'; files: number; bytes: number }[];
  trash: { files: number; bytes: number }; // deleted, still in the undo window
  removed: number; // deleted for good (never downloaded again)
  tags: number;
  tag_links: number;
  user_links: number;
  parents: number;
  siblings: number;
  untagged: number;
  favorites: number;
  imports: number[]; // per day, oldest first, ending today
  since: number; // first import (unix seconds; 0 = none)
  sites: { site: string; files: number; posts: number }[];
  subscriptions: { total: number; paused: number; failing: number };
  took_ms: number;
  storage: {
    originals: number;
    database: number;
    thumbs: number;
    previews: number;
    backups: number;
    disks: { for: 'data' | 'files' | 'both'; total: number; free: number }[];
    data_dir: string;
    files_dir: string;
  };
  server: {
    version: string;
    uptime_s: number;
    go: string;
    os: string;
    cpus: number;
    memory: number;
    heap: number;
    ffmpeg: string;
    vips: string;
    in_docker: boolean;
  };
}

export interface News {
  saved: { id: number; new: number; seen_at: number }[];
  groups: { tag: string; children: number; new: number; seen_at: number }[];
}

/** A failed post being tried again in the background, or just tried (kept a few minutes with its result). */
export interface RetryJob {
  site: string;
  post: string;
  query?: string;
  started: number;
  finished?: number;
  outcome?: string; // once finished: downloaded, already, failed, ...
  error?: string;
}

/** A gallery on a site being browsed (Comics > Browse). */
export interface BrowseGallery {
  id: number;
  token: string;
  url: string;
  title: string;
  title_alt?: string;
  category?: string;
  pages: number;
  posted?: number;
  cover?: string;
  rating?: number;
  uploader?: string;
  tags: { ns?: string; name: string }[];
  comic?: number; // in the library already: the comic's ID
  queued?: boolean; // its download waits or runs
}

/** One page's thumbnail: part of a picture holding several (x, y, w, h in pixels). */
export interface BrowseThumb {
  page: number;
  url: string;
  x: number;
  y: number;
  w: number;
  h: number;
}

export interface FailedPost {
  site: string;
  post: string; // as text: a Mastodon ID is too big for a number
  url?: string;
  query?: string;
  error?: string;
  at: number;
}

/** The Rankings page for a period (the "I came" button's counts). */
export interface Rankings {
  total: number;
  days: number[]; // per day, oldest first, ending today
  files: { id: number; count: number }[];
  pages: { comic: number; page: number; file: number; title: string; count: number }[]; // the comic reader's presses
  creators: { tag: string; count: number }[];
  characters: { tag: string; count: number }[];
  series: { tag: string; count: number }[];
  tags: { tag: string; count: number }[];
  streak: number;
  first: number;
}
