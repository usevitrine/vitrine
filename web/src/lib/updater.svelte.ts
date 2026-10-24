// The full-screen updater (components/Updater.svelte): Update in Settings starts it; it follows the server through
// the update and brings the page back on the new version without a visible reload.
//
// Steps: backup and download are the server's own (GET /api/update says which); restart is the server gone (it is
// being recreated: requests fail); start is the server back on a new version. Each completes when it really happens.
// Watchtower says nothing about the pull's progress, so the bar is honest about steps and smooth within one: it eases
// toward the step's end without reaching it (and never goes back). At the end the page is fetched for the service
// worker's copy (no old version on the way), the steps fade while the logo glides to the splash's place, and the
// library opens behind it as it does at the start (its default search and sort): the start-up splash takes over in
// the very same place and fades the app in (splash.js shows it for this load, also on Android).
import { api, type UpdateState } from './api';
import { refreshShell } from './appShell';
import { toasts } from './toast.svelte';
import { t } from './i18n.svelte';

export type UpdateStep = 'backup' | 'download' | 'restart' | 'start';
export const updateSteps: UpdateStep[] = ['backup', 'download', 'restart', 'start'];

// Where each step's part of the bar starts and ends, and how fast it eases toward the end (a time constant, ms).
const span: Record<UpdateStep, [number, number, number]> = {
  backup: [0, 0.12, 2500],
  download: [0.12, 0.72, 40000],
  restart: [0.72, 0.9, 8000],
  start: [0.9, 1, 1500],
};

/** sessionStorage: the version just updated to - the next load says so (and splash.js shows the splash for it). */
export const UPDATED_KEY = 'vitrine.updatedTo';

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

class Updater {
  active = $state(false);
  step = $state<UpdateStep>('backup');
  progress = $state(0); // 0..1, what the bar shows
  error = $state('');
  /** The last moment: the steps fade out, only the logo stays (the reload behind it). */
  leaving = $state(false);
  from = $state('');
  to = $state('');
  private stepAt = 0;
  private timer = 0;
  private run = 0;

  private go(step: UpdateStep) {
    if (updateSteps.indexOf(step) <= updateSteps.indexOf(this.step) && this.stepAt) return; // (never back)
    this.step = step;
    this.stepAt = performance.now();
  }

  private tick = () => {
    const [lo, hi, tau] = span[this.step];
    const ease = 1 - Math.exp(-(performance.now() - this.stepAt) / tau);
    this.progress = Math.max(this.progress, lo + (hi - lo) * 0.95 * ease);
  };

  async start(info: UpdateState) {
    const run = ++this.run;
    this.active = true;
    this.leaving = false;
    this.error = '';
    this.from = info.current;
    this.to = info.latest;
    this.progress = 0;
    this.stepAt = 0;
    this.step = 'backup';
    this.go('backup');
    clearInterval(this.timer);
    this.timer = window.setInterval(this.tick, 100);
    try {
      await api.startUpdate();
    } catch (e) {
      return this.fail((e as Error).message);
    }
    const until = Date.now() + 20 * 60_000;
    while (run === this.run && Date.now() < until) {
      await sleep(1000);
      if (run !== this.run) return;
      let s: UpdateState;
      try {
        s = await api.updateState();
      } catch {
        this.go('restart'); // (gone: being recreated)
        continue;
      }
      if (s.current !== this.from) {
        this.go('start');
        return this.finish(s.current);
      }
      if ((this.step as UpdateStep) === 'restart') return this.fail(t('updates.stillOld', { version: s.current })); // (back, but the same)
      if (s.update_error) return this.fail(s.update_error);
      if (s.updating === 'backup' || s.updating === 'download') this.go(s.updating);
    }
    if (run === this.run) this.fail(t('updates.notBack'));
  }

  private async finish(version: string) {
    try {
      await refreshShell();
    } catch {
      /* the reload fetches it anyway */
    }
    clearInterval(this.timer);
    this.progress = 1;
    await sleep(700); // (the bar fills, the last step checks)
    try {
      sessionStorage.setItem(UPDATED_KEY, version);
    } catch {
      /* no "Updated" message then */
    }
    this.leaving = true;
    await sleep(550); // (the steps fade, the logo glides to where the splash will draw it)
    location.replace('/'); // (the library as it opens: the default search and sort - the user)
  }

  private fail(message: string) {
    clearInterval(this.timer);
    this.error = message;
  }

  /** Closes the updater after a failure (the app as it was: the old version still runs). */
  close() {
    this.run++;
    clearInterval(this.timer);
    this.active = false;
  }

  /** After the reload: say what was updated to (once the splash has faded the app in). */
  announce() {
    let to: string | null = null;
    try {
      to = sessionStorage.getItem(UPDATED_KEY);
      sessionStorage.removeItem(UPDATED_KEY);
    } catch {
      return;
    }
    if (to) setTimeout(() => toasts.show(t('updates.updated', { version: to }), { kind: 'done', cat: 'release' }), 900);
  }
}

export const updater = new Updater();
