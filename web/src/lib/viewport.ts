// Viewport fixes, mostly for the iOS home-screen app (iOS is first-class).

/** Whether we run as an installed home-screen / standalone app. */
export const standalone =
  matchMedia('(display-mode: standalone)').matches ||
  (navigator as Navigator & { standalone?: boolean }).standalone === true;

/**
 * Tracks the on-screen keyboard's height as the CSS variable --kb, so
 * bottom sheets can sit above it (iOS shrinks the visual viewport, not the
 * layout viewport, when the keyboard opens), and how far the visible part is
 * scrolled down as --vv-top: a dialog in the middle centers between the two
 * (in the middle of the screen it opened behind the keyboard).
 */
export function trackKeyboard() {
  const vv = window.visualViewport;
  if (!vv) return;
  const update = () => {
    const kb = Math.max(0, window.innerHeight - vv.height - vv.offsetTop);
    document.documentElement.style.setProperty('--kb', `${Math.round(kb)}px`);
    document.documentElement.style.setProperty('--vv-top', `${Math.round(Math.max(0, vv.offsetTop))}px`);
  };
  vv.addEventListener('resize', update);
  vv.addEventListener('scroll', update);
  update();
}

/** Numbers for the Display section in Settings (to diagnose iOS layout issues). */
export function displayInfo() {
  const probe = document.createElement('div');
  probe.style.cssText =
    'position:fixed;visibility:hidden;padding:env(safe-area-inset-top) env(safe-area-inset-right) env(safe-area-inset-bottom) env(safe-area-inset-left)';
  document.body.append(probe);
  const cs = getComputedStyle(probe);
  const safe = [cs.paddingTop, cs.paddingRight, cs.paddingBottom, cs.paddingLeft].map((v) => parseFloat(v) || 0);
  probe.remove();
  const app = document.getElementById('app')?.getBoundingClientRect();
  return {
    window: `${innerWidth}×${innerHeight}`,
    screen: `${screen.width}×${screen.height}`,
    visual: window.visualViewport ? `${Math.round(visualViewport!.width)}×${Math.round(visualViewport!.height)}` : '-',
    app: app ? `${Math.round(app.width)}×${Math.round(app.height)}` : '-',
    safe: `top ${safe[0]}, right ${safe[1]}, bottom ${safe[2]}, left ${safe[3]}`,
    ratio: String(devicePixelRatio),
    standalone,
  };
}

// Marks the home-screen app, for CSS that applies only there.
if (standalone) document.documentElement.classList.add('standalone');
