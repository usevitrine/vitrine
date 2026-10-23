// Push notifications on this device: the service worker, the browser's push
// subscription and the server's list of devices.
import { api, ApiError, type PushDevice } from './api';
import { t, i18n } from './i18n.svelte';

export type PushSupport = 'ok' | 'needs-home-screen' | 'unsupported';

const isIOS = () =>
  /iPhone|iPad|iPod/.test(navigator.userAgent) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1);
const standalone = () =>
  matchMedia('(display-mode: standalone)').matches || (navigator as { standalone?: boolean }).standalone === true;
const isBrave = () => !!(navigator as { brave?: unknown }).brave;

export function pushSupport(): PushSupport {
  if ('serviceWorker' in navigator && 'PushManager' in window && 'Notification' in window) return 'ok';
  return isIOS() && !standalone() ? 'needs-home-screen' : 'unsupported';
}

/** "Home-screen app on iPhone", "Brave on Windows" - so the device list makes sense. */
export function deviceLabel(): string {
  const ua = navigator.userAgent;
  let device = t('push.device.unknown');
  if (/iPhone/.test(ua)) device = 'iPhone';
  else if (/iPad/.test(ua) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1)) device = 'iPad';
  else if (/Android/.test(ua)) device = 'Android';
  else if (/Windows/.test(ua)) device = 'Windows';
  else if (/Mac OS X/.test(ua)) device = 'Mac';
  else if (/Linux/.test(ua)) device = 'Linux';
  let browser = t('push.device.browser');
  if (isIOS()) browser = standalone() ? t('push.device.homeScreenApp') : 'Safari';
  else if (isBrave()) browser = 'Brave';
  else if (/Edg\//.test(ua)) browser = 'Edge';
  else if (/Firefox\//.test(ua)) browser = 'Firefox';
  else if (/Chrome\//.test(ua)) browser = 'Chrome';
  else if (/Safari\//.test(ua)) browser = 'Safari';
  return t('push.device.label', { browser, device });
}

/** Keeps the service worker up to date on devices that get notifications
 *  (the browser keeps it registered; other devices never get one). */
export function registerServiceWorker() {
  if (!('serviceWorker' in navigator)) return;
  // Not under browser automation (Playwright's Windows WebKit freezes when a
  // service worker registers), unless a test asks for it.
  let forced = false;
  try {
    forced = localStorage.getItem('vitrine.sw') === 'on';
  } catch {
    /* storage blocked */
  }
  if (navigator.webdriver && !forced) return;
  navigator.serviceWorker
    .register('/sw.js')
    .then(() => navigator.storage?.persist?.()) // ask the browser not to clear saved thumbnails under pressure
    .catch(() => {
      /* no saved thumbnails or notifications on this device - the app works without them */
    });
}

/** The language this device last told the server (its notifications'). */
function sentLanguage(set?: string): string | null {
  try {
    if (set) localStorage.setItem('vitrine.pushLanguage', set);
    return localStorage.getItem('vitrine.pushLanguage');
  } catch {
    return null;
  }
}

function keyBytes(b64: string): Uint8Array {
  const s = atob(b64.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat((4 - (b64.length % 4)) % 4));
  return Uint8Array.from(s, (c) => c.charCodeAt(0));
}

class Push {
  support = $state<PushSupport>(pushSupport());
  permission = $state<NotificationPermission>('Notification' in window ? Notification.permission : 'default');
  /** This device's subscription endpoint, if it gets notifications. */
  endpoint = $state<string | null>(null);
  devices = $state<PushDevice[]>([]);
  loaded = $state(false);
  busy = $state(false);
  error = $state('');

  async load() {
    this.error = '';
    try {
      const [info, sub] = await Promise.all([api.push(), this.current()]);
      this.devices = info.devices;
      // Subscribed in the browser but unknown to the server (removed elsewhere): off.
      this.endpoint = sub && info.devices.some((d) => d.endpoint === sub.endpoint) ? sub.endpoint : null;
    } catch (e) {
      this.error = (e as Error).message;
    } finally {
      this.loaded = true;
    }
  }

  private async current(): Promise<PushSubscription | null> {
    if (this.support !== 'ok') return null;
    const reg = await navigator.serviceWorker.getRegistration();
    return reg ? reg.pushManager.getSubscription() : null;
  }

  /** Asks for permission and subscribes (must run from a tap: iOS requires it). */
  async enable() {
    this.error = '';
    this.busy = true;
    try {
      const perm = await Notification.requestPermission();
      this.permission = perm;
      if (perm !== 'granted') return;
      const { key } = await api.push();
      const reg = await navigator.serviceWorker.register('/sw.js');
      await navigator.serviceWorker.ready;
      let sub = await reg.pushManager.getSubscription();
      if (!sub) {
        sub = await reg.pushManager.subscribe({
          userVisibleOnly: true,
          applicationServerKey: keyBytes(key) as BufferSource,
        });
      }
      const info = await api.pushSubscribe(sub.toJSON(), deviceLabel(), i18n.lang);
      this.devices = info.devices;
      this.endpoint = sub.endpoint;
      sentLanguage(i18n.lang);
    } catch (e) {
      const msg = (e as Error).message;
      // Brave turns its push service off unless "Use Google services for push messaging" is on
      // (only for the browser's own errors, not the server's).
      this.error = isBrave() && !(e instanceof ApiError)
        ? t('push.brave', { error: msg })
        : msg;
    } finally {
      this.busy = false;
    }
  }

  async disable() {
    this.error = '';
    this.busy = true;
    try {
      const sub = await this.current();
      const endpoint = this.endpoint ?? sub?.endpoint;
      await sub?.unsubscribe().catch(() => {});
      if (endpoint) this.devices = (await api.pushUnsubscribe(endpoint)).devices;
      this.endpoint = null;
    } catch (e) {
      this.error = (e as Error).message;
    } finally {
      this.busy = false;
    }
  }

  async remove(endpoint: string) {
    this.devices = (await api.pushUnsubscribe(endpoint)).devices;
    if (endpoint === this.endpoint) this.endpoint = null;
  }

  test(endpoint: string) {
    return api.pushTest(endpoint);
  }

  /** The app's language changed: this device's notifications follow (if it gets any). */
  async syncLanguage(lang: string) {
    if (sentLanguage() === lang || this.support !== 'ok' || Notification.permission !== 'granted') return;
    try {
      const sub = await this.current();
      if (!sub) return;
      await api.pushSubscribe(sub.toJSON(), deviceLabel(), lang);
      sentLanguage(lang);
    } catch {
      /* tried again next start */
    }
  }
}

export const push = new Push();
