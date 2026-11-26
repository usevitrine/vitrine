// The confirmation asked before a destructive action (deleting files,
// removing a site, restoring a backup, ...): one dialog for the whole app
// (ConfirmDialog.svelte, shown by App). `await confirmAction({...})` is true
// when confirmed.
import type { ComponentProps } from 'svelte';
import type Icon from '../components/Icon.svelte';

type IconName = ComponentProps<typeof Icon>['name'];

export interface ConfirmOptions {
  title: string; // a question: "Delete this file?"
  body?: string; // one short line
  action: string; // the button: "Delete"
  cancel?: string; // the other button, when Cancel would say too little ("Keep them"); default Cancel
  icon?: IconName;
  thumbs?: string[]; // pictures of what goes (files)
  undoable?: boolean; // it can be undone: Enter confirms (else the focus starts on Cancel)
  /** A word to type before the action can be pressed (deleting a lot for good: no accidents). */
  typed?: string;
  /** More of what happens, a line each (under body). */
  details?: string[];
  /** A box to tick, off at first (confirmChecked says whether it was). */
  check?: string;
}

type Asked = ConfirmOptions & { resolve: (ok: boolean) => void; answered?: boolean };

class Confirm {
  current = $state<Asked | null>(null);
  checked = $state(false); // (the check box)
  /** This question follows one just answered (removing a site asks up to three in a row): the dialog swaps - the
   *  answered one goes, then this one comes, the page behind staying dimmed (2026-11-17: the next one appeared
   *  over the one going, or took its place with no motion at all). */
  follows = $state(false);

  ask(o: ConfirmOptions): Promise<boolean> {
    const prev = this.current;
    if (prev && !prev.answered) prev.resolve(false); // one at a time
    this.follows = !!prev?.answered;
    this.checked = false;
    return new Promise((resolve) => (this.current = { ...o, resolve }));
  }

  answer(ok: boolean) {
    const c = this.current;
    if (!c || c.answered) return;
    c.answered = true;
    c.resolve(ok);
    // Closed after what the answer starts has had its turn: a next question asked straight away takes its place.
    setTimeout(() => {
      if (this.current === c) this.current = null;
    });
  }
}

export const confirmer = new Confirm();
export const confirmAction = (o: ConfirmOptions) => confirmer.ask(o);
/** confirmAction with a box to tick: whether it was confirmed, and whether the box was ticked. */
export async function confirmChecked(o: ConfirmOptions & { check: string }) {
  const ok = await confirmer.ask(o);
  return { ok, checked: ok && confirmer.checked };
}
