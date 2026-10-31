// How long the app took to open, in the server's log ("web app start", info) - once per start, when the splash goes
// (2026-10-31, the user: opening is "noticeably slower" on iOS than on desktop; the times come from the device itself,
// so its own side shows: the page, the scripts, the first search, the thumbnails, the service worker). The browser's
// own timings of each request (from the start of the page, ms; "sw" when the service worker answered, "cached" when
// nothing came over the network) and three marks of the app's own: its script running, its first screen drawn, the
// splash gone. Nothing private: paths without their searches, sizes and times.
const marks: Record<string, number> = {};
let sent = false;

/** A moment of the start (script, drawn, splash). */
export function startMark(name: string) {
  marks[name] ??= Math.round(performance.now());
}

const ms = (v: number) => Math.round(v);
const kb = (r: PerformanceResourceTiming) => {
  const n = r.encodedBodySize;
  return !n ? '' : n >= 1024 * 1024 ? `${(n / 1024 / 1024).toFixed(1)} MB` : n >= 1024 ? `${Math.round(n / 1024)} KB` : `${n} B`;
};
const via = (r: PerformanceResourceTiming) => (r.workerStart > 0 ? 'sw' : r.transferSize === 0 && r.encodedBodySize > 0 ? 'cached' : '');

function one(r: PerformanceResourceTiming): string {
  const wait = r.responseStart > 0 && r.requestStart > 0 ? ` wait ${ms(r.responseStart - r.requestStart)}` : '';
  const extra = [kb(r), via(r)].filter(Boolean).join(', ');
  return `${ms(r.startTime)}-${ms(r.responseEnd)}${wait}${extra ? ` (${extra})` : ''}`;
}

function device(): string {
  const ua = navigator.userAgent;
  const kind = /iPhone/.test(ua) ? 'iPhone' : /iPad|Macintosh.*Mobile/.test(ua) ? 'iPad' : /Android/.test(ua) ? 'Android' : 'desktop';
  const app = matchMedia('(display-mode: standalone)').matches || (navigator as { standalone?: boolean }).standalone ? ' app' : '';
  const sw = navigator.serviceWorker?.controller ? ', sw on' : ', sw off';
  return `${kind}${app}${sw}, ${innerWidth}x${innerHeight}`;
}

// How this start came about, when it's one of the unusual ones (2026-10-31, the user: "every now and again it takes
// several seconds ... whether it's when I open via a notification idk"): a reload into a new version (main.ts notes
// when; "on return" when the app came back on screen after a while and found one), or a notification that opened the
// app (the service worker notes it in its own storage, public/sw.js).
async function how(): Promise<string[]> {
  const out: string[] = [];
  try {
    const after = Number(sessionStorage.getItem('vitrine.reloadAfter') ?? NaN);
    sessionStorage.removeItem('vitrine.reloadAfter');
    if (Number.isFinite(after)) out.push(after > 60_000 ? 'reloaded into a new version on return' : `reloaded into a new version ${after} ms into the start before`);
  } catch {
    /* no storage */
  }
  try {
    const cache = await caches.open('vitrine-shell-v2');
    const note = await cache.match('/opened-by-notification');
    if (note) {
      await cache.delete('/opened-by-notification');
      const age = Date.now() - Number(await note.text());
      if (age < 60_000) out.push(`opened from a notification ${Math.round(age / 1000)} s ago`);
    }
  } catch {
    /* no caches */
  }
  return out;
}

// The service worker's side of this start (2026-10-31, an open at 14:18 took 6 s: the page asked for twice, seconds
// apart, each answered in ~1 ms): when it started and what it did with the page's request - asked the server, the
// answer in, the saved copy found, which one the page got and when (ms from the page's start; negative = before it).
function workerSide(): Promise<string> {
  const sw = navigator.serviceWorker;
  if (!sw?.controller) return Promise.resolve('');
  return new Promise((done) => {
    const rel = (t: number) => (t ? `${ms(Math.abs(t) - performance.timeOrigin)}${t < 0 ? ' failed' : ''}` : '-');
    const on = (e: MessageEvent) => {
      if (e.data?.type !== 'start-timing') return;
      clearTimeout(timer);
      sw.removeEventListener('message', on);
      const p = e.data.page;
      const page = p ? `, page asked ${rel(p.event)}, server ${rel(p.net)}, saved ${rel(p.saved)}, gave ${p.served || 'nothing yet'} ${rel(p.servedAt)}` : ', no page request';
      done(`sw started ${rel(e.data.swStarted)}${page}`);
    };
    const timer = setTimeout(() => {
      sw.removeEventListener('message', on);
      done('sw: no answer in 2 s');
    }, 2000);
    sw.addEventListener('message', on);
    sw.controller!.postMessage({ type: 'start-timing' });
  });
}

const clock = (t: number) => new Date(t).toISOString().slice(11, 23); // (UTC, like the proxy's log)

/** Sends the start's times (once). */
export async function reportStart() {
  if (sent) return;
  sent = true;
  try {
    const nav = performance.getEntriesByType('navigation')[0] as PerformanceNavigationTiming | undefined;
    const res = performance.getEntriesByType('resource') as PerformanceResourceTiming[];
    const path = (r: PerformanceResourceTiming) => new URL(r.name, location.href).pathname;
    const first = (p: string) => res.find((r) => path(r) === p);
    const parts: string[] = [`started ${clock(performance.timeOrigin)}`, device(), ...(await how())];
    if (nav) parts.push(`${nav.type}, worker ${ms(nav.workerStart)}, page ${one(nav as unknown as PerformanceResourceTiming)}, parsed ${ms(nav.domInteractive)}`);
    const sw = await workerSide();
    if (sw) parts.push(sw);
    const assets = res.filter((r) => path(r).startsWith('/assets/'));
    if (assets.length) parts.push(`assets ${assets.length} by ${ms(Math.max(...assets.map((r) => r.responseEnd)))}${assets.some((r) => r.workerStart > 0) ? ' (sw)' : ''}`);
    for (const k of ['script', 'drawn']) if (marks[k] !== undefined) parts.push(`${k} ${marks[k]}`);
    for (const p of ['/api/session', '/api/settings', '/api/search', '/api/files', '/api/sites', '/api/namespaces', '/api/notices']) {
      const r = first(p);
      if (r) parts.push(`${p.slice(5)} ${one(r)}`);
    }
    const thumbs = res.filter((r) => path(r).startsWith('/thumbs/'));
    if (thumbs.length) {
      const ends = thumbs.map((r) => r.responseEnd).sort((a, b) => a - b);
      const waits = thumbs.map((r) => r.responseEnd - r.startTime).sort((a, b) => a - b);
      parts.push(
        `thumbs ${thumbs.length}: from ${ms(Math.min(...thumbs.map((r) => r.startTime)))}, first ${ms(ends[0])}, last ${ms(ends.at(-1)!)}, each ${ms(waits[waits.length >> 1])} typical (${thumbs.filter((r) => r.workerStart > 0).length} via sw)`,
      );
    }
    if (marks.splash !== undefined) parts.push(`splash gone ${marks.splash}`);
    fetch('/api/log', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ kind: 'start', message: parts.join(' | '), where: location.pathname }),
      keepalive: true,
    }).catch(() => {});
  } catch {
    /* no report this time */
  }
}

/** The app came back on screen after a while (the home-screen app usually resumes rather than starting again). */
export function reportResume(hiddenMs: number) {
  try {
    fetch('/api/log', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ kind: 'start', message: `resumed ${clock(Date.now())} after ${Math.round(hiddenMs / 1000)} s away, ${device()}`, where: location.pathname }),
      keepalive: true,
    }).catch(() => {});
  } catch {
    /* no report this time */
  }
}
