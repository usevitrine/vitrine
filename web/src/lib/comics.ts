// Small helpers for comics (the Comics page, its grid tiles and selection).
import { api, type Comic } from './api';
import { t } from './i18n.svelte';
import { toasts } from './toast.svelte';

// A comic has a read state only once it was left past its cover (stopped on the cover: as if never
// opened) - except a comic of one page, read there.

/** Read to the last page. */
export const isRead = (c: Comic | undefined) =>
  !!c?.read_at && c.pages > 0 && c.read_page + 1 >= c.pages && (c.read_page > 0 || c.pages === 1);

/** Being read: left past the cover, not finished (it comes first in the list). */
export const isReading = (c: Comic) => !!c.read_at && c.read_page > 0 && !isRead(c);

/** How far it's read, 0..1 (0 = never opened). */
export const progress = (c: Comic) => (c.read_at && c.read_page > 0 && c.pages > 0 ? Math.min(1, (c.read_page + 1) / c.pages) : 0);

/** "24 pages", or "12 of 24 pages" while some are still to come. */
export const pagesText = (c: Comic) => (c.have < c.pages ? t('comics.havePages', { have: c.have, n: c.pages }) : t('comics.pages', { n: c.pages }));

/** A cropped cover in a box of the crop's shape: the picture placed so only the crop shows. */
export function cropStyle(crop: number[] | undefined): string | undefined {
  if (!crop || crop.length !== 4) return undefined;
  const [x, y, w, h] = crop;
  return `position:absolute;max-width:none;width:${100 / w}%;height:${100 / h}%;left:${(-x / w) * 100}%;top:${(-y / h) * 100}%;object-fit:fill`;
}

/** Marks comics read (to the last page) or unread (never opened), with a notice; the list again after. */
export async function markComicsRead(ids: number[], read: boolean, results: { forget: (id: number) => void; refresh: () => Promise<unknown> }) {
  try {
    await api.comicsRead(ids, read);
    toasts.show(t(read ? 'comics.markedRead' : 'comics.markedUnread', { n: ids.length }), { cat: 'comicRead', kind: 'done' });
    ids.forEach((id) => results.forget(id));
    await results.refresh();
  } catch (e) {
    toasts.error((e as Error).message);
  }
}
