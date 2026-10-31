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

/** Sends the start's times (once). */
export function reportStart() {
  if (sent) return;
  sent = true;
  try {
    const nav = performance.getEntriesByType('navigation')[0] as PerformanceNavigationTiming | undefined;
    const res = performance.getEntriesByType('resource') as PerformanceResourceTiming[];
    const path = (r: PerformanceResourceTiming) => new URL(r.name, location.href).pathname;
    const first = (p: string) => res.find((r) => path(r) === p);
    const parts: string[] = [device()];
    if (nav) parts.push(`${nav.type}, page ${one(nav as unknown as PerformanceResourceTiming)}, parsed ${ms(nav.domInteractive)}`);
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
