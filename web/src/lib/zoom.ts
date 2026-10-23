// Zooming a picture (the viewer, the comic reader): a scale and an offset in px from the middle of its box.
// Kept inside the box's edges; zoomed around a point; past the limits a pinch goes on with resistance; a flicked
// pan keeps gliding (like Photos), slowing down, stopping at the edges.

export interface Zoom {
  s: number;
  x: number;
  y: number;
}

/** Keeps a picture (w x h as fitted, at zoom 1) zoomed by z from being dragged away from its box's edges. */
export function clampZoom(z: Zoom, fitted: { w: number; h: number }, box: { w: number; h: number }): Zoom {
  const maxX = Math.max(0, (fitted.w * z.s - box.w) / 2);
  const maxY = Math.max(0, (fitted.h * z.s - box.h) / 2);
  return { s: z.s, x: Math.min(maxX, Math.max(-maxX, z.x)), y: Math.min(maxY, Math.max(-maxY, z.y)) };
}

/** z zoomed to scale s, the point c (from the box's middle) staying where it is. */
export function zoomAround(z: Zoom, s: number, c: { x: number; y: number }): Zoom {
  const k = s / z.s;
  return { s, x: c.x - (c.x - z.x) * k, y: c.y - (c.y - z.y) * k };
}

/** A pinch's scale (raw: as far as the fingers went): below fitted or above max it goes on with resistance (it springs back once let go). */
export function pinchScale(raw: number, max: number): number {
  return raw < 1 ? Math.max(0.55, 1 - (1 - raw) * 0.45) : raw > max ? max + (raw - max) * 0.25 : raw;
}

/** A pan that keeps going after the finger lets go: record() the pointer as it moves, start() on letting go. */
export class Glide {
  private trail: { t: number; x: number; y: number }[] = [];
  private frame = 0;

  record(x: number, y: number) {
    this.trail.push({ t: performance.now(), x, y });
    if (this.trail.length > 8) this.trail.shift();
  }

  stop() {
    cancelAnimationFrame(this.frame);
    this.frame = 0;
    this.trail = [];
  }

  /** From the last 100 ms of the pan: the flick's speed, slowing down; clamp keeps it inside (that way stops at an edge). */
  start(get: () => Zoom, set: (z: Zoom) => void, clamp: (z: Zoom) => Zoom) {
    const now = performance.now();
    const recent = this.trail.filter((p) => now - p.t < 100);
    this.trail = [];
    if (recent.length < 2 || matchMedia('(prefers-reduced-motion: reduce)').matches) return;
    const a = recent[0];
    const b = recent[recent.length - 1];
    const span = Math.max(1, b.t - a.t);
    let vx = (b.x - a.x) / span; // px per ms
    let vy = (b.y - a.y) / span;
    if (Math.hypot(vx, vy) < 0.1) return;
    let last = now;
    const step = (t: number) => {
      const elapsed = t - last; // (slows by time, whatever the frame rate)
      const dt = Math.min(32, elapsed);
      last = t;
      const z = get();
      const want = { s: z.s, x: z.x + vx * dt, y: z.y + vy * dt };
      const next = clamp(want);
      if (next.x !== want.x) vx = 0;
      if (next.y !== want.y) vy = 0;
      set(next);
      const k = Math.pow(0.95, elapsed / 16.7); // friction
      vx *= k;
      vy *= k;
      this.frame = Math.hypot(vx, vy) > 0.03 ? requestAnimationFrame(step) : 0;
    };
    this.frame = requestAnimationFrame(step);
  }
}
