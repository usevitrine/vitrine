// A tag's parent tags, picked in a menu (the details' tag menu, a search chip's menu): every parent there is, which of
// them have this tag as a child, and putting it in or taking it out (GET/POST /api/relations, as Tags > Rules).
import { api, type Relation } from './api';
import { toasts } from './toast.svelte';

export class TagParents {
  relations = $state<Relation[] | null>(null);
  busy = $state(false);
  private tag: () => string;
  // (one this tag was the only child of stays in the list once taken out - a parent is only its children - so a
  // second tap puts it back)
  private shown = new Set<string>();

  constructor(tag: () => string) {
    this.tag = tag;
  }

  /** Asked once the menu opens (for the parent's name on its item). */
  load() {
    api.relations().then(
      (r) => (this.relations = r.parents),
      () => (this.relations = []),
    );
  }

  get all(): string[] {
    for (const r of this.relations ?? []) this.shown.add(r.to);
    return [...this.shown].sort((a, b) => a.localeCompare(b));
  }

  get mine(): Set<string> {
    const tag = this.tag();
    return new Set((this.relations ?? []).filter((r) => r.from === tag).map((r) => r.to));
  }

  async set(parent: string, on: boolean): Promise<boolean> {
    const p = parent.trim();
    const tag = this.tag();
    if (!p || !tag || p === tag || this.busy) return false;
    this.busy = true;
    try {
      this.relations = (await api.editRelation('parent', on ? 'add' : 'remove', tag, p)).parents;
      return true;
    } catch (err) {
      toasts.error((err as Error).message);
      return false;
    } finally {
      this.busy = false;
    }
  }
}
