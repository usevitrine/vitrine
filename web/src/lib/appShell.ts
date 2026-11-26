// The app's page as the service worker keeps it. The worker answers with its saved copy first (fast, offline), so after
// the server changed version a plain reload would show the old app once more; this puts the server's page in its place.

/** Fetches the server's page, saves it as the worker's copy and returns its script's address ("" if it has none). */
export async function refreshShell(): Promise<string> {
  const resp = await fetch('/', { cache: 'no-store' });
  if (!resp.ok) throw new Error(`the page: ${resp.status}`);
  const src = (await resp.clone().text()).match(/src="(\/assets\/[^"]+\.js)"/)?.[1] ?? '';
  // The service worker's copy of the page - in its page cache, whatever its version
  // (vitrine-shell-v2, ...; writing to an old name left the stale copy: a reload loop).
  if (typeof caches !== 'undefined') {
    const shells = (await caches.keys()).filter((k) => k.startsWith('vitrine-shell-'));
    for (const k of shells.length ? shells : ['vitrine-shell-v2']) await (await caches.open(k)).put('/', resp.clone());
  }
  return src;
}
