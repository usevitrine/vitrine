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

// page: the saved page at once (every path of the app is the same page), and
// the server's copy fetched meanwhile for next time; when it is a newer
// version, the open app is told (it reloads if it has only just started).
async function page(req, e) {
  const cache = await caches.open(SHELL);
  const hit = await cache.match('/');
  const fresh = fetch(req).then(async (resp) => {
    // (a page the server says not to keep - the plain sign-in page, signed out - isn't saved as the app)
    if (resp.ok && resp.status === 200 && !(resp.headers.get('Cache-Control') ?? '').includes('no-store')) {
      const text = await resp.clone().text();
      const old = hit ? await hit.clone().text() : null;
      await cache.put('/', resp.clone());
      if (old !== null && old !== text) {
        for (const c of await self.clients.matchAll({ type: 'window' })) c.postMessage({ type: 'updated' });
      }
    }
    return resp;
  });
  if (hit) {
    e.waitUntil(fresh.catch(() => {}));
    return hit;
  }
  return fresh;
}

async function cacheFirst(req, e, name, max) {
  const cache = await caches.open(name);
  const hit = await cache.match(req);
  if (hit) return hit;
  const resp = await fetch(req);
  if (resp.ok && resp.status === 200) {
    e.waitUntil(
      cache.put(req, resp.clone()).then(() => {
        if (++puts % 200 === 0) return trim(name, max);
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
  const cache = await caches.open(API);
  const key = apiKey(req);
  try {
    const resp = await fetch(req);
    if (resp.ok) {
      e.waitUntil(
        cache.put(key, resp.clone()).then(() => {
          if (++puts % 50 === 0) return trim(API, MAX_API);
        }),
      );
    }
    return resp;
  } catch (err) {
    const hit = await cache.match(key);
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

self.addEventListener('notificationclick', (e) => {
  e.notification.close();
  const url = new URL((e.notification.data && e.notification.data.url) || '/', self.location.origin);
  e.waitUntil(
    (async () => {
      const wins = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
      const win = wins.find((w) => new URL(w.url).origin === url.origin);
      if (win) {
        await win.focus();
        win.postMessage({ type: 'open', path: url.pathname + url.search });
        return;
      }
      await self.clients.openWindow(url.href);
    })(),
  );
});
