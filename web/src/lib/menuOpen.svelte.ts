// Whether a menu, dropdown or popover is open anywhere (2026-11-14, the user: "every instance where a dropdown/menu is
// open and you click off to close it, the thumbnails shouldn't be interactable at all, including hover to zoom").
// Each open one counts itself in while open (pressOutside does for the menus it closes; the menus with a backdrop of
// their own, from their $effect); the grid reads it: no hover card, no presses on its tiles.

import { untrack } from 'svelte';

class Menus {
  open = $state(0);
}
export const menus = new Menus();

/** Counts a menu in as open; returns the clean-up (for an $effect, or pressOutside's). */
// (untracked: called from an $effect, `menus.open++` reads the count too - the effect then depended on what it
// changed and ran again and again until Svelte stopped it, effect_update_depth_exceeded; the page stuck after it - the
// user: "I can't navigate back to the downloads page until I restart the app", 2026-11-16)
export function menuOpened(): () => void {
  untrack(() => menus.open++);
  let done = false;
  return () => {
    if (done) return;
    done = true;
    untrack(() => (menus.open = Math.max(0, menus.open - 1)));
  };
}
