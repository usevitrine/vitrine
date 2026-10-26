// The website's demo (npm run build:demo <snapshot folder>), when asked: DEMO_URL=<where it's served, e.g.
// http://localhost:4340/demo/app/> npx playwright test demo --project=desktop-chromium. It loads with no server: the
// grid, a search picked from the suggestions (the index as WebAssembly), the details with their tags and source, an
// edit that searches - and a reload starts fresh.
import { expect, test } from '@playwright/test';

const url = process.env.DEMO_URL ?? '';
test.skip(!url, 'set DEMO_URL to a served demo build');

test('demo: the media screen with no server - search, details, an edit for the visit only', async ({ page }) => {
  const warnings: string[] = [];
  page.on('console', (m) => (m.type() === 'warning' || m.type() === 'error') && warnings.push(m.text()));
  page.on('pageerror', (e) => warnings.push(e.message));
  await page.goto(url);
  await expect(page.locator('.tile').first()).toBeVisible({ timeout: 15_000 });
  // The media screen only: no menu, Downloads or bell.
  await expect(page.getByRole('button', { name: 'Menu' })).toHaveCount(0);
  await expect(page.getByRole('button', { name: /^Downloads/ })).toHaveCount(0);
  // A search from the suggestions.
  const box = page.getByRole('combobox').first();
  await box.click();
  await box.fill('sintel');
  await page.getByRole('option', { name: /sintel, 5/ }).first().click();
  await expect(page.getByText(/^5 files/)).toBeVisible();
  // The details: tags by kind, where it's from. A clip plays with its sound (the demo's videos aren't muted).
  await page.locator('.tile').first().click();
  const video = page.locator('.viewer .slide[data-slot="1"] video');
  await expect.poll(() => video.evaluate((v: HTMLVideoElement) => !v.paused)).toBe(true);
  expect(await video.evaluate((v: HTMLVideoElement) => v.muted)).toBe(false);
  await page.getByRole('button', { name: 'Details', exact: true }).click();
  const panel = page.getByRole('complementary', { name: 'Details' });
  await expect(panel.getByText('blender.org', { exact: true }).first()).toBeVisible();
  await expect(panel.getByText('sintel', { exact: true })).toBeVisible();
  // An edit: found by a search - for this visit only.
  await page.evaluate(() =>
    fetch('/api/tags', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ids: [1], add: ['visit_tag'], remove: [] }) }),
  );
  const found = () => page.evaluate(async () => (await (await fetch('/api/search?q=visit_tag&limit=0')).json()).total);
  expect(await found()).toBe(1);
  await page.goto(url); // (a fresh load - the address kept the open file: the viewer would come back over the grid)
  await expect(page.locator('.tile').first()).toBeVisible({ timeout: 15_000 });
  expect(await found()).toBe(0);
  expect(warnings).toEqual([]);
});
