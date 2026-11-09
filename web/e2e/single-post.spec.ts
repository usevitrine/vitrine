import type { Page } from '@playwright/test';
import { expect, test, login } from './fixtures';

const dialog = (page: Page) => page.locator('[role="dialog"][aria-modal="true"]');

/** New download, on the built-in site, as far as its search box. */
async function toSearch(page: Page) {
  await page.goto('/');
  await page.getByRole('button', { name: /^Downloads/ }).click();
  await page.getByRole('dialog', { name: 'Downloads' }).getByRole('button', { name: 'New download' }).click();
  await dialog(page).getByRole('button', { name: 'safebooru.org' }).click();
  return dialog(page).getByRole('combobox', { name: 'Search on the site' });
}

test.beforeEach(async ({ page }) => {
  await login(page);
});

test('new download: one post by its link - only Download is left, once', async ({ page }) => {
  await page.route('**/api/downloads/single', (r) => r.fulfill({ json: { single: true, query: 'id:123', files: 1 } }));
  let sent: { query?: string } | undefined;
  await page.route('**/api/downloads', (r) => {
    if (r.request().method() !== 'POST') return r.continue();
    sent = r.request().postDataJSON();
    return r.fulfill({ json: { id: 1, site: 'safebooru', query: 'id:123' } });
  });
  const box = await toSearch(page);
  await expect(dialog(page).getByText('For one file: paste its post')).toBeVisible();
  const link = 'https://safebooru.org/index.php?page=post&s=view&id=123&tags=rating%3Asafe+';
  await box.fill(link);
  await dialog(page).getByRole('button', { name: 'Next' }).click();
  await expect(dialog(page).getByText('Just this post: 1 file, once.')).toBeVisible();
  await expect(dialog(page).getByRole('tab')).toHaveCount(0); // (nothing to choose)
  await dialog(page).getByRole('button', { name: 'Download' }).click();
  await expect(dialog(page)).toHaveCount(0);
  expect(sent?.query).toBe(link);
});

test('new download: a link for another site says so in the dialog', async ({ page }) => {
  const box = await toSearch(page);
  await box.fill('https://other.example/index.php?page=post&s=view&id=123');
  await dialog(page).getByRole('button', { name: 'Next' }).click();
  await expect(dialog(page).getByRole('alert')).toHaveText('that link is for other.example, not safebooru.org');
  await expect(box).toBeVisible();
});

test('new download: a post the site does not have says so in the dialog', async ({ page }) => {
  await page.route('**/api/downloads/single', (r) =>
    r.fulfill({ status: 404, json: { error: 'safebooru.org has no post 99 with a file (it may have been deleted)' } }),
  );
  const box = await toSearch(page);
  await box.fill('id:99');
  await dialog(page).getByRole('button', { name: 'Next' }).click();
  await expect(dialog(page).getByRole('alert')).toContainText('has no post 99');
});

test('new download: an ordinary search still chooses once or subscribe', async ({ page }) => {
  const box = await toSearch(page);
  await box.fill('blue_sky');
  await dialog(page).getByRole('button', { name: 'Next' }).click();
  await expect(dialog(page).getByRole('tab')).toHaveCount(2);
});
