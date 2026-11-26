// Dragging a list's row by its grip to another place (the saved searches, a collection's order): the row follows the
// pointer, the others make room, and on letting go `move(from, to)` is called.

export class ListDrag {
  /** The row being dragged (its index then and where it would go) and how far it has moved. */
  drag = $state<{ from: number; to: number; dy: number } | null>(null);
  private rows: { top: number; height: number }[] = [];
  private startY = 0;

  constructor(private move: (from: number, to: number) => void) {}

  /** On the grip: pointerdown (the list is the grip's closest <ul>). */
  start = (e: PointerEvent, i: number) => {
    if (e.button !== 0) return;
    const ul = (e.currentTarget as HTMLElement).closest('ul')!;
    this.rows = [...ul.children].map((li) => {
      const r = li.getBoundingClientRect();
      return { top: r.top, height: r.height };
    });
    this.startY = e.clientY;
    this.drag = { from: i, to: i, dy: 0 };
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    e.preventDefault();
  };

  /** On the grip: pointermove. */
  moveTo = (e: PointerEvent) => {
    const d = this.drag;
    if (!d) return;
    const dy = e.clientY - this.startY;
    const r = this.rows[d.from];
    const center = r.top + r.height / 2 + dy;
    let to = 0;
    for (let k = 0; k < this.rows.length; k++) if (k !== d.from && this.rows[k].top + this.rows[k].height / 2 < center) to++;
    this.drag = { ...d, dy, to };
  };

  /** On the grip: pointerup and pointercancel. */
  end = () => {
    const d = this.drag;
    if (!d) return;
    this.drag = null;
    if (d.from !== d.to) this.move(d.from, d.to);
  };

  /** How far row i is shifted meanwhile (the dragged one follows the pointer, the others make room). */
  shift(i: number): number {
    const d = this.drag;
    if (!d) return 0;
    if (i === d.from) return d.dy;
    const h = this.rows[d.from].height + 4;
    if (d.from < i && i <= d.to) return -h;
    if (d.to <= i && i < d.from) return h;
    return 0;
  }
}

/** Tiles in a grid (the saved searches menu) moved by pressing and holding one - with a mouse, clicking and holding -
 *  then dragging it: it lifts, the others glide to make room, and on letting go `move(from, to)` is called. A press
 *  that moves first is the page's (a scroll, a sheet's drag down); a short one is a tap. On each tile:
 *  {@attach drag.tile(i)}; its offset while dragging: drag.offset(i). */
export class HoldDrag {
  /** The tile being dragged (its index then and where it would go) and how far it has moved. */
  drag = $state<{ from: number; to: number; dx: number; dy: number } | null>(null);
  private rects: DOMRect[] = [];

  constructor(
    private move: (from: number, to: number) => void,
    private hold = 380,
  ) {}

  tile = (i: number) => (el: HTMLElement) => {
    let timer = 0;
    let id = -1;
    let start = { x: 0, y: 0 };
    const cancel = () => {
      clearTimeout(timer);
      timer = 0;
    };
    const lift = () => {
      timer = 0;
      const box = el.parentElement;
      if (!box) return;
      this.rects = [...box.children].map((c) => c.getBoundingClientRect());
      try {
        el.setPointerCapture(id);
      } catch {
        /* (the pointer went already) */
      }
      this.drag = { from: i, to: i, dx: 0, dy: 0 };
      navigator.vibrate?.(8);
    };
    const down = (e: PointerEvent) => {
      if (e.button !== 0 || this.drag || (e.target as HTMLElement).closest('input, [data-nodrag]')) return;
      id = e.pointerId;
      start = { x: e.clientX, y: e.clientY };
      timer = window.setTimeout(lift, this.hold);
    };
    const moveTo = (e: PointerEvent) => {
      if (e.pointerId !== id) return;
      const dx = e.clientX - start.x;
      const dy = e.clientY - start.y;
      const d = this.drag;
      if (!d) {
        if (timer && Math.hypot(dx, dy) > 8) cancel(); // (a scroll or the sheet's drag: not this)
        return;
      }
      e.stopPropagation(); // (the sheet doesn't drag down with it)
      const r = this.rects[d.from];
      const cx = r.left + r.width / 2 + dx;
      const cy = r.top + r.height / 2 + dy;
      let to = d.from;
      let best = Infinity;
      this.rects.forEach((o, k) => {
        const dist = Math.hypot(o.left + o.width / 2 - cx, o.top + o.height / 2 - cy);
        if (dist < best) [best, to] = [dist, k];
      });
      this.drag = { ...d, dx, dy, to };
    };
    const up = (e: PointerEvent) => {
      if (e.pointerId !== id) return;
      cancel();
      id = -1;
      const d = this.drag;
      if (!d) return;
      this.drag = null;
      e.stopPropagation();
      el.addEventListener('click', swallow, { capture: true, once: true }); // (the press that moved it isn't a tap)
      if (d.from !== d.to) this.move(d.from, d.to);
    };
    const swallow = (e: Event) => {
      e.preventDefault();
      e.stopPropagation();
    };
    // Touch: the page would scroll (and the sheet drag down) - not once a tile is lifted.
    const touchMove = (e: TouchEvent) => {
      if (!this.drag) return;
      e.stopPropagation();
      if (e.cancelable) e.preventDefault();
    };
    // (a long press's own menu - the picture's "Save image" - doesn't come up)
    const menu = (e: Event) => e.preventDefault();
    el.addEventListener('pointerdown', down);
    el.addEventListener('pointermove', moveTo);
    el.addEventListener('pointerup', up);
    el.addEventListener('pointercancel', up);
    el.addEventListener('touchmove', touchMove, { passive: false });
    el.addEventListener('contextmenu', menu);
    return () => {
      cancel();
      el.removeEventListener('pointerdown', down);
      el.removeEventListener('pointermove', moveTo);
      el.removeEventListener('pointerup', up);
      el.removeEventListener('pointercancel', up);
      el.removeEventListener('touchmove', touchMove);
      el.removeEventListener('contextmenu', menu);
      el.removeEventListener('click', swallow, { capture: true });
    };
  };

  /** Where tile i is drawn meanwhile, from its place: the lifted one under the pointer, the others in the place they'd
   *  take (they glide there). */
  offset(i: number): { x: number; y: number } {
    const d = this.drag;
    if (!d || !this.rects[i]) return { x: 0, y: 0 };
    if (i === d.from) return { x: d.dx, y: d.dy };
    const order = this.rects.map((_, k) => k).filter((k) => k !== d.from);
    order.splice(d.to, 0, d.from);
    const at = this.rects[order.indexOf(i)];
    return { x: at.left - this.rects[i].left, y: at.top - this.rects[i].top };
  }
}
