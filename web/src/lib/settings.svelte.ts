// Preferences. Most are shared by every device and kept on the server (each
// device also keeps a copy, so the app starts instantly and works offline);
// screen spacing is per device. Local reads and writes never throw: private
// modes may block storage.
import { api } from './api';

function load<T>(key: string, fallback: T): T {
  try {
    const v = localStorage.getItem('vitrine.' + key);
    return v === null ? fallback : (JSON.parse(v) as T);
  } catch {
    return fallback;
  }
}

function save(key: string, value: unknown) {
  try {
    localStorage.setItem('vitrine.' + key, JSON.stringify(value));
  } catch {
    /* not saved - fine */
  }
}

/** The settings every device shares (the server's allow-list). */
const shared = ['startMuted', 'loop', 'speed', 'defaultSearch', 'defaultSort', 'favoritesSort', 'comicsSort', 'comicsSearch', 'browseSearch', 'browseSort', 'cameButton', 'sfwMode', 'mediaOff', 'comicsOff', 'anonymousLogin', 'trashDays', 'skipVideoMB', 'skipImageMP', 'notifyNew', 'notifyDone', 'notifyFailed', 'notifyBlacklisted', 'hoverPreview', 'seenAt', 'newOrder', 'newMode', 'newFromBulk', 'backupAuto', 'backupKeep', 'welcome', 'checkDepth', 'siteCheckDepths', 'language', 'pauseAfter', 'noticeModes', 'checksFirst', 'updateCheck', 'whatsNew', 'whatsNewSeen'] as const;
type SharedKey = (typeof shared)[number];

class Settings {
  /** Every video starts muted; unmuting affects only the current video. */
  startMuted = $state(load('startMuted', false)); // (videos with their sound unless asked - the user, 2026-10-26; where the browser won't, the app plays one muted and offers sound)
  /** Remembered across videos. */
  loop = $state(load('loop', true));
  /** New files: imported after seenAt (set by "Mark all as seen"; shared by every
   *  device). Shown first in newOrder; newMode: 'auto' adds arrivals at once
   *  (when the grid is at the top), 'button' shows the N new files button. */
  seenAt = $state(load('seenAt', 0));
  newOrder = $state(load('newOrder', 'imported'));
  newMode = $state(load('newMode', 'button'));
  /** Whether files from one-off downloads, subscriptions' first checks and imports count as
   *  new (off: only files from subscriptions' regular checks). */
  newFromBulk = $state(load('newFromBulk', true));
  /** A database backup every day, keeping the newest backupKeep (Settings > Backups). */
  backupAuto = $state(load('backupAuto', true));
  backupKeep = $state(load('backupKeep', 7));
  /** Desktop: hovering a video's thumbnail plays it (muted). */
  hoverPreview = $state(load('hoverPreview', true));
  speed = $state(load('speed', 1));
  /** The search run when the app opens with no search in the URL. */
  defaultSearch = $state(load('defaultSearch', ''));
  /** The order used unless one is picked: imported, imported_asc, uploaded, uploaded_asc. */
  defaultSort = $state(load('defaultSort', 'uploaded')); // newest uploaded first
  /** The Favorites page's default order: the newest favorite first. */
  favoritesSort = $state(load('favoritesSort', 'favorite'));
  /** The Comics page's default order (its own: read, title, pages...); newest added first. */
  comicsSort = $state(load('comicsSort', 'imported'));
  /** The Comics page's search when it opens without one. */
  comicsSearch = $state(load('comicsSearch', ''));
  /** Comics > Browse's search when it opens ('': the site's newest galleries). */
  browseSearch = $state(load('browseSearch', ''));
  /** Comics > Browse's order when it opens: 'newest', or one of a site's own lists (it has no search then). */
  browseSort = $state(load('browseSort', 'newest'));
  /** The viewer's "I came" button and the Rankings page (off unless turned on). */
  cameButton = $state(load('cameButton', false));
  /** The app's sections, Media and Comics: either can be switched off (never both; lib/sections). */
  mediaOff = $state(load('mediaOff', false));
  comicsOff = $state(load('comicsOff', false));
  /** SFW mode: nothing adult in the app (2026-10-23) - the "I came" button and Rankings off, no adult sites offered. */
  sfwMode = $state(load('sfwMode', false));
  /** The "I came" button, Rankings and counts: on, and not in SFW mode. */
  get came() {
    return this.cameButton && !this.sfwMode;
  }
  /** Signed out: a plain sign-in page that says nothing about what it's for (off unless turned on). */
  anonymousLogin = $state(load('anonymousLogin', false));
  /** The trash empties itself of files deleted more than this many days ago (0 = never). */
  trashDays = $state(load('trashDays', 0));
  /** Download rules: skip videos over this many MB / images over this many megapixels (0 = no limit). */
  skipVideoMB = $state(load('skipVideoMB', 0));
  skipImageMP = $state(load('skipImageMP', 0));
  /** The app's language for every device: 'auto' (each browser's own) or a language tag (lib/i18n). */
  language = $state(load('language', 'auto'));
  /** Check depth: posts a subscription's check goes on past the last check's newest (see lib/depth). */
  checkDepth = $state(load('checkDepth', 0));
  /** Connection errors in a row that pause a site's downloads until it answers again (0: never). */
  pauseAfter = $state(load('pauseAfter', 5));
  /** Subscription checks go ahead of downloads; a download running steps aside for them (shared). */
  checksFirst = $state(load('checksFirst', true));
  /** Where each kind of notice goes (lib/noticeKinds): only the kinds changed from their default. */
  noticeModes = $state<Record<string, string>>(load('noticeModes', {}));
  /** Ask once a day whether a newer Vitrine is out (the server asks GitHub). */
  updateCheck = $state(load('updateCheck', true));
  /** After an update, the new version's changes in a window on the library (lib/whatsNew); the last version shown. */
  whatsNew = $state(load('whatsNew', true));
  whatsNewSeen = $state(load('whatsNewSeen', ''));
  siteCheckDepths = $state<Record<string, number>>(load('siteCheckDepths', {}));
  /** Which push notifications to send (to every device that turned them on). */
  notifyNew = $state(load('notifyNew', true));
  notifyDone = $state(load('notifyDone', true));
  notifyFailed = $state(load('notifyFailed', true));
  notifyBlacklisted = $state(load('notifyBlacklisted', true));
  /** The first-start steps (after the first account): the step shown; 'finished' = closed, '' = never started. */
  welcome = $state<'' | 'sections' | 'storage' | 'sites' | 'look' | 'app' | 'notify' | 'rules' | 'library' | 'done' | 'finished'>(load('welcome', ''));
  /** Per device. Extra space at the top in the iOS home-screen app, below the
   *  status bar (iOS 27's status bar blurs a strip of the page under it). */
  topSpacing = $state(load('topSpacing', 0));
  /** Per device. The grid: masonry ("Rows": each tile in its file's shape; the default), or square tiles. */
  gridLayout = $state<'squares' | 'masonry'>(load('gridLayout', 'masonry'));

  private synced = false; // the server's values have been read: changes go back to it
  /** Whether the server's values have been read (else the local copy stands, maybe stale). */
  get isSynced() {
    return this.synced;
  }
  /** This device has read the server's settings before (its copy is a good start). */
  syncedBefore = load('syncedOnce', false);
  private applying = false; // copying the server's values in: not a change to send
  private pending: Partial<Record<SharedKey, unknown>> = {};
  private timer = 0;
  private gotFirst!: () => void;
  private first = new Promise<void>((r) => (this.gotFirst = r));

  /** Resolves once the server's settings have been read (or tried), but waits
   *  no longer than cap ms - for what must use them (the library's first search). */
  whenSynced(cap = 1500) {
    return Promise.race([this.first, new Promise<void>((r) => setTimeout(r, cap))]);
  }

  constructor() {
    $effect.root(() => {
      for (const k of shared) {
        $effect(() => {
          const v = this[k];
          save(k, v);
          if (this.synced && !this.applying) this.push(k, v);
        });
      }
      $effect(() => {
        save('topSpacing', this.topSpacing);
        save('gridLayout', this.gridLayout);
        document.documentElement.style.setProperty('--top-extra', `${this.topSpacing}px`);
      });
    });
    // Back on screen: pick up changes made on another device. Leaving (app
    // switched away, tab closed, reload): a change still waiting goes now.
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'visible' && this.synced) this.pull();
      else if (document.visibilityState === 'hidden') this.flush();
    });
    addEventListener('pagehide', () => this.flush());
  }

  /** Reads the shared settings from the server (after logging in). The first
   *  time ever, this device's own settings become the shared ones. */
  async sync() {
    try {
      const server = await api.settings();
      if (Object.keys(server).length === 0) {
        await api.setSettings(Object.fromEntries(shared.map((k) => [k, this[k]])));
      } else {
        this.apply(server);
      }
      this.synced = true;
      if (!this.syncedBefore) save('syncedOnce', (this.syncedBefore = true));
      // First time with new files: everything already here counts as seen.
      if (!this.seenAt) this.seenAt = Date.now();
    } catch {
      // Offline: the local copy stands; the next sync tries again.
    }
    this.gotFirst();
  }

  private async pull() {
    try {
      this.apply(await api.settings());
    } catch {
      /* next time */
    }
  }

  private apply(server: Record<string, unknown>) {
    this.applying = true;
    for (const k of shared) {
      // A change made here and not sent yet wins over the server's older value.
      if (k in this.pending) continue;
      if (k in server && server[k] !== this[k]) {
        (this as Record<SharedKey, unknown>)[k] = server[k];
      }
    }
    // Effects run after this turn: clear the flag once they have.
    queueMicrotask(() => setTimeout(() => (this.applying = false)));
  }

  /** Sends a change still waiting at once (keepalive: it gets there even as the page goes). */
  private flush() {
    if (!Object.keys(this.pending).length) return;
    clearTimeout(this.timer);
    const values = this.pending;
    this.pending = {};
    fetch('/api/settings', { method: 'POST', keepalive: true, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(values) }).catch(() => {});
  }

  /** Sends changes in one request, a moment after the last one (sliders, typing). */
  private push(k: SharedKey, v: unknown) {
    this.pending[k] = v;
    clearTimeout(this.timer);
    this.timer = window.setTimeout(async () => {
      const values = this.pending;
      this.pending = {};
      try {
        await api.setSettings(values);
      } catch {
        // Not saved on the server (offline, or a value it refused): kept on this device.
      }
    }, 600);
  }
}

export const settings = new Settings();
