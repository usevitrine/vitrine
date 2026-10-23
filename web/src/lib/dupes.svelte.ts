// Duplicates to review: how alike two files must be (this device's choice)
// and how many pairs wait, for the menu's count.
import { api } from './api';
import { t } from './i18n.svelte';

const level = (value: number, key: string) => ({
  value,
  get label() {
    return t(`dupes.level.${key}.label`);
  },
  get meta() {
    return t(`dupes.level.${key}.meta`);
  },
});
export const levels = [level(0, 'same'), level(2, 'nearly'), level(6, 'very'), level(10, 'similar')];

function loadLevel(): number {
  try {
    const raw = localStorage.getItem('vitrine.dupeLevel');
    const v = Number(raw);
    return raw !== null && levels.some((l) => l.value === v) ? v : 0; // (no choice yet: Same picture)
  } catch {
    return 0;
  }
}

class Dupes {
  level = $state(loadLevel());
  /** Pairs to review at this level (null = not asked yet). */
  count = $state<number | null>(null);

  setLevel(v: number) {
    this.level = v;
    try {
      localStorage.setItem('vitrine.dupeLevel', String(v));
    } catch {
      /* this session only */
    }
  }

  async refreshCount() {
    try {
      this.count = (await api.dupes(this.level, 1)).total;
    } catch {
      /* the menu just shows no count */
    }
  }
}

export const dupes = new Dupes();
