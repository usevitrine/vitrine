// Fixes an update brings for what's already in the library (library/repairs.go): offered with what they do, to run or
// skip; followed in the top bar until the user has acknowledged that one finished. The same on every device (the live
// stream's "repairs"; read again whenever it opens).
import { api, type Repair } from './api';

class Repairs {
  list = $state<Repair[]>([]);
  /** How far the running ones are, 0..1 (none running: null). */
  progress = $derived.by(() => {
    const on = this.list.filter((r) => r.state === 'running');
    if (!on.length) return null;
    const total = on.reduce((n, r) => n + r.total, 0);
    return total ? on.reduce((n, r) => n + r.done, 0) / total : 0;
  });

  async load() {
    try {
      this.list = (await api.repairs()).repairs;
    } catch {
      /* the next live one */
    }
  }

  /** Run, skip or acknowledge (the answer is the list as it is now; a refused one: another device got there first). */
  async act(id: string, action: 'run' | 'skip' | 'ack') {
    try {
      this.list = (await api.repairAction(id, action)).repairs;
    } catch {
      await this.load();
    }
  }
}

export const repairs = new Repairs();
