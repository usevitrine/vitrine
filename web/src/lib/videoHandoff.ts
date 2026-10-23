// The hover card's video handed to the viewer opened from it: the viewer's carries on from where the card's was
// (and the card's is let go at once - two copies of a big video decoding at once made the opening stutter).
let kept: { id: number; t: number; at: number } | null = null;

export const handoff = {
  /** The card's video, at t seconds, as the viewer opens on file id. */
  give(id: number, t: number) {
    kept = { id, t, at: performance.now() };
  },
  /** Where the viewer's video for file id starts (0: from the start - nothing handed over, or long ago). */
  take(id: number): number {
    const k = kept;
    kept = null;
    return k && k.id === id && performance.now() - k.at < 5000 ? k.t : 0;
  },
};

/** A video element let go of when it leaves the page (paused, its file dropped: the decoder freed now, not later). */
export function release(v: HTMLVideoElement) {
  return () => {
    v.pause();
    v.removeAttribute('src');
    v.load();
  };
}
