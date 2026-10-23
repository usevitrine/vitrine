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
