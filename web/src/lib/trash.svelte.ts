// The trash (Menu > Trash: Media at /trash, Comics at /comics/trash): deleted files and comics stay restorable until
// removed for good - chosen ones, a page's everything (Empty trash, a word to type first), or after the days set in
// Settings > Library. Each has its own search index on the server (scope trash, comics_trash); the library's and the
// Comics page's grid, search bar and viewer show them, their Delete becoming Restore and Delete forever (lib/results'
// scope 'trash').
import { api } from './api';
import { router } from './router.svelte';
import { confirmAction } from './confirm.svelte';
import { toasts } from './toast.svelte';
import { formatSize } from './results.svelte';
import { itemName, t } from './i18n.svelte';

class Trash {
  /** A trash page is showing (its words: "Deleted" for the modified order). */
  get on() {
    return router.sub === 'trash' && (router.screen === 'library' || router.screen === 'comics');
  }
  files = $state(0);
  bytes = $state(0);
  comics = $state(0);
  comicBytes = $state(0);
  days = $state(0);
  /** Empty trash is removing everything (its button spins meanwhile: a big trash takes a while). */
  emptying = $state(false);
  async load() {
    try {
      const r = await api.trashInfo();
      this.files = r.files;
      this.bytes = r.bytes;
      this.comics = r.comics;
      this.comicBytes = r.comic_bytes;
      this.days = r.days;
    } catch {
      /* shown without the totals */
    }
  }
  get size() {
    return formatSize(this.bytes);
  }
  get comicSize() {
    return formatSize(this.comicBytes);
  }
}
export const trash = new Trash();

interface Refreshable {
  refresh(): Promise<unknown>;
  readonly index: string; // 'comics': the comics trash
  indexOf?(id: number): number; // (a file's place in the list: removed in that order)
}

/** How many files go in one turn of a removal for good: the list refreshed after each, so they leave as they go
 *  (2026-10-28, the user: they all stayed until every one was gone). */
const TURN = 50;
const ofComics = (r: Refreshable) => r.index === 'comics';

/** Files (or comics) back from the trash. */
export async function restoreFiles(results: Refreshable, ids: number[]) {
  try {
    await (ofComics(results) ? api.undeleteComics(ids) : api.restore(ids));
  } catch (e) {
    toasts.error((e as Error).message);
    return false;
  }
  const restoredWords = ids.length === 1 ? t('trash.restoredOne', { what: itemName(ids[0], ofComics(results)) }) : t(ofComics(results) ? 'trash.restoredComics' : 'trash.restored', { n: ids.length });
  toasts.show(restoredWords, { cat: 'deleted', kind: 'done' });
  await Promise.all([results.refresh(), trash.load()]);
  return true;
}

/** Files (or comics) removed for good (asked first: it can't be undone). */
export async function purgeFiles(results: Refreshable, ids: number[], thumbs: string[] = []) {
  const comics = ofComics(results);
  const ok = await confirmAction({
    title: t(comics ? 'trash.purgeComicsTitle' : 'trash.purgeTitle', { n: ids.length }),
    body: t('trash.purgeBody'),
    action: t('trash.purge'),
    thumbs,
  });
  if (!ok) return false;
  // In turns, in the list's order (from the top): each turn's files leave the grid before the next goes.
  const order = (id: number) => results.indexOf?.(id) ?? 0;
  const left = [...ids].sort((a, b) => (order(a) < 0 ? 1e9 : order(a)) - (order(b) < 0 ? 1e9 : order(b)));
  try {
    while (left.length) {
      const turn = left.splice(0, TURN);
      await (comics ? api.purgeComicsTrash(turn) : api.purgeTrash(turn));
      if (left.length) await Promise.all([results.refresh(), trash.load()]);
    }
  } catch (e) {
    toasts.error((e as Error).message);
    await Promise.all([results.refresh(), trash.load()]);
    return false;
  }
  toasts.show(ids.length === 1 ? t('trash.purgedOne', { what: itemName(ids[0], comics) }) : t(comics ? 'trash.purgedComics' : 'trash.purged', { n: ids.length }), { cat: 'deleted', kind: 'removed' });
  await Promise.all([results.refresh(), trash.load()]);
  return true;
}

/** Everything in a trash (the files', or the comics') removed for good: a word to type first. */
export async function emptyTrash(results: Refreshable) {
  await trash.load();
  const comics = ofComics(results);
  const ok = await confirmAction({
    title: t(comics ? 'trash.emptyComicsTitle' : 'trash.emptyTitle'),
    body: comics ? t('trash.emptyComicsBody', { n: trash.comics, size: trash.comicSize }) : t('trash.emptyBody', { n: trash.files, size: trash.size }),
    action: t('trash.empty'),
    typed: t('trash.emptyWord'),
  });
  if (!ok) return;
  trash.emptying = true;
  try {
    let purged = 0;
    if (comics) purged = (await api.emptyComicsTrash()).purged;
    else {
      // In turns, the last deleted first (the grid's top): the files leave as they go, the counts going down.
      for (;;) {
        const res = await api.emptyTrash(TURN);
        purged += res.purged;
        if (!res.left || !res.purged) break;
        await Promise.all([results.refresh(), trash.load()]);
      }
    }
    toasts.show(t(comics ? 'trash.purgedComics' : 'trash.purged', { n: purged }), { cat: 'deleted', kind: 'removed' });
  } catch (e) {
    toasts.error((e as Error).message);
  }
  try {
    await Promise.all([results.refresh(), trash.load()]);
  } finally {
    trash.emptying = false;
  }
}
