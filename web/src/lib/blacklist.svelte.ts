// The download blacklist (Tags > Tag rules), shared: the details mark the tags on it and add or take them off; the
// Tag rules card edits it - each sees the other's changes at once. Read once, when first needed.
import { api } from './api';

/** A blacklist entry ("ai_*": * matches anything) against a tag, ignoring case - as the server matches posts. */
function matches(pattern: string, tag: string): boolean {
  const re = new RegExp('^' + pattern.toLowerCase().replace(/[.+?^${}()|[\]\\]/g, '\\$&').replace(/\*/g, '.*') + '$');
  return re.test(tag.toLowerCase());
}

class Blacklist {
  /** The entries (null until read). */
  tags = $state<string[] | null>(null);
  private loading: Promise<void> | null = null;

  /** Reads it (once; again with force). */
  load(force = false): Promise<void> {
    if (this.loading && !force) return this.loading;
    this.loading = api
      .blacklist()
      .then((r) => {
        this.tags = r.tags;
      })
      .catch(() => {
        this.loading = null;
      });
    return this.loading;
  }

  /** Saves a new list (the server normalizes it); returns it. */
  async set(next: string[]): Promise<string[]> {
    this.tags = (await api.setBlacklist(next)).tags;
    return this.tags;
  }

  /** The entry a library tag is blacklisted by ("" if none): the whole tag, or its name without the namespace (sites
   *  send bare names; the library may file them under one). */
  entryFor(tag: string): string {
    if (!this.tags?.length) return '';
    const bare = tag.includes(':') ? tag.slice(tag.indexOf(':') + 1) : tag;
    return this.tags.find((p) => matches(p, tag) || matches(p, bare)) ?? '';
  }
}

export const blacklist = new Blacklist();
