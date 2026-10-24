import type { ComponentProps } from 'svelte';
import type Icon from '../components/Icon.svelte';
import { t } from './i18n.svelte';

type IconName = ComponentProps<typeof Icon>['name'];

/** A section whose name and hint follow the app's language (texts under prefix.id). */
// i18n-keys: settings.section.* tags.section.*
function section<I extends string>(prefix: string, id: I, icon: IconName, color: string) {
  return {
    id,
    icon,
    color,
    get label() {
      return t(`${prefix}.${id}.label`);
    },
    get hint() {
      return t(`${prefix}.${id}.hint`);
    },
  };
}

// The sections of Settings, in order (also listed under Settings in the
// navigation menu). Each has its own address: /settings/<id>.
export const settingsSections = [
  section('settings.section', 'library', 'library', 'mauve'),
  section('settings.section', 'sites', 'globe', 'blue'),
  section('settings.section', 'rules', 'download', 'green'),
  section('settings.section', 'notifications', 'bell', 'peach'),
  section('settings.section', 'viewing', 'play', 'flamingo'),
  section('settings.section', 'device', 'device', 'teal'),
  section('settings.section', 'backups', 'archive', 'yellow'),
  section('settings.section', 'stats', 'chart', 'sky'),
  section('settings.section', 'account', 'user', 'lavender'),
  section('settings.section', 'updates', 'star', 'pink'),
] as const;

// The sections of Tags: the rules first (where Tags opens).
export const tagSections = [section('tags.section', 'rules', 'link', 'mauve'), section('tags.section', 'all', 'tag', 'blue')] as const;
