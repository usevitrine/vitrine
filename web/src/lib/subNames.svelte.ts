// The subscriptions' names (their label or search), for a system:found_by=<id> chip's words ("found by blue_sky").
// The Downloads page keeps them current; elsewhere they're asked for once, when such a chip shows.
import { api, type Subscription } from './api';

class SubNames {
  names = $state<Record<number, string>>({});
  private asked = false;

  set(subs: Subscription[]) {
    this.names = Object.fromEntries(subs.map((s) => [s.id, s.label || s.query]));
    this.asked = true;
  }

  /** Its name, asking for all of them the first time one isn't known ('' until then). */
  of(id: number): string {
    const n = this.names[id];
    if (n === undefined && !this.asked) {
      this.asked = true;
      api.downloads().then((res) => this.set(res.subscriptions)).catch(() => {});
    }
    return n ?? '';
  }
}

export const subNames = new SubNames();
