// Motion recordings, for checking animations frame by frame (the user: "animations site-wide, should be clean,
// smooth, and pixel perfect"). Runs only when asked, in Chromium (it records through Chrome's screencast - what is
// painted, not what the page reports):
//   MOTION=<folder> npx playwright test motion --project=desktop-chromium
//   node scripts/motion-report.mjs <folder>      (contact sheets, and frames that jump late or settle slowly)
// MOTION_ONLY=<start of a name> records just those (d- desktop, p- phone; the others are still done, unrecorded).
import { type Page, type CDPSession, devices } from '@playwright/test';
import fs from 'node:fs';
import { test, login } from './fixtures';

const dir = process.env.MOTION ?? '';
const only = process.env.MOTION_ONLY ?? '';
test.skip(!dir, 'set MOTION=<folder> to record');
test.setTimeout(240_000);

/** Records what's painted while `act` runs, until the screen has been still for `still` ms. */
async function record(page: Page, name: string, act: () => Promise<unknown>, { still = 500, max = 4000 } = {}) {
  if (only && !name.startsWith(only)) {
    // (not recorded - still done, so what follows starts where it expects)
    await act().catch(() => {});
    await page.waitForTimeout(still);
    return;
  }
  const out = `${dir}/${name}`;
  fs.rmSync(out, { recursive: true, force: true });
  fs.mkdirSync(out, { recursive: true });
  const cdp: CDPSession = await page.context().newCDPSession(page);
  const times: number[] = [];
  let last = Date.now();
  const onFrame = async (f: { data: string; sessionId: number; metadata: { timestamp?: number } }) => {
    fs.writeFileSync(`${out}/f_${String(times.length).padStart(4, '0')}.png`, Buffer.from(f.data, 'base64'));
    times.push(f.metadata.timestamp ?? Date.now() / 1000);
    last = Date.now();
    await cdp.send('Page.screencastFrameAck', { sessionId: f.sessionId }).catch(() => {});
  };
  cdp.on('Page.screencastFrame', onFrame);
  await cdp.send('Page.startScreencast', { format: 'png', everyNthFrame: 1 });
  await page.waitForTimeout(150); // (a frame of the screen before)
  const t0 = Date.now();
  last = t0;
  try {
    await act();
  } catch (e) {
    fs.writeFileSync(`${out}/error.txt`, String((e as Error).message ?? e)); // (a step that didn't work: noted, the rest goes on)
  }
  while (Date.now() - last < still && Date.now() - t0 < max) await page.waitForTimeout(50);
  await cdp.send('Page.stopScreencast');
  cdp.off('Page.screencastFrame', onFrame);
  await cdp.detach().catch(() => {});
  fs.writeFileSync(`${out}/times.json`, JSON.stringify(times));
}

test('motion', async ({ page, browser, baseURL }, info) => {
  test.skip(info.project.name !== 'desktop-chromium', 'Chromium only (the screencast)');
  page.setDefaultTimeout(4000);
  await login(page);
  await page.evaluate(() => fetch('/api/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ defaultSearch: '' }) }));
  const settle = () => page.waitForTimeout(600);

  // ---- desktop
  await page.goto('/');
  await page.getByText(/^\d+ files/).waitFor();
  await settle();
  const tile = page.locator('.tile').nth(2);
  await record(page, 'd-viewer-open', () => tile.click());
  await record(page, 'd-viewer-next', () => page.keyboard.press('ArrowRight'));
  await record(page, 'd-viewer-details', () => page.keyboard.press('i'));
  await record(page, 'd-viewer-details-close', () => page.keyboard.press('i'));
  await record(page, 'd-viewer-close', () => page.keyboard.press('Escape'));
  await settle();
  await record(page, 'd-menu-open', () => page.getByRole('button', { name: 'Menu' }).click());
  await record(page, 'd-menu-close', () => page.keyboard.press('Escape'));
  await record(page, 'd-filters-open', () => page.locator('button.filt').first().click());
  await record(page, 'd-filters-close', () => page.keyboard.press('Escape'));
  await record(page, 'd-tile-menu', () => tile.click({ button: 'right' }));
  await record(page, 'd-tile-menu-close', () => page.keyboard.press('Escape'));
  await record(page, 'd-chip-add', async () => {
    await page.getByRole('combobox').first().fill('blue_sky');
    await page.keyboard.press('Enter');
  });
  await record(page, 'd-chip-remove', () => page.getByRole('combobox').first().press('Backspace'));
  await record(page, 'd-bell', () => page.locator('header.topbar .bell').click());
  await record(page, 'd-bell-close', () => page.keyboard.press('Escape'));
  await record(page, 'd-new-download', async () => {
    await page.locator('header.topbar button.dl').click();
    await page.getByRole('button', { name: /^New/ }).first().click();
  });
  await record(page, 'd-new-download-close', () => page.keyboard.press('Escape'));
  await settle();
  // Other pages
  // Other pages, from the menu
  for (const [name, entry] of [
    ['d-page-tags', 'Tags'],
    ['d-page-comics', 'Comics'],
    ['d-page-downloads', 'Downloads'],
    ['d-page-duplicates', 'Duplicates'],
    ['d-page-settings', 'Settings'],
    ['d-page-media', 'Media'],
  ] as const) {
    await page.getByRole('button', { name: 'Menu' }).click();
    await page.waitForTimeout(400);
    await record(page, name, () => page.locator('#nav-drawer .tab-row > .tab', { hasText: entry }).first().click());
    await settle();
  }
  // Sections (Settings, Tags): each in turn
  for (const screen of ['settings', 'tags']) {
    await page.goto('/' + screen);
    await settle();
    const n = await page.locator('nav.side button.section').count();
    for (let k = 0; k < n; k++) {
      const b = page.locator('nav.side button.section').nth(k);
      const label = ((await b.locator('.name').textContent()) ?? '').trim().replace(/\W+/g, '-').toLowerCase();
      await record(page, `d-${screen}-${label}`, () => b.click());
    }
  }

  // ---- a phone (Chromium with a phone's screen and touch)
  const phone = await browser.newContext({ ...devices['Pixel 7'], baseURL, locale: 'en-US' });
  const p = await phone.newPage();
  p.setDefaultTimeout(4000);
  await login(p);
  await p.goto('/');
  await p.getByText(/^\d+ files/).waitFor();
  await p.waitForTimeout(600);
  const ptile = p.locator('.tile').nth(1);
  await record(p, 'p-viewer-open', () => ptile.tap());
  await record(p, 'p-viewer-controls', () => p.locator('.stage').tap());
  await record(p, 'p-viewer-close', () => p.goBack());
  await p.waitForTimeout(600);
  await record(p, 'p-menu-open', () => p.getByRole('button', { name: 'Menu' }).tap());
  await record(p, 'p-menu-close', () => p.locator('.backdrop').tap({ position: { x: 360, y: 400 } }));
  await record(p, 'p-filters-open', () => p.locator('button.filt').first().tap());
  await record(p, 'p-filters-close', () => p.goBack());
  await record(p, 'p-bell', () => p.locator('header.topbar .bell').tap());
  await record(p, 'p-bell-close', () => p.goBack());
  await phone.close();
});
