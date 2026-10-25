// The bell's notifications: one list for every device, kept by the server - what the downloader did (new files
// from a subscription, a download finished or failed, a blacklisted tag, a site that stopped or started answering)
// and what was done in the app that's set to go to the bell (Settings > Notifications; lib/noticeKinds) - newest
// first. Read and cleared are the same everywhere: opening the bell reads them, its broom clears them, on every
// device (open pages follow at once). New ones ring the bell. A kind turned off (or set to show on screen only)
// isn't listed; the downloader's set to on screen show there as they come.
import { api } from './api';
import { t } from './i18n.svelte';
import { modeOf } from './noticeKinds';
import { settings } from './settings.svelte';
import type { ToastKind } from './toast.svelte';

export interface Notice {
  key: string; // its start and number on the server
  id: number;
  text: string;
  kind: ToastKind;
  cat: string; // the kind of notice (lib/noticeKinds)
  at: number; // Unix ms
  read: boolean;
  to?: 'downloads' | 'settings' | 'tags'; // where it leads (a download's: the Downloads page; a new release's: Settings > Updates; files with a blacklisted tag: Tags > Rules)
  sub?: string;
  /** Something to bring into view where a tap goes (router.focus). */
  focus?: string;
}

/** A notice as the server sends it: one of the downloader's events (worded here, in this page's language), or
 *  something done in the app (kind "app": its words as they were). */
export interface ServerNotice {
  id: number;
  at: number;
  kind: 'new' | 'failed' | 'done' | 'blacklisted' | 'paused' | 'back' | 'app' | 'release';
  site?: string;
  query?: string;
  subscription?: boolean;
  new?: number;
  error?: string;
  tag?: string;
  first?: boolean;
  pages?: number; // a gallery site's: the pages it brought (new counts galleries)
  text?: string;
  tone?: ToastKind;
  category?: string;
}

// (the bell kept on each device before it was one for all: gone)
for (const k of ['vitrine.notices', 'vitrine.noticesSynced', 'vitrine.noticesSeen']) {
  try {
    localStorage.removeItem(k);
  } catch {
    /* fine */
  }
}

/** A server event in words. */
function word(n: ServerNotice): { text: string; kind: ToastKind } {
  if (n.kind === 'app') return { text: n.text ?? '', kind: n.tone ?? 'done' };
  const what = t('push.on', { query: n.query || t('push.everything'), site: n.site ?? '' });
  switch (n.kind) {
    case 'new':
      // (a gallery site's: its pages - its galleries are comics; "1 new file" for 40 pages said nothing)
      return { text: `${n.pages ? t('push.newPages', { n: n.pages }) : t('push.newFiles', { n: n.new ?? 0 })} ${t('push.fromOne', { what })}`, kind: 'done' };
    case 'done':
      return {
        text: `${n.first ? t('push.subsReady', { n: 1 }) : t('push.downloadsDone', { n: 1 })}: ${n.pages ? t('push.countPages', { query: what, n: n.pages }) : t('push.countNew', { query: what, count: n.new ?? 0 })}`,
        kind: 'done',
      };
    case 'failed':
      return { text: `${n.subscription ? t('push.checkFailed') : t('push.downloadStopped')}: ${what}${n.error ? ' - ' + n.error : ''}`, kind: 'error' };
    case 'paused':
      return { text: t('downloads.pausedNotice', { site: n.site ?? '', error: n.error ?? '' }), kind: 'error' };
    case 'back':
      return { text: t('downloads.backNotice', { site: n.site ?? '' }), kind: 'done' };
    case 'blacklisted':
      return { text: `${t('push.blacklisted', { n: 1 })}: ${t('push.blacklistedBody', { tags: n.tag ?? '' })}`, kind: 'removed' };
    case 'release':
      return { text: t('updates.notice', { version: n.query ?? '' }), kind: 'info' };
  }
}

class Notices {
  /** Everything the server has, not cleared (shown or not: a kind's setting can change). */
  private all = $state<Notice[]>([]);
  /** What the bell lists: the kinds set to go there. */
  list = $derived(this.all.filter((n) => modeOf(n.cat, settings.noticeModes) === 'bell'));
  /** Goes up with each new one (the bell rings). */
  rings = $state(0);
  /** Something to say to screen readers (a notice that doesn't show) - with said going up each time. */
  say = $state('');
  said = $state(0);
  unread = $derived(this.list.filter((n) => !n.read).length);
  /** An error among the unread (the counter turns red). */
  alarm = $derived(this.list.some((n) => !n.read && n.kind === 'error'));
  private boot = '';
  private readUpTo = 0;
  /** Downloader events set to show on screen: shown as they come (lib/toast, given in by App). */
  onScreen: ((text: string, kind: ToastKind, cat: string) => void) | null = null;

  private make(e: ServerNotice): Notice {
    const w = word(e);
    // Where a tap takes you: a release to Settings > Updates; library files that got a blacklisted tag to Tags > Rules,
    // its Delete them button brought into view (the user); the rest to Downloads.
    const to = e.kind === 'app' ? undefined : e.kind === 'release' ? 'settings' : e.kind === 'blacklisted' ? 'tags' : 'downloads';
    const sub = e.kind === 'release' ? 'updates' : e.kind === 'blacklisted' ? 'rules' : undefined;
    const focus = e.kind === 'blacklisted' ? 'blacklist-delete' : undefined;
    return { key: `${this.boot}:${e.id}`, id: e.id, text: w.text, kind: w.kind, cat: e.kind === 'app' ? (e.category ?? '') : e.kind, at: e.at, read: e.id <= this.readUpTo, to, sub, focus };
  }

  /** The list as the server has it (at the start, and whenever the live stream opens again). */
  async catchUp() {
    try {
      const { boot, notices, read } = await api.notices();
      const before = new Set(this.all.map((n) => n.key));
      const first = this.boot === '';
      this.boot = boot;
      this.readUpTo = read ?? 0;
      const all = notices.map((e) => this.make(e)).reverse();
      const fresh = all.filter((n) => !before.has(n.key) && !n.read);
      this.all = all;
      // (the page opening: what's unread shows on the counter - it rings only for ones that came meanwhile)
      if (!first && fresh.some((n) => modeOf(n.cat, settings.noticeModes) === 'bell')) this.rings++;
    } catch {
      /* the next live one */
    }
  }

  /** New ones from the live stream. */
  fromServer(boot: string, events: ServerNotice[]) {
    if (boot !== this.boot && this.boot !== '') return void this.catchUp();
    this.boot = boot;
    const fresh = events.map((e) => this.make(e)).filter((n) => !this.all.some((x) => x.key === n.key));
    if (!fresh.length) return;
    this.all = [...fresh.reverse(), ...this.all].slice(0, 100);
    let ring = false;
    for (const n of fresh) {
      const mode = modeOf(n.cat, settings.noticeModes);
      if (mode === 'bell') ring = true;
      // (a downloader event set to on screen: shown now; the app's own showed where it was done)
      else if (mode === 'screen' && n.to) this.onScreen?.(n.text, n.kind, n.cat);
    }
    if (ring) this.rings++;
  }

  /** Read and cleared changed (on another device, or here). */
  state(read: number, cleared: number) {
    this.readUpTo = Math.max(this.readUpTo, read);
    this.all = this.all.filter((n) => n.id > cleared).map((n) => (n.read || n.id > this.readUpTo ? n : { ...n, read: true }));
  }

  /** Something done here, for the bell (every device's): the server keeps it and sends it back to all. */
  async post(text: string, kind: ToastKind, cat: string) {
    try {
      await api.addNotice(text, kind, cat);
    } catch {
      /* (offline: not in the bell) */
    }
  }

  /** For screen readers only. */
  announce(text: string) {
    this.say = text;
    this.said++;
  }

  markRead() {
    if (!this.unread) return;
    const upTo = Math.max(...this.all.map((n) => n.id));
    this.state(upTo, 0);
    api.markNotices(upTo, false).catch(() => {});
  }

  clear() {
    const upTo = Math.max(0, ...this.all.map((n) => n.id));
    this.all = this.all.filter((n) => n.id > upTo);
    if (upTo) api.markNotices(upTo, true).catch(() => {});
  }
}

export const notices = new Notices();
