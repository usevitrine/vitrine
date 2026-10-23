// Selected files (by ID) in the library grid. Selecting anything turns the
// grid into selection mode: taps toggle, and the action bar works on all of
// them.
import { SvelteSet } from 'svelte/reactivity';

export class Selection {
  ids = new SvelteSet<number>();
  /** Position of the last file picked, the start of a Shift-click range. */
  anchor: number | null = null;
  /** Picking (Browse's Select button): a tap selects even with nothing selected yet. */
  picking = $state(false);

  get count() {
    return this.ids.size;
  }

  get active() {
    return this.ids.size > 0 || this.picking;
  }

  has(id: number | undefined) {
    return id !== undefined && this.ids.has(id);
  }

  toggle(id: number, position: number) {
    if (this.ids.has(id)) this.ids.delete(id);
    else this.ids.add(id);
    this.anchor = position;
  }

  add(ids: Iterable<number>) {
    for (const id of ids) this.ids.add(id);
  }

  clear() {
    this.ids.clear();
    this.anchor = null;
    this.picking = false;
  }

  list(): number[] {
    return [...this.ids];
  }
}

export const selection = new Selection();
