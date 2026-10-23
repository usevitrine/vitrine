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
  setTimeout(() => window.removeEventListener('click', stop, true), 700); // a press that made no click
}

/** Whether a menu is already on its way out (Svelte makes an element fading out
 *  inert): a press then is meant for what's under it, not to close it again. */
export const leaving = (el: Element | null | undefined) => !!el?.closest('[inert]');
