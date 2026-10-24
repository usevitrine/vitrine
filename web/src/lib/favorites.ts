// Favorites: a heart on a file (the viewer, the file menu, the selection bar).
// The change shows at once (the cached details are patched) and the search
// runs again if it depends on favorites (system:favorite, sort:favorite).
import { results as libraryResults, type Results } from './results.svelte';
import { toasts } from './toast.svelte';
import { itemName, t } from './i18n.svelte';

const dependsOnFavorites = (results: Results) => results.scope === 'favorites' || /system:(favou?rite|fav)/i.test(results.query) || results.sort.startsWith('favorite');

/** Resolves once the list has followed (a search that depends on favorites runs again). */
export async function setFavorite(ids: number[], on: boolean, quiet = false, results: Results = libraryResults) {
  if (!ids.length) return;
  for (const id of ids) results.patch(id, { favorite: on });
  try {
    await results.favorite(ids, on);
  } catch (e) {
    for (const id of ids) results.patch(id, { favorite: !on });
    toasts.error((e as Error).message);
    return;
  }
  // Taken off a list of favorites: it leaves the list, so the notice offers Undo (even from the viewer's heart).
  const leaves = !on && dependsOnFavorites(results);
  if (!quiet || ids.length > 1 || leaves) {
    const n = ids.length;
    const text = n === 1 ? t(on ? 'favorites.added' : 'favorites.removed', { what: itemName(ids[0]) }) : t(on ? 'favorites.addedN' : 'favorites.removedN', { n });
    toasts.show(text, { cat: 'favorites',
      kind: on ? 'done' : 'removed',
      action: leaves ? { label: t('common.undo'), run: () => setFavorite(ids, true, true, results) } : undefined,
    });
  }
  if (dependsOnFavorites(results)) await results.refresh();
}
