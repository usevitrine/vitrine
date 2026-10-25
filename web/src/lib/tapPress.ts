// A finger's tap on a button presses it as the finger lifts - not waiting for the browser's click, which it may not
// send: a swipe that ended at speed leaves the browser thinking a fling is under way, and the next tap only "stops"
// it - its click is dropped (Chrome and Safari alike). In the viewer that tap was a button on its controls: after
// swiping to the next picture nothing could be pressed until a first tap was spent (the user, 2026-10-25).
// For a container ({@attach tapPress}): its buttons. A click that does come after is swallowed (no second press).

const SLOP = 10; // px a finger may move and still tap
const SLOW = 600; // ms: longer is a hold, not a tap

export function tapPress(box: HTMLElement) {
  let start: { id: number; button: HTMLButtonElement; x: number; y: number; t: number } | null = null;
  let pressedAt = 0;
  let pressed: HTMLButtonElement | null = null;
  const down = (e: PointerEvent) => {
    if (e.pointerType !== 'touch') return;
    const button = (e.target as Element).closest<HTMLButtonElement>('button');
    start = button && box.contains(button) && !button.disabled ? { id: e.pointerId, button, x: e.clientX, y: e.clientY, t: performance.now() } : null;
  };
  const up = (e: PointerEvent) => {
    const s = start;
    start = null;
    if (!s || e.pointerId !== s.id) return;
    const at = document.elementFromPoint(e.clientX, e.clientY);
    if (Math.hypot(e.clientX - s.x, e.clientY - s.y) > SLOP || performance.now() - s.t > SLOW || !at || !s.button.contains(at)) return;
    pressed = s.button;
    pressedAt = performance.now();
    s.button.click();
  };
  // (the browser's own click, if it sends one after: already pressed)
  const click = (e: MouseEvent) => {
    if (!pressed || !e.isTrusted || performance.now() - pressedAt > 700) return;
    if (pressed.contains(e.target as Node)) {
      e.stopImmediatePropagation();
      e.preventDefault();
    }
    pressed = null;
  };
  const cancel = () => (start = null);
  box.addEventListener('pointerdown', down);
  box.addEventListener('pointerup', up);
  box.addEventListener('pointercancel', cancel);
  box.addEventListener('click', click, true);
  return () => {
    box.removeEventListener('pointerdown', down);
    box.removeEventListener('pointerup', up);
    box.removeEventListener('pointercancel', cancel);
    box.removeEventListener('click', click, true);
  };
}
