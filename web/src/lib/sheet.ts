// Phone sheets (menus along the bottom): how they come and go, the same for
// every one. They slide up from the bottom and back down - from where a drag
// left them (sheetDrag sets data-dragged) - while the page behind dims and
// clears. Off (no motion) with reduced motion; `on: false` for the same
// component shown as a menu on wider screens. Their height: --sheet-max-h (app.css).
import { cubicOut } from 'svelte/easing';

const reduced = () => matchMedia('(prefers-reduced-motion: reduce)').matches;

export function sheetIn(node: Element, { on = true } = {}) {
  // Reopened while still going, the same element comes back: usable again, and
  // not where a drag left it.
  const el = node as HTMLElement;
  el.removeAttribute('inert');
  el.removeAttribute('aria-hidden');
  delete el.dataset.dragged;
  el.style.transform = '';
  el.style.transition = '';
  if (!on || reduced()) return { duration: 0 };
  return { duration: 240, easing: cubicOut, css: (t: number) => `transform: translateY(${(1 - t) * 100}%)` };
}

export function sheetOut(node: Element, { on = true } = {}) {
  if (!on) return { duration: 0 };
  node.setAttribute('inert', ''); // a closing sheet can't be pressed or read while it goes
  node.setAttribute('aria-hidden', 'true');
  if (reduced()) return { duration: 0 };
  const from = Number((node as HTMLElement).dataset.dragged ?? 0);
  return { duration: 220, easing: cubicOut, css: (t: number) => `transform: translateY(calc(${from * t}px + ${(1 - t) * 100}%))` };
}

/** The dimmed page behind a sheet: fades in and out (usable again at once). */
export function shade(node: HTMLElement, { out = false } = {}) {
  if (out) node.style.pointerEvents = 'none';
  return reduced() ? { duration: 0 } : { duration: 180, css: (t: number) => `opacity: ${t}` };
}
