// Viewport fixes, mostly for the iOS home-screen app (iOS is first-class).

/** Whether we run as an installed home-screen / standalone app. */
export const standalone =
  matchMedia('(display-mode: standalone)').matches ||
  (navigator as Navigator & { standalone?: boolean }).standalone === true;

// (iOS: the form bar over the keyboard, see trackKeyboard)
const IOS_FORM_BAR = 60;
const iosBar = /iPhone|iPad|iPod/.test(navigator.userAgent) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1);

/** A box the keyboard comes up for has the focus. */
function typing(): boolean {
  const el = document.activeElement as HTMLElement | null;
  return !!el && (el.isContentEditable || el.matches('textarea, input:not([type=checkbox], [type=radio], [type=button], [type=submit], [type=reset], [type=range], [type=file], [type=color])'));
}

/**
 * Tracks the on-screen keyboard as CSS variables, so sheets and dialogs sit above it:
 * - --kb: the room it takes at the bottom of the page (bottom sheets: `bottom: var(--kb)`);
 * - --vis-h: the height that can be seen above it (dialogs fit in it);
 * - --vv-top: how far the visible part is scrolled down (a dialog in the middle centers in what can be seen).
 * The keyboard shows two ways: Safari shrinks the visual viewport (the page stays as tall: --kb is the difference),
 * and the iPhone's home-screen app can shrink the page itself (the page's bottom is the keyboard's top: --kb 0) -
 * measured against the page's height with no box focused, per width (2026-11-13: only the first was handled, so
 * iOS's form bar was never allowed for in the app).
 */
export function trackKeyboard() {
  const vv = window.visualViewport;
  if (!vv) return;
  const full = new Map<number, number>(); // the page's height with the keyboard down, by width (a turned phone)
  let last = 0;
  const update = () => {
    const editing = typing();
    if (!editing) full.set(innerWidth, innerHeight);
    const shrunk = Math.max(0, (full.get(innerWidth) ?? innerHeight) - innerHeight);
    let kb = Math.max(0, innerHeight - vv.height - vv.offsetTop);
    // iOS's form bar (the ⌃ ⌄ ✓ pill) floats over the page above the keyboard since iOS 26, in neither height: things
    // placed above the keyboard went under it (2026-11-12, the user's screenshot: a site's login dialog's buttons).
    // Its room: ~47 px of bar and ~12 px of gap. (Older iOS, where the bar was part of the keyboard: some to spare.)
    const bar = iosBar && editing && (kb > 40 || shrunk > 120) ? IOS_FORM_BAR : 0;
    kb += bar;
    const root = document.documentElement.style;
    root.setProperty('--kb', `${Math.round(kb)}px`);
    root.setProperty('--vis-h', `${Math.round(vv.height - bar)}px`);
    root.setProperty('--vv-top', `${Math.round(Math.max(0, vv.offsetTop))}px`);
    if (kb > last) requestAnimationFrame(showField); // (the sheet moved up and got shorter: its field may be out of sight)
    last = kb;
  };
  vv.addEventListener('resize', update);
  vv.addEventListener('scroll', update);
  window.addEventListener('resize', update);
  // (the bar comes and goes with the focus; the focus has moved once these have run)
  document.addEventListener('focusin', () => setTimeout(update));
  document.addEventListener('focusout', () => setTimeout(update));
  update();
}

/** The text box being typed in, when it's in a phone sheet or a dialog: scrolled into its visible part. */
function showField() {
  const el = document.activeElement;
  if (el?.matches('input, textarea') && el.closest('[data-sheet], .details-panel, [role="dialog"]')) el.scrollIntoView({ block: 'nearest' });
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
