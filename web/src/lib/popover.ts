// The top bar's popovers (the bell, the downloads): a panel that grows out of its button, its rows sliding in one
// after another; a press outside or Escape closes it (the focus back on the button).
import { cubicOut } from 'svelte/easing';
import { px, settle, sizeOf, onPixels, reduced } from './motion';
import { pressOutside } from './outside';

/** The panel coming (t 0 -> 1) or going. */
export function grow(node: Element, { out = false } = {}) {
  if (reduced()) return { duration: 0 };
  if (!out) onPixels(node);
  return {
    duration: out ? 200 : 240,
    easing: cubicOut,
    css: (k: number) => `opacity: ${k}; transform: translateY(${(1 - k) * -8}px) scale(${settle(0.94 + 0.06 * k, sizeOf(node))})`,
  };
}

/** A row sliding in (the i-th a little after the one before it). */
export function rowIn(node: Element, { i = 0 } = {}) {
  if (reduced()) return { duration: 0 };
  return { delay: 60 + Math.min(i, 8) * 28, duration: 260, easing: cubicOut, css: (k: number) => `opacity: ${k}; transform: translateX(${px((1 - k) * 14)})` };
}

export function rowOut(_node: Element) {
  if (reduced()) return { duration: 0 };
  return { duration: 180, easing: cubicOut, css: (k: number) => `opacity: ${k}; transform: translateX(${px((1 - k) * 24)})` };
}

/** While open: a press outside the panel and its button, or Escape, closes it. Returns the clean-up (for an $effect). */
export function dismiss(panel: () => HTMLElement | undefined, button: () => HTMLElement | undefined, close: () => void) {
  const off = pressOutside((t) => !!(panel()?.contains(t) || button()?.contains(t)), close, panel);
  const key = (e: KeyboardEvent) => {
    if (e.key === 'Escape' && !e.defaultPrevented) {
      e.preventDefault();
      close();
      button()?.focus();
    }
  };
  window.addEventListener('keydown', key);
  return () => {
    off();
    window.removeEventListener('keydown', key);
  };
}
