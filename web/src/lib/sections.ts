// The app's sections (2026-10-23): Media (pictures and videos) and Comics. Either can be switched off in Settings >
// Viewing > Content, never both (the first steps ask which to use). Switching one off removes its subscriptions and
// downloads (the user's choice) and offers to delete everything in it for good (a word to type: no accidents); its
// menu entries and pages go (App), and its sites aren't offered for new downloads.
import { api } from './api';
import { settings } from './settings.svelte';
import { confirmAction } from './confirm.svelte';
import { toasts } from './toast.svelte';
import { formatSize } from './results.svelte';
import { sites } from './sites.svelte';
import { t } from './i18n.svelte';

export type Section = 'media' | 'comics';

/** Whether a site's downloads are for a section that's on (gallery sites: Comics; the others: Media). */
export const siteOn = (id: string) => (sites.engines[id] === 'ehentai' ? !settings.comicsOff : !settings.mediaOff);

/** Switches a section off - asked first, then whether to delete everything in it. */
export async function switchOff(section: Section) {
  let info: Awaited<ReturnType<typeof api.sections>>[Section] | null = null;
  try {
    info = (await api.sections())[section];
  } catch (e) {
    toasts.error((e as Error).message);
    return;
  }
  const name = t(section === 'media' ? 'menu.library' : 'comics.title');
  const ok = await confirmAction({
    title: t('appSections.offTitle', { name }),
    body: t('appSections.offBody', { subs: info.subscriptions, downloads: info.downloads }),
    action: t('appSections.off'),
    icon: section === 'media' ? 'library' : 'book',
  });
  if (!ok) return;
  // Everything in it, for good? (Cancel: it's all kept, hidden until switched on again.)
  const del =
    info.items > 0 &&
    (await confirmAction({
      title: t('appSections.deleteTitle', { name }),
      body: t(section === 'media' ? 'appSections.deleteMediaBody' : 'appSections.deleteComicsBody', { n: info.items, size: formatSize(info.bytes) }),
      action: t('appSections.deleteAll'),
      typed: t('appSections.deleteWord'),
    }));
  try {
    const res = await api.sectionOff(section, !!del);
    if (section === 'media') settings.mediaOff = true;
    else settings.comicsOff = true;
    toasts.show(del ? t('appSections.offDeleting', { name }) : t('appSections.offDone', { name }), { cat: 'deleted', kind: 'info' });
    void res;
  } catch (e) {
    toasts.error((e as Error).message);
  }
}
