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
  icon?: IconName;
  thumbs?: string[]; // pictures of what goes (files)
  undoable?: boolean; // it can be undone: Enter confirms (else the focus starts on Cancel)
  /** A word to type before the action can be pressed (deleting a lot for good: no accidents). */
  typed?: string;
}

class Confirm {
  current = $state<(ConfirmOptions & { resolve: (ok: boolean) => void }) | null>(null);

  ask(o: ConfirmOptions): Promise<boolean> {
    this.current?.resolve(false); // one at a time
    return new Promise((resolve) => (this.current = { ...o, resolve }));
  }

  answer(ok: boolean) {
    const c = this.current;
    this.current = null;
    c?.resolve(ok);
  }
}

export const confirmer = new Confirm();
export const confirmAction = (o: ConfirmOptions) => confirmer.ask(o);
