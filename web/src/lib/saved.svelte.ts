// Saved searches (favorites), kept on the server so every device shares them.
import { api, type SavedSearch, type News, type Scope, type RecentSearch } from './api';
import { settings } from './settings.svelte';
import { format, parse } from './query';

/** A search written the same way however it was typed (for "is this saved?"). */
function normalize(q: string): string {
  return format(parse(q)).toLowerCase();
}

export class Saved {
  /** Whose list: '' the library's, 'comics' the Comics page's (its own). */
  readonly scope: Scope;
  constructor(scope: Scope = '') {
    this.scope = scope;
  }
  list = $state<SavedSearch[]>([]);
  loaded = $state(false);
  /** The saved search last run (or matched): a changed search offers to update it. */
  lastRun = $state<number | null>(null);
  /** New files per saved search (by ID) and the group (parent) tags with theirs, since each was last opened. */
  newOf = $state<Record<number, number>>({});
  groups = $state<News['groups']>([]);
  /** Anything new in a saved search, in a group (each tab's dot), in any of them (the save button's dot). */
  get savedNew() {
    return Object.values(this.newOf).some((n) => n > 0);
  }
  get groupsNew() {
    return this.groups.some((g) => g.new > 0);
  }
  get anyNew() {
    return this.savedNew || this.groupsNew;
  }

  async refreshNews() {
    if (this.scope) return; // (NEW pills: the library's)
    try {
      const n = await api.news(settings.newFromBulk !== false);
      this.newOf = Object.fromEntries(n.saved.map((x) => [x.id, x.new]));
      this.groups = n.groups;
    } catch {
      // Offline: the pills stay as they were.
    }
  }

  async load() {
    try {
      this.list = (await api.saved(this.scope)).saved;
      this.loaded = true;
    } catch {
      // Offline or logged out: the list stays as it was.
    }
  }

  /** The saved search matching this search and order, if any. */
  find(query: string, sort: string, defaultSort: string): SavedSearch | undefined {
    const q = normalize(query);
    return this.list.find((s) => normalize(s.query) === q && (s.sort || defaultSort) === sort);
  }

  async add(name: string, query: string, sort: string): Promise<number> {
    const res = await api.addSaved(name, query, sort, this.scope);
    this.list = res.saved;
    return res.id;
  }

  async update(id: number, change: { name?: string; query?: string; sort?: string }) {
    this.list = (await api.updateSaved(id, change, this.scope)).saved;
  }

  async remove(id: number) {
    this.list = this.list.filter((s) => s.id !== id); // at once; the server confirms
    this.list = (await api.removeSaved(id, this.scope)).saved;
  }

  // ---- Recent searches (the search bar's drop-down): the page's last searches, newest first, kept on the server.
  recent = $state<RecentSearch[]>([]);
  recentLoaded = false;

  async loadRecent() {
    try {
      this.recent = (await api.recent(this.scope)).recent;
      this.recentLoaded = true;
    } catch {
      // Offline: the list stays as it was.
    }
  }

  /** A search run (and stayed on): to the top of the list - at once here, the server confirms. */
  async addRecent(query: string, sort: string) {
    const q = normalize(query);
    if (!q) return;
    const rest = this.recent.filter((r) => !(normalize(r.query) === q && r.sort === sort));
    this.recent = [{ query: format(parse(query)), sort, used_at: Date.now() }, ...rest].slice(0, 20);
    try {
      this.recent = (await api.addRecent(format(parse(query)), sort, this.scope)).recent;
    } catch {
      /* offline: kept here until the next load */
    }
  }

  async removeRecent(r: RecentSearch) {
    this.recent = this.recent.filter((x) => x !== r);
    try {
      this.recent = (await api.removeRecent(r.query, r.sort, this.scope)).recent;
    } catch {
      /* offline */
    }
  }

  async clearRecent() {
    this.recent = [];
    try {
      this.recent = (await api.clearRecent(this.scope)).recent;
    } catch {
      /* offline */
    }
  }

  async reorder(ids: number[]) {
    const pos = new Map(ids.map((id, i) => [id, i]));
    this.list = [...this.list].sort((a, b) => (pos.get(a.id) ?? 1e9) - (pos.get(b.id) ?? 1e9));
    this.list = (await api.reorderSaved(ids, this.scope)).saved;
  }
}

export const saved = new Saved();
