import { reduced } from './motion';

/** Scrolls something to the middle of the screen - a notice's or a menu's target (2026-11-12) - and again, without
 *  the glide, if the page was still filling in above it and moved it away (a page just opened: its lists coming). */
export function bringIntoView(el: Element) {
  el.scrollIntoView({ block: 'center', behavior: reduced() ? 'auto' : 'smooth' });
  for (const wait of [700, 1400]) {
    setTimeout(() => {
      if (!el.isConnected) return;
      const r = el.getBoundingClientRect();
      if (r.bottom < 0 || r.top > innerHeight || (r.top < 0 && r.bottom < innerHeight / 2)) el.scrollIntoView({ block: 'center' });
    }, wait);
  }
}
