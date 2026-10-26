// A scrollbar that floats over the content: the browser's own takes room when
// it appears (the page shifted sideways as it came and went), and looks like
// the browser's, not the app's. A thin thumb at the edge, shown while scrolling
// and when a mouse comes near (wider under it, so it's easy to grab), dragged to
// scroll - with a finger too; gone when idle or when there's nothing to scroll.
// On touch screens as well (2026-10-26, the user: the details sheet on a phone
// had the browser's own). Use as {@attach overlayScroll} on the scroller; its
// native bar is hidden by the .oscroll-host class (overlayScroll.css). The
// website (vitrine.software) uses it too: overlayScroll('page') for the page
// itself (a thumb along the window's right, under its header.top),
// overlayScroll(el, 'x') for a box that scrolls sideways (a thumb along its bottom).
import './overlayScroll.css';

export function overlayScroll(target: HTMLElement | 'page', axis: 'x' | 'y' = 'y') {
  const page = target === 'page';
  const el = page ? document.documentElement : target;
  const host = page ? document.body : el.parentElement;
  if (!host) return;
  el.classList.add(page ? 'oscroll-page' : 'oscroll-host');
  const thumb = document.createElement('div');
  thumb.className = `oscroll ${axis}${page ? ' page' : ''}`;
  thumb.setAttribute('aria-hidden', 'true'); // the page scrolls with the wheel, keys and touch as ever
  // Where the thumb is: the scroller's parent, or - one that isn't positioned - the nearest that is (looked at
  // again each time: a class can change it). The parent's own style is never changed: given a position, its top
  // and left took effect (a suggestion list placed below its box in another layout moved 172 px off it).
  let box: HTMLElement = host;
  const container = () => {
    const c = page || getComputedStyle(host).position !== 'static' ? host : ((host.offsetParent as HTMLElement | null) ?? document.body);
    if (c !== box || thumb.parentElement !== c) {
      box = c;
      c.appendChild(thumb);
    }
    return c;
  };
  container();

  const view = () => (axis === 'x' ? el.clientWidth : page ? innerHeight : el.clientHeight);
  const full = () => (axis === 'x' ? el.scrollWidth : el.scrollHeight);
  const at = () => (axis === 'x' ? el.scrollLeft : page ? scrollY : el.scrollTop);
  // (instantly: a page that scrolls smoothly for its links cut each step of a drag short)
  const go = (v: number) => (page ? window : el).scrollTo(axis === 'x' ? { left: v, behavior: 'instant' } : { top: v, behavior: 'instant' });
  // The page's thumb runs from under its header to the bottom of the window.
  const head = () => (page ? (document.querySelector<HTMLElement>('header.top')?.offsetHeight ?? 0) : 0);
  const track = () => (page ? innerHeight - head() : view());

  let hideTimer = 0;
  let dragging: { p: number; at: number } | null = null;
  let near = false;

  function place() {
    const max = full() - view();
    if (max <= 1) {
      thumb.classList.remove('on');
      thumb.style.display = 'none';
      return;
    }
    thumb.style.display = '';
    const len = track();
    const size = Math.max(32, (len * view()) / full());
    const off = (at() / max) * (len - size);
    if (page) {
      thumb.style.height = `${size}px`;
      thumb.style.top = `${head() + off}px`;
      thumb.style.right = '2px';
      return;
    }
    // The scroller's place in the thumb's container (its padding box, scrolled with it).
    const c = container();
    const r = el.getBoundingClientRect();
    const cr = c.getBoundingClientRect();
    const x0 = r.left - cr.left - c.clientLeft + c.scrollLeft;
    const y0 = r.top - cr.top - c.clientTop + c.scrollTop;
    if (axis === 'x') {
      thumb.style.width = `${size}px`;
      thumb.style.left = `${x0 + off}px`;
      thumb.style.top = `${y0 + el.clientHeight - 8}px`;
    } else {
      thumb.style.height = `${size}px`;
      thumb.style.top = `${y0 + off}px`;
      thumb.style.right = `${c.clientWidth - (x0 + el.clientWidth) + 2}px`;
    }
  }
  function show() {
    place();
    if (thumb.style.display === 'none') return;
    thumb.classList.add('on');
    clearTimeout(hideTimer);
    if (!dragging && !near) hideTimer = window.setTimeout(() => thumb.classList.remove('on'), 900);
  }

  const onScroll = () => show();
  // The pointer near the edge the thumb runs along: it shows (and stays while it's there).
  const onMove = (e: PointerEvent) => {
    const r = page ? new DOMRect(0, head(), innerWidth, innerHeight - head()) : el.getBoundingClientRect();
    const was = near;
    near =
      axis === 'x'
        ? e.clientY > r.bottom - 20 && e.clientY <= r.bottom + 2 && e.clientX >= r.left && e.clientX <= r.right
        : e.clientX > r.right - 24 && e.clientX <= r.right + 2 && e.clientY >= r.top && e.clientY <= r.bottom;
    if (near) show();
    else if (was) show(); // starts the fade
  };
  const onLeave = () => {
    near = false;
    show();
  };
  const coord = (e: PointerEvent) => (axis === 'x' ? e.clientX : e.clientY);
  const onDown = (e: PointerEvent) => {
    if (e.button !== 0) return;
    e.preventDefault();
    dragging = { p: coord(e), at: at() };
    thumb.setPointerCapture(e.pointerId);
    thumb.classList.add('drag');
  };
  const onDrag = (e: PointerEvent) => {
    if (!dragging) return;
    const room = track() - (axis === 'x' ? thumb.offsetWidth : thumb.offsetHeight);
    if (room <= 0) return;
    go(dragging.at + ((coord(e) - dragging.p) / room) * (full() - view()));
  };
  const onUp = () => {
    dragging = null;
    thumb.classList.remove('drag');
    show();
  };

  const scroller: Window | HTMLElement = page ? window : el;
  const pointer: HTMLElement = page ? document.documentElement : el;
  scroller.addEventListener('scroll', onScroll, { passive: true });
  pointer.addEventListener('pointermove', onMove);
  pointer.addEventListener('pointerleave', onLeave);
  thumb.addEventListener('pointerdown', onDown);
  thumb.addEventListener('pointermove', onDrag);
  thumb.addEventListener('pointerup', onUp);
  thumb.addEventListener('pointercancel', onUp);
  thumb.addEventListener('pointerenter', () => ((near = true), show()));
  thumb.addEventListener('pointerleave', () => ((near = false), show()));
  // The content or the window changing size moves it (nothing to scroll: gone).
  const ro = new ResizeObserver(() => place());
  const watched = page ? document.body : el;
  ro.observe(watched);
  for (const c of watched.children) ro.observe(c);
  const mo = new MutationObserver(() => {
    for (const c of watched.children) ro.observe(c);
    place();
  });
  mo.observe(watched, { childList: true });
  addEventListener('resize', place, { passive: true });
  place();

  return () => {
    clearTimeout(hideTimer);
    scroller.removeEventListener('scroll', onScroll);
    pointer.removeEventListener('pointermove', onMove);
    pointer.removeEventListener('pointerleave', onLeave);
    removeEventListener('resize', place);
    ro.disconnect();
    mo.disconnect();
    thumb.remove();
    el.classList.remove('oscroll-host', 'oscroll-page');
  };
}
