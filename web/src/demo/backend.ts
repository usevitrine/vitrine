// The website's demo: a pretend server in the page. The app asks /api/... as ever; this answers in the server's own
// shapes - searches, suggestions, tag edits, favorites and deletes from this program's index and query parser
// compiled to WebAssembly (cmd/demowasm, with the library from tools/demosnap), the rest with what a library of only
// those files would say. Nothing leaves the page and nothing is kept: a reload starts again, and no one else sees
// what a visitor changed.

type Wasm = Record<string, (...args: unknown[]) => string>;

declare global {
  interface Window {
    vitrineDemo?: Wasm;
    Go?: new () => { importObject: WebAssembly.Imports; run(i: WebAssembly.Instance): Promise<void> };
  }
}

import { demoIcons } from '../lib/demo';

const base = import.meta.env.BASE_URL; // (the demo's folder: its media, snapshot and engine are beside it)

// The shared settings as a fresh demo library has them: the first steps done, safe for work (no "I came").
const settings: Record<string, unknown> = { welcome: 'finished', sfwMode: true, cameButton: false, updateCheck: false, hoverPreview: true };

let engine: Wasm;

const site = (id: string, name: string, address: string) => ({
  id, name, site: address, version: '', engine: '', host: address, built_in: false, tag_types: false, fields: [], required: false,
});
const demoSites = [site('artic', 'Art Institute of Chicago', 'artic.edu'), site('blender', 'Blender Studio', 'blender.org')];

/** Answers the app's requests from now on (at once, before the app starts: they wait for the engine and the library,
 *  loading meanwhile). */
export function startDemo() {
  // No live updates (nothing changes on its own here): the app checks EventSource exists before connecting.
  (window as { EventSource?: unknown }).EventSource = undefined;
  installFetch(load());
}

async function load() {
  await loadScript(base + 'wasm_exec.js');
  const go = new window.Go!();
  const ready = new Promise((r) => addEventListener('vitrine-demo-ready', r, { once: true }));
  const [{ instance }, snapshot] = await Promise.all([
    WebAssembly.instantiateStreaming(fetch(base + 'demo.wasm'), go.importObject),
    fetch(base + 'snapshot.json').then((r) => r.text()),
  ]);
  void go.run(instance);
  await ready;
  engine = window.vitrineDemo!;
  check(engine.load(snapshot));
  for (const h of (JSON.parse(snapshot).icons as string[]) ?? []) demoIcons.add(h);
}

function loadScript(src: string) {
  return new Promise<void>((resolve, reject) => {
    const s = document.createElement('script');
    s.src = src;
    s.onload = () => resolve();
    s.onerror = () => reject(new Error('the demo engine did not load'));
    document.head.appendChild(s);
  });
}

function check(json: string) {
  const v = JSON.parse(json);
  if (v && v.error) throw new Error(v.error);
  return v;
}

const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

/** A file's media: in the demo's folder. */
function withMedia(files: { url: string; thumb?: string }[]) {
  for (const f of files) {
    f.url = base + f.url;
    if (f.thumb) f.thumb = base + f.thumb;
  }
  return files;
}

function installFetch(ready: Promise<void>) {
  const real = window.fetch.bind(window);
  window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
    const u = new URL(typeof input === 'string' ? input : input instanceof URL ? input.href : input.url, location.href);
    if (u.origin !== location.origin || !u.pathname.startsWith('/api/')) return real(input, init);
    await ready;
    const method = (init?.method ?? (input instanceof Request ? input.method : 'GET')).toUpperCase();
    let body: Record<string, unknown> = {};
    try {
      if (typeof init?.body === 'string') body = JSON.parse(init.body);
    } catch {}
    try {
      return answer(method, u.pathname, u.searchParams, body);
    } catch (e) {
      return json({ error: (e as Error).message }, 400);
    }
  };
}

const s = (v: unknown) => JSON.stringify(v ?? []);

// Saved searches (the star in the search bar): kept for the visit, each with how many files it finds now.
const saved: { id: number; name: string; query: string; sort: string }[] = [];
let savedID = 0;
const savedList = () =>
  saved.map((x) => ({ ...x, count: check(engine.search(s({ q: x.query + (x.sort ? ' sort:' + x.sort : ''), limit: 0 }))).total as number }));

function answer(method: string, path: string, q: URLSearchParams, body: Record<string, unknown>): Response {
  const r = (fn: string, ...args: unknown[]) => json(check(engine[fn](...args)));
  if (method === 'GET') {
    switch (path) {
      case '/api/health':
        return json({ ok: true });
      case '/api/session':
        return json({ setup_needed: false, logged_in: true, user: 'demo', language: 'en-US', anonymous: false });
      case '/api/settings':
        return json(settings);
      case '/api/search':
        return r('search', s({ q: q.get('q') ?? '', limit: Number(q.get('limit') ?? 200), shapes: q.get('shapes') === '1', find: q.get('find') ?? '' }));
      case '/api/files': {
        const out = check(engine.files(s((q.get('ids') ?? '').split(',').filter(Boolean).map(Number))));
        withMedia(out.files);
        return json(out);
      }
      case '/api/autocomplete':
        return r('autocomplete', q.get('q') ?? '', Number(q.get('limit') ?? 20));
      case '/api/namespaces':
        return r('namespaces');
      case '/api/relations':
        return json({ parents: [], siblings: [] });
      case '/api/sites': // (where the demo's files came from: their addresses, as the details name a file's site)
        return json({ sites: demoSites });
      case '/api/search/sites':
        return json({ sites: [] });
      case '/api/new':
        return json({ new: 0 });
      case '/api/blacklist':
        return json({ tags: [] });
      case '/api/news':
        return json({ new: 0, since: 0 });
      case '/api/notices':
        return json({ boot: 'demo', notices: [] });
      case '/api/sections':
        return json({ media: { items: 0, bytes: 0, subscriptions: 0, downloads: 0 }, comics: { items: 0, bytes: 0, subscriptions: 0, downloads: 0 } });
      case '/api/saved':
        return json({ saved: savedList() });
    }
    let m = path.match(/^\/api\/results\/([^/]+)$/);
    if (m) return r('slice', m[1], Number(q.get('offset') ?? 0), Number(q.get('limit') ?? 200));
    m = path.match(/^\/api\/results\/([^/]+)\/dates$/);
    if (m) return r('dates', m[1], Number(q.get('n') ?? 200), q.get('field') === 'uploaded');
    m = path.match(/^\/api\/files\/(\d+)\/tags$/);
    if (m) return json(check(engine.details(Number(m[1]))));
    // A video's preview frames for scrubbing: none in the demo (the server's answer while they're not made yet).
    if (/^\/api\/files\/\d+\/sprite$/.test(path)) return json({ error: 'no preview frames yet' }, 404);
  } else {
    switch (path) {
      case '/api/settings':
        Object.assign(settings, body);
        return json(settings);
      case '/api/tags':
        return r('editTags', s(body.ids), s(body.add), s(body.remove));
      case '/api/tags/of':
        return r('tagsOf', s(body.ids));
      case '/api/favorite':
        return r('favorite', s(body.ids), !!body.on);
      case '/api/delete':
        return r('remove', s(body.ids));
      case '/api/undelete':
        return r('restore', s(body.ids));
      case '/api/saved': {
        const id = ++savedID;
        saved.push({ id, name: String(body.name ?? ''), query: String(body.query ?? ''), sort: String(body.sort ?? '') });
        return json({ id, saved: savedList() });
      }
      case '/api/saved/reorder': {
        const order = (body.ids as number[]) ?? [];
        saved.sort((a, b) => order.indexOf(a.id) - order.indexOf(b.id));
        return json({ saved: savedList() });
      }
      case '/api/log': // (the app's error reports: the console has them)
        return json({ ok: true });
      case '/api/seen':
        return json({ previous: 0 });
    }
  }
  const m = method === 'POST' && path.match(/^\/api\/saved\/(\d+)(\/remove)?$/);
  if (m) {
    const i = saved.findIndex((x) => x.id === Number(m[1]));
    if (i >= 0 && m[2]) saved.splice(i, 1);
    else if (i >= 0) Object.assign(saved[i], body);
    return json({ saved: savedList() });
  }
  console.warn('demo: no answer for', method, path); // (a call the media screen doesn't need: nothing to find)
  return json({ error: 'not in the demo' }, 404);
}
