// Notices of what was done here. Each kind goes where Settings > Notifications says (lib/noticeKinds): the bell
// (one bell for every device: it rings, its counter goes up), on screen (at the bottom, a moment) or nowhere. A
// notice with a button (e.g. "Deleted - Undo") shows on screen in any case, as does an error. Screen readers hear
// the ones that don't show. Each says what kind it is with an icon and a color: done (the default), information
// (something started, or nothing happened), removed (something deleted or taken off a list) and errors.
//
// On screen, one of the same kind as one showing (its kind of notice and tone) takes that one's place instead of
// stacking up (the user: "notifications of the same type dont stack"): the newest words, the time starting again, and
// how many it stands for. One with a button merges with one with the same button, which then acts for the newest (Undo
// undoes the last action, as one expects - a deleted file before it is still in the trash). At most three show; a
// fourth sends the oldest away.
import { notices } from './notices.svelte';
import { modeOf } from './noticeKinds';
import { settings } from './settings.svelte';

export type ToastKind = 'done' | 'info' | 'removed' | 'error';

export interface Toast {
  id: number;
  text: string;
  kind: ToastKind;
  action?: { label: string; run: () => void };
  /** What makes two of the same kind (its kind of notice and tone). */
  key: string;
  /** How many it stands for (merged ones). */
  count: number;
}

const MAX = 3;

class Toasts {
  list = $state<Toast[]>([]);
  private next = 1;
  private timers = new Map<number, number>();

  /** cat: the kind of notice (lib/noticeKinds) - where it goes. */
  show(text: string, opts: { kind?: ToastKind; action?: Toast['action']; ms?: number; cat?: string } = {}) {
    const kind = opts.kind ?? 'done';
    const cat = opts.cat ?? (kind === 'error' ? 'errors' : '');
    const mode = modeOf(cat, settings.noticeModes);
    if (mode === 'bell') notices.post(text, kind, cat);
    const pops = kind === 'error' || !!opts.action || mode === 'screen';
    if (!pops) {
      notices.announce(text); // (screen readers)
      return;
    }
    this.put(text, kind, cat, opts.action, opts.ms ?? (opts.action ? 8000 : kind === 'error' ? 6000 : 3000));
  }

  error(text: string) {
    this.show(text, { kind: 'error', ms: 6000, cat: 'errors' });
  }

  /** On screen only (a downloader event set to show there: notices). */
  popup(text: string, kind: ToastKind, cat = '') {
    this.put(text, kind, cat, undefined, kind === 'error' ? 6000 : 4000);
  }

  private put(text: string, kind: ToastKind, cat: string, action: Toast['action'] | undefined, ms: number) {
    // (a kind of notice not named: its words are its kind)
    const key = `${cat || text}|${kind}`;
    const same = this.list.find((x) => x.key === key && x.action?.label === action?.label);
    let id: number;
    if (same) {
      id = same.id;
      this.list = this.list.map((x) => (x.id === id ? { ...x, text, count: x.count + 1, action } : x));
    } else {
      id = this.next++;
      const t: Toast = { id, text, kind, action, key, count: 1 };
      const kept = [...this.list, t];
      for (const old of kept.slice(0, Math.max(0, kept.length - MAX))) this.clear(old.id);
      this.list = kept.slice(-MAX);
    }
    this.clear(id);
    this.timers.set(id, window.setTimeout(() => this.dismiss(id), ms));
  }

  private clear(id: number) {
    clearTimeout(this.timers.get(id));
    this.timers.delete(id);
  }

  dismiss(id: number) {
    this.clear(id);
    this.list = this.list.filter((t) => t.id !== id);
  }
}

export const toasts = new Toasts();
notices.onScreen = (text, kind, cat) => toasts.popup(text, kind, cat);
