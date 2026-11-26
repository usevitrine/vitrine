// Installing Vitrine as an app. Chromium browsers (Android, desktop) offer it
// with an event at start-up: kept here, so an Install button can use it later
// (the first steps, Settings > This device). Other browsers: per-browser steps
// (InstallHelp.svelte).
import { standalone } from './viewport';

interface InstallPrompt extends Event {
  prompt(): Promise<void>;
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>;
}

class Install {
  /** The browser's own install offer, when it has one. */
  offer = $state<InstallPrompt | null>(null);
  /** Running as the installed app, or just installed. */
  installed = $state(standalone);

  start() {
    addEventListener('beforeinstallprompt', (e) => {
      e.preventDefault(); // (our button instead of the browser's banner)
      this.offer = e as InstallPrompt;
    });
    addEventListener('appinstalled', () => {
      this.installed = true;
      this.offer = null;
    });
  }

  /** Shows the browser's install dialog; true when installed. */
  async run(): Promise<boolean> {
    const o = this.offer;
    if (!o) return false;
    await o.prompt();
    const { outcome } = await o.userChoice;
    this.offer = null; // (used up either way)
    if (outcome === 'accepted') this.installed = true;
    return outcome === 'accepted';
  }
}

export const install = new Install();

export type Platform = 'ios' | 'android' | 'desktop';
export type Browser = 'safari' | 'chrome' | 'edge' | 'brave' | 'firefox' | 'samsung' | 'opera' | 'other';

/** Where we run, for the install steps. */
export function where(): { platform: Platform; browser: Browser } {
  const ua = navigator.userAgent;
  const ios = /iPhone|iPad|iPod/.test(ua) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1);
  const platform: Platform = ios ? 'ios' : /Android/.test(ua) ? 'android' : 'desktop';
  let browser: Browser = 'other';
  if (/SamsungBrowser\//.test(ua)) browser = 'samsung';
  else if (/OPR\/|OPT\/|Opera/.test(ua)) browser = 'opera';
  else if (/EdgA?\/|EdgiOS\//.test(ua) || /Edg\//.test(ua)) browser = 'edge';
  else if ((navigator as { brave?: unknown }).brave) browser = 'brave';
  else if (/Firefox\/|FxiOS\//.test(ua)) browser = 'firefox';
  else if (/CriOS\/|Chrome\//.test(ua)) browser = 'chrome';
  else if (/Safari\//.test(ua)) browser = 'safari';
  return { platform, browser };
}
