// Where a floating menu goes next to what opened it (a button, a tag, the point pressed): below it, or above when
// there's no room below and there is above; lined up with its start or its end; always inside the window.

export interface Anchor {
  left: number;
  top: number;
  right: number;
  bottom: number;
}

/** A point as an anchor (a right-click, a long-press). */
export const atPoint = (x: number, y: number): Anchor => ({ left: x, top: y, right: x, bottom: y });

export interface Placed {
  left: number;
  top: number;
  /** Placed above the anchor: from the window's bottom edge (so a menu that changes height grows upwards). */
  above: boolean;
  bottom: number;
}

export function place(
  size: { width: number; height: number },
  a: Anchor,
  { align = 'start', gap = 4, pad = 8, flip = false }: { align?: 'start' | 'end'; gap?: number; pad?: number; flip?: boolean } = {},
): Placed {
  const { width: w, height: h } = size;
  let left = align === 'start' ? a.left : a.right - w;
  // flip: past the right edge, it opens to the left of the anchor instead (a menu at a point).
  if (flip && left + w > innerWidth - pad) left = a.left - w;
  left = Math.min(Math.max(pad, left), innerWidth - w - pad);
  const fitsBelow = a.bottom + gap + h <= innerHeight - pad;
  const fitsAbove = a.top - gap - h >= pad;
  const above = !fitsBelow && fitsAbove;
  // (room on neither side: as low as it can go and still show whole)
  const top = above ? a.top - gap - h : fitsBelow ? a.bottom + gap : Math.max(pad, innerHeight - pad - h);
  return { left, top, above, bottom: innerHeight - (a.top - gap) };
}
