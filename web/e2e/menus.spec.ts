// Every menu, list and small panel on every screen fits: nothing in it sticks
// out past its edges, and it stays inside the window - on a phone and a desktop.
// Opens each menu button it finds (and each page of choices inside a menu).
import { type Page } from '@playwright/test';
import { expect, test, login } from './fixtures';

/** What sticks out of the open menus (empty = all fit). */
async function overflow(page: Page): Promise<string[]> {
  return page.evaluate(() => {
    const out: string[] = [];
    const W = innerWidth;
    const H = innerHeight;
    const name = (el: Element) => (el.getAttribute('aria-label') || el.textContent || el.className.toString()).trim().replace(/\s+/g, ' ').slice(0, 60);
    const roots = [...document.querySelectorAll('[role=menu], [role=listbox], [role=dialog]')].filter((m) => {
      const r = m.getBoundingClientRect();
      if (r.width === 0 || r.height === 0 || m.closest('[inert], [hidden]')) return false;
      return !(r.width >= W - 2 && r.height >= H - 2); // (full-screen views: the viewer, the reader)
    });
    for (const m of roots) {
      const mr = m.getBoundingClientRect();
      if (mr.left < -1 || mr.right > W + 1) out.push(`"${name(m)}" goes past the window (${Math.round(mr.left)}-${Math.round(mr.right)} of ${W})`);
      for (const el of m.querySelectorAll('*')) {
        const r = el.getBoundingClientRect();
        if (r.width === 0 || r.height === 0) continue;
        if (r.right <= mr.right + 1 && r.left >= mr.left - 1) continue;
        // Inside something that scrolls sideways (a row of chips), or that clips well inside the
        // menu (a shortened label): meant. (Clipped only at the menu's own edge - a wrapper as wide
        // as the menu - is the bug this looks for.)
        let clipped = false;
        for (let p = el.parentElement; p && p !== m; p = p.parentElement) {
          const ox = getComputedStyle(p).overflowX;
          if (ox === 'visible') continue;
          if (ox === 'auto' || ox === 'scroll' || p.getBoundingClientRect().width < mr.width - 16) {
            clipped = true;
            break;
          }
        }
        if (!clipped) out.push(`"${name(el)}" sticks out of "${name(m)}" by ${Math.round(Math.max(r.right - mr.right, mr.left - r.left))}px`);
      }
    }
    return [...new Set(out)];
  });
}

const screens = ['/', '/favorites', '/comics', '/comics/favorites', '/downloads', '/tags', '/tags/all', '/duplicates',
  '/settings/library', '/settings/sites', '/settings/rules', '/settings/notifications', '/settings/viewing', '/settings/device',
  '/settings/backups', '/settings/stats', '/settings/account'];

test('menus fit: nothing sticks out of a menu or off the window', async ({ page }) => {
  test.setTimeout(480_000); // (every menu on every screen: a few minutes on a phone)
  await login(page);
  // Something in every list: a subscription in a group with a long name, a download.
  const made = await page.evaluate(async () => {
    const post = async (path: string, body: object) =>
      (await fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })).json();
    const sub = await post('/api/subscriptions', { site: 'test-down', query: 'menus_fit_a_rather_long_search', interval: 3600, initial_max: 0, group_name: 'Individual Galleries' });
    const dl = await post('/api/downloads', { site: 'test-down', query: 'menus_fit_download', max_posts: 0 });
    return { sub: sub.id as number, dl: dl.id as number };
  });
  const problems: string[] = [];
  for (const path of screens) {
    await page.goto(path);
    await page.waitForTimeout(600);
    // Folded groups unfolded (their rows have menus too).
    for (let k = 0; k < 20; k++) {
      const fold = page.locator('main button[aria-expanded=false]:not([aria-haspopup]):visible').first();
      if (!(await fold.count())) break;
      await fold.click({ timeout: 2000 }).catch(() => {});
      await page.waitForTimeout(150);
      if (!page.url().endsWith(path)) await page.goto(path);
    }
    const buttons = page.locator('main button[aria-haspopup]:visible, main button.filt:visible, main button.star:visible');
    const n = Math.min(await buttons.count(), 25);
    for (let k = 0; k < n; k++) {
      const b = buttons.nth(k);
      if (!(await b.isVisible().catch(() => false))) continue;
      const label = (await b.getAttribute('aria-label')) || (await b.textContent()) || '?';
      await b.click({ timeout: 3000 }).catch(() => {});
      await page.waitForTimeout(350);
      for (const p of await overflow(page)) problems.push(`${path} ${label}: ${p}`);
      // A menu's pages of choices (an item with a value and an arrow).
      const items = page.locator('[role=menu] [role=menuitem]:has(.hint):visible');
      const m = Math.min(await items.count(), 6);
      for (let j = 0; j < m; j++) {
        const it = items.nth(j);
        if (!(await it.isVisible().catch(() => false))) break;
        const text = ((await it.textContent()) || '').trim();
        await it.click({ timeout: 2000 }).catch(() => {});
        await page.waitForTimeout(350);
        for (const p of await overflow(page)) problems.push(`${path} ${label} > ${text}: ${p}`);
        const back = page.locator('[role=menu] button.back:visible');
        if (await back.count()) await back.first().click().catch(() => {});
        else break; // (it acted instead of opening a page)
        await page.waitForTimeout(300);
      }
      await page.keyboard.press('Escape');
      await page.waitForTimeout(250);
      if (!page.url().endsWith(path)) await page.goto(path); // (an item moved elsewhere)
    }
  }
  await page.evaluate(async ({ sub, dl }) => {
    await fetch(`/api/subscriptions/${sub}/remove`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' });
    await fetch(`/api/downloads/${dl}/remove`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' });
  }, made);
  expect([...new Set(problems)]).toEqual([]);
});
