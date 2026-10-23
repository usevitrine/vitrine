// The viewer full screen (the browser's own bars gone; our player, controls and
// gestures stay) - wherever the browser lets a page do that: desktops, Android,
// iPads. An iPhone's Safari only lets Apple's player go full screen, so there's
// no button there (the home-screen app fills the screen anyway).
type Doc = Document & { webkitFullscreenEnabled?: boolean; webkitFullscreenElement?: Element; webkitExitFullscreen?: () => void };
type El = HTMLElement & { webkitRequestFullscreen?: () => void };

class Fullscreen {
  on = $state(false);
  readonly supported = typeof document !== 'undefined' && !!(document.fullscreenEnabled || (document as Doc).webkitFullscreenEnabled);

  constructor() {
    if (typeof document === 'undefined') return;
    const sync = () => (this.on = !!(document.fullscreenElement || (document as Doc).webkitFullscreenElement));
    document.addEventListener('fullscreenchange', sync);
    document.addEventListener('webkitfullscreenchange', sync);
  }

  enter(el: Element | null) {
    const e = el as El | null;
    if (!e) return;
    if (e.requestFullscreen) e.requestFullscreen().catch(() => {});
    else e.webkitRequestFullscreen?.();
  }

  exit() {
    if (!this.on) return;
    const d = document as Doc;
    if (document.exitFullscreen) document.exitFullscreen().catch(() => {});
    else d.webkitExitFullscreen?.();
  }

  toggle(el: Element | null) {
    if (this.on) this.exit();
    else this.enter(el);
  }
}

export const fullscreen = new Fullscreen();
