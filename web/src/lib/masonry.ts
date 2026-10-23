// The masonry grid (Settings > This device > Grid): justified rows (2026-10-21, user: the column layout's order
// "is insane" - columns of different heights drift apart, so file 32's neighbors were 36 and 39). Every row is as
// tall as its tiles need to fill the width exactly, each tile as wide as its picture's shape at that height; files
// go left to right, row by row, so the order reads like a page. Rows aim at about the old column width in height
// (as many tiles to a row as there were columns, for square pictures); the last row takes the height of the
// row before it (not stretched). Laid out from the shapes the search sends (width/height x 1024 per file); very tall or wide pictures
// are kept within limits (the tile crops them, like a square tile does).

export interface Layout {
  x: Float32Array;
  y: Float32Array;
  w: Float32Array;
  h: Float32Array;
  /** Where each row starts (file positions; one more at the end: the total), its top and its height. */
  rowStart: Int32Array;
  rowY: Float32Array;
  rowH: Float32Array;
  height: number;
  gap: number;
}

const MIN_H = 0.45; // a very wide picture: at least this many widths tall
const MAX_H = 2.0; // a very tall one: at most

/** cols and colW: the grid's columns for square tiles - the width to fill, and the rows' aimed height (colW). */
export function layOut(shapes: Uint16Array | null, total: number, cols: number, colW: number, gap: number): Layout {
  const width = cols * colW + (cols - 1) * gap;
  const x = new Float32Array(total);
  const y = new Float32Array(total);
  const w = new Float32Array(total);
  const h = new Float32Array(total);
  const aspect = (i: number) => {
    const r = shapes && i < shapes.length ? shapes[i] / 1024 : 1;
    return 1 / Math.min(MAX_H, Math.max(MIN_H, 1 / (r || 1))); // width / height, within the limits
  };
  const starts: number[] = [];
  const tops: number[] = [];
  const heights: number[] = [];
  let top = 0;
  let i = 0;
  while (i < total) {
    // Tiles for this row: until they fill the width at the aimed height.
    const from = i;
    let sum = 0;
    do {
      sum += aspect(i);
      i++;
    } while (i < total && sum * colW + (i - from - 1) * gap < width);
    const gaps = (i - from - 1) * gap;
    const full = sum * colW + gaps >= width;
    // (the last row, short of the width: as tall as the row before it - not stretched, and not taller than the rest)
    const rh = full ? (width - gaps) / sum : (heights.at(-1) ?? colW);
    let left = 0;
    for (let k = from; k < i; k++) {
      const tw = k === i - 1 && full ? width - left : aspect(k) * rh; // (the last tile takes the rounding)
      x[k] = left;
      y[k] = top;
      w[k] = tw;
      h[k] = rh;
      left += tw + gap;
    }
    starts.push(from);
    tops.push(top);
    heights.push(rh);
    top += rh + gap;
  }
  starts.push(total);
  return {
    x,
    y,
    w,
    h,
    rowStart: Int32Array.from(starts),
    rowY: Float32Array.from(tops),
    rowH: Float32Array.from(heights),
    height: Math.max(0, top - gap),
    gap,
  };
}

/** The first row reaching below y (binary search). */
function rowBelow(l: Layout, top: number): number {
  let lo = 0;
  let hi = l.rowY.length;
  while (lo < hi) {
    const mid = (lo + hi) >> 1;
    if (l.rowY[mid] + l.rowH[mid] < top) lo = mid + 1;
    else hi = mid;
  }
  return lo;
}

/** The files with any part between top and bottom, in result order. */
export function visible(l: Layout, top: number, bottom: number): number[] {
  const out: number[] = [];
  for (let r = rowBelow(l, top); r < l.rowY.length && l.rowY[r] <= bottom; r++) {
    for (let i = l.rowStart[r]; i < l.rowStart[r + 1]; i++) out.push(i);
  }
  return out;
}

/** The file at a point in the grid (the nearest one in that row). */
export function indexAt(l: Layout, px: number, py: number): number {
  if (!l.rowY.length) return 0;
  const r = Math.min(l.rowY.length - 1, rowBelow(l, py));
  let i = l.rowStart[r];
  while (i < l.rowStart[r + 1] - 1 && px >= l.x[i] + l.w[i] + l.gap / 2) i++;
  return i;
}

/** Shapes from the search's answer (base64 of little-endian uint16s). */
export function decodeShapes(b64: string): Uint16Array {
  const bin = atob(b64);
  const out = new Uint16Array(bin.length >> 1);
  for (let i = 0; i < out.length; i++) out[i] = bin.charCodeAt(2 * i) | (bin.charCodeAt(2 * i + 1) << 8);
  return out;
}
