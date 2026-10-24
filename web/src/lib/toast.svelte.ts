// Notices of what was done here. Each kind goes where Settings > Notifications says (lib/noticeKinds): the bell
// (one bell for every device: it rings, its counter goes up), on screen (at the bottom, a moment) or nowhere. A
// notice with a button (e.g. "Deleted - Undo") shows on screen in any case, as does an error. Screen readers hear
// the ones that don't show. Each says what kind it is with an icon and a color: done (the default), information
// (something started, or nothing happened), removed (something deleted or taken off a list) and errors.
//
// On screen, notices of the same kind (their kind of notice and tone) form a stack like the Downloads page's (the user:
// "the same stack style as on the downloads page" - components/Toasts.svelte): the newest in front, the others peeking
// out behind it; each keeps its own words, time and button. At most three stacks show (a fourth sends the oldest
// away), at most five notices in one.
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
}

const MAX_STACKS = 3;
const MAX_IN_STACK = 5;

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
    const id = this.next++;
    let list = [...this.list, { id, text, kind, action, key }];
    // Too many: the oldest of its stack, then the oldest stack.
    const same = list.filter((x) => x.key === key);
    if (same.length > MAX_IN_STACK) list = list.filter((x) => x.id !== same[0].id);
    const keys = [...new Set(list.map((x) => x.key))];
    while (keys.length > MAX_STACKS) {
      const old = keys.shift();
      list = list.filter((x) => x.key !== old);
    }
    for (const x of this.list) if (!list.includes(x)) this.clear(x.id);
    this.list = list;
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
