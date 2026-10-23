import { test, login } from './fixtures';
const dir = process.env.SHOTS ?? '';
test.skip(!dir, 'SHOTS');
// Where the text in the new tab sits, frame by frame after a tab switch (each heading and card's top).
test('tabs', async ({ page }, info) => {
  await login(page);
  // some history to show: two finished downloads
  await page.evaluate(async () => {
    for (const q of ['tabs_probe_a', 'tabs_probe_b']) await fetch('/api/downloads', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ site: 'test-down', query: q, max_posts: 1 }) });
  });
  await page.goto('/downloads');
  await page.getByRole('tab').first().waitFor();
  await page.waitForTimeout(1500);
  for (const tab of [/History/, /Queue/, /Subscriptions/, /History/]) {
    await page.getByRole('tab', { name: tab }).click();
    const rows: string[] = [];
    const t0 = Date.now();
    for (let i = 0; i < 24; i++) {
      const ys = await page.evaluate(() => {
        const p = document.querySelector('[role=tabpanel]:not([inert])') ?? document.querySelector('[role=tabpanel]');
        if (!p) return '-';
        return [...p.querySelectorAll('h2, h3, .stack')].slice(0, 4).map((e) => Math.round(e.getBoundingClientRect().top * 10) / 10).join(' ');
      });
      rows.push(`${Date.now() - t0}ms: ${ys}`);
      await page.waitForTimeout(25);
    }
    console.log('TAB', String(tab), info.project.name, '\n  ' + rows.join('\n  '));
    await page.waitForTimeout(800);
  }
});
