// A list that scrolls only when its rows don't fit (the bell's, the Downloads menu's): a row sliding in from the side
// or a stack's new card standing below the stack while it grows to hold it stuck out of the list for a moment, and
// the browser showed a scrollbar for it (the user). Its rows' own boxes count - not what moves or hangs out of them.
// Use as {@attach scrollsWhenFull} on the list, styled overflow: hidden with overflow-y: auto under :global(.full).
export function scrollsWhenFull(el: HTMLElement) {
  if (getComputedStyle(el).position === 'static') el.style.position = 'relative'; // (rows' offsetTop from the list)
  const check = () => {
    const last = el.lastElementChild as HTMLElement | null;
    const need = last ? last.offsetTop + last.offsetHeight + parseFloat(getComputedStyle(el).paddingBottom) : 0;
    el.classList.toggle('full', need > el.clientHeight + 1);
  };
  const ro = new ResizeObserver(check);
  ro.observe(el);
  for (const c of el.children) ro.observe(c);
  const mo = new MutationObserver(() => {
    for (const c of el.children) ro.observe(c);
    check();
  });
  mo.observe(el, { childList: true });
  check();
  return () => {
    ro.disconnect();
    mo.disconnect();
  };
}
