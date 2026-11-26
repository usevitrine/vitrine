// Press and hold (as on the grid's tiles): after 450 ms without moving, run - with a tick where the device has one -
// and the press doesn't also count as a click. A right-click does the same with a mouse; touch screens' own
// long-press menu (iOS's callout, Android's context menu) is kept away.
const HOLD = 450;

export function longPress(run: () => void) {
  return (el: HTMLElement) => {
    let timer = 0;
    let start = { x: 0, y: 0 };
    let fired = false;
    const cancel = () => clearTimeout(timer);
    const down = (e: PointerEvent) => {
      if (e.button !== 0) return;
      fired = false;
      start = { x: e.clientX, y: e.clientY };
      cancel();
      timer = window.setTimeout(() => {
        fired = true;
        navigator.vibrate?.(10);
        run();
      }, HOLD);
    };
    const move = (e: PointerEvent) => {
      if (Math.hypot(e.clientX - start.x, e.clientY - start.y) > 8) cancel(); // (a scroll, not a hold)
    };
    // The click that ends a hold: not a click.
    const click = (e: MouseEvent) => {
      if (!fired) return;
      fired = false;
      e.preventDefault();
      e.stopImmediatePropagation();
    };
    const menu = (e: MouseEvent) => {
      e.preventDefault();
      if (fired) return; // (Android: the hold already ran)
      cancel();
      run();
    };
    el.addEventListener('pointerdown', down);
    el.addEventListener('pointermove', move);
    el.addEventListener('pointerup', cancel);
    el.addEventListener('pointercancel', cancel);
    el.addEventListener('pointerleave', cancel);
    el.addEventListener('click', click, true);
    el.addEventListener('contextmenu', menu);
    return () => {
      cancel();
      el.removeEventListener('pointerdown', down);
      el.removeEventListener('pointermove', move);
      el.removeEventListener('pointerup', cancel);
      el.removeEventListener('pointercancel', cancel);
      el.removeEventListener('pointerleave', cancel);
      el.removeEventListener('click', click, true);
      el.removeEventListener('contextmenu', menu);
    };
  };
}
