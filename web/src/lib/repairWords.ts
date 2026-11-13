// A fix's words in this page's language (the server only knows its ID): its name, what it does, how much it has to do
// and what it did. A new fix adds its entry here and its words under "repairs" in locales/. A fix this page doesn't
// know yet (an older page open during an update): its ID, and no explanation until it's reloaded.
import { t } from './i18n.svelte';
import type { Repair } from './api';

type Words = { name: () => string; what: () => string; count: (r: Repair) => string; done: (r: Repair) => string };

const words: Record<string, Words> = {
  video_shapes: {
    name: () => t('repairs.videoShapes.name'),
    what: () => t('repairs.videoShapes.what'),
    count: (r) => t('repairs.videoShapes.count', { n: r.total }),
    done: (r) => t('repairs.videoShapes.done', { n: r.done, changed: r.changed }),
  },
  general_tags: {
    name: () => t('repairs.generalTags.name'),
    what: () => t('repairs.generalTags.what'),
    count: (r) => t('repairs.generalTags.count', { n: r.total }),
    done: (r) => t('repairs.generalTags.done', { n: r.changed }),
  },
  source_links: {
    name: () => t('repairs.sourceLinks.name'),
    what: () => t('repairs.sourceLinks.what'),
    count: (r) => t('repairs.sourceLinks.count', { n: r.total }),
    done: (r) => t('repairs.sourceLinks.done', { n: r.changed }),
  },
};

export function repairName(id: string): string {
  return words[id]?.name() ?? id;
}

export function repairWhat(id: string): string {
  return words[id]?.what() ?? '';
}

/** How much it has to do - or, finished, what it did. */
export function repairDetail(r: Repair): string {
  const w = words[r.id];
  if (!w) return '';
  return r.state === 'done' ? w.done(r) : w.count(r);
}
