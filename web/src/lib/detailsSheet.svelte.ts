// The details sheet on phones (the viewer's, and DetailsPanel's): dragged down from anywhere on it - a list scrolled
// down scrolls first (lib/sheetDrag) - it follows the finger; let go far enough down (or flicked), it closes and slides
// on from where it was let go (put back to its place first, it jumped up for a frame before sliding away).
import { cubicOut } from 'svelte/easing';
import { px, onPixels } from './motion';
import { away } from './sheet';
import { dragDown, closes } from './sheetDrag';

export class DetailsSheet {
  dragging = $state(false);
  dy = $state(0); // how far it's dragged down now
  #gone = 0; // how far it was dragged when let go to close
  drag: ReturnType<typeof dragDown>; // ({@attach} it to the sheet)

  /** active: whether it's a sheet now (phones); close: closes it (let go far enough down). */
  constructor(active: () => boolean, close: () => void) {
    this.drag = dragDown({
      active,
      onstart: () => (this.dragging = true),
      onmove: (d) => (this.dy = d),
      onend: (d, fling) => {
        this.dragging = false;
        if (closes(d, fling, 90)) {
          this.#gone = d;
          close();
          return;
        }
        this.dy = 0;
      },
    });
  }

  /** The sheet coming up from the bottom or going down - from where a drag left it (no reduced-motion check here). */
  slide(node: Element, out: boolean) {
    if (!out) onPixels(node);
    if (out && this.#gone > 0) {
      const from = this.#gone;
      this.#gone = 0;
      return { duration: 200, easing: cubicOut, css: (k: number) => `transform: translateY(${px(from * k + (1 - k) * away(node))})` };
    }
    return { duration: 240, easing: cubicOut, css: (k: number) => `transform: translateY(${px((1 - k) * away(node))})` };
  }

  /** Whether it was let go to close (the viewer glides the picture faster then). */
  get gone() {
    return this.#gone;
  }
}
