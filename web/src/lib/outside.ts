import { menuOpened } from './menuOpen.svelte';

// A press outside an open menu only closes it: the press - and the click it
// would become - never reaches what's underneath (no tile opening by accident).

/** Call from a capture-phase pointerdown listener when the press closes a menu. */
export function swallowPress(e: PointerEvent) {
  e.preventDefault();
  e.stopPropagation();
  const stop = (ev: Event) => {
    ev.preventDefault();
    ev.stopPropagation();
  };
  window.addEventListener('click', stop, { capture: true, once: true });
  // Gone once the press ends - its click comes in the same moment (just after pointerup), or never: a press whose
  // target vanished meanwhile makes none, and the guard ate the next click instead (2026-11-14: a click on a menu's
  // button right after closing another did nothing). 700 ms at most either way.
  const off = () => window.removeEventListener('click', stop, true);
  window.addEventListener('pointerup', () => setTimeout(off), { capture: true, once: true });
  window.addEventListener('pointercancel', () => setTimeout(off), { capture: true, once: true });
  setTimeout(off, 700);
}

/** Whether a menu is already on its way out (Svelte makes an element fading out
 *  inert): a press then is meant for what's under it, not to close it again. */
export const leaving = (el: Element | null | undefined) => !!el?.closest('[inert]');

/** Closes a menu on a press outside it (swallowed): `inside` says what counts as the menu (its button too), `panel`
 *  - when given - is skipped while it's on its way out. Returns the clean-up (for an $effect). */
export function pressOutside(inside: (t: HTMLElement) => boolean, close: () => void, panel?: () => Element | null | undefined) {
  const down = (e: PointerEvent) => {
    if (panel && leaving(panel())) return;
    if (inside(e.target as HTMLElement)) return;
    swallowPress(e);
    close();
  };
  document.addEventListener('pointerdown', down, true);
  const counted = menuOpened(); // (open: the grid's tiles take no hover or press meanwhile)
  return () => {
    document.removeEventListener('pointerdown', down, true);
    counted();
  };
}
