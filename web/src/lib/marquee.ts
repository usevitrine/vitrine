// A line too long for its place, kept on one line, that slides to show the rest (design rule, docs/ui/design-language.md):
// it rests at its start, goes forward slowly at an even reading speed, rests at its end, comes back quickly and rests
// again - its soft edges only on a side where text is out of view. The look and the motion are app.css's (.marquee,
// marquee-line, marquee-edges); this measures how far it goes and for how long. On the box: {@attach marquee}, with
// the text in one child element. Where it slides only on hover (a comic's title), `measure` is called on pointerenter.

/** Sets the box's --shift (how far) and --dur (one whole round), and .long when the text doesn't fit. */
export function measure(box: HTMLElement) {
  const text = box.firstElementChild as HTMLElement | null;
  if (!text) return;
  const over = text.scrollWidth - box.clientWidth;
  box.classList.toggle('long', over > 2);
  box.style.setProperty('--shift', `${-over}px`);
  // (forward is 55 % of the round: about 40 px a second)
  box.style.setProperty('--dur', `${Math.max(5, over / 22 + 2).toFixed(1)}s`);
}

/** Keeps measuring as the box changes size. */
export function marquee(box: HTMLElement) {
  const ro = new ResizeObserver(() => measure(box));
  ro.observe(box);
  measure(box);
  return () => ro.disconnect();
}
