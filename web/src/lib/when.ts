// How the details panels and cards say when something happened, and a link's site.
import { t, date, time, dateTime } from './i18n.svelte';

/** "15 Oct", or "15 Oct 2025" for another year. */
export function shortDate(ms: number): string {
  const d = new Date(ms);
  const year = d.getFullYear() === new Date().getFullYear() ? undefined : ('numeric' as const);
  return date(d, { day: 'numeric', month: 'short', year });
}

/** "Today at 22:14", "Yesterday at 08:10", "3 Oct at 09:12", "3 Oct 2025 at 09:12". */
export function when(ms: number): string {
  const d = new Date(ms);
  const at = time(d);
  const day = new Date(d).setHours(0, 0, 0, 0);
  const today = new Date().setHours(0, 0, 0, 0);
  if (day === today) return t('when.todayAt', { time: at });
  if (day === today - 86_400_000) return t('when.yesterdayAt', { time: at });
  return t('when.dayAt', { date: shortDate(ms), time: at });
}

/** The full date and time (a hover text): "Thursday, 23 October 2026 at 07:21". */
export const fullDate = (ms: number) => dateTime(ms, { dateStyle: 'full', timeStyle: 'short' });

/** A link's site: its host without "www.". */
export function host(u: string): string {
  try {
    return new URL(u).hostname.replace(/^www\./, '');
  } catch {
    return u;
  }
}
