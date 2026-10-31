// Vitrine's service worker:
// - keeps thumbnails (and scrub-bar frames) on the device, so a grid seen before
//   fills in even with no connection (the newest ~15,000 kept). Fetched first -
//   the browser's own cache answers a thumbnail seen before at once - and the
//   saved copy used only when that fails: the saved copies' store does one thing
//   at a time, so looking each one up there first made a screenful wait in line
//   and arrive all together;
// - keeps the last answers of the library's API reads, used only when the
//   server can't be reached (marked with an X-Vitrine-Offline header, so the
//   app can say it's offline);
// - keeps the app itself (the page and its built files), so it opens without a
//   connection (the page network first, built files cache first);
// - shows push notifications and opens the app when one is tapped.
// Originals (/files/) are never stored: they're big and stream from the server.

const THUMBS = 'vitrine-thumbs-v1';
const API = 'vitrine-api-v1';
const SHELL = 'vitrine-shell-v2'; // (v2: the new icons - icons are kept for good, so a new name)
const MAX_SHELL = 80;
// The app's own files outside /assets/ (icons, the web app manifest).
const SHELL_FILES = /^\/(icon[^/]*\.(png|svg)|manifest\.webmanifest)$/;
const MAX_THUMBS = 15000;
const MAX_API = 400;
// API reads worth keeping for offline use (GET only; never logins or changes).
const API_READS = /^\/api\/(session|search|results\/|files(\?|$)|files\/\d+\/tags|settings|sites|namespaces|saved|search\/sites)/;

self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', (e) =>
  e.waitUntil(
    (async () => {
      for (const k of await caches.keys()) {
        if (k.startsWith('vitrine-') && k !== THUMBS && k !== API && k !== SHELL) await caches.delete(k);
      }
      await self.clients.claim();
    })(),
  ),
);

self.addEventListener('fetch', (e) => {
  const req = e.request;
  if (req.method !== 'GET') return;
  const url = new URL(req.url);
  if (url.origin !== self.location.origin) return;
  if (url.pathname === '/signin') return; // (the plain sign-in page: always the server's, never kept)
  if (req.mode === 'navigate') {
    e.respondWith(page(req, e));
  } else if (url.pathname.startsWith('/assets/') || SHELL_FILES.test(url.pathname)) {
    e.respondWith(cacheFirst(req, e, SHELL, MAX_SHELL));
  } else if (url.pathname.startsWith('/thumbs/') || url.pathname.startsWith('/sprites/')) {
    e.respondWith(thumb(req, e));
  } else if (API_READS.test(url.pathname + url.search)) {
    e.respondWith(networkFirst(req, e));
  }
});

let puts = 0;

// page: the server's page when it comes within a moment (every path of the app is the same page), else the saved
// one - and the server's kept for next time; when that is a newer version, the open app is told (it reloads if it
// has only just started). (Until 2026-10-31 the saved page always came first: the first start after an update ran
// the old version, found the new one and started again - two starts, the second fetching the new scripts; the user:
// "every now and again it takes several seconds". The server's page takes some 10-50 ms at home.)
// The server's page goes to the page the moment it arrives; comparing it with the saved one and saving it come after.
// (Until 2026-10-31 the page waited for those too, and on an iPhone the saved pages' store took longer than the wait:
// every start waited the full 700 ms and then got the saved copy anyway - measured from the proxy's log, the page read
// 0.8-1.3 s after its request instead of 0.0-0.4 s.)
const PAGE_WAIT = 700;

async function page(req, e) {
  const net = fetch(req); // (asked first: the saved pages' store isn't waited for)
  const saved = caches
    .open(SHELL)
    .then((c) => c.match('/'))
    .catch(() => undefined);
  let decide;
  const gotSaved = new Promise((r) => (decide = r)); // (whether the page got the saved copy)
  e.waitUntil(net.then((resp) => keepPage(resp.clone(), gotSaved), () => {}));
  const quick = await Promise.race([net.then((r) => (r.ok ? r : null), () => null), new Promise((r) => setTimeout(() => r(null), PAGE_WAIT))]);
  if (quick) {
    decide(false);
    return quick;
  }
  const hit = await saved;
  decide(!!hit);
  return hit ?? net; // (nothing saved: the server's answer whenever it comes, or its error)
}

// keepPage saves the server's page as the app and, when the page got the saved copy and this one differs (a new
// version), tells the open app.
async function keepPage(resp, gotSaved) {
  // (a page the server says not to keep - the plain sign-in page, signed out - isn't saved as the app)
  if (!resp.ok || resp.status !== 200 || (resp.headers.get('Cache-Control') ?? '').includes('no-store')) return;
  const text = await resp.clone().text();
  const cache = await caches.open(SHELL);
  const old = await cache.match('/').then((r) => (r ? r.text() : null));
  await cache.put('/', resp);
  // (told only when the page got the saved copy: one that got this one is the new version already)
  if ((await gotSaved) && old !== null && old !== text) {
    for (const c of await self.clients.matchAll({ type: 'window' })) c.postMessage({ type: 'updated' });
  }
}

// cacheFirst: the saved copy - unless the saved copies' store doesn't answer within a moment, then the server's (the
// built files never change under their name, so either will do). (2026-10-31: on an iPhone a worker that had just
// started took 1.4 s to open its store, and the app's scripts waited for it - every start from a notification.)
const SAVED_WAIT = 250;

async function cacheFirst(req, e, name, max) {
  const lookup = caches
    .open(name)
    .then(async (cache) => [cache, await cache.match(req)])
    .catch(() => [null, undefined]);
  const quick = await Promise.race([lookup, new Promise((r) => setTimeout(() => r(null), SAVED_WAIT))]);
  if (quick?.[1]) return quick[1];
  let resp;
  try {
    resp = await fetch(req);
  } catch (err) {
    const [, hit] = await lookup; // (no connection: the saved copy whenever the store answers)
    if (hit) return hit;
    throw err;
  }
  if (resp.ok && resp.status === 200) {
    const copy = resp.clone();
    e.waitUntil(
      lookup.then(([cache, hit]) => {
        if (!cache || hit) return;
        return cache.put(req, copy).then(() => {
          if (++puts % 200 === 0) return trim(name, max);
        });
      }),
    );
  }
  return resp;
}

// A thumbnail (or scrub-bar frames): the network (and so the browser's cache)
// first, the saved copy when that fails; saved in the background for offline use.
async function thumb(req, e) {
  try {
    const resp = await fetch(req);
    if (resp.ok && resp.status === 200 && !kept.has(req.url)) e.waitUntil(save(req, resp.clone()));
    return resp;
  } catch (err) {
    const hit = await (await caches.open(THUMBS)).match(req);
    if (hit) return hit;
    throw err;
  }
}

// Keeps a thumbnail for offline use (once), now and then dropping the oldest. Saved in batches, a moment later:
// a fast scroll through a big library fetched hundreds, and looking each one up in the store (one thing at a time)
// and saving it there at once kept the worker busy while the screen waited for its thumbnails. The ones saved since
// the worker started are known here (kept) and skipped; put() replaces one saved before that.
const kept = new Set();
let saving = [];
let waiting = []; // (each fetch's waitUntil: done once its batch is saved)
let savingTimer = 0;
let thumbPuts = 0;
function save(req, resp) {
  kept.add(req.url);
  if (kept.size > 20000) kept.clear(); // (a long session: start over - a few saved twice)
  saving.push([req, resp]);
  const p = new Promise((done) => waiting.push(done));
  clearTimeout(savingTimer);
  if (saving.length >= 100) flushSaves(); // (a long scroll: not hundreds held)
  else savingTimer = setTimeout(flushSaves, 1500);
  return p;
}
async function flushSaves() {
  const batch = saving;
  const done = waiting;
  saving = [];
  waiting = [];
  try {
    const cache = await caches.open(THUMBS);
    for (const [q, p] of batch) {
      try {
        await cache.put(q, p);
      } catch {
        /* (full: trimmed below) */
      }
    }
    thumbPuts += batch.length;
    if (thumbPuts >= 200) {
      thumbPuts = 0;
      await trim(THUMBS, MAX_THUMBS);
    }
  } finally {
    done.forEach((d) => d());
  }
}

// The key a read is kept under: a search without its "new files" parameters
// (they change whenever files are marked as seen - the saved copy still counts).
function apiKey(req) {
  const url = new URL(req.url);
  if (url.pathname !== '/api/search') return req;
  for (const k of ['new_since', 'pin', 'new_sort']) url.searchParams.delete(k);
  return url.href;
}

async function networkFirst(req, e) {
  const key = apiKey(req);
  try {
    const resp = await fetch(req); // (the store isn't waited for first: see cacheFirst)
    if (resp.ok) {
      const copy = resp.clone();
      e.waitUntil(
        caches
          .open(API)
          .then((cache) => cache.put(key, copy))
          .then(() => {
            if (++puts % 50 === 0) return trim(API, MAX_API);
          }),
      );
    }
    return resp;
  } catch (err) {
    const hit = await (await caches.open(API)).match(key);
    if (!hit) throw err;
    const headers = new Headers(hit.headers);
    headers.set('X-Vitrine-Offline', '1');
    return new Response(await hit.blob(), { status: hit.status, statusText: hit.statusText, headers });
  }
}

// trim drops the oldest entries over the limit (keys come back oldest first),
// all at once rather than one after another.
async function trim(name, max) {
  const cache = await caches.open(name);
  const keys = await cache.keys();
  if (keys.length > max) await Promise.all(keys.slice(0, keys.length - max).map((k) => cache.delete(k)));
}

self.addEventListener('message', (e) => {
  const d = e.data || {};
  if (d.type === 'logout') {
    // Logged out: nothing of the library stays on the device.
    e.waitUntil(Promise.all([caches.delete(THUMBS), caches.delete(API), caches.delete(SHELL)]));
  } else if (d.type === 'clear-thumbs') {
    e.waitUntil(caches.delete(THUMBS));
  }
});

self.addEventListener('push', (e) => {
  let d = {};
  try {
    d = e.data ? e.data.json() : {};
  } catch {
    d = { body: e.data ? e.data.text() : '' };
  }
  e.waitUntil(
    Promise.all([
      self.registration.showNotification(d.title || 'Vitrine', {
        body: d.body || '',
        tag: d.tag || undefined,
        icon: '/icon-192.png?v=2',
        badge: '/badge-96.png', // Android's status bar (one color)
        data: { url: d.url || '/' },
      }),
      updateBadge(),
    ]),
  );
});

// The app icon's count of new files (since "Mark all as seen"), where the
// browser supports it (iPhone home-screen apps, installed desktop apps).
async function updateBadge() {
  if (!self.navigator.setAppBadge) return;
  try {
    const settings = await (await fetch('/api/settings')).json();
    if (!settings.seenAt) return;
    const bulk = settings.newFromBulk === false ? '&new_bulk=0' : '';
    const { new: n } = await (await fetch('/api/new?q=&since=' + settings.seenAt + bulk)).json();
    if (n > 0) await self.navigator.setAppBadge(n);
    else await self.navigator.clearAppBadge();
  } catch {
    /* no badge this time */
  }
}

// A tapped notification: the open app goes to its page; with none open, a window opens there. Safari (an iPhone's
// home-screen app) opens the app by itself when a notification is tapped, and that window isn't among the clients yet
// when this runs - so there the page to open is left as a note (OPEN_NEXT) the starting app reads (router.svelte.ts),
// and a window is opened only when none has appeared after a while. (Until 2026-10-31 a window was opened at once:
// the app started twice side by side, the second seconds later; the user: "It felt a bit slower than usual".)
const OPEN_NEXT = '/open-next';
// (Apple's WebKit: an iPhone's service worker says "AppleWebKit/605.1.15 (KHTML, like Gecko)" and no more - no "Safari")
const SAFARI = /AppleWebKit/.test(self.navigator.userAgent) && !/Chrome|Chromium|CriOS|FxiOS|EdgiOS|Android/.test(self.navigator.userAgent);

async function openWin(path) {
  const wins = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
  const win = wins.find((w) => new URL(w.url).origin === self.location.origin);
  if (!win) return false;
  try {
    await win.focus();
  } catch {
    /* (not allowed this late: it's on screen anyway) */
  }
  win.postMessage({ type: 'open', path });
  return true;
}

self.addEventListener('notificationclick', (e) => {
  e.notification.close();
  const url = new URL((e.notification.data && e.notification.data.url) || '/', self.location.origin);
  const path = url.pathname + url.search;
  e.waitUntil(
    (async () => {
      if (await openWin(path)) return;
      const shell = await caches.open(SHELL).catch(() => null);
      if (SAFARI && shell) {
        await shell.put(OPEN_NEXT, new Response(JSON.stringify({ path, at: Date.now() }))).catch(() => {});
        for (let i = 0; i < 40; i++) {
          await new Promise((r) => setTimeout(r, 100));
          if (!(await shell.match(OPEN_NEXT))) return; // (the starting app took it)
          const wins = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
          // (a window that has shown up: told, unless it took the note meanwhile - whoever deletes it acts)
          if (wins.length && (await shell.delete(OPEN_NEXT))) return void (await openWin(path));
        }
        if (!(await shell.delete(OPEN_NEXT))) return;
      }
      await self.clients.openWindow(url.href);
    })(),
  );
});
