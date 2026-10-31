import { px, onPixels } from './motion';
// Menus with pages (a page of choices, Sort by, a tag's Namespace): moving
// between pages looks the same in every one. A deeper page slides in from the
// right and the old one out to the left (going back, the other way round),
// both fading, while the menu's height follows smoothly.
//
// Markup: a box `.mpages` (class:animating={p.animating}, style:height from
// p.height, {@attach p.box}) holding `{#key p.nav}<div class="mpage"
// {@attach p.page} in:p.pageIn out:p.pageOut>` (base styles in app.css).
import { tick } from 'svelte';
import { cubicOut } from 'svelte/easing';

const reduced = () => matchMedia('(prefers-reduced-motion: reduce)').matches;

export class Pages {
  nav = $state(0); // each move gets a page of its own (one on its way out is never reused)
  height = $state<number | null>(null); // held while the height animates
  animating = $state(false);
  private way = 1; // 1 = deeper, -1 = back
  private boxEl: HTMLElement | null = null;
  private pageEl: HTMLElement | null = null;
  private settle = 0;

  /** fit: the most the height can be (a page taller than that scrolls inside the menu). */
  constructor(private fit: (h: number) => number = (h) => h) {}

  box = (el: HTMLElement) => {
    this.boxEl = el;
    return () => {
      if (this.boxEl === el) this.boxEl = null;
    };
  };

  page = (el: HTMLElement) => {
    this.pageEl = el;
  };

  /** To another page: change() switches what's shown (or it changed already -
   *  then call this before the page is drawn, e.g. from $effect.pre). Resolves
   *  once the new page is there. */
  go(deeper: boolean, change: () => void = () => {}): Promise<HTMLElement | null> {
    this.way = deeper ? 1 : -1;
    return this.follow(() => {
      change();
      this.nav++;
    });
  }

  /** The page shown changes in place (what it waited for came: "Loading…" for the list): the height follows smoothly
   *  instead of jumping (a dialog centered by it jumped to its new place). */
  grow(change: () => void): Promise<HTMLElement | null> {
    return this.follow(change);
  }

  private follow(change: () => void): Promise<HTMLElement | null> {
    const animate = !reduced() && !!this.boxEl;
    if (animate) {
      this.height = this.fit(this.boxEl!.offsetHeight); // hold the old height...
      this.animating = true;
      clearTimeout(this.settle);
    }
    change();
    return tick().then(() => {
      if (animate) {
        void this.boxEl?.offsetHeight; // the old height is laid out first, so there is something to animate from
        if (this.pageEl) this.height = this.fit(this.pageEl.offsetHeight); // ...and move to the new page's
        this.settle = window.setTimeout(() => {
          this.animating = false;
          this.height = null;
        }, 300);
      }
      return this.pageEl;
    });
  }

  pageIn = (node: Element) => {
    node.removeAttribute('inert');
    node.removeAttribute('aria-hidden');
    if (reduced()) return { duration: 0 };
    onPixels(node);
    const w = this.way;
    return { delay: 60, duration: 220, easing: cubicOut, css: (t: number) => `opacity: ${t}; transform: translateX(${px((1 - t) * 32 * w)})` };
  };

  pageOut = (node: Element) => {
    node.setAttribute('inert', ''); // only the new page can be pressed or read
    node.setAttribute('aria-hidden', 'true');
    if (reduced()) return { duration: 0 };
    const w = this.way;
    return { duration: 120, css: (t: number) => `opacity: ${t}; transform: translateX(${px((t - 1) * 24 * w)})` };
  };
}
