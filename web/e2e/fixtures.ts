// Each worker has a test server of its own (e2e/setup.ts): its tests go there.
import { test as base, expect, type Page } from '@playwright/test';

export const PORT0 = 8100;

export const test = base.extend({
  baseURL: async ({}, use, info) => {
    await use(`http://127.0.0.1:${PORT0 + info.parallelIndex}`);
  },
  // Square tiles unless a test picks the grid itself (most tests were written for them; Rows became the default,
  // and its own tests pick it).
  page: async ({ page }, use) => {
    await page.addInitScript(() => {
      if (localStorage.getItem('vitrine.gridLayout') === null) localStorage.setItem('vitrine.gridLayout', JSON.stringify('squares'));
    });
    await use(page);
  },
});

export { expect };

/** The test server's account (made on first use). */
export const USER = 'e2e';
export const PASSWORD = 'e2e password';

/**
 * Makes sure the browser is logged in (creating the account on first use).
 * It logs in from inside the page, like the app: a cookie set through
 * Playwright's request API is stored differently, and WebKit then refuses
 * to load videos with it.
 */
export async function login(page: Page) {
  // (What's new after an update off on this device: the test library has never shown it - a window over every test.
  // The test of it switches it on through the server, whose value wins.)
  await page.addInitScript(() => localStorage.setItem('vitrine.whatsNew', 'false'));
  await page.goto('/api/health');
  const status = await page.evaluate(
    async ([name, password]) => {
      const s = await (await fetch('/api/session')).json();
      if (s.logged_in) return 200;
      const r = await fetch(s.setup_needed ? '/api/setup' : '/api/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name, password }),
      });
      return r.status;
    },
    [USER, PASSWORD],
  );
  expect(status).toBe(200);
}
