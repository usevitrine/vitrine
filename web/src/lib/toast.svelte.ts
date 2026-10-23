// Notices of what was done here. Each kind goes where Settings > Notifications says (lib/noticeKinds): the bell
// (one bell for every device: it rings, its counter goes up), on screen (at the bottom, a moment) or nowhere. A
// notice with a button (e.g. "Deleted - Undo") shows on screen in any case, as does an error. Screen readers hear
// the ones that don't show. Each says what kind it is with an icon and a color: done (the default), information
// (something started, or nothing happened), removed (something deleted or taken off a list) and errors.
import { notices } from './notices.svelte';
import { modeOf } from './noticeKinds';
import { settings } from './settings.svelte';

export type ToastKind = 'done' | 'info' | 'removed' | 'error';

export interface Toast {
  id: number;
  text: string;
  kind: ToastKind;
  action?: { label: string; run: () => void };
}

class Toasts {
  list = $state<Toast[]>([]);
  private next = 1;

  /** cat: the kind of notice (lib/noticeKinds) - where it goes. */
  show(text: string, opts: { kind?: ToastKind; action?: Toast['action']; ms?: number; cat?: string } = {}) {
    const t: Toast = { id: this.next++, text, kind: opts.kind ?? 'done', action: opts.action };
    const cat = opts.cat ?? (t.kind === 'error' ? 'errors' : '');
    const mode = modeOf(cat, settings.noticeModes);
    if (mode === 'bell') notices.post(text, t.kind, cat);
    const pops = t.kind === 'error' || !!t.action || mode === 'screen';
    if (!pops) {
      notices.announce(text); // (screen readers)
      return;
    }
    this.list = [...this.list.slice(-2), t];
    setTimeout(() => this.dismiss(t.id), opts.ms ?? (opts.action ? 8000 : t.kind === 'error' ? 6000 : 3000));
  }

  error(text: string) {
    this.show(text, { kind: 'error', ms: 6000, cat: 'errors' });
  }

  /** On screen only (a downloader event set to show there: notices). */
  popup(text: string, kind: ToastKind) {
    const t: Toast = { id: this.next++, text, kind };
    this.list = [...this.list.slice(-2), t];
    setTimeout(() => this.dismiss(t.id), kind === 'error' ? 6000 : 4000);
  }

  dismiss(id: number) {
    this.list = this.list.filter((t) => t.id !== id);
  }
}

export const toasts = new Toasts();
notices.onScreen = (text, kind) => toasts.popup(text, kind);
