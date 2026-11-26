// Errors in the app (uncaught errors, failed promises) go to the server's log,
// so the problem report (Settings > Stats) has them too. Each one once, ten
// per page load at most; the page's path only (never its search).
const sent = new Set<string>();

function send(message: string, stack = '') {
  if (!message || sent.size >= 10 || sent.has(message)) return;
  if (/ResizeObserver loop|AbortError|The user aborted|Load failed$/i.test(message)) return; // harmless noise
  sent.add(message);
  try {
    fetch('/api/log', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ message, stack, where: location.pathname }),
      keepalive: true,
    }).catch(() => {});
  } catch {
    /* never an error about an error */
  }
}

export function logErrors() {
  addEventListener('error', (e) => {
    if (!(e instanceof ErrorEvent)) return; // (a resource that failed to load: not a code error)
    send(e.message, e.error?.stack ?? `${e.filename}:${e.lineno}:${e.colno}`);
  });
  addEventListener('unhandledrejection', (e) => {
    const r = e.reason;
    if (r instanceof Error) send(`${r.name}: ${r.message}`, r.stack);
    else send(String(r));
  });
}
