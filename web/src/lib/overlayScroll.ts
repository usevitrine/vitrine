// A scrollbar that floats over the content (with a mouse): the browser's own
// takes room when it appears, so the page shifted sideways as it came and went.
// A thin thumb at the right edge, shown while scrolling and when the pointer
// comes near (wider under it, so it's easy to grab), dragged to scroll; gone
// when idle or when there's nothing to scroll. Touch screens keep their own
// (it floats there already). Use as {@attach overlayScroll} on the scroller;
// its native bar is hidden by the .oscroll-host class (app.css).
const fine = () => matchMedia('(hover: hover) and (pointer: fine)').matches;

export function overlayScroll(el: HTMLElement) {
  if (!fine()) return;
  const host = el.parentElement;
  if (!host) return;
  el.classList.add('oscroll-host');
  if (getComputedStyle(host).position === 'static') host.style.position = 'relative';
  const thumb = document.createElement('div');
  thumb.className = 'oscroll';
  thumb.setAttribute('aria-hidden', 'true'); // the page scrolls with the wheel, keys and touch as ever
  host.appendChild(thumb);

  let hideTimer = 0;
  let dragging: { y: number; top: number } | null = null;
  let near = false;

  function place() {
    const max = el.scrollHeight - el.clientHeight;
    if (max <= 1) {
      thumb.classList.remove('on');
      thumb.style.display = 'none';
      return;
    }
    thumb.style.display = '';
    const h = el.clientHeight;
    const size = Math.max(32, (h * h) / el.scrollHeight);
    const at = (el.scrollTop / max) * (h - size);
    thumb.style.height = `${size}px`;
    thumb.style.top = `${el.offsetTop + at}px`;
    thumb.style.right = `${host!.clientWidth - (el.offsetLeft + el.clientWidth) + 2}px`;
  }
  function show() {
    place();
    if (thumb.style.display === 'none') return;
    thumb.classList.add('on');
    clearTimeout(hideTimer);
    if (!dragging && !near) hideTimer = window.setTimeout(() => thumb.classList.remove('on'), 900);
  }

  const onScroll = () => show();
  // The pointer near the right edge: it shows (and stays while it's there).
  const onMove = (e: PointerEvent) => {
    const r = el.getBoundingClientRect();
    const was = near;
    near = e.clientX > r.right - 24 && e.clientX <= r.right + 2 && e.clientY >= r.top && e.clientY <= r.bottom;
    if (near) show();
    else if (was) show(); // starts the fade
  };
  const onLeave = () => {
    near = false;
    show();
  };
  const onDown = (e: PointerEvent) => {
    if (e.button !== 0) return;
    e.preventDefault();
    dragging = { y: e.clientY, top: el.scrollTop };
    thumb.setPointerCapture(e.pointerId);
    thumb.classList.add('drag');
  };
  const onDrag = (e: PointerEvent) => {
    if (!dragging) return;
    const size = thumb.offsetHeight;
    const room = el.clientHeight - size;
    if (room <= 0) return;
    el.scrollTop = dragging.top + ((e.clientY - dragging.y) / room) * (el.scrollHeight - el.clientHeight);
  };
  const onUp = () => {
    dragging = null;
    thumb.classList.remove('drag');
    show();
  };

  el.addEventListener('scroll', onScroll, { passive: true });
  el.addEventListener('pointermove', onMove);
  el.addEventListener('pointerleave', onLeave);
  thumb.addEventListener('pointerdown', onDown);
  thumb.addEventListener('pointermove', onDrag);
  thumb.addEventListener('pointerup', onUp);
  thumb.addEventListener('pointercancel', onUp);
  thumb.addEventListener('pointerenter', () => ((near = true), show()));
  thumb.addEventListener('pointerleave', () => ((near = false), show()));
  // The content or the window changing size moves it (nothing to scroll: gone).
  const ro = new ResizeObserver(() => place());
  ro.observe(el);
  for (const c of el.children) ro.observe(c);
  const mo = new MutationObserver(() => {
    for (const c of el.children) ro.observe(c);
    place();
  });
  mo.observe(el, { childList: true });
  place();

  return () => {
    clearTimeout(hideTimer);
    el.removeEventListener('scroll', onScroll);
    el.removeEventListener('pointermove', onMove);
    el.removeEventListener('pointerleave', onLeave);
    ro.disconnect();
    mo.disconnect();
    thumb.remove();
    el.classList.remove('oscroll-host');
  };
}
