// TEMP probe (not committed).
import { test, expect, login } from './fixtures';
import fs from 'node:fs';
const OUT = process.env.PROBE_OUT!;
test('probe last dupe', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-chromium');
  await login(page);
  await page.addInitScript(() => localStorage.setItem('vitrine.dupeLevel', '6'));
  const list = () => page.evaluate(() => fetch('/api/dupes?max=6&limit=100').then((r) => r.json()));
  await expect.poll(async () => (await list()).total).toBeGreaterThan(1);
  await page.goto('/duplicates');
  await expect(page.getByRole('heading', { name: 'Duplicates' })).toBeVisible();
  await page.getByRole('tab', { name: 'Side by side' }).click();
  for (;;) {
    const n = (await list()).total;
    if (n <= 1) break;
    await page.keyboard.press('k');
    await expect.poll(async () => (await list()).total).toBe(n - 1);
    await page.waitForTimeout(300);
  }
  await page.goto('/duplicates');
  await page.waitForTimeout(1500);
  fs.mkdirSync(OUT, { recursive: true });
  const cdp = await page.context().newCDPSession(page);
  let i = 0;
  cdp.on('Page.screencastFrame', async (f) => {
    fs.writeFileSync(`${OUT}/f_${String(i++).padStart(3, '0')}.jpg`, Buffer.from(f.data, 'base64'));
    await cdp.send('Page.screencastFrameAck', { sessionId: f.sessionId }).catch(() => {});
  });
  await cdp.send('Page.startScreencast', { format: 'jpeg', quality: 70, everyNthFrame: 1 });
  await page.waitForTimeout(200);
  await page.getByRole('button', { name: 'Keep A' }).click();
  await page.waitForTimeout(1500);
  await cdp.send('Page.stopScreencast');
});
