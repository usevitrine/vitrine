import { t } from './i18n.svelte';
// What the service worker (public/sw.js) keeps on this device: thumbnails and
// the last API answers (used when the server can't be reached). The app shows
// when it's working from those, and Settings can show and clear them.

const THUMBS = 'vitrine-thumbs-v1';
const API = 'vitrine-api-v1';

class Connection {
  /** The last API answer came from the device's copy (no connection). */
  offline = $state(false);
}

export const connection = new Connection();

export const canSave = () => typeof caches !== 'undefined' && 'serviceWorker' in navigator;

/** How many thumbnails are kept, and the space this site uses on the device. */
export async function savedStats(): Promise<{ thumbs: number; bytes: number } | null> {
  if (!canSave()) return null;
  try {
    const keys = await (await caches.open(THUMBS)).keys();
    const est = navigator.storage?.estimate ? await navigator.storage.estimate() : {};
    return { thumbs: keys.length, bytes: est.usage ?? 0 };
  } catch {
    return null;
  }
}

/** Removes the saved thumbnails (and with all: the saved API answers too). */
export async function clearSaved(all = false) {
  if (typeof caches === 'undefined') return;
  try {
    await caches.delete(THUMBS);
    if (all) await caches.delete(API);
  } catch {
    /* nothing saved, or storage blocked */
  }
}

/** Whether the background helper (the service worker that saves thumbnails)
 *  runs in this browser: 'running', 'starting' (next load), or why it doesn't. */
export async function helperState(): Promise<{ state: 'running' | 'starting' | 'off'; why?: string }> {
  if (!('serviceWorker' in navigator) || typeof caches === 'undefined') {
    return { state: 'off', why: t('offline.noWorkers') };
  }
  try {
    if (navigator.serviceWorker.controller) return { state: 'running' };
    if (await navigator.serviceWorker.getRegistration()) return { state: 'starting' };
    if (navigator.webdriver) return { state: 'off', why: t('offline.automation') }; // (tests: see registerServiceWorker)
    await navigator.serviceWorker.register('/sw.js');
    return { state: 'starting' };
  } catch (e) {
    return { state: 'off', why: (e as Error).message || t('offline.refused') };
  }
}
