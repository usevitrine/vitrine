import { mount } from 'svelte';
import '@fontsource-variable/plus-jakarta-sans';
import './app.css';
import App from './App.svelte';
import { toasts } from './lib/toast.svelte';
import { trackKeyboard } from './lib/viewport';
import { registerServiceWorker } from './lib/push.svelte';
import { logErrors } from './lib/errorlog';
import { install } from './lib/install.svelte';
import { t } from './lib/i18n.svelte';
import { quietKeyboard } from './lib/privacy';
import { refreshShell } from './lib/appShell';
import { ownTouches } from './lib/ownTouches';
import { DEMO } from './lib/demo';
import { startDemo } from './demo/backend'; // (left out of the app's build: DEMO is false there)

logErrors();
ownTouches(); // (the app's own swipes: the browser takes no fling from them - lib/ownTouches)
trackKeyboard();
// The website's demo: its pretend server first (src/demo); no service worker, install offer or version checks.
if (DEMO) startDemo();
else {
  registerServiceWorker();
  install.start(); // (the browser's install offer comes early)
}

mount(App, { target: document.getElementById('app')! });
quietKeyboard(); // (text fields: the keyboard doesn't suggest or learn what's typed)

// The splash in index.html goes once the app shows something of its own - on
// the library, once the thumbnails on screen are in (else their gray tiles
// flashed between the splash and the pictures), but never later than 1.5 s
// after the app drew. Then it fades out (no hard cut).
const splash = document.getElementById('splash');
if (splash) {
  let drawnAt = 0;
  const shown = () => !!document.querySelector('#app > .shell, #app > .wrap, #app > .center, #app > .first');
  const thumbsIn = () => {
    const grid = document.getElementById('grid-scroller');
    if (!grid || grid.closest('[hidden]')) return true; // another screen, or the library's welcome
    const box = grid.getBoundingClientRect();
    const tiles = [...grid.querySelectorAll<HTMLElement>('.tile')].filter((t) => {
      const r = t.getBoundingClientRect();
      return r.bottom > box.top && r.top < box.bottom;
    });
    if (!tiles.length) return false; // the first search is still running
    return tiles.every((t) => t.querySelector('img.in') || t.querySelector('.making'));
  };
  const done = () => {
    if (!shown()) return false;
    drawnAt ||= performance.now();
    if (!thumbsIn() && performance.now() - drawnAt < 1500) return false;
    splash.style.transition = 'opacity 0.25s ease-out';
    splash.style.opacity = '0';
    setTimeout(() => splash.remove(), 260);
    return true;
  };
  if (!done()) {
    const mo = new MutationObserver(() => done() && stop());
    const timer = setInterval(() => done() && stop(), 100); // (the 1.5 s cap, with nothing changing)
    const stop = () => {
      mo.disconnect();
      clearInterval(timer);
    };
    mo.observe(document.getElementById('app')!, { childList: true, subtree: true, attributes: true, attributeFilter: ['class'] });
  }
}

// A newer version of the app arrived while starting (the saved page was shown
// first): take it now, before anything has happened on the page.
const started = Date.now();

/** Reloads into the new version - but not again within half a minute (whatever
 *  goes wrong, never a reload loop: Ctrl+R used to keep reloading). */
function reloadOnce() {
  try {
    const last = Number(sessionStorage.getItem('vitrine.versionReload') ?? 0);
    if (Date.now() - last < 30_000) return;
    sessionStorage.setItem('vitrine.versionReload', String(Date.now()));
  } catch {
    return; // (no storage: no automatic reload either)
  }
  location.reload();
}
navigator.serviceWorker?.addEventListener('message', (e) => {
  if (e.data?.type !== 'updated') return;
  if (Date.now() - started < 4000) reloadOnce();
  else toasts.show(t('app.newVersion'), { cat: 'update', kind: 'info', action: { label: t('app.reload'), run: () => location.reload() }, ms: 15000 });
});

// The page checks the server's version itself, at every start and when it
// comes back on screen after a minute or more (the home-screen app usually
// resumes instead of starting again). The message above can get lost: it may
// be sent while the new page doesn't exist yet. The server's page always
// replaces the saved copy (a hard reload skips the service worker and would
// leave the old one there); when it's a newer version, the app reloads into
// it at once.
const ownScript = document.querySelector<HTMLScriptElement>('script[type="module"][src]')?.getAttribute('src') ?? '';
async function checkVersion() {
  if (!ownScript.startsWith('/assets/')) return; // (not in development)
  try {
    const src = await refreshShell();
    if (src && src !== ownScript) reloadOnce();
  } catch {
    // Offline: checked again next time.
  }
}
if (!DEMO) checkVersion();
let hiddenAt = 0;
document.addEventListener('visibilitychange', () => {
  if (document.visibilityState === 'hidden') hiddenAt = Date.now();
  else if (hiddenAt && Date.now() - hiddenAt >= 60_000) checkVersion();
});
