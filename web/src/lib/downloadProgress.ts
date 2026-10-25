// How far a download is (the Downloads page and the top bar's downloads menu): the posts gone through (new, had,
// blacklisted, skipped, failed) out of its goal - the limit picked, or the site's count when that's smaller, or the
// site's count without a limit (the user, 2026-10-24); no goal: not measurable (a striped bar). A gallery site's
// search by gallery, the one downloading now by its pages stored.
import type { DownloadSearch } from './api';
import type { FileProgress } from './live.svelte';

/** A download of one gallery (its link): counted by its pages. */
export const oneGallery = (x: { query: string }) => /\/g\/\d+\/[0-9a-f]+/i.test(x.query);

/** The gallery downloading now in a search: its pages stored, out of how many. */
export function galleryNow(s: DownloadSearch, files: FileProgress[]): { done: number; pages: number } | null {
  const f = files.find((x) => x.search === s.id && x.pages);
  return f ? { done: f.done ?? 0, pages: f.pages ?? 0 } : null;
}

/** What a search counts its posts against: the limit picked (the site's count when that's smaller), else the site's
 *  count; 0 = nothing (no limit, and the site doesn't say how many match). The downloader stops at the limit. */
export function goalOf(s: DownloadSearch): number {
  if (s.max_posts > 0) return s.site_total > 0 ? Math.min(s.max_posts, s.site_total) : s.max_posts;
  return Math.max(0, s.site_total);
}

/** Whether a search's progress can be measured (a goal, or a gallery's pages). */
export const measurable = (s: DownloadSearch, files: FileProgress[]) => goalOf(s) > 0 || !!galleryNow(s, files);

/** 0 to 1. gallery: a gallery site's search (its posts are whole galleries of many pages). */
export function progress(s: DownloadSearch, files: FileProgress[], gallery: boolean): number {
  // A gallery site: the galleries done, plus how far the one downloading now is.
  const g = gallery ? galleryNow(s, files) : null;
  const goal = goalOf(s);
  if (g && g.pages > 0) return Math.min(1, (s.handled + g.done / g.pages) / Math.max(1, goal, s.handled + 1));
  if (goal <= 0) return s.state === 'done' ? 1 : 0;
  return Math.min(1, s.handled / goal);
}
