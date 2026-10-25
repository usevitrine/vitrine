import { px, settle, sizeOf } from './motion';
// Phone sheets (menus along the bottom): how they come and go, the same for
// every one. They slide up from the bottom and back down - from where a drag
// left them (sheetDrag sets data-dragged) - while the page behind dims and
// clears. Off (no motion) with reduced motion; `on: false` for the same
// component shown otherwise on wider screens - `desk` says how it comes and goes there: 'pop' (a window in the
// middle: grows in, shrinks and sinks a little as it fades - like Modal), 'drop' (a menu by what opened it: drops
// in from a few px above), none (something around it animates it already). Their height: --sheet-max-h (app.css).
import { cubicOut } from 'svelte/easing';

const reduced = () => matchMedia('(prefers-reduced-motion: reduce)').matches;

/** How far a sheet goes to be out of sight: its height and its shadow above it (0 -12px 40px) - by its height alone,
 *  the shadow was still a dark band along the bottom when it had gone, then vanished at once. */
export const away = (node: Element) => (node as HTMLElement).offsetHeight + 56;

type Desk = 'pop' | 'drop' | '';

/** A window or menu on a wide screen, coming (t 0 -> 1) or going. */
function desk(node: Element, kind: Desk, out: boolean) {
  if (!kind || reduced()) return { duration: 0 };
  const size = sizeOf(node);
  if (kind === 'pop')
    return { duration: out ? 200 : 220, easing: cubicOut, css: (t: number) => `opacity: ${t}; transform: translateY(${px(out ? (1 - t) * 8 : 0)}) scale(${settle(0.94 + 0.06 * t, size)})` };
  return { duration: 180, easing: cubicOut, css: (t: number) => `opacity: ${t}; transform: translateY(${px((1 - t) * -8)}) scale(${settle(0.96 + 0.04 * t, size)})` };
}

export function sheetIn(node: Element, { on = true, desk: kind = '' as Desk } = {}) {
  // Reopened while still going, the same element comes back: usable again, and
  // not where a drag left it.
  const el = node as HTMLElement;
  el.removeAttribute('inert');
  el.removeAttribute('aria-hidden');
  delete el.dataset.dragged;
  el.style.transform = '';
  el.style.transition = '';
  if (!on) return desk(node, kind, false);
  if (reduced()) return { duration: 0 };
  return { duration: 240, easing: cubicOut, css: (t: number) => `transform: translateY(${px((1 - t) * away(node))})` };
}

export function sheetOut(node: Element, { on = true, desk: kind = '' as Desk } = {}) {
  node.setAttribute('inert', ''); // a closing sheet can't be pressed or read while it goes
  node.setAttribute('aria-hidden', 'true');
  if (!on) return desk(node, kind, true);
  if (reduced()) return { duration: 0 };
  const from = Number((node as HTMLElement).dataset.dragged ?? 0);
  return { duration: 220, easing: cubicOut, css: (t: number) => `transform: translateY(${px(from * t + (1 - t) * away(node))})` };
}

/** The dimmed page behind a sheet: fades in and out (usable again at once). */
export function shade(node: HTMLElement, { out = false } = {}) {
  if (out) node.style.pointerEvents = 'none';
  return reduced() ? { duration: 0 } : { duration: 180, css: (t: number) => `opacity: ${t}` };
}
