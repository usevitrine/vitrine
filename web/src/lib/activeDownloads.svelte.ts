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
      }
    };
  }

  async refresh() {
    if (this.busy) {
      this.again = true; // (one more read after this one: changes come in bursts)
      return;
    }
    this.busy = true;
    try {
      const res = await api.downloads();
      this.sitesPaused = res.sites_paused ?? {};
      this.list = res.searches.filter((s) => s.state in rank).sort((a, b) => rank[a.state] - rank[b.state]);
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
