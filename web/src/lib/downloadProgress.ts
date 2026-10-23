// How far a download is (the Downloads page and the top bar's downloads menu): files in the library out of its file
// limit, else posts handled out of the site's total; a gallery site's search by gallery, the one downloading now by its
// pages stored.
import type { DownloadSearch } from './api';
import type { FileProgress } from './live.svelte';

/** The gallery downloading now in a search: its pages stored, out of how many. */
export function galleryNow(s: DownloadSearch, files: FileProgress[]): { done: number; pages: number } | null {
  const f = files.find((x) => x.search === s.id && x.pages);
  return f ? { done: f.done ?? 0, pages: f.pages ?? 0 } : null;
}

/** Whether a search's progress can be measured (a file limit or the site's total). */
export const measurable = (s: DownloadSearch, files: FileProgress[]) => s.max_posts > 0 || s.site_total > 0 || !!galleryNow(s, files);

/** 0 to 1. gallery: a gallery site's search (its posts are whole galleries of many pages). */
export function progress(s: DownloadSearch, files: FileProgress[], gallery: boolean): number {
  // A gallery site: the galleries done, plus how far the one downloading now is.
  const g = gallery ? galleryNow(s, files) : null;
  if (g && g.pages > 0) return Math.min(1, (s.handled + g.done / g.pages) / Math.max(1, s.site_total, s.handled + 1));
  // With a limit: files in the library out of the limit (skipped posts don't count).
  if (s.max_posts > 0 && !(s.site_total > 0 && s.site_total < s.max_posts)) return Math.min(1, (s.downloaded + s.already) / s.max_posts);
  const total = s.site_total;
  if (total <= 0) return s.state === 'done' ? 1 : 0;
  return Math.min(1, s.handled / total);
}
