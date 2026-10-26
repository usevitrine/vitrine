// What's new (2026-10-26, the user: "when a user updates and it goes to their media, i want a 'what's new' popup to
// show by default that shows the changes for the new version, with a 'don't show again' checkbox and the same option
// in the settings"). The app's own changelog (CHANGELOG.md, built in) names the version installed; the shared setting
// whatsNewSeen the last one whose changes were shown. On the library (Comics with Media off), once the settings are
// read, a newer installed version opens WhatsNew.svelte with the changes since then (or, the first time, this
// version's). Shown once for every device; closing it marks it seen. Off (whatsNew false): marked seen quietly, so
// switching it on again doesn't bring a pile of old ones. A new server's first steps mark the version seen (no window
// right after setting up).
import { readChangelog, newerThan, type ChangelogVersion } from './changelog';
import { settings } from './settings.svelte';

/** A section title's version ("1.0.6 - 2026-10-27" -> "1.0.6"; "1.0.3-hotfix - ..." -> "1.0.3-hotfix"). */
const versionOf = (title: string) => title.match(/^v?(\d+\.\d+\.\d+(?:-hotfix)?)/)?.[1] ?? '';

/** The released versions in the built-in changelog, newest first. */
async function released(): Promise<ChangelogVersion[]> {
  const md = (await import('../../../CHANGELOG.md?raw')).default;
  return readChangelog(md).filter((v) => versionOf(v.title));
}

class WhatsNew {
  /** The versions shown in the window (null: closed). */
  shown = $state<ChangelogVersion[] | null>(null);
  private latest = '';
  private checked = false;

  /** Opens the window if the installed version's changes haven't been shown (once per page load). */
  async check() {
    if (this.checked) return;
    this.checked = true;
    await settings.whenSynced(5000);
    if (!settings.isSynced) return void (this.checked = false); // (offline: the next time)
    const all = await released().catch(() => []);
    if (!all.length) return;
    this.latest = versionOf(all[0].title);
    const seen = settings.whatsNewSeen;
    if (seen === this.latest) return;
    const fresh = seen ? newerThan(all, seen) : all.slice(0, 1);
    if (!settings.whatsNew || !fresh.length) return void (settings.whatsNewSeen = this.latest); // (off, or gone back)
    this.shown = fresh.slice(0, 5);
  }

  /** Closed: seen (and, ticked, never again - Settings > Updates switches it back on). */
  close(dontShow: boolean) {
    this.shown = null;
    settings.whatsNewSeen = this.latest;
    if (dontShow) settings.whatsNew = false;
  }

  /** The installed version counts as seen (the first steps just finished: nothing new to tell). */
  async markSeen() {
    this.checked = true; // (no window this time)
    const all = await released().catch(() => []);
    if (all.length) settings.whatsNewSeen = versionOf(all[0].title);
  }
}

export const whatsNew = new WhatsNew();
