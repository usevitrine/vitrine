// Phones: a sheet drags down to close. A drag starts anywhere on it - its grab
// bar, its header, its list, a button - once the finger goes down (more down
// than sideways) with whatever it's on scrolled to the top; otherwise that
// scrolls as usual. Fields and sliders keep their own drags. A press that
// became a drag doesn't press what it started on.

export interface DragDown {
  /** Whether a drag may start now (e.g. only as a sheet, on a phone). */
  active?: () => boolean;
  onstart?: () => void;
  /** How far down it is (0 and up). */
  onmove: (dy: number) => void;
  /** Let go (fling: flicked down 1, flicked back up -1, neither 0 - see closes). */
  onend: (dy: number, fling: number) => void;
}

/** Whether a sheet let go there closes: flicked down, once it has moved a little; flicked back up, never (the user:
 *  "close to being fully closed and I flick back up, it still closes"); otherwise once it's past `far`. */
export const closes = (dy: number, fling: number, far: number) => (fling < 0 ? false : fling > 0 ? dy > 20 : dy > far);

/** How fast the finger moved at the end (px/ms, down positive), from its last ~80 ms; 0 when it had stopped. */
const FLING = 0.4;
function flingOf(trail: { t: number; y: number }[], now: number) {
  const last = trail[trail.length - 1];
  if (!last || now - last.t > 100) return 0;
  const first = trail.find((p) => last.t - p.t <= 80) ?? last;
  const v = last.t > first.t ? (last.y - first.y) / (last.t - first.t) : 0;
  return v > FLING ? 1 : v < -FLING ? -1 : 0;
}

/** The gesture alone (the element follows by the callbacks): for sheets that move themselves (the details panels). */
export function dragDown(o: DragDown) {
  return (handle: HTMLElement) => {
    let id = -1;
    let start = { x: 0, y: 0 };
    let trail: { t: number; y: number }[] = []; // the finger's last moves (its speed when let go)
    let dy = 0;
    let mode: '' | 'maybe' | 'drag' | 'no' = '';
    let from: HTMLElement | null = null;

    // Something between the finger and the sheet that's scrolled down: it scrolls back first.
    const scrolledDown = (t: HTMLElement | null) => {
      for (let n = t; n && n !== handle.parentElement; n = n.parentElement) {
        if (n.scrollTop > 0 && n.scrollHeight > n.clientHeight) return true;
      }
      return false;
    };
    const begin = () => {
      mode = 'drag';
      getSelection()?.removeAllRanges(); // (a mouse: text selected on the way would be dragged by the next press)
      try {
        handle.setPointerCapture(id);
      } catch {
        /* (the pointer went already) */
      }
      o.onstart?.();
    };
    const decide = (x: number, y: number) => {
      const mx = x - start.x;
      const my = y - start.y;
      if (Math.hypot(mx, my) < 8) return;
      mode = my > 0 && my > Math.abs(mx) && !scrolledDown(from) ? 'drag' : 'no';
      if (mode === 'drag') {
        start.y = y; // (it follows from here: no jump by the slop)
        begin();
      }
    };
    const down = (e: PointerEvent) => {
      if (e.button !== 0 || (o.active && !o.active())) return;
      const t = e.target as HTMLElement;
      if (t.closest('input, textarea, select, [contenteditable], [role=slider]')) return;
      id = e.pointerId;
      start = { x: e.clientX, y: e.clientY };
      trail = [{ t: performance.now(), y: e.clientY }];
      dy = 0;
      from = t;
      // The grab bar and the header drag at once; elsewhere the first moves decide.
      mode = 'maybe';
      if (t.closest('.grab-zone, .grab, .head, .panel-head, [data-drag]') && !t.closest('button, a')) begin();
    };
    const move = (e: PointerEvent) => {
      if (e.pointerId !== id) return;
      if (mode === 'maybe') decide(e.clientX, e.clientY);
      const now = performance.now();
      trail.push({ t: now, y: e.clientY });
      while (trail.length > 2 && now - trail[0].t > 150) trail.shift();
      if (mode !== 'drag') return;
      dy = Math.max(0, e.clientY - start.y);
      o.onmove(dy);
    };
    // Touch: the browser would scroll (or bounce) the list instead - it doesn't once this is a drag.
    const touchMove = (e: TouchEvent) => {
      const t = e.touches[0];
      if (mode === 'maybe' && t) decide(t.clientX, t.clientY);
      if (mode === 'drag' && e.cancelable) e.preventDefault();
    };
    const up = (e: PointerEvent) => {
      if (e.pointerId !== id) return;
      id = -1;
      const was = mode;
      mode = '';
      if (was !== 'drag') return;
      // (the press became a drag: what it started on isn't pressed)
      if (dy > 4) handle.addEventListener('click', swallow, { capture: true, once: true });
      o.onend(dy, flingOf(trail, performance.now()));
    };
    const cancel = (e: PointerEvent) => {
      if (e.pointerId !== id) return;
      if (mode === 'drag') return up(e);
      id = -1;
      mode = '';
    };
    const swallow = (e: Event) => {
      e.preventDefault();
      e.stopPropagation();
    };
    // While it's a drag, no text selection and no native drag (of text or a picture): either took the pointer away and
    // the sheet stopped following it.
    const noNative = (e: Event) => {
      if (mode === 'drag') e.preventDefault();
    };
    handle.addEventListener('pointerdown', down);
    handle.addEventListener('pointermove', move);
    handle.addEventListener('pointerup', up);
    handle.addEventListener('pointercancel', cancel);
    handle.addEventListener('touchmove', touchMove, { passive: false });
    handle.addEventListener('selectstart', noNative);
    handle.addEventListener('dragstart', noNative);
    return () => {
      handle.removeEventListener('selectstart', noNative);
      handle.removeEventListener('dragstart', noNative);
      handle.removeEventListener('pointerdown', down);
      handle.removeEventListener('pointermove', move);
      handle.removeEventListener('pointerup', up);
      handle.removeEventListener('pointercancel', cancel);
      handle.removeEventListener('touchmove', touchMove);
      handle.removeEventListener('click', swallow, { capture: true });
    };
  };
}

/** A sheet that moves with the finger itself: the attachment goes on it (its closest [data-sheet] element moves).
 *  Let go far enough (or flicked) and it closes, leaving data-dragged on the sheet so its exit can start from
 *  where the finger left it; otherwise it springs back. */
export function sheetDrag(close: () => void) {
  return (handle: HTMLElement) => {
    const sheet = () => handle.closest<HTMLElement>('[data-sheet]');
    return dragDown({
      onstart: () => {
        const s = sheet();
        if (s) s.style.transition = 'none';
      },
      onmove: (dy) => {
        const s = sheet();
        if (s) s.style.transform = dy ? `translateY(${dy}px)` : '';
      },
      onend: (dy, fling) => {
        const s = sheet();
        if (!s) return;
        if (closes(dy, fling, 80)) {
          s.dataset.dragged = String(dy);
          close();
        } else {
          s.style.transition = 'transform 0.2s cubic-bezier(0.2, 0.8, 0.2, 1)';
          s.style.transform = '';
        }
      },
    })(handle);
  };
}
