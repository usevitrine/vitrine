// A finger's tap the browser didn't click, clicked: after a swipe that ended at speed (the viewer swiped to the next
// picture or down to close, the reader's pages, a sheet flicked away) the browser thinks a fling is under way, and
// the next tap - anywhere - only "stops" it: Chrome and Safari drop its click. The user: after opening a picture and
// closing it, the top bar's buttons took two taps (2026-10-25).
// For the whole page, once: a short touch tap on something pressable waits a moment for the browser's click; when
// none comes, it gets one (only then - never a second press). The click is one like the browser's (detail 1): what
// handles touches itself and clicks only from the keyboard (detail 0: the grid's tiles) doesn't take it as its own.
// The viewer's and the reader's bars press as the finger lifts (lib/tapPress) - this catches everywhere else.
// Something tappable that isn't a button, link or control (a backdrop that closes a menu): data-tap.

const PRESSABLE =
  '[data-tap], button, a[href], summary, label, input[type="checkbox"], input[type="radio"], [role="button"], [role="tab"], [role="menuitem"], [role="menuitemradio"], [role="menuitemcheckbox"], [role="option"], [role="radio"], [role="switch"], [role="checkbox"], [role="link"]';
const SLOP = 10; // px a finger may move and still tap
const SLOW = 600; // ms: longer is a hold
const WAIT = 350; // ms for the browser's own click (it comes at once when it comes)

export function fixDroppedTaps() {
  let down: { id: number; el: Element; x: number; y: number; t: number; ev: PointerEvent } | null = null;
  // (the touch's own events: one the page cancelled - preventDefault - it handles the press itself, no click wanted)
  let touches: Event[] = [];
  for (const type of ['touchstart', 'touchend'])
    addEventListener(type, (e) => (type === 'touchstart' ? (touches = [e]) : touches.push(e)), { capture: true, passive: true });
  let waiting: { el: Element; timer: number } | null = null;
  // A tap while something still scrolls (a list coasting after a flick) only stops it - as everywhere on a phone,
  // it presses nothing: left alone. (Vitrine's own swipes - the viewer, the reader - scroll nothing.)
  let scrolledAt = -Infinity;
  addEventListener('scroll', () => (scrolledAt = performance.now()), { capture: true, passive: true });

  addEventListener(
    'pointerdown',
    (e) => {
      if (e.pointerType !== 'touch') return;
      if (performance.now() - scrolledAt < 150) return void (down = null);
      const el = (e.target as Element | null)?.closest?.(PRESSABLE);
      down = el ? { id: e.pointerId, el, x: e.clientX, y: e.clientY, t: performance.now(), ev: e } : null;
    },
    true,
  );
  addEventListener(
    'pointerup',
    (e) => {
      const d = down;
      down = null;
      if (!d || e.pointerId !== d.id) return;
      if (Math.hypot(e.clientX - d.x, e.clientY - d.y) > SLOP || performance.now() - d.t > SLOW) return;
      const at = document.elementFromPoint(e.clientX, e.clientY);
      if (!at || !d.el.contains(at)) return;
      if (waiting) clearTimeout(waiting.timer);
      const el = d.el;
      const own = [d.ev, e];
      waiting = {
        el,
        timer: window.setTimeout(() => {
          waiting = null;
          if (!el.isConnected || (el as HTMLButtonElement).disabled || el.closest('[inert]')) return;
          if ([...own, ...touches].some((x) => x.defaultPrevented)) return; // (handled there: no click meant)
          const under = document.elementFromPoint(e.clientX, e.clientY);
          if (!under || !el.contains(under)) return; // (it moved or something came over it meanwhile: not what was tapped now)
          el.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, composed: true, detail: 1, view: window, clientX: e.clientX, clientY: e.clientY }));
        }, WAIT),
      };
    },
    true,
  );
  addEventListener('pointercancel', () => (down = null), true);
  // Its click came (the browser's, or one pressed for it already - lib/tapPress): nothing to do.
  addEventListener(
    'click',
    (e) => {
      if (waiting && (e.target === waiting.el || waiting.el.contains(e.target as Node) || (e.target as Element)?.contains?.(waiting.el))) {
        clearTimeout(waiting.timer);
        waiting = null;
      }
    },
    true,
  );
}
