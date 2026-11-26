// Kept despite the blacklist (2026-11-17): a file with a blacklisted tag the blacklist's clean-up (Delete them) and its
// notice leave alone; system:kept finds them. Set in the details (under the tags) and from the selection bar on the
// blacklist's Show them pages.
import { api } from './api';
import { results as libraryResults, type Results } from './results.svelte';
import { toasts } from './toast.svelte';
import { itemName, t } from './i18n.svelte';

const dependsOnKept = (results: Results) => /system:kept/i.test(results.query);

export async function setKept(ids: number[], on: boolean, results: Results = libraryResults, undo = true) {
  if (!ids.length) return;
  for (const id of ids) results.patch(id, { kept: on });
  try {
    await api.keepFiles(ids, on);
  } catch (e) {
    for (const id of ids) results.patch(id, { kept: !on });
    toasts.error((e as Error).message);
    return;
  }
  const n = ids.length;
  const text = n === 1 ? t(on ? 'kept.added' : 'kept.removed', { what: itemName(ids[0]) }) : t(on ? 'kept.addedN' : 'kept.removedN', { n });
  toasts.show(text, {
    cat: 'tags',
    kind: on ? 'done' : 'removed',
    action: undo ? { label: t('common.undo'), run: () => setKept(ids, !on, results, false) } : undefined,
  });
  if (dependsOnKept(results)) await results.refresh(); // (the blacklist's Show them pages: it leaves them)
}
