// The downloads going on now (running, waiting, paused), for the top bar's downloads menu and its counter. Read while
// something shows them (use()), again whenever the live stream says the queue changed; polled only without the stream.
import { api, type DownloadSearch, type SitePause } from './api';
import { live } from './live.svelte';

const rank = { running: 0, queued: 1, paused: 2 } as Record<string, number>;

class ActiveDownloads {
  /** Running first, then waiting, then paused (each in queue order). */
  list = $state<DownloadSearch[]>([]);
  /** Sites paused by themselves: their running downloads wait. */
  sitesPaused = $state<Record<string, SitePause>>({});
  /** Running or waiting (not paused): the counter. */
  get count() {
    return this.list.filter((s) => s.state !== 'paused').length;
  }

  private users = 0;
  private stop: (() => void) | null = null;
  private busy = false;
  private again = false;
  private last = 0;
  private later = 0;
  // At most once a second: while downloads run the queue changes all the time, and each read is work for the page
  // (scrolling the library stalled - the user saw the background at the bottom). A change meanwhile is read once
  // the second is up.
  private static GAP = 1000;

  /** Keeps the list current while it's in use; returns the release. */
  use(): () => void {
    if (this.users++ === 0) {
      this.refresh();
      const off = live.onDownloads(() => this.refresh());
      const timer = setInterval(() => !live.connected && this.refresh(), 5000);
      this.stop = () => {
        off();
        clearInterval(timer);
      };
    }
    return () => {
      if (--this.users === 0) {
        this.stop?.();
        this.stop = null;
        clearTimeout(this.later);
        this.later = 0;
      }
    };
  }

  async refresh() {
    const wait = this.last + ActiveDownloads.GAP - performance.now();
    if (wait > 0) {
      if (!this.later) this.later = window.setTimeout(() => ((this.later = 0), this.refresh()), wait);
      return;
    }
    if (this.busy) {
      this.again = true; // (one more read after this one: changes come in bursts)
      return;
    }
    this.busy = true;
    this.last = performance.now();
    try {
      const res = await api.activeDownloads();
      this.sitesPaused = res.sites_paused ?? {};
      this.list = res.searches.filter((s) => s.state in rank).sort((a, b) => rank[a.state] - rank[b.state]); // (only those come)
    } catch {
      /* kept as it was: the next change or poll tries again */
    } finally {
      this.busy = false;
      if (this.again) {
        this.again = false;
        this.refresh();
      }
    }
  }
}

export const activeDownloads = new ActiveDownloads();
