// How often a subscription is checked: the choices offered and their words (the Downloads page, the New download
// dialog).
import { t } from './i18n.svelte';

/** The intervals offered (s). */
export const intervals = [3600, 6 * 3600, 12 * 3600, 86400, 3 * 86400, 7 * 86400];
/** A new subscription's, unless its group has one. */
export const DEFAULT_INTERVAL = 12 * 3600;

export const cap = (s: string) => s[0].toUpperCase() + s.slice(1);

/** "every 12 hours", "every day", ... */
export function every(sec: number): string {
  const d = sec / 86400;
  if (Number.isInteger(d)) return d === 7 ? t('downloads.every.week') : t('downloads.every.days', { n: d });
  return t('downloads.every.hours', { n: +(sec / 3600).toFixed(1) });
}
