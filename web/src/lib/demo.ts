// The website's demo (vitrine.software/demo; `npm run build:demo`): this app with a pretend server in the page
// (src/demo/backend.ts) - the media screen only: no menu, Downloads or bell.
export const DEMO = import.meta.env.MODE === 'demo';

/** The demo's site icons (its media/favicon/<host>; the server's /api/favicon otherwise): the hosts it has one for. */
export const demoIcons = new Set<string>();
