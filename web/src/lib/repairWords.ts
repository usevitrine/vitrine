// A fix's name and what it does, in this page's language (the server only knows its ID). A fix this page doesn't know
// yet (an older page open during an update): its ID, and no explanation until it's reloaded.
import { t } from './i18n.svelte';

export function repairName(id: string): string {
  if (id === 'video_shapes') return t('repairs.videoShapes.name');
  if (id === 'general_tags') return t('repairs.generalTags.name');
  return id;
}

export function repairWhat(id: string): string {
  if (id === 'video_shapes') return t('repairs.videoShapes.what');
  if (id === 'general_tags') return t('repairs.generalTags.what');
  return '';
}
