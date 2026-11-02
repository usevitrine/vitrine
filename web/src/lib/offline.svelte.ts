import { t } from './i18n.svelte';
// What the service worker (public/sw.js) keeps on this device: thumbnails and
// the last API answers (used when the server can't be reached). The app shows
// when it's working from those, and Settings can show and clear them.

const THUMBS_PREFIX = 'vitrine-thumbs-'; // (public/sw.js: two batches, the count kept beside them)
const THUMB_STATE = 'vitrine-thumbs-state';
const OLD_THUMBS = 'vitrine-thumbs-v1'; // (the store before batches: gone when the first batch fills)
const API = 'vitrine-api-v1';

class Connection {
  /** The last API answer came from the device's copy (no connection). */
  offline = $state(false);
}

export const connection = new Connection();

export const canSave = () => typeof caches !== 'undefined' && 'serviceWorker' in navigator;

/** How many thumbnails are kept (many: more in the old store than the browser will list), and the space this site
 *  uses on the device. */
export async function savedStats(): Promise<{ thumbs: number; many: boolean; bytes: number } | null> {
  if (!canSave()) return null;
  try {
    const state = await (await (await caches.open(THUMB_STATE)).match('/state'))?.json();
    let thumbs = (state?.count ?? 0) + (state?.older ?? 0);
    let many = false;
    if (await caches.has(OLD_THUMBS)) {
      try {
        thumbs += (await (await caches.open(OLD_THUMBS)).keys()).length;
      } catch {
        many = true; // (a browser refuses a list that's too long)
      }
    }
    const est = navigator.storage?.estimate ? await navigator.storage.estimate() : {};
    return { thumbs, many, bytes: est.usage ?? 0 };
  } catch {
    return null;
  }
}

/** Removes the saved thumbnails (and with all: the saved API answers too). */
export async function clearSaved(all = false) {
  if (typeof caches === 'undefined') return;
  navigator.serviceWorker?.controller?.postMessage({ type: 'clear-thumbs' }); // (it forgets which it has saved)
  try {
    for (const k of await caches.keys()) {
      if (k.startsWith(THUMBS_PREFIX)) await caches.delete(k);
    }
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
