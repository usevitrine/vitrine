// Whether a menu, dropdown or popover is open anywhere (2026-11-14, the user: "every instance where a dropdown/menu is
// open and you click off to close it, the thumbnails shouldn't be interactable at all, including hover to zoom").
// Each open one counts itself in while open (pressOutside does for the menus it closes; the menus with a backdrop of
// their own, from their $effect); the grid reads it: no hover card, no presses on its tiles.

class Menus {
  open = $state(0);
}
export const menus = new Menus();

/** Counts a menu in as open; returns the clean-up (for an $effect, or pressOutside's). */
export function menuOpened(): () => void {
  menus.open++;
  let done = false;
  return () => {
    if (done) return;
    done = true;
    menus.open = Math.max(0, menus.open - 1);
  };
}
