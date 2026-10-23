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
  /** Let go (fast: flicked). */
  onend: (dy: number, fast: boolean) => void;
}

/** The gesture alone (the element follows by the callbacks): for sheets that move themselves (the details panels). */
export function dragDown(o: DragDown) {
  return (handle: HTMLElement) => {
    let id = -1;
    let start = { x: 0, y: 0 };
    let startAt = 0;
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
      startAt = performance.now();
      dy = 0;
      from = t;
      // The grab bar and the header drag at once; elsewhere the first moves decide.
      mode = 'maybe';
      if (t.closest('.grab-zone, .grab, .head, .panel-head, [data-drag]') && !t.closest('button, a')) begin();
    };
    const move = (e: PointerEvent) => {
      if (e.pointerId !== id) return;
      if (mode === 'maybe') decide(e.clientX, e.clientY);
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
      o.onend(dy, dy / Math.max(1, performance.now() - startAt) > 0.5);
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
    handle.addEventListener('pointerdown', down);
    handle.addEventListener('pointermove', move);
    handle.addEventListener('pointerup', up);
    handle.addEventListener('pointercancel', cancel);
    handle.addEventListener('touchmove', touchMove, { passive: false });
    return () => {
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
      onend: (dy, fast) => {
        const s = sheet();
        if (!s) return;
        if (dy > 80 || (fast && dy > 20)) {
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
