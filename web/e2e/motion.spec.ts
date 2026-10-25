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
  // Selecting, a chip's editor, a notice with Undo, a comic, a dialog
  await page.goto('/');
  await settle();
  await record(page, 'd-select', () => page.locator('.tile').nth(1).click({ modifiers: ['Control'] }));
  await record(page, 'd-select-more', () => page.locator('.tile').nth(3).click({ modifiers: ['Control'] }));
  await record(page, 'd-select-clear', () => page.keyboard.press('Escape'));
  await page.goto('/?q=blue_sky');
  await settle();
  await record(page, 'd-chip-editor', () => page.locator('.bar .chip').first().click());
  await record(page, 'd-chip-editor-close', () => page.keyboard.press('Escape'));
  await page.goto('/');
  await settle();
  await page.locator('.tile').nth(4).click({ button: 'right' });
  await page.waitForTimeout(300);
  await record(page, 'd-confirm', () => page.getByRole('menuitem', { name: /^Delete/ }).first().click());
  await record(page, 'd-toast', () => page.getByRole('alertdialog').getByRole('button', { name: 'Delete' }).click());
  await record(page, 'd-toast-undo', () => page.locator('.toast button', { hasText: 'Undo' }).first().click(), { still: 800 });
  await page.goto('/comics');
  await settle();
  await record(page, 'd-comic-open', () => page.locator('.tile').first().click());
  await record(page, 'd-comic-close', () => page.keyboard.press('Escape'));
  const gone = await page.evaluate(async () => {
    const r = await (await fetch('/api/search?q=&limit=1')).json().catch(() => null);
    const id = r?.ids?.[0] ?? r?.files?.[0]?.id;
    if (id) await fetch('/api/delete', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ids: [id] }) });
    return id;
  });
  await page.goto('/trash');
  await settle();
  await page.locator('.tile').first().click({ button: 'right' });
  await page.waitForTimeout(300);
  await record(page, 'd-dialog', () => page.getByRole('menuitem', { name: /forever/i }).first().click());
  await record(page, 'd-dialog-close', () => page.keyboard.press('Escape'));
  if (gone) await page.evaluate((id) => fetch('/api/restore', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ids: [id] }) }), gone);
  await page.goto('/');
  await settle();

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

  // ---- every other page: Downloads, Duplicates, a video, Browse, Rankings, Tags' list
  const post = (url: string, body: unknown) =>
    page.evaluate(([u, b]) => fetch(u as string, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(b) }).then((r) => r.json()), [url, body] as const);
  // (paused at once: a running one's stripes and the top bar's bobbing arrow never let the screen be still)
  const dls: number[] = [];
  for (const q of ['slow_a', 'slow_b', 'slow_c']) dls.push((await post('/api/downloads', { site: 'test-slow', query: q, max_posts: 0 })).id);
  for (const id of dls) await post(`/api/downloads/${id}/pause`, {});
  dls.push((await post('/api/downloads', { site: 'test-down', query: 'down_a', max_posts: 0 })).id);
  await page.goto('/downloads');
  await settle();
  const queue = page.locator('section.card', { has: page.getByRole('heading', { name: 'Queue' }) });
  await record(page, 'd-dl-stack-open', () => queue.getByRole('button', { name: /^Show all/ }).first().click());
  await record(page, 'd-dl-stack-close', () => queue.getByRole('button', { name: 'Show less' }).first().click());
  await record(page, 'd-dl-stack-hover', () => queue.locator('.unfold').first().hover());
  await page.mouse.move(5, 300);
  await settle();
  await record(page, 'd-dl-tab-subs', () => page.getByRole('tab', { name: /^Subscriptions/ }).click());
  await record(page, 'd-dl-tab-history', () => page.getByRole('tab', { name: /^History/ }).click());
  await record(page, 'd-dl-tab-queue', () => page.getByRole('tab', { name: /^Queue/ }).click());
  await record(page, 'd-dl-repeat', () => page.getByRole('button', { name: 'Repeat' }).click());
  await record(page, 'd-dl-repeat-pick', () => page.getByRole('option', { name: 'Every day' }).click()); // (the subscription's options open)
  await record(page, 'd-dl-repeat-back', async () => {
    await page.getByRole('button', { name: 'Repeat' }).click();
    await page.waitForTimeout(300);
    await page.getByRole('option', { name: 'Once' }).click();
  });
  await record(page, 'd-dl-menu-open', () => page.locator('header.topbar button.dl').click());
  await record(page, 'd-dl-menu-close', () => page.keyboard.press('Escape'));
  for (const id of dls) await post(`/api/downloads/${id}/remove`, {});
  await page.evaluate(() => localStorage.setItem('vitrine.dupeLevel', '6')); // (Very similar: the test library has no exact copies)
  await page.goto('/duplicates');
  await settle();
  await record(page, 'd-dupes-flip', () => page.getByRole('tab', { name: 'Flip' }).click());
  await record(page, 'd-dupes-switch', () => page.getByRole('button', { name: /Switch between A and B/ }).click());
  await record(page, 'd-dupes-side', () => page.getByRole('tab', { name: 'Side by side' }).click());
  await record(page, 'd-dupes-skip', () => page.getByRole('button', { name: /^Skip/ }).click());
  await record(page, 'd-dupes-level', () => page.getByRole('button', { name: /How alike/i }).click());
  await record(page, 'd-dupes-level-close', () => page.keyboard.press('Escape'));
  await page.goto('/?q=system:type%3Dvideo');
  await settle();
  await page.locator('.tile').first().click();
  await page.waitForTimeout(900);
  await record(page, 'd-video-pause', () => page.keyboard.press('k'));
  await record(page, 'd-video-play', () => page.keyboard.press('k'));
  await record(page, 'd-video-seek', () => page.keyboard.press('ArrowRight'));
  await record(page, 'd-video-close', () => page.keyboard.press('Escape'));
  // Browse (a gallery site answered here; its covers: the library's own thumbnails)
  const thumbs: string[] = await page.evaluate(async () => {
    const ids = (await (await fetch('/api/search?q=system:type%3Dimage&limit=5')).json()).ids;
    return (await (await fetch('/api/files?ids=' + ids.join(','))).json()).files.map((f: { thumb: string }) => f.thumb);
  });
  const gallery = (id: number, title: string, extra = {}) => ({
    id, token: 'abcdef0123', url: `https://g.test/g/${id}/abcdef0123/`, title, category: 'Manga', pages: 10, tags: [{ ns: 'female', name: 'blue_sky' }],
    cover: `https://img.g.test/c/${id}.webp`, ...extra,
  });
  await page.route('**/api/browse/sites', (r) => r.fulfill({ json: { sites: ['gallery-site'], sorts: { 'gallery-site': [] } } }));
  await page.route('**/api/browse?*', (r) =>
    r.fulfill({ json: { galleries: [1, 2, 3, 4, 5].map((i) => gallery(i, `Gallery ${i}`, i === 1 ? { comic: 3 } : {})), total: 5, next: 0 } }));
  await page.route('**/api/browse/image?*', async (r) => {
    const n = Number(/c%2F(\d+)\.webp/.exec(r.request().url())?.[1] ?? 1);
    await r.fulfill({ response: await page.request.get(thumbs[(n - 1) % thumbs.length]) });
  });
  await page.route('**/api/browse/gallery?*', (r) => r.fulfill({ json: { thumbs: [], pages: 1 } }));
  await page.route('**/api/downloads', (r) => (r.request().method() === 'POST' ? r.fulfill({ json: { id: 99 } }) : r.continue()));
  await page.goto('/browse');
  await settle();
  await record(page, 'd-browse-search', async () => {
    const box = page.locator('.browse').getByRole('combobox', { name: 'Search' });
    await box.fill('blue_sky');
    await box.press('Enter');
  });
  await settle();
  const btile = page.locator('.browse .tile').nth(1);
  await record(page, 'd-browse-hover', () => btile.hover(), { still: 900 });
  await record(page, 'd-browse-unhover', () => page.mouse.move(5, 300), { still: 900 });
  await record(page, 'd-browse-open', () => btile.click());
  await record(page, 'd-browse-download', () => page.getByRole('button', { name: 'Download', exact: true }).click());
  await record(page, 'd-browse-download-confirm', () => page.locator('[role="dialog"][aria-modal="true"]').getByRole('button', { name: 'Download', exact: true }).click());
  await record(page, 'd-browse-close', () => page.keyboard.press('Escape'));
  await record(page, 'd-browse-hover-queued', () => btile.hover(), { still: 900 });
  await record(page, 'd-browse-unhover-queued', () => page.mouse.move(5, 300), { still: 900 });
  await page.unrouteAll({ behavior: 'ignoreErrors' });
  // Rankings (the I came button on for it)
  await post('/api/settings', { cameButton: true });
  await page.goto('/rankings');
  await settle();
  for (const r of await page.getByRole('radio').all()) {
    const label = ((await r.textContent()) ?? '').trim().replace(/\W+/g, '-').toLowerCase();
    if (label) await record(page, `d-rankings-${label}`, () => r.click());
  }
  await post('/api/settings', { cameButton: false });
  // Tags' list: filtering it
  await page.goto('/tags/all');
  await settle();
  await record(page, 'd-tags-filter', async () => {
    await page.locator('.content input').first().fill('blue');
  });
  await record(page, 'd-tags-filter-clear', () => page.locator('.content input').first().fill(''));

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
  // Phones: a section and back, Downloads' tabs and a dropdown's sheet, a comic
  await p.goto('/settings');
  await p.waitForTimeout(600);
  await record(p, 'p-settings-section', () => p.locator('nav.side button.section').first().tap());
  await record(p, 'p-settings-back', () => p.locator('.content button.back').first().tap());
  await p.goto('/downloads');
  await p.waitForTimeout(800);
  await record(p, 'p-dl-tab-subs', () => p.getByRole('tab', { name: /^Subscriptions/ }).tap());
  await record(p, 'p-dl-tab-queue', () => p.getByRole('tab', { name: /^Queue/ }).tap());
  await record(p, 'p-dropdown-sheet', () => p.getByRole('button', { name: 'Repeat' }).tap());
  await record(p, 'p-dropdown-sheet-close', () => p.locator('.backdrop').last().tap({ position: { x: 200, y: 100 } }));
  await p.goto('/comics');
  await p.waitForTimeout(800);
  await record(p, 'p-comic-open', () => p.locator('.tile').first().tap());
  await record(p, 'p-comic-close', () => p.goBack());
  await phone.close();
});
