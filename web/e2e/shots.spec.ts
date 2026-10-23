// Screenshots of every screen, for design reviews. Runs only when asked:
//   SHOTS=<folder> npx playwright test shots
import { type Page } from '@playwright/test';
import { test, login } from './fixtures';

const dir = process.env.SHOTS ?? '';
test.skip(!dir, 'set SHOTS=<folder> to take screenshots');

test('screens', async ({ page, browser }, info) => {
  const shot = async (name: string) => {
    await page.waitForTimeout(400);
    await page.screenshot({ path: `${dir}/${info.project.name}-${name}.png` });
  };
  await login(page);
  // A clean start (an earlier run may have left a default search).
  await page.evaluate(() => fetch('/api/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ defaultSearch: '' }) }));

  await page.goto('/?q=blue_sky -red');
  await page.getByText(/^\d+ files?/).waitFor();
  await shot('1-library');
  await page.locator('.chip').first().click();
  await shot('2-chip-editor');
  await page.keyboard.press('Escape');
  // Phones: the fast scroller on the left after a left-hand touch, awake while scrolling.
  if (info.project.name === 'iphone') {
    await page.setViewportSize({ width: 393, height: 430 });
    await page.goto('/');
    await page.getByText(/^26 files/).waitFor();
    const r = (await page.locator('#grid-scroller').boundingBox())!;
    await page.touchscreen.tap(r.x + 40, r.y + 60);
    await page.getByRole('button', { name: 'Close' }).click();
    await page.locator('#grid-scroller').evaluate((el) => el.scrollBy(0, 300));
    await page.waitForTimeout(250);
    await page.screenshot({ path: `${dir}/${info.project.name}-1c-fast-left.png` });
    await page.setViewportSize(info.project.use.viewport!);
  }
  await page.goto('/?q=blue_sky%20-blue_sky');
  await page.getByText('Nothing matches this search.').waitFor();
  await shot('1b-empty');

  await page.goto('/');
  await page.getByText(/^26 files/).waitFor();
  await page.getByRole('button', { name: 'Menu' }).click();
  await shot('3-drawer');
  await page.keyboard.press('Escape');

  await page.locator('.tile').nth(1).click();
  await page.waitForTimeout(600);
  await shot('4-viewer');
  await page.getByRole('button', { name: /details/i }).first().click().catch(() => {});
  await shot('5-viewer-details');
  await page.goto('/');
  await page.getByText(/^26 files/).waitFor();
  await page.locator('.tile').nth(0).click({ modifiers: ['Control'] }).catch(() => {});
  await page.locator('.tile').nth(2).click({ modifiers: ['Control'] }).catch(() => {});
  await shot('6-selection');

  // A video: the controls, the volume slider open (hover) where the browser allows.
  await page.goto('/?q=system:type%3Dvideo%20system:has_audio');
  await page.locator('.tile').first().click();
  await page.getByRole('button', { name: 'Unmute' }).hover();
  await page.waitForTimeout(500);
  await shot('6b-video-volume');
  {
    const st = (await page.locator('.stage').boundingBox())!;
    await page.mouse.dblclick(st.x + st.width / 2, st.y + st.height / 2);
    await page.waitForTimeout(120);
    await page.screenshot({ path: `${dir}/${info.project.name}-6c-sound-flash.png` });
  }
  await page.keyboard.press('Escape');

  await page.goto('/downloads');
  await shot('7-downloads');
  // Queue rows (faked): a long search, many numbers, a paused one, a failed check.
  {
    const now = Date.now();
    const base = { site: 'safebooru', max_posts: 0, deleted: 0, skipped: 0, blacklisted: 0, failed: 0, created_at: now };
    const searches = [
      { ...base, id: 91, query: 'blue_sky clouds -rain some_artist landscape sunset scenery highres absurdres', state: 'running', site_total: 4210, handled: 1320, downloaded: 1180, already: 96, deleted: 12, blacklisted: 31, position: 0 },
      { ...base, id: 92, query: 'red', state: 'paused', site_total: 860, handled: 120, downloaded: 100, already: 20, position: 1 },
      { ...base, id: 93, query: 'some_artist', state: 'failed', subscription_id: 5, site_total: 0, handled: 0, downloaded: 0, already: 0, error: 'safebooru.org: the site answered 503', position: 2, finished_at: now },
    ];
    await page.route('**/api/downloads', async (route) => {
      if (route.request().method() !== 'GET') return route.continue();
      const res = await route.fetch();
      const body = await res.json();
      body.searches = searches;
      await route.fulfill({ response: res, json: body });
    });
    await page.reload();
    await page.getByRole('heading', { name: 'Queue' }).scrollIntoViewIfNeeded();
    await shot('7e-queue-rows');
    await page.getByRole('button', { name: /^More for the download blue_sky/ }).click();
    await shot('7f-queue-row-menu');
    await page.keyboard.press('Escape');
    await page.unroute('**/api/downloads');
  }
  const subIds: number[] = await page.evaluate(async () => {
    const out: number[] = [];
    for (const [site, q] of [['test-down', 'blue_sky'], ['test-down', 'some_artist'], ['test-slow', 'landscape']]) {
      const r = await fetch('/api/subscriptions', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ site, query: q, interval: 604800, initial_max: 0 }) });
      out.push((await r.json()).id);
    }
    return out;
  });
  await page.reload();
  await page.getByRole('button', { name: 'Fold test-slow.invalid' }).last().click(); // the subscriptions' (the queue has one too)
  await page.getByRole('heading', { name: 'Subscriptions' }).scrollIntoViewIfNeeded();
  await shot('7b-downloads-site-folded');
  // Many sites on several engines (faked): the site menu by engine.
  const extra = [['shimmie_a', 'shimmie-a.invalid', 'shimmie2'], ['shimmie_b', 'shimmie-b.invalid', 'shimmie2'], ['shimmie_c', 'shimmie-c.invalid', 'shimmie2'], ['masto_a', 'masto-a.invalid', 'mastodon'], ['html_a', 'html-a.invalid', 'gelbooru-v02-html']];
  await page.route('**/api/downloads', async (route) => {
    if (route.request().method() !== 'GET') return route.continue();
    const res = await route.fetch();
    const body = await res.json();
    body.sites = [...body.sites, ...extra.map((x) => x[0])];
    await route.fulfill({ response: res, json: body });
  });
  await page.route('**/api/sites', async (route) => {
    const res = await route.fetch();
    const body = await res.json();
    body.sites = [...body.sites, ...extra.map(([id, site, engine]) => ({ id, site, name: site, engine, fields: [], set: {}, version: '1.0.0' }))];
    await route.fulfill({ response: res, json: body });
  });
  await page.reload();
  await page.getByRole('button', { name: 'Site', exact: true }).click();
  await shot('7c-site-menu');
  await page.getByRole('menuitemradio', { name: /^Shimmie2/ }).click();
  await shot('7d-site-menu-engine');
  await page.keyboard.press('Escape');
  await page.keyboard.press('Escape');
  await page.unroute('**/api/downloads');
  await page.unroute('**/api/sites');
  await page.evaluate(async (ids) => {
    for (const id of ids) await fetch(`/api/subscriptions/${id}/remove`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' });
    localStorage.removeItem('vitrine.foldedSites');
  }, subIds);
  const rel = (action: string, from: string) =>
    page.evaluate(([action, from]) => fetch('/api/relations', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ kind: 'parent', action, from, to: 'favorite_artists' }) }), [action, from]);
  for (const c of ['some_artist', 'other_artist', 'blue_sky']) await rel('add', c);
  await page.goto('/tags/rules');
  const input = page.getByRole('combobox', { name: 'Add to the blacklist' });
  await input.fill('red ai_*');
  await input.press('Enter');
  await page.getByText(/files in the library have/).waitFor();
  await shot('8-tags-blacklist');
  for (const c of ['some_artist', 'other_artist', 'blue_sky']) await rel('remove', c);
  for (const t of ['red', 'ai_*']) await page.getByRole('button', { name: `Remove ${t} from the blacklist` }).click();
  await page.goto('/tags');
  await shot('8b-tags');
  await page.goto('/tags/all');
  await shot('8c-tags-all');

  // Duplicates: a pair of videos (faked), one bar for both.
  const vids = await page.evaluate(async () => {
    const ids = (await (await fetch('/api/search?limit=26&q=')).json()).ids as number[];
    const files = (await (await fetch('/api/files?ids=' + ids.join(','))).json()).files as { mime: string }[];
    return files.filter((f) => f.mime.startsWith('video/')).map((f) => ({ ...f, tags: 0, sites: null }));
  });
  await page.route('**/api/dupes?*', (route) =>
    route.fulfill({ json: { total: 1, pairs: [{ a: vids[0], b: vids[1], distance: 3 }], progress: { checked: 1, total: 1 } } }),
  );
  await page.goto('/duplicates');
  await page.getByRole('tab', { name: 'Side by side' }).click();
  await page.waitForTimeout(1200);
  await shot('8d-dupes-videos');
  await page.unroute('**/api/dupes?*');

  await page.goto('/settings');
  await shot('9-settings');
  await page.goto('/settings/library');
  await page.getByRole('button', { name: 'Default sort' }).click();
  await shot('9b-default-sort');
  await page.getByRole('menuitemradio', { name: /File size/ }).click();
  await shot('9c-default-sort-page');
  await page.keyboard.press('Escape');
  await page.keyboard.press('Escape');
  await page.getByRole('group', { name: 'Default search' }).getByRole('combobox').click();
  await page.keyboard.type('blue_s');
  await shot('9d-default-search');
  await page.keyboard.press('Escape');
  await page.getByRole('group', { name: 'Default search' }).getByRole('button', { name: 'Filters', exact: true }).click();
  await shot('9e-default-search-filters');
  await page.keyboard.press('Escape');
  await page.evaluate(() => fetch('/api/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ defaultSearch: '' }) }));
  await page.goto('/settings/notifications');
  await page.getByLabel('Blacklisted tags in the library').scrollIntoViewIfNeeded();
  await shot('9f-notifications');
  await page.evaluate(() => fetch('/api/sites/safebooru/off', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ off: true }) }));
  await page.goto('/settings/sites');
  await page.getByRole('heading', { name: 'safebooru.org' }).scrollIntoViewIfNeeded();
  await shot('9g-site-off');
  await page.getByRole('button', { name: 'More for safebooru.org', exact: true }).click();
  await shot('9h-site-menu');
  await page.keyboard.press('Escape');
  await page.evaluate(() => fetch('/api/sites/safebooru/off', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ off: false }) }));

  const ctx = await browser.newContext({ ...info.project.use });
  const fresh = await ctx.newPage();
  await fresh.goto('/');
  await fresh.waitForTimeout(800);
  await fresh.screenshot({ path: `${dir}/${info.project.name}-0-login.png` });
  await ctx.close();
});
