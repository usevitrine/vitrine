// The viewer's open/close animation (like iOS Photos): a copy of the
// thumbnail moves and grows between its tile and the image's place in the
// viewer, its crop opening from the tile's square to the whole image, while a
// dark backdrop fades. The real viewer is shown only at the end (opening) or
// is already gone (closing), so nothing jumps.

export interface Rect {
  x: number;
  y: number;
  w: number;
  h: number;
}

/** Where an image of w x h appears inside box with object-fit: contain. */
export function fitContain(w: number, h: number, box: Rect): Rect {
  if (!w || !h) return box;
  const s = Math.min(box.w / w, box.h / h);
  const fw = w * s;
  const fh = h * s;
  return { x: box.x + (box.w - fw) / 2, y: box.y + (box.h - fh) / 2, w: fw, h: fh };
}

export function rectOf(el: Element): Rect {
  const r = el.getBoundingClientRect();
  return { x: r.left, y: r.top, w: r.width, h: r.height };
}

export const reducedMotion = () => matchMedia('(prefers-reduced-motion: reduce)').matches;

/** A picture that flies: its address, its shape (width / height) and, when only part of it shows (a comic's
 *  cropped cover), that part as fractions [x, y, w, h] - then the shape is the part's. */
export interface HeroPicture {
  src: string;
  aspect: number;
  crop?: number[];
}

/** Animates the copy from one rect to the other; resolves when done.
 *  Only transforms and opacity move (run by the compositor, so a busy page
 *  can't make it stutter): the frame is the crop box, scaled from the end
 *  size; the picture inside is counter-scaled to stay "cover"-fitted. The steps
 *  are worked out here, since the two scales don't interpolate as inverses. */
export async function hero(opts: {
  src: string;
  aspect: number; // the image's width / height (its shape never changes, only the crop)
  crop?: number[]; // only this part of it shows (HeroPicture)
  from: Rect;
  to: Rect;
  backdrop: [number, number]; // opacity at start and end
  duration?: number;
  /** Another picture it turns into on the way (crossfading, mid-flight): the comic reader's page out of its
   *  cover, and back. Each stays "cover"-fitted in the moving frame at its own shape. */
  into?: HeroPicture;
  /** Fades the copy (and the backdrop) out over this long once it has arrived, over what's now shown under it -
   *  e.g. the comic reader's page, which may be another picture than the one that flew (ms; 0 = gone at once). */
  fade?: number;
  /** Over the screen it opens (z-index): what it fades into shows under it (the comic reader). */
  above?: boolean;
  /** Waits for this (at most 800 ms) once it has arrived, before what's under it is shown (with fade). */
  hold?: Promise<unknown>;
}): Promise<void> {
  const { from, to, backdrop } = opts;
  const duration = opts.duration ?? 280;
  const layer = document.createElement('div');
  layer.className = 'hero-layer';
  if (opts.above) layer.style.zIndex = '50';
  layer.setAttribute('aria-hidden', 'true');
  const shade = document.createElement('div');
  shade.className = 'hero-shade';
  shade.style.opacity = String(backdrop[0]);
  const frame = document.createElement('div');
  frame.className = 'hero-frame';
  Object.assign(frame.style, { left: '0', top: '0', width: `${to.w}px`, height: `${to.h}px` });

  const ease = (t: number) => cubicBezier(0.2, 0.8, 0.2, 1, t);
  const steps = 24;
  const boxes: Rect[] = [];
  const outer: Keyframe[] = [];
  for (let i = 0; i <= steps; i++) {
    const e = ease(i / steps);
    const box = {
      x: from.x + (to.x - from.x) * e,
      y: from.y + (to.y - from.y) * e,
      w: from.w + (to.w - from.w) * e,
      h: from.h + (to.h - from.h) * e,
    };
    boxes.push(box);
    outer.push({ offset: i / steps, transform: `translate(${box.x}px, ${box.y}px) scale(${box.w / to.w}, ${box.h / to.h})` });
  }
  // A picture at its own shape (base size: as wide as the end box), covering the moving box at each step.
  function picture(p: HeroPicture) {
    const el = document.createElement('div');
    el.className = 'hero-pic';
    const img = document.createElement('img');
    img.src = p.src;
    img.alt = '';
    img.draggable = false;
    const iw = to.w;
    const ih = to.w / (p.aspect || to.w / to.h);
    Object.assign(el.style, { width: `${iw}px`, height: `${ih}px` });
    if (p.crop?.length === 4) {
      const [x, y, w, h] = p.crop;
      Object.assign(img.style, { position: 'absolute', width: `${100 / w}%`, height: `${100 / h}%`, left: `${(-x / w) * 100}%`, top: `${(-y / h) * 100}%` });
    }
    el.append(img);
    const frames: Keyframe[] = boxes.map((box, i) => {
      const sx = box.w / to.w;
      const sy = box.h / to.h;
      const c = Math.max(box.w / iw, box.h / ih); // (real px per base px)
      const ox = (box.w - c * iw) / 2 / sx;
      const oy = (box.h - c * ih) / 2 / sy;
      return { offset: i / steps, transform: `translate(${ox}px, ${oy}px) scale(${c / sx}, ${c / sy})` };
    });
    el.style.transform = frames[0].transform as string;
    frame.append(el);
    return { el, img, frames };
  }
  const main = picture(opts);
  const into = opts.into ? picture(opts.into) : null;
  if (into) into.el.style.opacity = '0';
  frame.style.transform = outer[0].transform as string;
  layer.append(shade, frame);
  // At once, in its start place (closing: where the viewer was). Inside #app,
  // where the viewer is: #app is fixed, so it layers its own content - from
  // the body this would be drawn over the viewer's controls, whatever its z-index.
  (document.getElementById('app') ?? document.body).append(layer);
  // The thumbnail is usually loaded already (it's on the grid): decoded before it
  // moves - but never waited for long (a busy server or a picture not in memory
  // would hold the opening and closing up): it moves anyway, the picture joins it.
  const decoded = [main, into].filter((p) => p !== null).map((p) => p.img.decode().catch(() => {}));
  await Promise.race([Promise.all(decoded), new Promise((r) => setTimeout(r, 80))]);

  const a = frame.animate(outer, { duration, fill: 'forwards' });
  main.el.animate(main.frames, { duration, fill: 'forwards' });
  if (into) {
    into.el.animate(into.frames, { duration, fill: 'forwards' });
    // (it turns into the other picture on the way: mostly while it moves the most)
    into.el.animate([{ opacity: 0 }, { opacity: 0, offset: 0.08 }, { opacity: 1, offset: 0.6 }, { opacity: 1 }], { duration, fill: 'forwards' });
  }
  shade.animate([{ opacity: backdrop[0] }, { opacity: backdrop[1] }], { duration, easing: 'ease-out', fill: 'forwards' });
  return a.finished.then(
    async () => {
      if (opts.hold) await Promise.race([opts.hold, new Promise((r) => setTimeout(r, 800))]);
      if (!opts.fade) return layer.remove();
      // (resolved now: what's under it is shown; the copy fades away over it)
      layer.animate([{ opacity: 1 }, { opacity: 0 }], { duration: opts.fade, easing: 'ease-out', fill: 'forwards' }).finished.then(
        () => layer.remove(),
        () => layer.remove(),
      );
    },
    () => layer.remove(),
  );
}

/** CSS cubic-bezier(x1, y1, x2, y2) at progress t (solved for x by bisection). */
function cubicBezier(x1: number, y1: number, x2: number, y2: number, t: number): number {
  const at = (a: number, b: number, u: number) => 3 * a * u * (1 - u) ** 2 + 3 * b * u * u * (1 - u) + u ** 3;
  let lo = 0;
  let hi = 1;
  for (let i = 0; i < 30; i++) {
    const mid = (lo + hi) / 2;
    if (at(x1, x2, mid) < t) lo = mid;
    else hi = mid;
  }
  return at(y1, y2, (lo + hi) / 2);
}
