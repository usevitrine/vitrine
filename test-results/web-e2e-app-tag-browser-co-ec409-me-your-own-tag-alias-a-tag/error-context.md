# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: web\e2e\app.spec.ts >> tag browser: counts, filter, namespace, rename your own tag, alias a tag
- Location: web\e2e\app.spec.ts:1671:1

# Error details

```
Error: page.goto: net::ERR_CONNECTION_REFUSED at http://127.0.0.1:8100/api/health
Call log:
  - navigating to "http://127.0.0.1:8100/api/health", waiting until "load"

```

# Test source

```ts
  1  | // Each worker has a test server of its own (e2e/setup.ts): its tests go there.
  2  | import { test as base, expect, type Page } from '@playwright/test';
  3  | 
  4  | export const PORT0 = 8100;
  5  | 
  6  | export const test = base.extend({
  7  |   baseURL: async ({}, use, info) => {
  8  |     await use(`http://127.0.0.1:${PORT0 + info.parallelIndex}`);
  9  |   },
  10 |   // Square tiles unless a test picks the grid itself (most tests were written for them; Rows became the default,
  11 |   // and its own tests pick it).
  12 |   page: async ({ page }, use) => {
  13 |     await page.addInitScript(() => {
  14 |       if (localStorage.getItem('vitrine.gridLayout') === null) localStorage.setItem('vitrine.gridLayout', JSON.stringify('squares'));
  15 |     });
  16 |     await use(page);
  17 |   },
  18 | });
  19 | 
  20 | export { expect };
  21 | 
  22 | /** The test server's account (made on first use). */
  23 | export const USER = 'e2e';
  24 | export const PASSWORD = 'e2e password';
  25 | 
  26 | /**
  27 |  * Makes sure the browser is logged in (creating the account on first use).
  28 |  * It logs in from inside the page, like the app: a cookie set through
  29 |  * Playwright's request API is stored differently, and WebKit then refuses
  30 |  * to load videos with it.
  31 |  */
  32 | export async function login(page: Page) {
> 33 |   await page.goto('/api/health');
     |              ^ Error: page.goto: net::ERR_CONNECTION_REFUSED at http://127.0.0.1:8100/api/health
  34 |   const status = await page.evaluate(
  35 |     async ([name, password]) => {
  36 |       const s = await (await fetch('/api/session')).json();
  37 |       if (s.logged_in) return 200;
  38 |       const r = await fetch(s.setup_needed ? '/api/setup' : '/api/login', {
  39 |         method: 'POST',
  40 |         headers: { 'Content-Type': 'application/json' },
  41 |         body: JSON.stringify({ name, password }),
  42 |       });
  43 |       return r.status;
  44 |     },
  45 |     [USER, PASSWORD],
  46 |   );
  47 |   expect(status).toBe(200);
  48 | }
  49 | 
```