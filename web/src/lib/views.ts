// View counts (Settings > Viewing, "Count views"; 2026-11-01, the user: "Everything you suggest sounds good"): a
// file in the viewer is a view once it's been on screen 2 s (an image) or played 3 s (a video) - swiping past doesn't
// count. How long it lasted goes with it (a video: the time it played). Sent when it goes out of view (the next file,
// the viewer closing, the app put away - then with keepalive, so it isn't lost); one that can't be sent waits on the
// device for the next. The server adds a view within half an hour of the file's last to that one.
import { api } from './api';
import { results } from './results.svelte';
import { settings } from './settings.svelte';
import { trash } from './trash.svelte';

const IMAGE_MS = 2000;
const VIDEO_MS = 3000;
const KEY = 'vitrine.views'; // views not sent yet

type View = { file: number; at: number; ms: number };
type Shown = { file: number; video: boolean; el: () => HTMLVideoElement | undefined };

function loadQueue(): View[] {
  try {
    const q = JSON.parse(localStorage.getItem(KEY) ?? '[]');
    return Array.isArray(q) ? q : [];
  } catch {
    return [];
  }
}

class Views {
  private cur: (Shown & { at: number; shown: number; played: number }) | null = null;
  private away: Shown | null = null; // the file in view as the app was put away (counted again on coming back)
  private last = 0;
  private timer = 0;
  private queue: View[] = loadQueue();
  private sending = false;

  constructor() {
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'hidden') {
        if (this.cur) {
          const { file, video, el } = this.cur;
          this.away = { file, video, el };
        }
        this.end(true);
      } else if (this.away) {
        const a = this.away;
        this.away = null;
        this.show(a.file, a.video, a.el);
      }
    });
    addEventListener('pagehide', () => this.end(true));
  }

  /** The file now in view (null: none - the viewer closed or hidden). el: its video element, once there is one. */
  show(file: number | null, video = false, el: () => HTMLVideoElement | undefined = () => undefined) {
    if (file !== null && this.cur?.file === file) return;
    this.end();
    this.away = null;
    if (file === null || !settings.views || trash.on) return;
    this.cur = { file, video, el, at: Date.now(), shown: 0, played: 0 };
    this.last = performance.now();
    this.timer = window.setInterval(() => this.tick(), 250);
    if (this.queue.length) this.send();
  }

  private tick() {
    const c = this.cur;
    if (!c) return;
    const now = performance.now();
    const dt = Math.min(now - this.last, 1000); // (a frozen page - a phone asleep - isn't viewing)
    this.last = now;
    if (document.visibilityState !== 'visible') return;
    c.shown += dt;
    const v = c.el();
    if (v && !v.paused && !v.ended && v.readyState >= 3) c.played += dt;
  }

  /** The file goes out of view: a view, if it was long enough. */
  private end(keepalive = false) {
    const c = this.cur;
    if (!c) return;
    this.tick();
    clearInterval(this.timer);
    this.cur = null;
    const ms = c.video ? c.played : c.shown;
    if (ms < (c.video ? VIDEO_MS : IMAGE_MS)) return;
    this.queue.push({ file: c.file, at: c.at, ms: Math.round(ms) });
    this.save();
    this.send(keepalive);
  }

  private save() {
    try {
      localStorage.setItem(KEY, JSON.stringify(this.queue.slice(-500)));
    } catch {
      /* private browsing: they're sent now or not at all */
    }
  }

  private async send(keepalive = false) {
    if (this.sending || !this.queue.length) return;
    this.sending = true;
    const batch = this.queue.splice(0, 200);
    this.save();
    try {
      const res = await api.addViews(batch, keepalive);
      for (const f of res.files) results.patch(f.id, { views: f.views, view_ms: f.view_ms, viewed_at: f.viewed_at });
    } catch {
      this.queue.unshift(...batch); // (offline, signed out: next time)
      this.save();
    } finally {
      this.sending = false;
    }
  }
}

export const views = new Views();
