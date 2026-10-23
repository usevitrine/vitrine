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
  return { ...s, css: (t: number, u: number) => `${s.css?.(t, u) ?? ''}; opacity: ${Math.min(1, t * 1.4)};` };
}

export function pop(node: Element, { duration = 200, delay = 0 } = {}) {
  if (reduced()) return { duration: 0 };
  return { duration, delay, easing: cubicOut, css: (t: number) => `opacity: ${t}; transform: scale(${0.6 + 0.4 * t})` };
}

export function swap(node: Element, { duration = 160, delay = 0 } = {}) {
  if (reduced()) return { duration: 0 };
  return fade(node, { duration, delay });
}

/** animate:moved on a keyed {#each}'s rows. */
export function moved(node: Element, fromTo: { from: DOMRect; to: DOMRect }, { duration = 220 } = {}) {
  return flip(node, fromTo, { duration: reduced() ? 0 : duration, easing: cubicOut });
}
