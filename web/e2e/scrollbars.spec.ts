// Tests that need scrollbars that take room, as on most desktops (the other tests'
// browser hides scrollbars, so it can't see bugs where one comes and goes).
import { test, expect, login } from './fixtures';

test.use({ launchOptions: { ignoreDefaultArgs: ['--hide-scrollbars'], args: ['--mute-audio'] } });

test.beforeEach(async ({ page }) => login(page));

test('grid: back from another screen, the tiles still reach the right edge (the fast scroller stays, no scrollbar comes and goes)', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-chromium', 'Chromium with real scrollbars');
  expect(await page.evaluate(() => { const d = document.createElement('div'); d.style.cssText = 'width:100px;height:100px;overflow:scroll'; document.body.append(d); const w = d.offsetWidth - d.clientWidth; d.remove(); return w; })).toBeGreaterThan(0);
  await page.setViewportSize({ width: 1000, height: 400 }); // a long grid: the fast scroller shows
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  await expect(page.locator('.fast')).toBeAttached();
  const edges = () =>
    page.evaluate(() => {
      const g = document.getElementById('grid-scroller')!;
      const right = Math.max(...[...g.querySelectorAll('.tile')].map((t) => t.getBoundingClientRect().right));
      return { right: Math.round(right), box: Math.round(g.getBoundingClientRect().right - parseFloat(getComputedStyle(g.parentElement!.parentElement!).paddingRight || '0')) };
    });
  const before = await edges();
  await page.getByRole('button', { name: 'Menu' }).click();
  await page.locator('#nav-drawer').getByRole('button', { name: 'Settings', exact: true }).click();
  await expect(page).toHaveURL(/settings/);
  await page.getByRole('button', { name: 'Menu' }).click();
  await page.locator('#nav-drawer').getByRole('button', { name: 'Media', exact: true }).click();
  await expect(page.locator('#grid-scroller')).toBeVisible();
  await page.waitForTimeout(300);
  expect(await edges()).toEqual(before);
});
