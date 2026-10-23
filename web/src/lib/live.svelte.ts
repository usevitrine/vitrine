import { t, i18n } from './i18n.svelte';
import { notices } from './notices.svelte';
// Live updates from the server (GET /api/events, Server-Sent Events): the
// download queue changed, and the files downloading right now. A dropped
// stream is opened again (the browser gives up by itself after an error answer
// - e.g. the proxy's 502 while the server restarts): soon, then less often
// (at most every 15 s), and at once when the app comes back on screen. While
// it's down, the files' progress is cleared (frozen bars looked like a stalled
// download); once it's back, pages catch up at once.

export interface FileProgress {
  search: number;
  post: number;
  read: number;
  total: number; // -1 if the site didn't say
  pages?: number; // a gallery: its pages
  done?: number; // ... and how many are stored
  site?: string; // a retried failed post (search 0): its site
  key?: string; // ... and "site/post"
}

class Live {
  /** Files downloading now. */
  files = $state<FileProgress[]>([]);
  /** Bytes per second per search (smoothed). */
  speed = $state<Record<number, number>>({});
  /** Whether the stream is open (else pages fall back to polling). */
  connected = $state(false);

  private es: EventSource | null = null;
  private retryTimer = 0;
  private retries = 0;
  private staleTimer = 0;
  private listeners = new Set<() => void>();
  private last: { at: number; read: Record<string, number> } | null = null;

  start() {
    if (this.es || typeof EventSource === 'undefined') return;
    this.open();
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'visible' && this.es?.readyState === EventSource.CLOSED) {
        this.stop();
        this.open();
        this.emit(); // catch up on what happened while away
      }
    });
  }

  stop() {
    clearTimeout(this.retryTimer);
    this.es?.close();
    this.es = null;
    this.connected = false;
  }

  /** Calls fn whenever the download queue changes; returns the unsubscribe. */
  onDownloads(fn: () => void): () => void {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  private open() {
    const es = new EventSource('/api/events');
    this.es = es;
    es.onopen = () => {
      this.connected = true;
      notices.catchUp(); // (one that came just before the stream opened)
      if (this.retries > 0) this.emit(); // back after a drop: catch up
      this.retries = 0;
    };
    es.onerror = () => {
      this.connected = es.readyState === EventSource.OPEN;
      if (this.connected) return;
      // No frozen progress while there's no news: cleared once the stream is given up or has been
      // down a few seconds (a quick reconnect - the browser's own - keeps it).
      clearTimeout(this.staleTimer);
      if (es.readyState === EventSource.CLOSED) this.clearProgress();
      else this.staleTimer = window.setTimeout(() => !this.connected && this.clearProgress(), 3000);
      if (es.readyState === EventSource.CLOSED && this.es === es) {
        // The browser gave up: open it again ourselves, soon and then less often.
        clearTimeout(this.retryTimer);
        const wait = Math.min(15_000, 1000 * 2 ** this.retries);
        this.retries++;
        this.retryTimer = window.setTimeout(() => {
          if (this.es !== es) return;
          this.es = null;
          this.open();
        }, wait);
      } else {
        this.retries++; // (the browser is trying again itself)
      }
    };
    es.addEventListener('downloads', () => this.emit());
    es.addEventListener('notice', (e) => {
      try {
        const { boot, notices: list } = JSON.parse((e as MessageEvent).data);
        notices.fromServer(boot, list);
      } catch {
        /* ignore a bad message */
      }
    });
    es.addEventListener('notices-state', (e) => {
      try {
        const { read, cleared } = JSON.parse((e as MessageEvent).data);
        notices.state(read, cleared);
      } catch {
        /* ignore a bad message */
      }
    });
    es.addEventListener('progress', (e) => {
      try {
        this.setFiles(JSON.parse((e as MessageEvent).data) as FileProgress[]);
      } catch {
        /* ignore a bad message */
      }
    });
  }

  private clearProgress() {
    this.files = [];
    this.speed = {};
    this.last = null;
  }

  private emit() {
    for (const fn of this.listeners) fn();
  }

  private setFiles(files: FileProgress[]) {
    const now = performance.now();
    const read: Record<string, number> = {};
    for (const f of files) read[`${f.search}/${f.post}`] = f.read;
    if (this.last) {
      const dt = (now - this.last.at) / 1000;
      const gained: Record<number, number> = {};
      for (const f of files) {
        const before = this.last.read[`${f.search}/${f.post}`] ?? 0;
        gained[f.search] = (gained[f.search] ?? 0) + Math.max(0, f.read - before);
      }
      const speed: Record<number, number> = {};
      for (const [id, bytes] of Object.entries(gained)) {
        const rate = bytes / Math.max(dt, 0.1);
        const prev = this.speed[+id];
        speed[+id] = prev === undefined ? rate : prev * 0.6 + rate * 0.4;
      }
      this.speed = speed;
    }
    this.last = files.length ? { at: now, read } : null;
    this.files = files;
  }
}

export const live = new Live();

/** 1536 -> "1.5 KB", 12_345_678 -> "11.8 MB" (written the language's way). */
export function bytes(n: number): string {
  if (n < 1024) return t('unit.size.B', { n });
  const units = ['KB', 'MB', 'GB'];
  let v = n / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  const digits = v < 9.95 ? 1 : 0;
  const x = new Intl.NumberFormat(i18n.lang, { minimumFractionDigits: digits, maximumFractionDigits: digits }).format(v);
  return t(`unit.size.${units[i]}`, { x });
}
