// A tiny router: the path picks the screen, the query string holds the
// search (?q=) and the open file (?i= position, &f= file ID), so a reload or
// reopening the home-screen app returns to the same place.

export type Screen = 'library' | 'comics' | 'browse' | 'downloads' | 'tags' | 'duplicates' | 'rankings' | 'settings';

const paths: Record<Screen, string> = {
  library: '/',
  comics: '/comics', // a comic open in the reader: /comics/<id>
  browse: '/browse', // a gallery site's search (Comics > Browse)
  downloads: '/downloads',
  tags: '/tags',
  duplicates: '/duplicates',
  rankings: '/rankings',
  settings: '/settings',
};

// The app's own path in the address: "" at the site's root, as always; the website's demo is in a folder of its own
// (vite's base, e.g. /demo/app/).
const base = import.meta.env.BASE_URL.replace(/\/$/, '');
const own = (path: string) => (base && path.startsWith(base) ? path.slice(base.length) || '/' : path);

/** Settings sections renamed (2026-10-31, lib/settingsSections): their old addresses open the new ones. */
const settingsRenamed: Record<string, string> = { rules: 'downloads', viewing: 'appearance', updates: 'about' };

/** A screen's sub-page: the path after the screen's own ("/tags/rules" -> "rules"; "" = its first page); a renamed
 *  Settings section's old name gives the new one. */
function subOf(path: string): string {
  path = own(path);
  const s = screenOf(path);
  if (s === 'library') {
    const p = path.replace(/^\/+|\/+$/g, '');
    return p === 'favorites' || p === 'trash' ? p : '';
  }
  const sub = path.slice(paths[s].length).replace(/^\/+|\/+$/g, '');
  return (s === 'settings' && settingsRenamed[sub]) || sub;
}

/** A screen's path (the library's Favorites and Trash pages: /favorites, /trash). */
const pathOf = (screen: Screen, sub: string) => base + (screen === 'library' ? (sub ? '/' + sub : '/') : paths[screen] + (sub ? '/' + sub : ''));

function screenOf(path: string): Screen {
  path = own(path);
  for (const [s, p] of Object.entries(paths)) {
    if (p !== '/' && path.startsWith(p)) return s as Screen;
  }
  return 'library';
}

class Router {
  screen = $state<Screen>(screenOf(location.pathname));
  sub = $state(subOf(location.pathname));
  /** A search another screen asked the library to run (see openSearch). */
  pendingSearch = $state<string | null>(null);
  /** Something the next screen should bring into view (e.g. "sub:12" on Downloads). */
  focus = $state<string | null>(null);

  constructor() {
    // Opened with ?focus=x (a tapped push notification starting the app): brought into view, the address tidied.
    const start = new URL(location.href);
    const f = start.searchParams.get('focus');
    if (f) {
      this.focus = f;
      start.searchParams.delete('focus');
      history.replaceState(history.state, '', start.pathname + start.search + start.hash);
    }
    // (a renamed Settings section's old address: the new one in its place)
    if (this.screen === 'settings' && settingsRenamed[own(location.pathname).replace(/^\/settings\/?|\/+$/g, '')]) {
      history.replaceState(history.state, '', pathOf('settings', this.sub) + location.search + location.hash);
    }
    // A tapped notification while the app is open (see public/sw.js).
    navigator.serviceWorker?.addEventListener('message', (e) => {
      if (e.data?.type === 'open' && typeof e.data.path === 'string') this.openPath(e.data.path);
    });
    addEventListener('popstate', () => {
      this.screen = screenOf(location.pathname);
      this.sub = subOf(location.pathname);
    });
  }

  go(screen: Screen, sub = '') {
    if (screen === this.screen && sub === this.sub) return;
    // The library and its Favorites page each come back to their own search and file.
    const search = screen === 'library' ? (libraryParams[sub] ?? '') : '';
    if (this.screen === 'library') libraryParams[this.sub] = location.search;
    history.pushState(null, '', pathOf(screen, sub) + search);
    this.screen = screen;
    this.sub = sub;
  }

  /** Goes to a screen in place of the current one (no new history step: Back skips it). */
  replace(screen: Screen, sub = '') {
    history.replaceState(history.state, '', pathOf(screen, sub));
    this.screen = screen;
    this.sub = sub;
  }

  /** Goes to an app path ("/downloads", "/?q=x"), e.g. from a tapped notification. */
  openPath(path: string) {
    const u = new URL(path, location.origin);
    if (u.searchParams.get('new') === '1') {
      // From a "new files" notification: the whole library, new files first, at
      // the top (Back returns to the search).
      this.pendingSearch = '';
      this.focus = 'library-top';
      this.go('library');
      return;
    }
    // (?focus=x: what to bring into view there - a push notification's: files that got a blacklisted tag, Delete them)
    const f = u.searchParams.get('focus');
    if (f) {
      this.focus = f;
      u.searchParams.delete('focus');
    }
    if (this.screen === 'library') libraryParams[this.sub] = location.search;
    history.pushState(null, '', u.pathname + u.search);
    this.screen = screenOf(u.pathname);
    this.sub = subOf(u.pathname);
  }

  /** Shows the library with this search. */
  openSearch(q: string) {
    this.pendingSearch = q;
    this.go('library');
  }

  /** Replaces the library's URL parameters without adding history entries. */
  setParams(params: Record<string, string | undefined>) {
    const u = new URL(location.href);
    for (const [k, v] of Object.entries(params)) {
      if (v === undefined || v === '') u.searchParams.delete(k);
      else u.searchParams.set(k, v);
    }
    history.replaceState(history.state, '', u.pathname + u.search);
  }

  param(name: string): string {
    return new URL(location.href).searchParams.get(name) ?? '';
  }
}

// The library pages' URL parameters (search, open file) while another screen shows: '' the library, 'favorites'.
const libraryParams: Record<string, string> = {};

export const router = new Router();
