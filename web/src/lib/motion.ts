// The app's shared motion: everything comes and goes smoothly (the user: "everything should be smoothly animated in
// and out of view/reshuffling in all situations"), none of it with reduced motion.
//   reveal  a block in the flow (a line, a banner, a form): its height opens and closes - what's below moves with
//           it - as it fades. axis 'x' for something beside others in a row.
//   pop     something small on its own (a badge, a chip, a counter): grows in, shrinks out.
//   swap    content replaced in the same place (one state's text for another's): a quick cross-fade.
//   moved   for animate:flip on keyed lists: rows glide to their new places.
import { slide, fade } from 'svelte/transition';
import { flip } from 'svelte/animate';
import { cubicOut } from 'svelte/easing';

export const reduced = () => typeof matchMedia !== 'undefined' && matchMedia('(prefers-reduced-motion: reduce)').matches;

// Text must not shift when a motion ends (the user saw it after every opening): an ease-out spends its last frames
// moving by fractions of a pixel, the text drawn between pixels, then snapping onto them when the motion stops. So
// moves go in whole pixels, and a scale counts as done once it's within half a pixel of the element's size.
/** A move in whole pixels. */
export const px = (v: number) => `${Math.round(v)}px`;
/** An element's larger side (for settle). */
export const sizeOf = (node: Element) => {
  const r = (node as HTMLElement).getBoundingClientRect();
  return Math.max(r.width, r.height, 1);
};
/** A scale that is 1 once the difference is under half a pixel on an element that big. */
export const settle = (s: number, size: number) => (Math.abs(1 - s) * size < 0.5 ? 1 : s);

/** Like Svelte's fly, in whole pixels: from x / y away (and opacity), to where it is. */
export function glide(
  node: Element,
  { x = 0, y = 0, duration = 200, delay = 0, easing = cubicOut, opacity = 0 }: { x?: number; y?: number; duration?: number; delay?: number; easing?: (t: number) => number; opacity?: number } = {},
) {
  if (reduced()) return { duration: 0 };
  return { duration, delay, easing, css: (t: number, u: number) => `opacity: ${1 - (1 - opacity) * u}; transform: translate(${px(u * x)}, ${px(u * y)})` };
}

export function reveal(node: Element, { duration = 220, axis = 'y' as 'x' | 'y', delay = 0 } = {}) {
  if (reduced()) return { duration: 0 };
  const s = slide(node, { duration, easing: cubicOut, axis, delay });
  const g = gapAway(node, axis, duration + delay);
  return { ...s, css: (t: number, u: number) => `${s.css?.(t, u) ?? ''}; opacity: ${Math.min(1, t * 1.4)};${g(t)}` };
}

// A thing sliding shut in a column (or a row) with a gap: its height closed, but the gap beside it stayed until it
// was gone, then went at once - everything after it jumped (the user: the Sources header "jumps" as it goes). So the
// gap slides away with it: a negative margin of the gap's size as it closes (none as it opens). That takes the gap
// back in a flex box; in a grid it doesn't (a track never goes below 0), so a single-column grid lays out as the
// same flex column while something in it moves.
const asFlex = new WeakMap<HTMLElement, number>(); // parents laid out as a flex column now (how many moving)
function gapAway(node: Element, axis: 'x' | 'y', ms: number): (t: number) => string {
  const parent = node.parentElement;
  if (!parent) return () => '';
  const ps = getComputedStyle(parent);
  const gap = parseFloat(axis === 'y' ? ps.rowGap : ps.columnGap) || 0;
  if (!gap) return () => '';
  const flexColumn = ps.display.includes('flex') && ps.flexDirection.startsWith('column');
  const flexRow = ps.display.includes('flex') && !ps.flexDirection.startsWith('column');
  const oneColumnGrid = ps.display.includes('grid') && axis === 'y' && ps.gridTemplateColumns.trim().split(/\s+/).length === 1;
  if (!(axis === 'y' ? flexColumn || oneColumnGrid : flexRow)) return () => '';
  if (oneColumnGrid) {
    // (back to the grid when the last thing moving in it is done)
    const n = asFlex.get(parent) ?? 0;
    if (!n) {
      // (the grid's alignment as the flex box's: centered stays centered)
      const flexAlign = (v: string) => (v.includes('center') ? 'center' : v.includes('end') ? 'flex-end' : v.includes('start') ? 'flex-start' : '');
      parent.style.alignItems = flexAlign(ps.justifyItems);
      parent.style.justifyContent = flexAlign(ps.alignContent);
      parent.style.display = ps.display.startsWith('inline') ? 'inline-flex' : 'flex';
      parent.style.flexDirection = 'column';
    }
    asFlex.set(parent, n + 1);
    setTimeout(() => {
      const left = (asFlex.get(parent) ?? 1) - 1;
      asFlex.set(parent, left);
      if (!left) {
        parent.style.display = '';
        parent.style.flexDirection = '';
        parent.style.alignItems = '';
        parent.style.justifyContent = '';
      }
    }, ms + 50);
  }
  const own = parseFloat(axis === 'y' ? getComputedStyle(node).marginBottom : getComputedStyle(node).marginRight) || 0;
  const side = axis === 'y' ? 'margin-bottom' : 'margin-right';
  return (t: number) => ` ${side}: ${own * t - gap * (1 - t)}px;`;
}

export function pop(node: Element, { duration = 200, delay = 0 } = {}) {
  if (reduced()) return { duration: 0 };
  return { duration, delay, easing: cubicOut, css: (t: number) => `opacity: ${t}; transform: scale(${0.6 + 0.4 * t})` };
}

/** pop for something small in a row (a button beside others, a chip after a name): its width opens and closes too,
 *  so what's beside it glides instead of jumping when it comes or goes. */
export function nudge(node: Element, { duration = 220, delay = 0 } = {}) {
  if (reduced()) return { duration: 0 };
  const inline = getComputedStyle(node).display === 'inline'; // (a width needs a box)
  const size = sizeOf(node);
  const s = slide(node, { duration, delay, easing: cubicOut, axis: 'x' });
  const g = gapAway(node, 'x', duration + delay);
  return {
    ...s,
    css: (t: number, u: number) =>
      `${inline ? 'display: inline-block; ' : ''}white-space: nowrap; ${s.css?.(t, u) ?? ''}; opacity: ${t}; transform: scale(${settle(0.6 + 0.4 * t, size)});${g(t)}`,
  };
}

export function swap(node: Element, { duration = 160, delay = 0 } = {}) {
  if (reduced()) return { duration: 0 };
  return fade(node, { duration, delay });
}

/** animate:moved on a keyed {#each}'s rows. */
export function moved(node: Element, fromTo: { from: DOMRect; to: DOMRect }, { duration = 220 } = {}) {
  return flip(node, fromTo, { duration: reduced() ? 0 : duration, easing: cubicOut });
}
