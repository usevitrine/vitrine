// The kinds of notice and where each goes (Settings > Notifications, for every device): the bell (it rings; one bell
// for every device), on screen (a moment, then gone) or nowhere (screen readers still hear it). Whatever its setting,
// a notice with a button (Undo, Reload) shows on screen until it's had its chance, and an error always does.

export type NoticeMode = 'bell' | 'screen' | 'off';

export interface NoticeKind {
  id: string;
  group: 'downloader' | 'library' | 'comics' | 'downloads' | 'settings' | 'app';
  mode: NoticeMode; // its default
  undo?: boolean; // it has a button (Undo, Reload): it shows on screen in any case
}

export const noticeKinds: NoticeKind[] = [
  // What the downloader did on its own (these also go to push notifications, set per device).
  { id: 'new', group: 'downloader', mode: 'bell' },
  { id: 'done', group: 'downloader', mode: 'bell' },
  { id: 'failed', group: 'downloader', mode: 'bell' },
  { id: 'blacklisted', group: 'downloader', mode: 'bell' },
  { id: 'paused', group: 'downloader', mode: 'bell' },
  { id: 'back', group: 'downloader', mode: 'bell' },
  // What was just done here.
  { id: 'deleted', group: 'library', mode: 'bell', undo: true },
  { id: 'tags', group: 'library', mode: 'bell', undo: true },
  { id: 'favorites', group: 'library', mode: 'bell', undo: true },
  { id: 'came', group: 'library', mode: 'bell', undo: true },
  { id: 'copied', group: 'library', mode: 'screen' },
  { id: 'refreshed', group: 'library', mode: 'screen' },
  { id: 'tagEdits', group: 'library', mode: 'screen' },
  { id: 'comicRead', group: 'comics', mode: 'off' },
  { id: 'comicEdits', group: 'comics', mode: 'bell', undo: true },
  { id: 'collections', group: 'comics', mode: 'screen' },
  { id: 'downloadsStarted', group: 'downloads', mode: 'screen' },
  { id: 'downloadsControl', group: 'downloads', mode: 'off' },
  { id: 'groups', group: 'downloads', mode: 'off' },
  { id: 'retrying', group: 'downloads', mode: 'off' },
  { id: 'retryResult', group: 'downloads', mode: 'screen' },
  { id: 'downloadsMisc', group: 'downloads', mode: 'off' },
  { id: 'backups', group: 'settings', mode: 'screen' },
  { id: 'sites', group: 'settings', mode: 'screen' },
  { id: 'savedSearches', group: 'settings', mode: 'bell', undo: true },
  { id: 'device', group: 'settings', mode: 'screen' },
  { id: 'update', group: 'app', mode: 'bell', undo: true },
  { id: 'release', group: 'app', mode: 'bell' }, // a newer Vitrine is out (the server's daily check)
  { id: 'errors', group: 'app', mode: 'screen' },
];

/** Where a kind of notice goes, given the choices made (a kind not chosen: its default; an unknown one: on screen). */
export function modeOf(id: string, chosen: Record<string, string>): NoticeMode {
  const c = chosen[id];
  if (c === 'bell' || c === 'screen' || c === 'off') return c;
  return noticeKinds.find((k) => k.id === id)?.mode ?? 'screen';
}
