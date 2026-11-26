// Where the app handles touches itself - `touch-action: none`: the viewer's and the reader's swipes, sheets dragged
// down, sliders, the crop box, the fast scroller - the browser scrolls nothing, but it still watched the touch: a
// quick swipe there it took for a fling, and the next tap, anywhere and however much later, went to "stopping" that
// fling - its click dropped (Chrome and Safari). The user: after swiping a picture away, the top bar took two taps
// (2026-10-25; measured with real touches: a slow drag was fine, a quick swipe lost the next tap's click even 2.5 s
// later; with the swipe's moves handled, the click came).
// So on those surfaces a touch's moves are marked handled (preventDefault): the browser runs no gesture of its own.
// Pointer events - what the app listens to - come as before. Not in text fields or native controls (their own
// dragging), and nowhere a page can scroll.

const NATIVE = 'input, textarea, select, [contenteditable="true"]';

/** Does a touch at el belong to the app (touch-action none on it or an ancestor, before anything that scrolls)? */
function owned(el: Element | null): boolean {
  if (!el || el.closest(NATIVE)) return false;
  for (let e: Element | null = el; e && e !== document.documentElement; e = e.parentElement) {
    const cs = getComputedStyle(e);
    if (cs.touchAction === 'none') return true;
    if (/(auto|scroll)/.test(cs.overflowY + cs.overflowX) && (e.scrollHeight > e.clientHeight || e.scrollWidth > e.clientWidth)) return false;
  }
  return false;
}

export function ownTouches() {
  let own = false;
  addEventListener('touchstart', (e) => (own = owned(e.target as Element)), { capture: true, passive: true });
  addEventListener(
    'touchmove',
    (e) => {
      if (own && e.cancelable) e.preventDefault();
    },
    { capture: true, passive: false },
  );
}
