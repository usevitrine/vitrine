import { type Page } from '@playwright/test';
import { expect, test, login, USER, PASSWORD } from './fixtures';

const isPhone = (page: Page) => (page.viewportSize()?.width ?? 0) < 900;

/** Unfolds a menu item's sub-pages (folded to begin with). */
async function unfold(page: Page, screen: string) {
  const b = page.locator(`#nav-drawer .fold[aria-describedby="menu-${screen}"]`);
  if ((await b.getAttribute('aria-expanded')) === 'false') await b.click();
}

/** Answers the confirmation dialog (a destructive action) with its action button. */
async function confirmIn(page: Page, action: string | RegExp) {
  const d = page.getByRole('alertdialog');
  await expect(d).toBeVisible();
  await d.getByRole('button', { name: action, exact: typeof action === 'string' }).click();
  await expect(d).toHaveCount(0);
}

test.beforeEach(async ({ page }) => {
  await login(page);
});

/** The New download dialog (the Library's download button, a tag's New download). */
const newDl = (page: Page) => page.locator('[role="dialog"][aria-modal="true"]');

test('library loads without horizontal overflow', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
  expect(overflow).toBeLessThanOrEqual(0);
  // Tiles fill the width: at least 3 columns on a phone.
  const tiles = page.locator('.tile');
  await expect(tiles.first()).toBeVisible();
  const first = await tiles.nth(0).boundingBox();
  const third = await tiles.nth(2).boundingBox();
  expect(first!.y).toBe(third!.y); // same row
});

test('inputs are at least 16px on touch screens (no zoom on focus)', async ({ page }) => {
  test.skip(!isPhone(page), 'phone only');
  await page.goto('/');
  const size = await page.locator('.field input').evaluate((el) => parseFloat(getComputedStyle(el).fontSize));
  expect(size).toBeGreaterThanOrEqual(16);
});

/** Unfolds the stacks in scope (folded, only each site's front card can be used). */
/** The Downloads page's tabs: Queue, Subscriptions, History (one shows at a time). */
async function dlTab(page: import('@playwright/test').Page, name: 'Queue' | 'Subscriptions' | 'History') {
  const tab = page.getByRole('tab', { name: new RegExp('^' + name) });
  await tab.click();
  await expect(tab).toHaveAttribute('aria-selected', 'true');
}

async function unfoldStacks(scope: import('@playwright/test').Locator) {
  for (const b of await scope.getByRole('button', { name: /^Show all/ }).all()) await b.click();
}

/** Picks an action (and a choice on its page) from the ⋯ menu of a subscription or group. */
async function rowMenu(page: import('@playwright/test').Page, row: string, item: string, choice?: string) {
  // (Opened again if a refresh of the list closed it before it showed - the row re-rendering under the click.)
  const entry = page.getByRole('menuitem', { name: new RegExp('^' + item) });
  for (let i = 0; i < 3; i++) {
    await page.getByRole('button', { name: `More for ${row}`, exact: true }).click();
    if (await entry.waitFor({ timeout: 2000 }).then(() => true, () => false)) break;
  }
  await entry.click();
  if (choice) await page.getByRole('menuitemradio', { name: choice, exact: true }).click();
}

/** Picks a result order ("Oldest imported") in the search bar's Sort and filters menu. */
/** New download from the top bar's downloads menu. */
async function openNewDownload(page: import('@playwright/test').Page) {
  await page.getByRole('button', { name: /^Downloads/ }).click();
  await page.getByRole('dialog', { name: 'Downloads' }).getByRole('button', { name: 'New download' }).click();
}

async function pickSort(page: import('@playwright/test').Page, label: string) {
  const [dir, field] = label.split(' ');
  await page.getByRole('button', { name: 'Filters', exact: true }).click();
  await page.getByRole('button', { name: /^Sort by/ }).click(); // the Sort by page
  await page.getByRole('dialog', { name: 'Sort' }).getByRole('button', { name: new RegExp('^' + field, 'i') }).click(); // its directions
  await page.getByRole('button', { name: new RegExp('^' + dir + ' first') }).click(); // back to the menu
  await expect(page.getByRole('button', { name: /^Sort by/ })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('button', { name: /^Sort by/ })).toHaveCount(0);
}

/** The order the menu shows as picked ("Newest uploaded"). */
async function shownSort(page: import('@playwright/test').Page) {
  await page.getByRole('button', { name: 'Filters', exact: true }).click();
  const pick = page.getByRole('button', { name: /^Sort by/ });
  const field = await pick.getAttribute('data-value');
  const dir = await pick.getAttribute('data-dir');
  await page.keyboard.press('Escape');
  return `${dir} ${field}`;
}

test('top bar: menu button, logo and the file count above the search bar', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByRole('navigation')).toHaveCount(0);
  const top = (await page.locator('.topbar').boundingBox())!;
  const btn = (await page.getByRole('button', { name: 'Menu' }).boundingBox())!;
  const logo = (await page.locator('.topbar .logo').boundingBox())!;
  await expect(page.locator('.topbar .count')).toHaveText('26 files');
  const sort = (await page.locator('.topbar .count').boundingBox())!;
  const bar = (await page.locator('.field').boundingBox())!;
  for (const b of [btn, logo, sort]) {
    expect(b.y).toBeGreaterThanOrEqual(top.y);
    expect(b.y + b.height).toBeLessThanOrEqual(top.y + top.height + 0.5);
  }
  expect(btn.x).toBeLessThan(logo.x);
  expect(sort.x).toBeGreaterThan(logo.x + logo.width);
  expect(bar.y).toBeGreaterThanOrEqual(top.y + top.height); // search bar below the top bar
  await expect(page.getByText(/under 1 ms|\d ms\b/)).toHaveCount(0); // no search timing any more
  // Other screens keep the bar but not the library's count.
  await page.getByRole('button', { name: 'Menu' }).click();
  await page.getByRole('navigation').getByRole('button', { name: 'Settings' }).click();
  await expect(page.locator('.topbar .logo')).toBeVisible();
  await expect(page.locator('.topbar .count')).toHaveCount(0);
  await page.locator('.topbar .home').click();
  await expect(page.locator('.topbar .count')).toBeVisible();
});

test('navigation: a drawer behind the menu button on every device', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'Menu' }).click();
  await page.getByRole('navigation', { name: 'Main' }).getByRole('button', { name: 'Tags', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Parents' })).toBeVisible();
  await expect(page.getByRole('navigation', { name: 'Main' })).toHaveCount(0); // closed after picking
  // Tapping outside closes it too.
  await page.getByRole('button', { name: 'Menu' }).click();
  await page.mouse.click(page.viewportSize()!.width - 10, page.viewportSize()!.height / 2);
  await expect(page.getByRole('navigation', { name: 'Main' })).toHaveCount(0);
});

test('typing a tag and a space makes a chip and searches', async ({ page }) => {
  await page.goto('/');
  const input = page.locator('.field input');
  await input.click();
  await input.pressSequentially('blue_s');
  await expect(page.getByRole('option', { name: /blue_sky/ })).toBeVisible();
  await input.pressSequentially('ky ');
  await expect(page.locator('.chip')).toHaveText(/blue_sky/);
  await expect(page.getByText(/^4 files/)).toBeVisible();
  expect(new URL(page.url()).searchParams.get('q')).toBe('blue_sky');
});

test('chip editor: OR alternative', async ({ page }) => {
  await page.goto('/?q=blue_sky');
  await expect(page.getByText(/^4 files/)).toBeVisible();
  await page.locator('.chip').first().click();
  await page.getByRole('button', { name: 'Add alternative (OR)' }).click();
  await page.keyboard.type('red');
  await page.keyboard.press('Escape'); // close suggestions
  await page.keyboard.press('Enter');
  await expect(page.getByText(/^6 files/)).toBeVisible();
  await expect(page.locator('.chip').first()).toContainText('OR');
  // A group: each alternative has its own include/exclude toggle.
  await page.locator('.chip-main').first().click();
  await expect(page.getByRole('dialog', { name: 'Edit search chip' }).getByRole('button', { name: /^Included: exclude it/ })).toHaveCount(2);
});

test('viewer: open, swipe to the next file, swipe down to close', async ({ page }) => {
  await page.goto('/');
  await page.locator('.tile').nth(0).click();
  await expect(page.getByText('1 / 26')).toBeVisible();

  // Swipe left with a drag (pointer events, like a finger).
  const stage = page.locator('.stage');
  const box = (await stage.boundingBox())!;
  const y = box.y + box.height / 2;
  await page.mouse.move(box.x + box.width * 0.8, y);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width * 0.5, y, { steps: 5 });
  await page.mouse.move(box.x + box.width * 0.2, y, { steps: 5 });
  await page.mouse.up();
  await expect(page.getByText('2 / 26')).toBeVisible();

  // Swipe down closes.
  const x = box.x + box.width / 2;
  await page.mouse.move(x, box.y + 100);
  await page.mouse.down();
  await page.mouse.move(x, box.y + 250, { steps: 5 });
  await page.mouse.move(x, box.y + 400, { steps: 5 });
  await page.mouse.up();
  await expect(page.locator('.viewer')).toHaveCount(0);
});

test('viewer: the file being viewed has no tile behind it (dragging down shows an empty place to shrink into), and gets it back once closed', async ({ page }) => {
  await page.goto('/');
  const tile = page.locator('.tile').nth(2);
  const id = await tile.getAttribute('data-id');
  await tile.click();
  await expect(page.getByText('3 / 26')).toBeVisible();
  const behind = page.locator(`.tile[data-id="${id}"]`);
  await expect(behind).toHaveCSS('visibility', 'hidden');
  // Dragged down a little (the viewer fades, the grid shows): still no tile there.
  const box = (await page.locator('.stage').boundingBox())!;
  const x = box.x + box.width / 2;
  await page.mouse.move(x, box.y + 100);
  await page.mouse.down();
  await page.mouse.move(x, box.y + 160, { steps: 5 });
  await expect(behind).toHaveCSS('visibility', 'hidden');
  await page.mouse.up();
  // Closed: the picture lands on its place, which shows its tile again - its badges fading in (landed).
  await behind.evaluate((t) => {
    (window as any).landed = false;
    new MutationObserver(() => t.classList.contains('landed') && ((window as any).landed = true)).observe(t, { attributes: true });
  });
  await page.keyboard.press('Escape');
  await expect(page.locator('.viewer')).toHaveCount(0);
  await expect(behind).toHaveCSS('visibility', 'visible');
  await expect.poll(() => page.evaluate(() => (window as any).landed)).toBe(true);
});

test('videos play inline, muted, with our own controls', async ({ page }) => {
  await page.goto('/?q=system:type%3Dvideo');
  await expect(page.getByText(/^2 files/)).toBeVisible();
  await page.locator('.tile').nth(0).click();
  const video = page.locator('.viewer video');
  await expect(video).toHaveCount(1);
  const attrs = await video.evaluate((v: HTMLVideoElement) => ({
    playsinline: v.hasAttribute('playsinline'),
    controls: v.controls,
    muted: v.muted,
  }));
  expect(attrs).toEqual({ playsinline: true, controls: false, muted: true });
  await expect(page.getByRole('slider', { name: 'Position' })).toBeVisible();
  await expect(page.getByRole('button', { name: /Unmute/ })).toBeVisible();
  // Once the video shows a frame, its thumbnail underneath is hidden (no edge peeking out).
  await expect(page.locator('.viewer .slide[data-slot="1"] > img')).toBeHidden();
});

test('tall and wide images fit the viewer', async ({ page }) => {
  for (const q of ['system:height>2000', 'system:width>3000']) {
    await page.goto('/?q=' + encodeURIComponent(q));
    await page.locator('.tile').first().click();
    const img = page.locator('.viewer .slide[data-slot="1"] img');
    await expect(img).toBeVisible();
    const b = (await img.boundingBox())!;
    const vp = page.viewportSize()!;
    expect(b.width).toBeLessThanOrEqual(vp.width + 1);
    expect(b.height).toBeLessThanOrEqual(vp.height + 1);
    await page.keyboard.press('Escape');
  }
});

test('reload reopens the open file', async ({ page }) => {
  await page.goto('/');
  await page.locator('.tile').nth(3).click();
  await expect(page.getByText('4 / 26')).toBeVisible();
  await page.reload();
  await expect(page.getByText('4 / 26')).toBeVisible();
});

test('tagging from the viewer: the pencil edits (add, remove), the check saves', async ({ page }) => {
  await page.goto('/');
  await page.locator('.tile').nth(10).click();
  await page.getByRole('button', { name: 'Details', exact: true }).click(); // hidden by default
  const panel = page.locator('.viewer aside.panel');
  await expect(panel.getByRole('button', { name: /^Remove / })).toHaveCount(0); // no × until editing
  await expect(panel.getByRole('combobox', { name: 'Add tags' })).toHaveCount(0);
  const tagsOf = () =>
    page.evaluate(async () => {
      const i = Number(new URL(location.href).searchParams.get('f'));
      return ((await (await fetch('/api/files/' + i + '/tags')).json()).tags as { tag: string }[]).map((t) => t.tag);
    });
  // Add: shown at once (not saved yet), saved with the check.
  await panel.getByRole('button', { name: 'Edit tags' }).click();
  const add = panel.getByRole('combobox', { name: 'Add tags' });
  await add.pressSequentially('e2e_tag');
  await add.press('Enter');
  await expect(panel.getByRole('button', { name: 'e2e_tag', exact: true })).toBeVisible();
  await expect(panel.getByText('1 to add')).toBeVisible();
  expect(await tagsOf()).not.toContain('e2e_tag');
  await panel.getByRole('button', { name: 'Save tags' }).click();
  await expect(page.getByText('Tags saved (added 1)')).toBeVisible();
  await expect.poll(tagsOf).toContain('e2e_tag');
  await expect(panel.getByRole('button', { name: 'Edit tags' })).toBeVisible();
  // Remove: × marks it, × again keeps it, × once more and save removes it.
  await panel.getByRole('button', { name: 'Edit tags' }).click();
  await panel.getByRole('button', { name: 'Remove e2e_tag' }).click();
  await panel.getByRole('button', { name: 'Keep e2e_tag' }).click();
  await panel.getByRole('button', { name: 'Remove e2e_tag' }).click();
  await expect(panel.getByText('1 to remove')).toBeVisible();
  await panel.getByRole('button', { name: 'Save tags' }).click();
  await expect.poll(tagsOf).not.toContain('e2e_tag');
  await expect(panel.getByRole('button', { name: 'e2e_tag', exact: true })).toHaveCount(0);
});

test('video gestures: double-tap seeks, hold plays at 2x, tap hides controls', async ({ page }) => {
  test.skip(test.info().project.name !== 'iphone', 'touch gestures (a mouse: a click zooms or plays/pauses, keys seek)');
  await page.goto('/?q=system:type%3Dvideo%20system:has_audio');
  await page.locator('.tile').first().click();
  const video = page.locator('.viewer video');
  await expect(video).toHaveCount(1);
  const time = () => video.evaluate((v: HTMLVideoElement) => v.currentTime);

  await expect.poll(time).toBeGreaterThan(0.2); // playing (muted autoplay)

  const box = (await page.locator('.stage').boundingBox())!;
  const y = box.y + box.height / 2;

  // Double tap on the right third: +10 s.
  const before = await time();
  await page.mouse.dblclick(box.x + box.width * 0.85, y);
  await expect(page.locator('.flash.right')).toBeVisible(); // shown briefly
  await expect.poll(time).toBeGreaterThan(before + 8);

  // Press and hold: 2x while held, back to normal on release.
  await page.mouse.move(box.x + box.width / 2, y);
  await page.mouse.down();
  await page.waitForTimeout(700);
  expect(await video.evaluate((v: HTMLVideoElement) => v.playbackRate)).toBe(2);
  await expect(page.locator('.flash.hold')).toBeVisible();
  await page.mouse.up();
  expect(await video.evaluate((v: HTMLVideoElement) => v.playbackRate)).toBe(1);

  // A single tap hides the controls (after the double-tap window).
  await page.waitForTimeout(400);
  await page.mouse.click(box.x + box.width / 2, y);
  await expect(page.locator('.viewer .top')).toHaveClass(/hide/);
});

/** The current image's zoom scale (1 = not zoomed). */
async function imageScale(page: Page) {
  return page.locator('.viewer .slide[data-slot="1"] img').evaluate((img) => {
    const t = getComputedStyle(img).transform;
    return t === 'none' ? 1 : new DOMMatrix(t).a;
  });
}

test('details are hidden by default; swipe up opens them', async ({ page }) => {
  await page.goto('/?q=system:type%3Dimage');
  await page.locator('.tile').first().click();
  await expect(page.locator('.viewer aside.panel')).toHaveCount(0);
  const box = (await page.locator('.stage').boundingBox())!;
  const x = box.x + box.width / 2;
  await page.mouse.move(x, box.y + box.height * 0.7);
  await page.mouse.down();
  await page.mouse.move(x, box.y + box.height * 0.5, { steps: 4 });
  await page.mouse.move(x, box.y + box.height * 0.3, { steps: 4 });
  await page.mouse.up();
  await expect(page.locator('.viewer aside.panel')).toBeVisible();
});

test('images: zoom to 100 % (phones: a double tap; a mouse: a click) and back to fit; the size shows at the top', async ({ page }) => {
  await page.goto('/?q=system:type%3Dimage');
  await page.locator('.tile').first().click();
  const box = (await page.locator('.stage').boundingBox())!;
  const pct = page.locator('.viewer .pct');
  await expect(pct).toHaveText(/^\d+ %$/); // fitted: some %
  const fitted = await pct.textContent();
  const mouseUI = test.info().project.name !== 'iphone';
  const zoom = () =>
    mouseUI ? page.mouse.click(box.x + box.width / 2, box.y + box.height / 2) : page.mouse.dblclick(box.x + box.width / 2, box.y + box.height / 2);
  await zoom();
  await expect(pct).toHaveText('100 %');
  await expect.poll(() => imageScale(page)).toBeGreaterThan(1);
  await page.waitForTimeout(400);
  await zoom();
  await expect.poll(() => imageScale(page)).toBe(1);
  await expect(pct).toHaveText(fitted!);
  if (mouseUI) await expect(page.locator('.viewer .top')).not.toHaveClass(/hide/); // a mouse never hides the controls
});

test('a mouse: moving to the next file is instant (no slide); a click on a video plays/pauses it', async ({ page }) => {
  test.skip(test.info().project.name === 'iphone', 'a mouse');
  await page.goto('/');
  await page.locator('.tile').nth(1).click();
  await expect(page.getByText(/^2 \/ /)).toBeVisible();
  await page.keyboard.press('ArrowLeft');
  expect(await page.locator('.viewer .track .slide.animate').count()).toBe(0);
  await expect(page.getByText(/^1 \/ /)).toBeVisible(); // at once
  // Still instant after a click zoomed in and back out.
  await page.goto('/?q=system:type%3Dimage');
  await page.locator('.tile').first().click();
  await expect(page.getByText(/^1 \/ /)).toBeVisible();
  await expect(page.locator('.viewer .pct')).toBeVisible();
  const stage = (await page.locator('.stage').boundingBox())!;
  await page.mouse.click(stage.x + stage.width / 2, stage.y + stage.height / 2);
  await expect.poll(() => imageScale(page)).toBeGreaterThan(1);
  await page.mouse.click(stage.x + stage.width / 2, stage.y + stage.height / 2);
  await expect.poll(() => imageScale(page)).toBe(1);
  await page.keyboard.press('ArrowRight');
  expect(await page.locator('.viewer .track .slide.animate').count()).toBe(0);
  await expect(page.getByText(/^2 \/ /)).toBeVisible();
  // A drag with the mouse held down still slides on when let go.
  await page.mouse.move(stage.x + stage.width * 0.8, stage.y + stage.height / 2);
  await page.mouse.down();
  await page.mouse.move(stage.x + stage.width * 0.2, stage.y + stage.height / 2, { steps: 8 });
  await page.mouse.up();
  expect(await page.locator('.viewer .track .slide.animate').count()).toBeGreaterThan(0);
  await expect(page.getByText(/^3 \/ /)).toBeVisible();
  await page.goto('/?q=system:type%3Dvideo%20system:has_audio');
  await page.locator('.tile').first().click();
  const video = page.locator('.viewer video');
  await expect.poll(() => video.evaluate((v: HTMLVideoElement) => v.paused)).toBe(false);
  const box = (await page.locator('.stage').boundingBox())!;
  await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
  await expect.poll(() => video.evaluate((v: HTMLVideoElement) => v.paused)).toBe(true);
  await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
  await expect.poll(() => video.evaluate((v: HTMLVideoElement) => v.paused)).toBe(false);
  await expect(page.locator('.viewer .pct')).toHaveCount(0); // videos don't zoom
});

test('images: pinch zooms (two touch pointers)', async ({ page }) => {
  await page.goto('/?q=system:type%3Dimage');
  await page.locator('.tile').first().click();
  await expect(page.locator('.viewer .slide[data-slot="1"] img')).toBeVisible();
  await page.locator('.stage').evaluate((stage) => {
    const r = stage.getBoundingClientRect();
    const cx = r.left + r.width / 2;
    const cy = r.top + r.height / 2;
    const fire = (type: string, id: number, x: number) =>
      stage.dispatchEvent(
        new PointerEvent(type, { pointerId: id, pointerType: 'touch', clientX: x, clientY: cy, button: 0, bubbles: true }),
      );
    fire('pointerdown', 11, cx - 40);
    fire('pointerdown', 12, cx + 40);
    for (let d = 40; d <= 120; d += 20) {
      fire('pointermove', 11, cx - d);
      fire('pointermove', 12, cx + d);
    }
    fire('pointerup', 11, cx - 120);
    fire('pointerup', 12, cx + 120);
  });
  expect(await imageScale(page)).toBeGreaterThan(2.5); // 240 / 80 = 3x
});

test('Back closes the viewer and stays in the library', async ({ page }) => {
  await page.goto('/');
  await page.locator('.tile').nth(2).click();
  await expect(page.locator('.viewer')).toHaveCount(1);
  await page.goBack();
  await expect(page.locator('.viewer')).toHaveCount(0);
  await expect(page.getByText(/^26 files/)).toBeVisible();
  expect(new URL(page.url()).pathname).toBe('/');
});

test('desktop: wheel moves, ctrl+wheel zooms, middle-click closes (not while zoomed: it pans)', async ({ page }) => {
  test.skip(isPhone(page), 'desktop only');
  await page.goto('/?q=system:type%3Dimage');
  await page.locator('.tile').first().click();
  const box = (await page.locator('.stage').boundingBox())!;
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.wheel(0, 120);
  await expect(page.getByText(/^2 \/ /)).toBeVisible();
  // As fast as the wheel turns: five quick clicks, five files (no waiting between).
  await page.locator('.stage').evaluate((el) => {
    for (let k = 0; k < 5; k++) el.dispatchEvent(new WheelEvent('wheel', { deltaY: 100, bubbles: true, cancelable: true }));
  });
  await expect(page.getByText(/^7 \/ /)).toBeVisible();
  // A trackpad's small steps add up: 10 x 15 px is one file, not ten.
  await page.locator('.stage').evaluate((el) => {
    for (let k = 0; k < 10; k++) el.dispatchEvent(new WheelEvent('wheel', { deltaY: -15, bubbles: true, cancelable: true }));
  });
  await expect(page.getByText(/^6 \/ /)).toBeVisible();
  await page.keyboard.down('Control');
  await page.mouse.wheel(0, -200);
  await page.keyboard.up('Control');
  await expect.poll(() => imageScale(page)).toBeGreaterThan(1.5);
  // Zoomed, the middle button is for panning: it doesn't close.
  await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2, { button: 'middle' });
  await expect(page.locator('.viewer')).toBeVisible();
  await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2); // back to the fit
  await expect.poll(() => imageScale(page)).toBe(1);
  await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2, { button: 'middle' });
  await expect(page.locator('.viewer')).toHaveCount(0);
});

/** Simulated touch on the grid (Playwright's touchscreen can only tap). */
async function touchGrid(page: Page, steps: { type: 'down' | 'move' | 'up'; tile: number; wait?: number }[]) {
  for (const s of steps) {
    const box = (await page.locator(`.tile[data-i="${s.tile}"]`).boundingBox())!;
    await page.locator('.scroller').evaluate(
      (el, { type, x, y }) => {
        const target = document.elementFromPoint(x, y) ?? el;
        target.dispatchEvent(
          new PointerEvent('pointer' + type, { pointerId: 7, pointerType: 'touch', clientX: x, clientY: y, button: 0, bubbles: true }),
        );
      },
      { type: s.type, x: box.x + box.width / 2, y: box.y + box.height / 2 },
    );
    if (s.wait) await page.waitForTimeout(s.wait);
  }
}

test('grid: long-press menu, then selection mode and bulk tagging', async ({ page }) => {
  await page.goto('/?q=system:type%3Dimage');
  await expect(page.locator('.tile[data-i="0"]')).toBeVisible();

  // Long-press (touch) or right-click (mouse) opens the file menu.
  if (isPhone(page)) await touchGrid(page, [{ type: 'down', tile: 0, wait: 600 }, { type: 'up', tile: 0 }]);
  else await page.locator('.tile[data-i="0"]').click({ button: 'right' });
  await expect(page.getByRole('menu')).toBeVisible();
  await page.getByRole('menuitem', { name: 'Select' }).click();
  await expect(page.getByText('1 selected')).toBeVisible();

  // In selection mode a tap selects instead of opening.
  await page.locator('.tile[data-i="1"]').click();
  await expect(page.getByText('2 selected')).toBeVisible();
  await expect(page.locator('.viewer')).toHaveCount(0);

  // Bulk add a tag to both.
  await page.getByRole('toolbar').getByRole('button', { name: 'Tags', exact: true }).click();
  const box = page.getByRole('combobox', { name: 'Add tags to the selected files' });
  const bulkTag = 'bulk_e2e_' + Date.now().toString(36); // (new each run: the files keep it - a second run on the same server found it there)
  await box.fill(bulkTag);
  await box.press('Enter');
  await expect(page.getByText('1 to add')).toBeVisible();
  await page.getByRole('dialog', { name: 'Tags of the selected files' }).getByRole('button', { name: 'Save' }).click();
  await expect(page.getByText(/Saved for 2 files: added 1 tag/)).toBeVisible();

  await page.goto('/?q=' + bulkTag);
  await expect(page.getByText(/^2 files/)).toBeVisible();
});

test('grid: drag across thumbnails selects them', async ({ page }) => {
  await page.goto('/?q=system:type%3Dimage');
  await expect(page.locator('.tile[data-i="4"]')).toBeVisible();
  if (isPhone(page)) {
    // Long-press, then slide over tiles 0..4.
    await touchGrid(page, [
      { type: 'down', tile: 0, wait: 600 },
      { type: 'move', tile: 1 },
      { type: 'move', tile: 4 },
      { type: 'up', tile: 4 },
    ]);
  } else {
    const a = (await page.locator('.tile[data-i="0"]').boundingBox())!;
    const b = (await page.locator('.tile[data-i="4"]').boundingBox())!;
    await page.mouse.move(a.x + a.width / 2, a.y + a.height / 2);
    await page.mouse.down();
    await page.mouse.move(b.x + b.width / 2, b.y + b.height / 2, { steps: 8 });
    await page.mouse.up();
  }
  await expect(page.getByText('5 selected')).toBeVisible();
  await expect(page.locator('.viewer')).toHaveCount(0);
});

test('grid: ctrl/shift-click select, bulk delete with undo', async ({ page }) => {
  test.skip(isPhone(page), 'desktop only');
  await page.goto('/?q=system:type%3Dimage');
  await page.locator('.tile[data-i="0"]').click({ modifiers: ['Control'] });
  await page.locator('.tile[data-i="3"]').click({ modifiers: ['Shift'] });
  await expect(page.getByText('4 selected')).toBeVisible();
  await page.locator('.tile[data-i="5"]').click({ modifiers: ['Control'] });
  await expect(page.getByText('5 selected')).toBeVisible();

  await page.getByRole('button', { name: 'Delete selected' }).click();
  const dialog = page.getByRole('alertdialog', { name: 'Delete 5 files?' });
  await expect(dialog.locator('.thumbs img')).toHaveCount(3); // what goes, in pictures
  await confirmIn(page, 'Delete 5');
  await expect(page.getByText(/^19 files/)).toBeVisible();
  await page.getByRole('button', { name: 'Undo' }).click();
  await expect(page.getByText(/^24 files/)).toBeVisible();
});

test('viewer: the next image keeps its element (no reload flash) and fast moves are not throttled', async ({ page }) => {
  await page.goto('/?q=system:type%3Dimage');
  await page.locator('.tile').first().click();
  await expect(page.getByText(/^1 \/ /)).toBeVisible();

  // Mark the preloaded next image, move, and check the current image is that same element.
  await page.locator('.viewer .slide[data-slot="2"] img').evaluate((img) => ((img as any).__marker = 'next'));
  await page.keyboard.press('ArrowRight');
  await expect(page.getByText(/^2 \/ /)).toBeVisible();
  await expect
    .poll(() => page.locator('.viewer .slide[data-slot="1"] img').evaluate((img) => (img as any).__marker ?? null))
    .toBe('next');

  // Four quick moves in a row all count (a move in progress is finished, not ignored).
  for (let k = 0; k < 4; k++) await page.keyboard.press('ArrowRight');
  await expect(page.getByText(/^6 \/ /)).toBeVisible();
});


test('the app fills the whole window', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  const fill = await page.evaluate(() => {
    const r = document.getElementById('app')!.getBoundingClientRect();
    return { top: r.top, bottom: r.bottom, h: innerHeight, w: r.width, iw: innerWidth };
  });
  expect(fill.top).toBe(0);
  expect(fill.bottom).toBe(fill.h);
  expect(fill.w).toBe(fill.iw);
});

test('sort menu: kept apart from the search, default sort setting', async ({ page }) => {
  const firstId = () =>
    page.evaluate(async () => {
      const p = new URL(location.href).searchParams;
      const q = (p.get('q') ?? '') + (p.get('sort') ? ' sort:' + p.get('sort') : '');
      const r = await (await fetch('/api/search?limit=1&q=' + encodeURIComponent(q))).json();
      return r.ids[0] as number;
    });
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  const newest = await firstId();

  await pickSort(page, 'Oldest imported');
  await expect(page.getByRole('button', { name: 'Filters', exact: true }).locator('.dot')).toBeVisible(); // not the default order
  await expect.poll(() => new URL(page.url()).searchParams.get('sort')).toBe('imported_asc');
  expect(new URL(page.url()).searchParams.get('q') ?? '').toBe('');
  await expect(page.locator('.chip')).toHaveCount(0); // no sort chip
  await expect.poll(firstId).toBe(1);

  // A sort typed into the search goes to the menu, not into the chips.
  const input = page.getByPlaceholder(/Tag search/);
  await input.fill('sort:imported');
  await input.press('Enter');
  await expect.poll(() => new URL(page.url()).searchParams.get('sort')).toBe('imported');
  await expect(page.locator('.chip')).toHaveCount(0);
  expect(await shownSort(page)).toBe('Newest imported');
  expect(await firstId()).toBe(newest); // (no upload times in the test library: the same order as the default)

  // Default sort from Settings (shared, on the server) applies when nothing else is picked.
  const setSetting = (values: Record<string, unknown>) =>
    page.evaluate((v) => fetch('/api/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(v) }), values);
  await setSetting({ defaultSort: 'imported_asc' });
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  expect(await shownSort(page)).toBe('Oldest imported');
  await expect(page.getByRole('button', { name: 'Filters', exact: true }).locator('.dot')).toHaveCount(0); // it's the default
  expect(new URL(page.url()).searchParams.get('sort')).toBeNull(); // the default isn't written to the URL
  await setSetting({ defaultSort: 'uploaded' });
});

test('settings are shared through the server; top spacing stays on the device', async ({ page }) => {
  test.skip(test.info().project.name !== 'iphone', 'one run is enough: settings are on the server (top spacing: phones only)');
  await page.goto('/settings/viewing');
  const muted = page.getByRole('checkbox', { name: /Start videos muted/ });
  await expect(muted).toBeChecked();
  const saved = page.waitForRequest((r) => r.url().endsWith('/api/settings') && r.method() === 'POST' && 'startMuted' in (r.postDataJSON() ?? {}));
  await muted.uncheck();
  expect((await saved).postDataJSON()).toMatchObject({ startMuted: false }); // what changed (sent with any other change still waiting)
  await page.getByRole('slider', { name: 'Top spacing' }).fill('40');
  await page.waitForTimeout(900);
  const server = await page.evaluate(async () => (await fetch('/api/settings')).json());
  expect(server.startMuted).toBe(false);
  expect(server.topSpacing).toBeUndefined();

  // "Another device": nothing in this browser's storage - the shared values come from the server.
  await page.evaluate(() => localStorage.clear());
  await page.reload();
  await expect(muted).not.toBeChecked();
  await expect(page.getByRole('slider', { name: 'Top spacing' })).toHaveValue('0'); // per device: the default

  await muted.check();
  await page.waitForTimeout(900);
  expect((await page.evaluate(async () => (await fetch('/api/settings')).json())).startMuted).toBe(true);
});

test('blacklist: add tags, see matching files, show them, delete and undo', async ({ page }) => {
  // The blacklist and the delete are answered here: the worker's server keeps no blacklist or deleted files from it
  // for the tests after it (they count on 26 files and an empty blacklist).
  // "Show them" is the real search (it only reads).
  let list: string[] = [];
  let deleted = false;
  const deletes: unknown[] = [];
  const restores: unknown[] = [];
  await page.route('**/api/blacklist', async (r) => {
    if (r.request().method() === 'POST') list = (r.request().postDataJSON() as { tags: string[] }).tags;
    await r.fulfill({ json: { tags: list } });
  });
  await page.route('**/api/blacklist/matches', (r) => {
    const red = list.includes('red') && !deleted;
    r.fulfill({ json: red ? { total: 2, size: 1000, tags: [{ tag: 'red', files: 2 }] } : { total: 0, size: 0, tags: [] } });
  });
  await page.route('**/api/blacklist/delete', (r) => {
    deletes.push(r.request().postDataJSON());
    deleted = true;
    r.fulfill({ json: { deleted: 2, ids: [101, 102] } });
  });
  await page.route('**/api/undelete', (r) => {
    restores.push(r.request().postDataJSON());
    deleted = false;
    r.fulfill({ json: { restored: 2 } });
  });
  await page.goto('/tags/rules');
  const panel = page.locator('.blacklist');
  const input = panel.getByRole('combobox', { name: 'Add to the blacklist' });
  // A pasted list adds several tags.
  await input.fill('red, nothing_like_this');
  await input.press('Enter');
  await expect(panel.locator('.pill')).toHaveCount(2);
  await expect(panel.getByText(/^2\s+files in the library have blacklisted tags/)).toBeVisible();
  await expect(panel.getByText(/red 2/)).toBeVisible();

  await panel.getByRole('button', { name: 'Show them' }).click();
  await expect(page.getByText(/^2 files/)).toBeVisible();
  await expect(page.locator('.chip')).toContainText('OR');

  await page.goto('/tags/rules');
  await panel.getByRole('button', { name: 'Delete them…' }).click();
  await expect(page.getByRole('alertdialog', { name: 'Delete 2 files with blacklisted tags?' })).toBeVisible();
  await confirmIn(page, 'Delete 2');
  await expect(page.getByText('Deleted 2 files')).toBeVisible();
  await expect(panel.getByText('No files in the library have these tags.')).toBeVisible();
  await page.getByRole('button', { name: 'Undo' }).click();
  await expect(page.getByText('Restored')).toBeVisible();
  await expect(panel.getByText(/^2\s+files in the library/)).toBeVisible();
  expect(deletes).toHaveLength(1);
  expect(restores).toEqual([{ ids: [101, 102] }]); // (Undo brings back exactly what was deleted)

  // Clean up: remove both entries.
  await panel.getByRole('button', { name: 'Remove red from the blacklist' }).click();
  await panel.getByRole('button', { name: 'Remove nothing_like_this from the blacklist' }).click();
  await expect(panel.locator('.pill')).toHaveCount(0);
});

test('consistent controls: the download form lines up, sort is an icon button', async ({ page }) => {
  await page.goto('/downloads');
  const controls = page.locator('form.add input, form.add .dropdown > button, form.add .picker, form.add .go');
  await expect(controls).toHaveCount(5);
  const boxes = await controls.evaluateAll((els) => els.map((e) => e.getBoundingClientRect()).map((r) => ({ h: Math.round(r.height), l: r.left, r: r.right, t: r.top, b: r.bottom })));
  expect(new Set(boxes.map((b) => b.h)).size).toBe(1); // site, search, posts, repeat and Download: one height
  // Nothing overlaps (a long site name used to run into the search box).
  for (let i = 0; i < boxes.length; i++)
    for (let j = i + 1; j < boxes.length; j++) {
      const [a, b] = [boxes[i], boxes[j]];
      const overlap = a.l < b.r - 1 && b.l < a.r - 1 && a.t < b.b - 1 && b.t < a.b - 1;
      expect(overlap, `controls ${i} and ${j} overlap`).toBe(false);
    }
});

test('selection mode fits the screen (no sideways scrolling)', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  await page.locator('.tile').nth(0).click({ modifiers: ['Control'] });
  await expect(page.getByText('1 selected')).toBeVisible();
  const overflow = await page.evaluate(() => {
    const lib = document.querySelector('.library')!;
    return Math.max(document.documentElement.scrollWidth - innerWidth, lib.scrollWidth - lib.clientWidth);
  });
  expect(overflow).toBeLessThanOrEqual(0);
  await expect(page.getByRole('button', { name: 'Clear selection' })).toBeInViewport();
  await page.getByRole('button', { name: 'Clear selection' }).click();
});

test('custom dropdown: keyboard, outside click, site picker', async ({ page }) => {
  test.skip(isPhone(page), 'keyboard: desktop');
  // The New download form's Repeat: changing it saves nothing (no shared state for other tests).
  await page.goto('/downloads');
  const repeat = page.getByRole('button', { name: 'Repeat' });
  await repeat.focus();
  await page.keyboard.press('Enter');
  const list = page.getByRole('listbox', { name: 'Repeat' });
  await expect(list).toBeVisible();
  await page.keyboard.press('ArrowDown');
  await page.keyboard.press('ArrowDown');
  await page.keyboard.press('Enter'); // Every 6 hours
  await expect(repeat).toHaveAttribute('data-value', '21600');
  await expect(repeat).toBeFocused();
  await repeat.click();
  await page.mouse.click(5, 400); // outside closes without changing
  await expect(list).toHaveCount(0);
  await expect(repeat).toHaveAttribute('data-value', '21600');
  await repeat.click();
  await page.keyboard.press('Escape');
  await expect(list).toHaveCount(0);

  const site = page.getByRole('button', { name: 'Site', exact: true });
  await expect(site).toHaveText('safebooru.org'); // sites are shown by their address
  await site.click();
  await expect(page.getByRole('menuitemradio', { name: 'safebooru.org' })).toHaveAttribute('aria-checked', 'true');
});

test('viewer: swiping to a video is clean (poster, created as the move starts, thumbnail kept)', async ({ page }) => {
  test.skip(test.info().project.name !== 'iphone', 'touch (with a mouse, moves are instant)');
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  await page.locator('.tile').nth(1).click(); // an image; the newest file (before it) is a video
  await expect(page.getByText(/^2 \/ /)).toBeVisible();
  const slot0 = page.locator('.viewer .slide[data-slot="0"]');
  await expect(slot0.locator('video')).toHaveCount(0); // a neighbor shows only its thumbnail
  await slot0.locator('img').evaluate((img) => ((img as any).__marker = 'prev'));
  // Like hydrus-web: the video exists (with its poster) as soon as the move starts, while sliding in.
  const early = await page.evaluate(
    () =>
      new Promise<string | null>((resolve) => {
        dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft' }));
        requestAnimationFrame(() => {
          const v = document.querySelector<HTMLVideoElement>('.viewer .slide[data-slot="0"] video');
          if (v) (v as any).__marker = 'early';
          resolve(v ? v.getAttribute('poster') : null);
        });
      }),
  );
  expect(early).toMatch(/\/thumbs\//);
  await expect(page.getByText(/^1 \/ /)).toBeVisible();
  // Same thumbnail element underneath, and the same video carries on.
  await expect
    .poll(() => page.locator('.viewer .slide[data-slot="1"] img').evaluate((img) => (img as any).__marker ?? null))
    .toBe('prev');
  await expect(page.locator('.viewer .slide[data-slot="1"] video')).toHaveCount(1);
  expect(await page.locator('.viewer .slide[data-slot="1"] video').evaluate((v) => (v as any).__marker)).toBe('early');
  await expect(page.getByRole('slider', { name: 'Position' })).toBeVisible(); // our controls attached
  // With a frame on screen the video fades in over the thumbnail, then the thumbnail goes.
  const video = page.locator('.viewer .slide[data-slot="1"] video');
  await expect(video).toHaveClass(/shown/);
  await expect.poll(() => video.evaluate((v) => getComputedStyle(v).opacity)).toBe('1');
  await expect(page.locator('.viewer .slide[data-slot="1"] img')).toHaveCSS('visibility', 'hidden');
});

test('downloads: pause, resume, reorder, remove, retry (offline test sites)', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-webkit', 'one run is enough: the server-side queue is shared');
  await page.goto('/downloads');
  const add = (site: string, query: string) =>
    page.evaluate(
      async ([site, query]) =>
        (
          await fetch('/api/downloads', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ site, query, max_posts: 0 }),
          })
        ).json(),
      [site, query],
    );
  const a = await add('test-slow', 'slow_a');
  const b = await add('test-slow', 'slow_b');
  const c = await add('test-slow', 'slow_c');
  const d = await add('test-down', 'down_a');
  const queue = page.locator('section.card', { has: page.getByRole('heading', { name: 'Queue' }) });
  const finished = page.locator('section.card', { has: page.getByRole('heading', { name: 'Finished' }) });
  const item = (q: string) => page.locator('.stack .item', { hasText: q });

  await expect(item('slow_a').locator('.state')).toHaveText('Downloading');
  // Folded: the one downloading in front, the one waiting behind it; unfolded, both.
  await expect(item('slow_b')).toHaveAttribute('aria-hidden', 'true');
  await unfoldStacks(queue);
  await expect(item('slow_b')).not.toHaveAttribute('aria-hidden', 'true');
  await expect(item('slow_b').locator('.state')).toHaveText('Waiting');
  await dlTab(page, 'History');
  await expect(finished.locator('.stack .item', { hasText: 'down_a' }).locator('.state')).toHaveText('Failed');
  await expect(finished.getByRole('button', { name: 'Clear finished' })).toHaveCount(0); // nothing done yet
  await dlTab(page, 'Queue');
  await unfoldStacks(queue);

  // Pause and resume.
  await rowMenu(page, 'the download slow_b', 'Pause');
  await expect(item('slow_b').locator('.state')).toHaveText('Paused');
  await rowMenu(page, 'the download slow_b', 'Resume');
  await expect(item('slow_b').locator('.state')).toHaveText('Waiting');

  // Drag slow_c above slow_b (the waiting ones reorder; slow_a, downloading, stays in front - no handle).
  const order = () =>
    queue.locator('.stack .item .query').allTextContents().then((t) => t.filter((x) => x.startsWith('slow_')));
  expect(await order()).toEqual(['slow_a', 'slow_b', 'slow_c']);
  await expect(page.getByRole('button', { name: /^Reorder slow_a/ })).toHaveCount(0);
  const grip = (await page.getByRole('button', { name: /^Reorder slow_c/ }).boundingBox())!;
  const top = (await item('slow_b').boundingBox())!;
  await page.mouse.move(grip.x + grip.width / 2, grip.y + grip.height / 2);
  await page.mouse.down();
  await page.mouse.move(grip.x + grip.width / 2, top.y + 4, { steps: 8 });
  await page.mouse.up();
  await expect.poll(order).toEqual(['slow_a', 'slow_c', 'slow_b']);
  const saved = await page.evaluate(async () => (await (await fetch('/api/downloads')).json()).searches);
  expect(saved.filter((s: any) => s.site === 'test-slow').map((s: any) => s.query)).toEqual(['slow_a', 'slow_c', 'slow_b']);
  // Arrow keys on the handle move it too.
  await page.getByRole('button', { name: /^Reorder slow_c/ }).press('ArrowDown');
  await expect.poll(order).toEqual(['slow_a', 'slow_b', 'slow_c']);

  // Retry a failed one (it fails again: the site is down), remove the rest.
  await dlTab(page, 'History');
  await rowMenu(page, 'the download down_a', 'Retry');
  await expect(finished.locator('.stack .item', { hasText: 'down_a' }).locator('.state')).toHaveText('Failed');
  await rowMenu(page, 'the download down_a', 'Remove');
  await expect(item('down_a')).toHaveCount(0);
  await dlTab(page, 'Queue');
  for (const q of ['slow_a', 'slow_b', 'slow_c']) {
    await rowMenu(page, `the download ${q}`, 'Remove');
    await expect(item(q)).toHaveCount(0);
  }
  await expect(page.getByText(/files stay in the library/).first()).toBeVisible();
  await expect(queue.getByText('Nothing waiting.', { exact: false })).toBeVisible();
  expect([a.id, b.id, c.id, d.id].every(Boolean)).toBe(true);
});

test('downloads: a site that keeps failing to answer pauses by itself - its download waits (not failed), Try now', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-webkit', 'one run is enough: the server-side queue is shared');
  await page.goto('/downloads');
  const call = (path: string, body?: unknown) =>
    page.evaluate(
      async ([path, body]) => (await fetch(path as string, body === undefined ? undefined : { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })).json(),
      [path, body] as const,
    );
  await call('/api/settings', { pauseAfter: 2 });
  const d = await call('/api/downloads', { site: 'test-down', query: 'paused_a', max_posts: 0 });
  try {
    const line = page.locator('.paused-line');
    await expect(line).toBeVisible({ timeout: 15_000 });
    await expect(line).toContainText("test-down.invalid isn't answering"); // (at the top of the page, naming the site)
    await expect(page.locator('.paused-chip').first()).toHaveText('Paused');
    const item = page.locator('.stack .item', { hasText: 'paused_a' });
    await expect(item.locator('.state')).toHaveText('Waiting'); // (not Failed, nor Downloading)
    await line.getByRole('button', { name: 'Try now' }).click();
    await expect(page.getByText('Trying test-down.invalid again').first()).toBeVisible();
    // Never pausing (Settings > Sites): the pause ends at once.
    await call('/api/settings', { pauseAfter: 0 });
    await page.reload();
    await expect(page.locator('.paused-line')).toHaveCount(0);
  } finally {
    await call('/api/settings', { pauseAfter: 0 });
    await call(`/api/downloads/${d.id}/remove`, {});
  }
});

test('subscriptions: subscribe, failed check, pause, interval, unsubscribe (offline test site)', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-webkit', 'one run is enough: the server-side list is shared');
  await page.goto('/downloads');
  const subsCard = page.locator('section.card', { has: page.getByRole('heading', { name: 'Subscriptions' }) });
  const finished = page.locator('section.card', { has: page.getByRole('heading', { name: 'Finished' }) });
  await dlTab(page, 'Subscriptions');
  await expect(subsCard.getByText('No subscriptions yet.')).toBeVisible();

  // Subscribe from the form: repeat every day.
  await page.getByRole('button', { name: 'Site', exact: true }).click();
  await page.getByRole('menuitemradio', { name: 'test-down.invalid' }).click();
  await page.getByRole('combobox', { name: 'Search on the site' }).fill('sub_a');
  await page.getByRole('button', { name: 'Repeat' }).click();
  await page.getByRole('option', { name: 'Every day' }).click();
  await page.getByRole('button', { name: 'Subscribe', exact: true }).click();
  await expect(page.getByText(/Subscribed to "sub_a"/)).toBeVisible();
  const sub = subsCard.locator('li', { hasText: 'sub_a' });
  // The first check fails (the site is down): the subscription says so, the check waits in Finished.
  await expect(sub.locator('.state')).toHaveText('Failed');
  await expect(sub).toContainText('Every day');
  await expect(sub).toContainText('Retry in 1 h');
  await expect(sub.locator('.error')).toBeVisible();
  await dlTab(page, 'History');
  await expect(finished.locator('.stack .item', { hasText: 'sub_a' }).locator('.kind')).toHaveCount(0); // no "Check" chip
  await page.getByRole('button', { name: 'More for the check of sub_a' }).click();
  await expect(page.getByRole('menuitem', { name: /^Subscribe/ })).toHaveCount(0); // a check is a subscription already
  await page.keyboard.press('Escape');
  await dlTab(page, 'Subscriptions');

  // Check now retries it; pause; a weekly interval.
  await rowMenu(page, 'sub_a', 'Check now');
  await expect(sub.locator('.state')).toHaveText('Failed');
  await rowMenu(page, 'sub_a', 'Pause');
  await expect(sub).not.toContainText('Retry in');
  await rowMenu(page, 'sub_a', 'Schedule', 'Every week');
  await expect(sub).toContainText('Every week');
  await rowMenu(page, 'sub_a', 'Resume');
  await page.getByRole('button', { name: 'More for sub_a' }).click();
  await expect(page.getByRole('menuitem', { name: /^Pause/ })).toBeVisible();
  await page.keyboard.press('Escape');

  // A search turned into a subscription with the bell.
  await page.evaluate(() =>
    fetch('/api/downloads', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ site: 'test-down', query: 'sub_b', max_posts: 0 }),
    }),
  );
  // It has run (and failed: the site is down), so it stays as it is - a search that hasn't started would be replaced.
  await dlTab(page, 'History');
  await expect(finished.locator('.stack .item', { hasText: 'sub_b' }).locator('.state')).toHaveText('Failed');
  await unfoldStacks(finished);
  await rowMenu(page, 'the download sub_b', 'Subscribe');
  await expect(page.getByText(/Subscribed to "sub_b"/).first()).toBeVisible();
  await unfoldStacks(finished);
  await page.getByRole('button', { name: 'More for the download sub_b' }).click();
  await expect(page.getByRole('menuitem', { name: /^Subscribe/ })).toHaveCount(0);
  await page.keyboard.press('Escape');
  await dlTab(page, 'Subscriptions');
  await expect(subsCard.locator('li', { hasText: 'sub_b' })).toContainText('Every 12 hours');

  // Unsubscribe: gone, with its failed check; files stay.
  for (const q of ['sub_a', 'sub_b']) {
    await rowMenu(page, q, 'Unsubscribe');
    await confirmIn(page, 'Unsubscribe');
    await expect(subsCard.locator('li', { hasText: q })).toHaveCount(0);
  }
  await expect(subsCard.getByText('No subscriptions yet.')).toBeVisible();
  await dlTab(page, 'History');
  await expect(finished.locator('.stack .item', { hasText: 'sub_a' })).toHaveCount(0);
  await rowMenu(page, 'the download sub_b', 'Remove'); // the plain search stays until removed
});

test('tag namespaces: details grouped by namespace, each with its own color', async ({ page }) => {
  await page.goto('/');
  await page.locator('.tile').nth(12).click();
  await page.getByRole('button', { name: 'Details' }).click();
  await page.getByRole('button', { name: 'Edit tags' }).click();
  const add = page.getByRole('combobox', { name: 'Add tags' });
  await add.click();
  await add.pressSequentially('meta:ns_meta e2e_general creator:ns_artist series:ns_series character:ns_char');
  await add.press('Enter');
  await page.getByRole('button', { name: 'Save tags' }).click();
  const panel = page.locator('.viewer aside.panel');
  await expect(panel.getByRole('button', { name: 'ns_artist', exact: true })).toBeVisible();
  // Booru order: creator, series, character, general, meta.
  const heads = (await panel.locator('h4').allTextContents()).map((t) => t.replace(/\s*\d+$/, '').trim());
  expect(heads).toEqual(['Creator', 'Series', 'Character', 'General', 'Meta']);
  const color = (name: string) =>
    panel.getByRole('button', { name, exact: true }).evaluate((el) => getComputedStyle(el).color);
  const colors = await Promise.all(['ns_artist', 'ns_series', 'ns_char', 'e2e_general', 'ns_meta'].map(color));
  expect(new Set(colors).size).toBe(5);
  expect(colors[0]).toBe('rgb(243, 139, 168)'); // creator: red
  // The tag's menu: copy it with or without its namespace.
  if (test.info().project.name === 'desktop-chromium') await page.context().grantPermissions(['clipboard-read', 'clipboard-write']);
  const menu = page.getByRole('menu', { name: /^ns_artist/ });
  await panel.getByRole('button', { name: 'ns_artist', exact: true }).click();
  await expect(menu).toContainText('Creator');
  await menu.getByRole('menuitem', { name: /^Copy creator:ns_artist/ }).click();
  await expect(page.getByText('Copied creator:ns_artist')).toBeVisible();
  if (test.info().project.name === 'desktop-chromium') expect(await page.evaluate(() => navigator.clipboard.readText())).toBe('creator:ns_artist');
  await panel.getByRole('button', { name: 'ns_artist', exact: true }).click();
  await menu.getByRole('menuitem', { name: /^Copy without namespace/ }).click();
  await expect(page.getByText('Copied ns_artist', { exact: true })).toBeVisible();
  if (test.info().project.name === 'desktop-chromium') expect(await page.evaluate(() => navigator.clipboard.readText())).toBe('ns_artist');
  // A tag without a namespace has one copy item.
  await panel.getByRole('button', { name: 'e2e_general', exact: true }).click();
  await expect(page.getByRole('menu', { name: /^e2e_general/ }).getByRole('menuitem', { name: /^Copy/ })).toHaveCount(1);
  await page.keyboard.press('Escape');
  await expect(page.getByRole('menu')).toHaveCount(0);
  await expect(panel).toBeVisible(); // Escape closed only the menu
  // New download: the New download dialog, the plain tag on the site.
  await panel.getByRole('button', { name: 'ns_artist', exact: true }).click();
  await menu.getByRole('menuitem', { name: 'New download' }).click();
  await newDl(page).getByRole('button', { name: 'safebooru.org' }).click();
  await expect(newDl(page).getByRole('combobox', { name: 'Search on the site' })).toHaveValue('ns_artist');
  await page.keyboard.press('Escape');
  await expect(newDl(page)).toHaveCount(0);
  if (!(await panel.isVisible())) await page.getByRole('button', { name: 'Details', exact: true }).click();
  // New search: the tag with its namespace.
  await panel.getByRole('button', { name: 'ns_artist', exact: true }).click();
  await menu.getByRole('menuitem', { name: 'New search' }).click();
  await expect(page).toHaveURL(/q=creator%3Ans_artist/);
  await expect(page.locator('.chip .atom').first()).toHaveCSS('color', 'rgb(243, 139, 168)');
});

test('tag refresh: selection action and the Downloads card (files from disk: nothing to refresh)', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  await page.locator('.tile').nth(0).click({ modifiers: ['Control'] });
  if (!(await page.getByText('1 selected').isVisible())) {
    // Phones: long-press menu instead of Ctrl-click.
    await page.locator('.tile').nth(0).dispatchEvent('contextmenu');
    await page.getByRole('menuitem', { name: 'Select' }).click();
  }
  await expect(page.getByText('1 selected')).toBeVisible();
  await page.getByRole('button', { name: 'Refresh tags of selected' }).click();
  await expect(page.getByText('None of these files came from a site')).toBeVisible();
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - innerWidth);
  expect(overflow).toBeLessThanOrEqual(0);
  await page.getByRole('button', { name: 'Clear selection' }).click();

  // A file from disk has no Refresh tags button.
  await page.locator('.tile').nth(3).click();
  await page.getByRole('button', { name: 'Details' }).click();
  await expect(page.locator('.viewer aside.panel')).toBeVisible();
  await expect(page.getByRole('button', { name: /Refresh tags/ })).toHaveCount(0);

  await page.goto('/downloads');
  // Nothing downloaded from a site yet: no Tag refresh part at all (an empty part isn't shown).
  await expect(page.getByRole('tab', { name: /^Queue/ })).toBeVisible();
  await expect(page.locator('section.card', { has: page.getByRole('heading', { name: 'Tag refresh' }) })).toHaveCount(0);
});

test('details: a tapped tag can be added to or excluded from the current search', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  // Two files with pick_a, the first of them also with pick_b.
  await page.evaluate(async () => {
    const ids: number[] = (await (await fetch('/api/search?q=&limit=2')).json()).ids;
    const tag = (ids: number[], add: string[]) =>
      fetch('/api/tags', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ids, add, remove: [] }) });
    await tag(ids, ['pick_a']);
    await tag([ids[0]], ['pick_b']);
  });
  const panel = page.locator('.viewer aside.panel');
  const openFirst = async () => {
    await page.locator('.tile').first().click();
    if (!(await panel.isVisible())) await page.getByRole('button', { name: 'Details' }).click();
  };
  const tap = (t: string) => panel.getByRole('button', { name: t, exact: true }).click();

  // Search pick_b, add pick_a.
  await page.goto('/?q=pick_b');
  await expect(page.getByText(/^1 file/)).toBeVisible();
  await openFirst();
  await tap('pick_a');
  const menu = page.getByRole('menu');
  await expect(page.getByRole('menu', { name: /^pick_a/ })).toBeVisible();
  await page.mouse.click(5, 5); // a tap outside closes it
  await expect(menu).toHaveCount(0);
  await tap('pick_a');
  await menu.getByRole('menuitem', { name: 'Add to search' }).click();
  await expect(page).toHaveURL(/q=pick_b(\+|%20)pick_a/);
  await expect(page.getByText(/^1 file/)).toBeVisible();

  // pick_b is in the search: exclude it instead -> the other file.
  await openFirst();
  await tap('pick_a');
  await expect(menu.getByRole('menuitem', { name: 'Remove from search' })).toBeVisible();
  await page.keyboard.press('Escape');
  await tap('pick_b');
  await menu.getByRole('menuitem', { name: 'Exclude from search' }).click();
  await expect(page).toHaveURL(/q=pick_a(\+|%20)-pick_b/);
  await expect(page.getByText(/^1 file/)).toBeVisible();

  // Take pick_a out again: everything without pick_b.
  await openFirst();
  await tap('pick_a');
  await menu.getByRole('menuitem', { name: 'Remove from search' }).click();
  await expect(page).toHaveURL(/q=-pick_b/);
  await expect(page.getByText(/^25 files/)).toBeVisible();
});

test('viewer: swiping down hides the neighboring files while the image shrinks', async ({ page }) => {
  await page.goto('/?q=system:type%3Dimage');
  await page.locator('.tile').nth(1).click();
  await expect(page.locator('.viewer .slide[data-slot="0"]')).toHaveCount(1);
  const box = (await page.locator('.stage').boundingBox())!;
  const x = box.x + box.width / 2;
  await page.mouse.move(x, box.y + box.height * 0.3);
  await page.mouse.down();
  await page.mouse.move(x, box.y + box.height * 0.3 + 60, { steps: 6 });
  const vis = (slot: number) =>
    page.locator(`.viewer .slide[data-slot="${slot}"]`).evaluate((el) => getComputedStyle(el).visibility);
  expect(await vis(0)).toBe('hidden');
  expect(await vis(2)).toBe('hidden');
  expect(await vis(1)).toBe('visible');
  // Let go before the close threshold: it springs back, then the neighbors return.
  await page.mouse.up();
  await expect(page.locator('.viewer')).toBeVisible();
  await expect.poll(() => vis(0)).toBe('visible');
});

test('search chips: the × on a chip removes it from the search', async ({ page }) => {
  await page.goto('/?q=system:type%3Dimage%20-system:has_audio');
  const chips = page.locator('.bar .chip');
  await expect(chips).toHaveCount(2);
  // Tapping a filter chip opens its filter form (filled in).
  await chips.first().locator('.chip-main').click();
  await expect(page.getByRole('dialog', { name: 'Type filter' })).toBeVisible();
  // Phones: the form is a sheet over the page - closed first (a tap on the dimmed page).
  if (isPhone(page)) await page.mouse.click(page.viewportSize()!.width / 2, 10);
  await page.getByRole('button', { name: 'Remove system:type=image from the search' }).click();
  await expect(chips).toHaveCount(1);
  await expect(page.getByRole('dialog', { name: 'Type filter' })).toHaveCount(0); // its form closed
  await expect(page).toHaveURL(/q=-system(%3A|:)has_audio/);
  await page.getByRole('button', { name: 'Remove -system:has_audio from the search' }).click();
  await expect(chips).toHaveCount(0);
  await expect(page.getByText(/^26 files/)).toBeVisible();
});

test('saved searches: save, run with its order, update after a change, rename, reorder, delete with undo', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-webkit', 'one run is enough: the list is on the server (phone layout: screenshots)');
  // (the list is shared with the tests running meanwhile: only this test's own searches are looked at)
  const ours = /^(Images|Pictures|videos)$/;
  const panel = page.getByRole('dialog', { name: 'Saved searches' });
  const mine = panel.locator('ul.list:not(.groups) > li').filter({ has: page.locator('.name, input.rename') }).filter({ hasText: /Images|Pictures|videos/ });
  const names = () => panel.locator('input.rename').evaluateAll((els) => els.map((e) => (e as HTMLInputElement).value)).then((v) => v.filter((n) => ours.test(n)));
  // None yet (the server's list made empty here: other tests may have theirs).
  await page.route('**/api/saved*', (r) => (r.request().method() === 'GET' ? r.fulfill({ json: { saved: [] } }) : r.continue()));
  await page.route('**/api/news*', (r) => r.fulfill({ json: { saved: [], groups: [] } }));
  await page.goto('/');
  await expect(page.getByText(/^\d+ files/)).toBeVisible();
  const star = page.getByRole('button', { name: /^Saved searches/ });
  await star.click();
  await expect(panel.getByText('No saved searches yet')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(panel).toHaveCount(0);
  await expect(star).toBeFocused();
  await page.unrouteAll({ behavior: 'ignoreErrors' });

  // Save "system:type=image" as Images.
  await page.goto('/?q=system:type%3Dimage');
  await expect(page.getByText(/files/).first()).toBeVisible();
  const images = await page.evaluate(async () => (await (await fetch('/api/search?q=system:type%3Dimage&limit=0')).json()).total);
  await star.click();
  await panel.getByRole('textbox', { name: 'Name for the current search' }).fill('Images');
  await panel.getByRole('button', { name: 'Save' }).click();
  await expect(panel.getByText('This search is saved as')).toBeVisible();
  await expect(star).toHaveAttribute('aria-label', /saved as Images/);
  await expect(panel.locator('li', { hasText: 'Images' }).locator('.count')).toHaveText(String(images));

  // Videos, newest imported first (not the default) (the order is saved too).
  await page.keyboard.press('Escape');
  await page.goto('/?q=system:type%3Dvideo&sort=imported');
  await star.click();
  await panel.getByRole('button', { name: 'Save' }).click(); // no name typed: named after the search
  await expect(panel.locator('li', { hasText: 'videos' })).toContainText('Newest imported');

  // Run Images from an empty search: its search, the default order.
  await page.goto('/');
  await star.click();
  await panel.getByRole('button', { name: /^Images/ }).click();
  await expect(panel).toHaveCount(0);
  await expect(page).toHaveURL(/q=system(:|%3A)type(%3D|=)image/);
  await expect(page).not.toHaveURL(/sort=/);
  await expect(page.getByText(new RegExp(`^${images} files`))).toBeVisible();
  await expect(star).toHaveAttribute('aria-label', /saved as Images/);
  // Run the video one: the sort comes with it.
  await star.click();
  await panel.getByRole('button', { name: /^videos/ }).click();
  await expect(page).toHaveURL(/sort=imported/);

  // Kept on the server: still there after a reload.
  await page.reload();
  await star.click();
  await expect(mine).toHaveCount(2);

  // Run Images, then change the search: the menu offers to update it in one tap (or save it as a new one).
  await panel.getByRole('button', { name: /^Images/ }).click();
  await expect(page).toHaveURL(/q=system(:|%3A)type(%3D|=)image/);
  const box = page.locator('.bar').getByRole('combobox', { name: 'Search' });
  await box.fill('blue_sky');
  await box.press('Enter');
  await expect(page).toHaveURL(/blue_sky/);
  await star.click();
  await expect(panel.locator('.changed')).toContainText('Changed from Images');
  await expect(panel.getByRole('button', { name: 'Save as new' })).toBeVisible();
  await panel.locator('.changed').getByRole('button', { name: 'Update' }).click();
  await expect(panel.getByText('This search is saved as')).toContainText('Images');
  await expect(star).toHaveAttribute('aria-label', /saved as Images/);
  await expect(mine).toHaveCount(2);
  await expect(mine.filter({ hasText: 'Images' })).toContainText(/blue.sky/);

  // Edit: rename, move, delete + undo (no Update there: that's for after a change).
  await panel.getByRole('button', { name: 'Edit' }).click();
  const rename = panel.getByRole('textbox', { name: 'Name of Images' });
  await rename.fill('Pictures');
  await rename.press('Enter');
  await expect(panel.getByRole('textbox', { name: 'Name of Pictures' })).toBeVisible();
  await expect(panel.getByRole('button', { name: 'Update' })).toHaveCount(0);
  await panel.getByRole('button', { name: /^Reorder Pictures/ }).press('ArrowDown');
  await expect.poll(names).toEqual(['videos', 'Pictures']);
  await panel.getByRole('button', { name: 'Delete Pictures' }).click();
  await expect.poll(names).toEqual(['videos']);
  await page.getByRole('button', { name: 'Undo' }).click();
  await expect.poll(names).toEqual(['videos', 'Pictures']);
  // Clean up.
  for (const n of ['Pictures', 'videos']) await panel.getByRole('button', { name: `Delete ${n}` }).click();
  await expect.poll(names).toEqual([]);
});

test('saved searches: a long search takes two lines in the menu, "+N" for the rest', async ({ page }) => {
  const tags = Array.from({ length: 24 }, (_, i) => `some_long_tag_name_${i}`);
  await page.route('**/api/saved*', (r) =>
    r.request().method() === 'GET' ? r.fulfill({ json: { saved: [{ id: 901, name: 'Long one', query: tags.join(' '), sort: '', count: 3 }, { id: 902, name: 'Short one', query: 'blue_sky', sort: '', count: 4 }] } }) : r.continue(),
  );
  await page.goto('/');
  await page.getByRole('button', { name: /^Saved searches/ }).click();
  const panel = page.getByRole('dialog', { name: 'Saved searches' });
  const long = panel.locator('li', { hasText: 'Long one' }).locator('.q');
  await expect(long).toHaveClass(/clipped/);
  await expect(long).toHaveAttribute('data-more', /^\+\d+$/);
  const h = await long.evaluate((el) => el.getBoundingClientRect().height);
  const line = await panel.locator('li', { hasText: 'Short one' }).locator('.q').evaluate((el) => el.getBoundingClientRect().height);
  expect(h).toBeLessThanOrEqual(line * 2 + 6);
  await expect(panel.locator('li', { hasText: 'Short one' }).locator('.q')).not.toHaveClass(/clipped/);
  await page.unrouteAll({ behavior: 'ignoreErrors' });
});

test('fast scroller: drag the handle to jump, with a date bubble; a press beside the handle is not the scroller\'s', async ({ page }) => {
  await page.setViewportSize({ width: 393, height: 430 }); // a short window: 26 files fill several screens
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  const thumb = page.getByRole('scrollbar', { name: 'Fast scroll' });
  await expect(thumb).toBeAttached();
  const grid = page.locator('#grid-scroller');
  const scrollbarWidth = await grid.evaluate((el) => el.offsetWidth - el.clientWidth);
  expect(scrollbarWidth).toBe(0); // no second (native) scrollbar

  const box = (await thumb.boundingBox())!;
  const gridBox = (await grid.boundingBox())!;
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width / 2, gridBox.y + gridBox.height / 2, { steps: 5 });
  // While dragging: the bubble says when these files are from.
  await expect(page.locator('.fast .bubble')).toHaveText(/\d/);
  await page.mouse.move(box.x + box.width / 2, gridBox.y + gridBox.height + 40, { steps: 5 });
  await page.mouse.up();
  await expect(page.locator('.fast .bubble')).toHaveCount(0);
  const atEnd = await grid.evaluate((el) => el.scrollHeight - el.clientHeight - el.scrollTop);
  expect(atEnd).toBeLessThan(2);
  await expect(page.locator('.tile[data-i="25"]')).toBeVisible();
  await expect(page.locator('.tile[data-i="25"] img')).toHaveClass(/in/); // loaded where it stopped

  // Only the handle takes presses: beside it (where the track is) is the grid's.
  await page.waitForTimeout(300);
  const nowBox = (await grid.boundingBox())!;
  const under = await page.evaluate(([x, y]) => !!document.elementFromPoint(x, y)?.closest('.fast'), [box.x + box.width / 2, nowBox.y + 10]);
  expect(under).toBe(false);
});

test('viewer opens out of its thumbnail and closes back into it', async ({ page }) => {
  await page.goto('/?q=system:type%3Dimage');
  await expect(page.locator('.tile img.in').first()).toBeVisible();
  const tile = (await page.locator('.tile').nth(2).boundingBox())!;
  await page.locator('.tile').nth(2).click();
  // The copy starts on the tile, the real viewer waits hidden.
  const frame = page.locator('.hero-layer .hero-frame');
  await expect(frame).toHaveCount(1);
  await expect(page.locator('.viewer')).toHaveClass(/hidden/);
  await expect(page.locator('.hero-layer')).toHaveCount(0); // done
  await expect(page.locator('.viewer')).not.toHaveClass(/hidden/);

  // Closing shrinks it back onto the tile.
  await page.getByRole('button', { name: 'Close' }).click();
  await expect(page.locator('.viewer')).toHaveCount(0);
  await expect(frame).toHaveCount(1);
  const end = await frame.evaluate(
    (el) =>
      new Promise<DOMRect>((resolve) => {
        const a = el.getAnimations()[0];
        a.finish();
        resolve(el.getBoundingClientRect());
      }),
  );
  expect(Math.abs(end.x - tile.x)).toBeLessThan(2);
  expect(Math.abs(end.width - tile.width)).toBeLessThan(2);
  await expect(page.locator('.hero-layer')).toHaveCount(0);
});

// Two files "arrive" at the top: the server's answer, reordered (the last two
// files moved to the front, as a newest-first search gets new downloads).
async function arriveTwo(page: import('@playwright/test').Page) {
  let arrived = false;
  await page.route('**/api/search?*', async (route) => {
    const res = await route.fetch();
    const body = await res.json();
    if (arrived) {
      const ids: number[] = body.ids;
      body.ids = [...ids.slice(-2), ...ids.slice(0, -2)];
    }
    await route.fulfill({ response: res, json: body });
  });
  return async () => {
    arrived = true;
    await page.route('**/api/new?*', (route) => route.fulfill({ json: { new: 2 } }));
    const searched = page.waitForResponse((r) => r.url().includes('/api/search?'));
    await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')));
    await searched;
    await page.unroute('**/api/new?*');
  };
}

test('arriving files are taken in without moving anything on screen (no button)', async ({ page }) => {
  await page.setViewportSize({ width: 400, height: 700 }); // a long list: 3 columns, it scrolls
  const arrive = await arriveTwo(page);
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  const grid = page.locator('#grid-scroller');
  await grid.evaluate((el) => (el.scrollTop = 300));
  await page.waitForTimeout(300);
  // A tile in view and where it is.
  const tile = page.locator('.tile[data-i="9"]');
  const id = await tile.getAttribute('data-id');
  const before = (await tile.boundingBox())!;
  await arrive();
  const same = page.locator(`.tile[data-id="${id}"]`);
  await expect(same).toHaveAttribute('data-i', '11'); // two places later in the list...
  await expect.poll(async () => Math.round((await same.boundingBox())!.y)).toBe(Math.round(before.y)); // ...and not moved on screen
  expect(Math.round((await same.boundingBox())!.x)).toBe(Math.round(before.x));
  await expect(page.locator('.new-pill')).toHaveCount(0);
  // Pressing it opens that file.
  await same.click();
  await expect(page.getByText('12 / 26')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.locator('.viewer')).toHaveCount(0);
  // Scrolled up to the new files: no empty cells - the top row's gap closes
  // (one new file per row of 3 would otherwise leave a hole before it).
  await grid.evaluate((el) => (el.scrollTop = 0));
  const gridLeft = (await grid.boundingBox())!.x;
  await expect.poll(async () => Math.round((await page.locator('.tile[data-i="0"]').boundingBox())!.x - gridLeft)).toBeLessThan(14);
  // (one row: compared once the tiles have slid into place)
  await expect
    .poll(async () => Math.round((await page.locator('.tile[data-i="0"]').boundingBox())!.y) - Math.round((await page.locator('.tile[data-i="2"]').boundingBox())!.y))
    .toBe(0);
});

test('scrolled to the very top, arriving files come into view: it stays at the top, the new files first', async ({ page }) => {
  await page.setViewportSize({ width: 400, height: 700 });
  const arrive = await arriveTwo(page);
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  const grid = page.locator('#grid-scroller');
  const lastTwo = await page.evaluate(async () => (await (await fetch('/api/search?q=&limit=26')).json()).ids.slice(-2));
  await arrive();
  await expect(page.locator('.tile[data-i="0"]')).toHaveAttribute('data-id', String(lastTwo[0])); // the new ones first...
  await expect(page.locator('.tile[data-i="1"]')).toHaveAttribute('data-id', String(lastTwo[1]));
  expect(await grid.evaluate((el) => el.scrollTop)).toBeLessThan(2); // ...and still at the top, where they show
  await expect(page.locator('.tile[data-i="0"]')).toBeInViewport();
});

test('a file being viewed stays on screen when files arrive', async ({ page }) => {
  const arrive = await arriveTwo(page);
  await page.goto('/');
  await page.locator('.tile').nth(3).click();
  await expect(page.getByText('4 / 26')).toBeVisible();
  const src = await page.locator('.viewer .slide[data-slot="1"] img').getAttribute('style');
  await arrive();
  await expect(page.getByText('6 / 26')).toBeVisible(); // the same file, now two places later
  expect(await page.locator('.viewer .slide[data-slot="1"] img').getAttribute('style')).toBe(src);
  await expect.poll(() => new URL(page.url()).searchParams.get('i')).toBe('5');
});

test('the details stay open and where they were scrolled when files arrive', async ({ page }) => {
  const arrive = await arriveTwo(page);
  await page.goto('/');
  await page.locator('.tile').nth(3).click();
  await page.getByRole('button', { name: 'Details', exact: true }).click();
  const panel = page.getByRole('complementary', { name: 'Details' });
  await expect(panel).toBeVisible();
  await panel.evaluate((el) => ((el as any).__marker = 'same'));
  const body = panel.locator('.panel-body');
  await body.evaluate((el) => {
    el.style.paddingBottom = '2000px'; // room to scroll, whatever the file has
    el.scrollTop = 120;
  });
  let reloads = 0;
  page.on('request', (r) => {
    if (/\/api\/files\/\d+\/tags$/.test(new URL(r.url()).pathname)) reloads++;
  });
  await arrive();
  await expect(page.getByText('6 / 26')).toBeVisible();
  await page.waitForTimeout(300);
  expect(reloads).toBe(0); // the details never switched to another file for a moment
  expect(await panel.evaluate((el) => (el as any).__marker)).toBe('same'); // not closed and reopened
  expect(await body.evaluate((el) => el.scrollTop)).toBe(120);
});

test('settings: a site login is entered in a dialog from its ⋯ menu, tested, saved and removed - never shown back', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-webkit', 'one run is enough: credentials are on the server');
  await page.goto('/settings/sites');
  const site = page.locator('article.site', { has: page.getByRole('heading', { name: 'test-auth.invalid' }) });
  await expect(site.locator('.status')).toHaveText('Needs an account');
  // The card only says a login is needed; the boxes are in a dialog (its ⋯ item, or that line).
  await expect(site.getByRole('textbox')).toHaveCount(0);
  await expect(site.getByRole('button', { name: /^Login needed/ })).toBeVisible();
  await rowMenu(page, 'test-auth.invalid', 'Log in');
  const dialog = page.getByRole('dialog', { name: 'Log in to test-auth.invalid' });
  await expect(dialog.getByText('Copy them from your account options.')).toBeVisible();
  const key = dialog.getByLabel('test-auth.invalid API key');
  const user = dialog.getByLabel('test-auth.invalid User ID');
  await expect(key).toHaveAttribute('type', 'password');
  await expect(dialog.getByRole('button', { name: 'Save' })).toBeDisabled();
  await expect(dialog.getByRole('button', { name: 'Test' })).toBeDisabled();
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await expect(dialog).toHaveCount(0);

  await site.getByRole('button', { name: /^Login needed/ }).click();
  await key.fill('e2e-test-key');
  await user.fill('1234');
  await dialog.getByRole('button', { name: 'Show what I typed' }).first().click();
  await expect(key).toHaveAttribute('type', 'text');
  // Test: what's typed, before saving (the test site is down) - nothing saved by it.
  const tested = page.waitForRequest((r) => r.url().includes('/api/sites/test-auth/test'));
  await dialog.getByRole('button', { name: 'Test' }).click();
  expect((await tested).postDataJSON()).toEqual({ values: { api_key: 'e2e-test-key', user_id: '1234' } });
  await expect(dialog.getByRole('status')).toContainText("didn't accept");
  await expect(site.locator('.status')).toHaveText('Needs an account');
  await user.fill('12345');
  await expect(dialog.getByRole('status')).toHaveCount(0); // (a result for other values goes)

  const listed = page.waitForResponse((r) => r.url().includes('/api/sites/test-auth/credentials'));
  await dialog.getByRole('button', { name: 'Save' }).click();
  const body = await (await listed).text();
  expect(body).not.toContain('e2e-test-key'); // never sent back
  // Saved: the dialog closes, the card says so, and it was tested straight away.
  await expect(dialog).toHaveCount(0);
  await expect(site.locator('.account:not([inert])')).toContainText('Login saved');
  await expect(site.locator('.status')).toHaveText('Not connected');
  await expect(site.getByRole('status')).toContainText("didn't accept");

  // After a reload: still saved, still not shown; Change login shows empty boxes marked saved.
  await page.reload();
  await expect(site.locator('.status')).toHaveText('Ready');
  await expect(site.locator('.account:not([inert])')).toContainText('Login saved');
  expect(await page.content()).not.toContain('e2e-test-key');
  await rowMenu(page, 'test-auth.invalid', 'Change login');
  await expect(dialog.locator('.saved')).toHaveCount(2);
  await expect(key).toHaveValue('');
  await expect(key).toHaveAttribute('placeholder', /Saved/);
  await expect(dialog.getByRole('button', { name: 'Test' })).toBeEnabled(); // (the saved login)
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);

  await rowMenu(page, 'test-auth.invalid', 'Remove login');
  await confirmIn(page, 'Remove');
  await expect(site.locator('.status')).toHaveText('Needs an account');
  await expect(site.getByRole('button', { name: /^Login needed/ })).toBeVisible();
});

test('tag browser: counts, filter, namespace, rename your own tag, alias a tag', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-webkit', 'one run is enough: renames change the shared library');
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  await page.evaluate(async () => {
    const ids: number[] = (await (await fetch('/api/search?q=&limit=3')).json()).ids;
    await fetch('/api/tags', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ids, add: ['browse_typo'], remove: [] }) });
  });
  await page.goto('/tags/all');
  const card = page.getByRole('region', { name: 'All tags' });
  const row = (t: string) => card.locator('li', { has: page.getByRole('button', { name: new RegExp(`^${t}( |$)`) }) });
  await expect(row('blue_sky').locator('.count')).toHaveText('4');

  // Filter and namespace.
  await card.getByRole('textbox', { name: 'Filter tags' }).fill('browse');
  await expect(card.locator('li')).toHaveCount(1);
  await expect(card.getByText(/^1 tag$/)).toBeVisible();
  await card.getByRole('radio', { name: 'Creator' }).click();
  await expect(card.getByText('No tags match.')).toBeVisible();
  await card.getByRole('radio', { name: 'All' }).click();

  // Rename (a tag only the user added).
  await rowMenu(page, 'browse_typo', 'Rename');
  await card.getByRole('combobox', { name: 'New name' }).fill('browse_fixed');
  await card.getByRole('button', { name: 'Rename', exact: true }).last().click();
  await expect(page.getByText(/Renamed browse_typo to browse_fixed on 3 files/)).toBeVisible();
  await expect(row('browse_fixed').locator('.count')).toHaveText('3');
  await expect(row('browse_typo')).toHaveCount(0);

  // Alias: browse_fixed stands for blue_sky (shows in Siblings), then undone.
  await rowMenu(page, 'browse_fixed', 'Alias');
  await card.getByRole('combobox', { name: 'Ideal tag' }).fill('blue_sky');
  await card.getByRole('button', { name: 'Alias', exact: true }).last().click();
  await expect(row('browse_fixed')).toContainText('→ blue_sky');
  // It shows under Tag rules > Siblings (the other sub-page); remove it there.
  await page.goto('/tags/rules');
  await expect(page.locator('section.card', { has: page.getByRole('heading', { name: 'Siblings' }) })).toContainText('browse_fixed');
  await page.getByRole('button', { name: 'Remove sibling browse_fixed' }).click();
  await page.goto('/tags/all');
  await expect(card.getByRole('textbox', { name: 'Filter tags' })).toBeVisible();
  await card.getByRole('textbox', { name: 'Filter tags' }).fill('browse');

  // Tapping a tag searches for it (once the filtered list has loaded).
  await expect(card.getByText('1 tag', { exact: true })).toBeVisible();
  await row('browse_fixed').getByRole('button').first().click();
  await expect(page).toHaveURL(/q=browse_fixed/);
  await expect(page.getByText(/^3 files/)).toBeVisible();
  // (the tag off again: the next test on this server - or this one run again - starts without it)
  await page.evaluate(async () => {
    const ids: number[] = (await (await fetch('/api/search?q=browse_fixed&limit=10')).json()).ids;
    await fetch('/api/tags', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ids, add: [], remove: ['browse_fixed'] }) });
  });
});

test('tags: sections like Settings - Tags opens on Tag rules, All tags beside it (wide) or from the list (phones)', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'Menu' }).click();
  const nav = page.getByRole('navigation', { name: 'Main' });
  await expect(nav.getByRole('button', { name: 'All tags' })).toHaveCount(0); // folded to begin with
  await unfold(page, 'tags');
  await expect(nav.getByRole('button', { name: 'All tags' })).toBeVisible();
  await nav.getByRole('button', { name: 'Tags', exact: true }).click();
  await expect(page).toHaveURL(/\/tags\/rules$/);
  await expect(page.getByRole('heading', { name: 'Parents' })).toBeVisible();
  await expect(page.getByRole('region', { name: 'All tags' })).toHaveCount(0); // not in this section
  await page.reload(); // stays on the section
  await expect(page.getByRole('heading', { name: 'Blacklist' })).toBeVisible();

  if (isPhone(page)) await page.getByRole('button', { name: 'Tags', exact: true }).click(); // back to the list
  await page.getByRole('navigation', { name: 'Tags sections' }).getByRole('button', { name: /^All tags/ }).click();
  await expect(page).toHaveURL(/\/tags\/all$/);
  await expect(page.getByRole('region', { name: 'All tags' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Parents' })).toHaveCount(0);

  // The menu marks the section you're on.
  await page.getByRole('button', { name: 'Menu' }).click();
  await expect(nav.getByRole('button', { name: 'All tags' })).toHaveAttribute('aria-current', 'page');
});

test('details: "Found by" shows the search or subscription, Open jumps to the subscription', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-webkit', 'one run is enough: the subscription is on the server');
  await page.goto('/');
  const sub = await page.evaluate(() =>
    fetch('/api/subscriptions', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ site: 'test-down', query: 'found_by_sub', interval: 86400, initial_max: 0 }),
    }).then((r) => r.json()),
  );
  // Files from disk have no sources: pretend this one was downloaded by the subscription and also met by a search.
  await page.route('**/api/files/*/tags', async (route) => {
    const res = await route.fetch();
    const body = await res.json();
    body.sources = [
      { site: 'test-down', query: 'found_by_sub', subscription_id: sub.id, downloaded: true, first_at: Date.now() },
      { site: 'test-down', query: 'some_search', downloaded: false, first_at: Date.now() },
    ];
    await route.fulfill({ response: res, json: body });
  });
  await page.locator('.tile').nth(3).click();
  await page.getByRole('button', { name: 'Details' }).click();
  const found = page.getByRole('region', { name: "Where it's from" });
  // Only what downloaded it (not the search that met it later).
  await expect(found.locator('li')).toHaveCount(1);
  await expect(found.locator('li').first()).toContainText('Subscription on test-down.invalid');

  await found.getByRole('button', { name: 'Open the subscription found_by_sub' }).click();
  await expect(page).toHaveURL(/\/downloads$/);
  const row = page.locator(`li[data-sub="${sub.id}"]`);
  await expect(row).toHaveClass(/flash/);
  await expect(row).toBeInViewport();
  await rowMenu(page, 'found_by_sub', 'Unsubscribe');
  await confirmIn(page, 'Unsubscribe');
});

test('changing the sort order loads the new first screen at once (no scroll needed)', async ({ page }) => {
  // The 3 newest files: switching to oldest first gives 3 other files - the same count,
  // the same rows on screen - none of them loaded yet.
  await page.goto('/?q=system:limit%3D3');
  await expect(page.getByText(/^3 files/)).toBeVisible();
  await expect(page.locator('.tile[data-i="0"] img.in')).toBeVisible();
  await pickSort(page, 'Oldest imported');
  await expect.poll(() => new URL(page.url()).searchParams.get('sort')).toBe('imported_asc');
  // The oldest files weren't loaded before; they must appear without scrolling.
  for (const i of [0, 1, 2]) await expect(page.locator(`.tile[data-i="${i}"] img.in`)).toBeVisible({ timeout: 3000 });
  await pickSort(page, 'Newest imported');
});

test('video scrub bar: the picture follows the finger (preview frame in the player), small frame on hover', async ({ page }) => {
  await page.goto('/?q=system:type%3Dvideo');
  await expect(page.getByText(/^2 files/)).toBeVisible();
  // The server makes preview frames in the background when it starts.
  const id = await page.evaluate(async () => (await (await fetch('/api/search?q=system:type%3Dvideo&limit=1')).json()).ids[0]);
  await expect
    .poll(() => page.evaluate(async (id) => (await fetch(`/api/files/${id}/sprite`)).status, id), { timeout: 20000 })
    .toBe(200);
  const sprite = await page.evaluate(async (id) => (await fetch(`/api/files/${id}/sprite`)).json(), id);
  expect(sprite.frames).toBeGreaterThan(1);
  const img = await page.evaluate(async (u) => { const r = await fetch(u); return r.status + ' ' + r.headers.get('content-type'); }, sprite.url);
  expect(img).toBe('200 image/webp');

  await page.locator('.tile').nth(0).click();
  const track = page.getByRole('slider', { name: 'Position' });
  await expect(track).toBeVisible();
  const video = page.locator('.viewer .slide[data-slot="1"] video');
  await expect.poll(() => video.evaluate((v: HTMLVideoElement) => v.duration || 0)).toBeGreaterThan(1);
  await page.waitForResponse((r) => r.url().includes('/sprite'), { timeout: 2000 }).catch(() => {});
  const box = (await track.boundingBox())!;
  const y = box.y + box.height / 2;

  // Hovering with a mouse: the small frame above the bar, the picture untouched.
  await page.mouse.move(box.x + box.width * 0.5, y);
  await expect(page.locator('.controls .preview .frame')).toBeVisible();
  await expect(page.locator('.viewer .scrub-frame')).toHaveCount(0);

  // Scrubbing: the frame shows in the player, over the picture and the same size; no small frame.
  // (Recorded as it appears: with a tiny test video the real picture can catch up almost at once,
  // and then the stand-in is rightly gone again.)
  await page.evaluate(() => {
    const w = window as any;
    w.__frames = [];
    new MutationObserver(() => {
      const el = document.querySelector('.viewer .scrub-frame') as HTMLElement | null;
      const v = document.querySelector('.viewer .slide[data-slot="1"] video') as HTMLVideoElement | null;
      if (!el || !v) return;
      const r = el.getBoundingClientRect();
      const vr = v.getBoundingClientRect();
      const k = Math.min(vr.width / v.videoWidth, vr.height / v.videoHeight);
      w.__frames.push({ w: r.width, h: r.height, vw: v.videoWidth * k, vh: v.videoHeight * k, pos: getComputedStyle(el).backgroundPosition, img: getComputedStyle(el).backgroundImage, small: !!document.querySelector('.controls .preview .frame') });
    }).observe(document.querySelector('.viewer')!, { subtree: true, childList: true, attributes: true, attributeFilter: ['style'] });
  });
  await page.mouse.move(box.x + box.width * 0.2, y);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width * 0.7, y, { steps: 5 });
  await expect(page.locator('.controls .preview .at')).toBeVisible(); // the time stays above the bar
  await page.mouse.move(box.x + box.width * 0.05, y, { steps: 3 });
  const frames: any[] = await page.evaluate(() => (window as any).__frames);
  expect(frames.length).toBeGreaterThan(0);
  const f0 = frames[0];
  expect(Math.abs(f0.w - f0.vw)).toBeLessThan(3);
  expect(Math.abs(f0.h - f0.vh)).toBeLessThan(3);
  expect(f0.img).toContain('/sprites/');
  expect(frames.some((f) => f.small)).toBe(false); // no small frame while scrubbing
  expect(new Set(frames.map((f) => f.pos)).size).toBeGreaterThan(1); // other frames as it moves
  // One picture while the finger is down: the preview stays, even once the video
  // has caught up underneath (switching back and forth flickered).
  for (let i = 0; i < 4; i++) {
    await page.waitForTimeout(150);
    await expect(page.locator('.viewer .scrub-frame')).toHaveCount(1);
  }
  await page.mouse.up();
  // Once the video shows the spot itself, the preview gives way.
  await expect(page.locator('.viewer .scrub-frame')).toHaveCount(0);
});

test('video scrubbing: one seek at a time, never queued; exact seek on release; playback resumes', async ({ page }) => {
  await page.goto('/?q=system:type%3Dvideo');
  await expect(page.getByText(/^2 files/)).toBeVisible();
  await page.locator('.tile').nth(0).click();
  const video = page.locator('.viewer .slide[data-slot="1"] video');
  await expect.poll(() => video.evaluate((v: HTMLVideoElement) => v.duration || 0)).toBeGreaterThan(1);
  await expect(page.getByRole('button', { name: 'Pause' })).toBeVisible(); // playing
  await video.evaluate((v: HTMLVideoElement) => {
    const w = window as any;
    w.__seeks = 0;
    w.__overlap = 0;
    let inFlight = false;
    v.addEventListener('seeking', () => {
      w.__seeks++;
      if (inFlight) w.__overlap++;
      inFlight = true;
    });
    v.addEventListener('seeked', () => (inFlight = false));
  });
  const track = page.getByRole('slider', { name: 'Position' });
  const box = (await track.boundingBox())!;
  const y = box.y + box.height / 2;
  await page.mouse.move(box.x + box.width * 0.1, y);
  await page.mouse.down();
  // Count from here: before the scrub paused it, the looping clip may have wrapped (a seek of its own).
  await page.evaluate(() => {
    const w = window as any;
    w.__seeks = 0;
    w.__overlap = 0;
  });
  await page.mouse.move(box.x + box.width * 0.8, y, { steps: 40 });
  await page.waitForTimeout(300);
  expect(await video.evaluate((v: HTMLVideoElement) => v.paused)).toBe(true); // paused while scrubbing
  const seeks = await page.evaluate(() => (window as any).__seeks);
  expect(seeks).toBeGreaterThan(0); // the picture follows
  expect(seeks).toBeLessThan(40); // but not one seek per movement
  expect(await page.evaluate(() => (window as any).__overlap)).toBe(0); // never queued
  // The dot and the played bar are right under the finger.
  const knob = (await page.locator('.controls .knob').boundingBox())!;
  expect(Math.abs(knob.x + knob.width / 2 - (box.x + box.width * 0.8))).toBeLessThan(3);
  const played = (await page.locator('.controls .played').boundingBox())!;
  expect(Math.abs(played.x + played.width - (box.x + box.width * 0.8))).toBeLessThan(3);
  await page.mouse.up();
  const t = await video.evaluate((v: HTMLVideoElement) => v.currentTime / v.duration);
  expect(t).toBeGreaterThan(0.7);
  await expect.poll(() => video.evaluate((v: HTMLVideoElement) => v.paused)).toBe(false); // playing again
});


test('library: Sort and filters > From picks which sites to show files from (one or more: a chip; All sites removes it); phones: a bottom sheet', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  await expect(page.locator('.library .status')).toHaveCount(0); // nothing to say under the search: no line
  const filters = page.getByRole('button', { name: 'Filters', exact: true });
  await filters.click();
  if (isPhone(page)) {
    // A sheet along the bottom, edge to edge.
    const panel = page.locator('.panel[data-sheet]');
    await expect(panel).toBeVisible();
    await page.waitForTimeout(300); // it has risen
    const b = (await panel.boundingBox())!;
    expect(b.x).toBeLessThanOrEqual(0.5);
    expect(Math.abs(b.x + b.width - page.viewportSize()!.width)).toBeLessThan(1);
    expect(Math.abs(b.y + b.height - page.viewportSize()!.height)).toBeLessThan(1);
  }
  const picker = page.getByRole('button', { name: /^Show files from/ });
  await expect(picker).toHaveText(/All sites/);
  await picker.click();
  const from = page.getByRole('dialog', { name: 'Show files from' });
  await expect(from.getByRole('button', { name: /^All sites/ })).toContainText('26'); // counts for this search
  await expect(from.getByRole('button', { name: /^All sites/ })).toHaveAttribute('aria-pressed', 'true');
  await expect(from.getByRole('button', { name: /^safebooru.org/ })).toBeVisible(); // sites by address
  await from.getByRole('button', { name: /^test-down.invalid/ }).click();
  await expect(page).toHaveURL(/system(%3A|:)site(%3D|=)test-down(&|$)/);
  await expect(page.locator('.bar .chip')).toHaveText(/from test-down.invalid/);
  await expect(from.getByRole('button', { name: /^test-down.invalid/ })).toHaveAttribute('aria-pressed', 'true'); // the page stays
  await expect(page.getByText('Nothing matches this search.')).toBeVisible(); // files from disk come from no site
  // A second site: files from either.
  await from.getByRole('button', { name: /^test-slow.invalid/ }).click();
  await expect(page).toHaveURL(/system(%3A|:)site(%3D|=)test-down(%2C|,)test-slow/);
  await expect(page.locator('.bar .chip')).toHaveText(/from test-down.invalid or test-slow.invalid/);
  await from.getByRole('button', { name: 'Back to sort and filters' }).click();
  await expect(picker).toHaveText(/test-down.invalid, test-slow.invalid/);
  // Unticking one leaves the other.
  await picker.click();
  await from.getByRole('button', { name: /^test-down.invalid/ }).click();
  await expect(page.locator('.bar .chip')).toHaveText('from test-slow.invalid');

  // All sites takes the chip out (other chips stay).
  await page.goto('/?q=system:type%3Dimage%20system:site%3Dtest-down');
  await filters.click();
  await picker.click();
  await from.getByRole('button', { name: /^All sites/ }).click();
  await expect(page.locator('.bar .chip')).toHaveCount(1);
  await expect(page.locator('.bar .chip')).toHaveText(/images/);
  await from.getByRole('button', { name: 'Back to sort and filters' }).click();
  await expect(picker).toHaveText(/All sites/);
  // The Other group has no Site entry any more (the From row does it).
  await expect(page.getByRole('dialog', { name: /filter/i }).getByRole('button', { name: /^Site/ })).toHaveCount(0);
});

test('downloads: a failed search says when it tries again by itself', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-webkit', 'one run is enough: the queue is on the server');
  await page.goto('/downloads');
  const d = await page.evaluate(() =>
    fetch('/api/downloads', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ site: 'test-down', query: 'retry_demo', max_posts: 0 }) }).then((r) => r.json()),
  );
  // (Shown live too - another test - but this one is about the retry line: read once it has failed.)
  await expect.poll(async () => (await (await page.request.get('/api/downloads')).json()).searches.find((x: { id: number }) => x.id === d.id)?.state).toBe('failed');
  await page.reload();
  await dlTab(page, 'History');
  const row = page.locator('.stack .item', { hasText: 'retry_demo' });
  await expect(row.locator('.state')).toHaveText('Failed');
  await expect(row.locator('.retry')).toHaveText(/Trying again in 1 min/);
  // Finished has Retry failed: every failed download tries again at once.
  const finished = page.locator('section.card', { has: page.getByRole('heading', { name: 'Finished' }) });
  const tries = (await (await page.request.get('/api/downloads')).json()).searches.find((s: { id: number }) => s.id === d.id)?.attempts ?? 0;
  await finished.getByRole('button', { name: 'Retry failed' }).click();
  await expect(page.getByText(/^Retrying \d+ failed downloads?$/)).toBeVisible();
  await expect
    .poll(async () => (await (await page.request.get('/api/downloads')).json()).searches.find((s: { id: number }) => s.id === d.id)?.attempts ?? 0)
    .toBeGreaterThan(tries); // it ran again (the test site is down: failed again)
  await expect(row.locator('.state')).toHaveText('Failed');
  await expect(row.locator('.retry')).toHaveText(/Trying again in/);
  await rowMenu(page, 'the download retry_demo', 'Remove');
  expect(d.id).toBeTruthy();
});

test('settings: download rules are saved on the server (empty = no limit)', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-webkit', 'one run is enough: settings are on the server');
  await page.goto('/settings/rules');
  const video = page.getByLabel('Skip videos larger than (MB)');
  await expect(video).toHaveValue('');
  await expect(video).toHaveAttribute('placeholder', 'No limit');
  await video.fill('500');
  await video.press('Tab');
  await expect.poll(async () => (await page.evaluate(async () => (await fetch('/api/settings')).json())).skipVideoMB).toBe(500);
  await page.reload();
  await expect(video).toHaveValue('500');
  await video.fill('');
  await video.press('Tab');
  await expect.poll(async () => (await page.evaluate(async () => (await fetch('/api/settings')).json())).skipVideoMB).toBe(0);
});

test('settings: notifications card - other devices, kinds, service worker', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-webkit', 'one run is enough: devices and settings are on the server');
  const sw = await page.request.get('/sw.js');
  expect(sw.ok()).toBeTruthy();
  expect(sw.headers()['content-type']).toContain('javascript');
  await page.goto('/settings/notifications');
  // A device added elsewhere (a made-up but valid subscription).
  const added = await page.evaluate(async () => {
    const key = await crypto.subtle.generateKey({ name: 'ECDH', namedCurve: 'P-256' }, true, ['deriveBits']);
    const raw = new Uint8Array(await crypto.subtle.exportKey('raw', key.publicKey));
    const b64 = (b: Uint8Array) => btoa(String.fromCharCode(...b)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
    const r = await fetch('/api/push/subscribe', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        subscription: { endpoint: 'https://push.example.invalid/abc', expirationTime: null, keys: { p256dh: b64(raw), auth: b64(crypto.getRandomValues(new Uint8Array(16))) } },
        label: 'Test phone',
      }),
    });
    return r.status;
  });
  expect(added).toBe(200);
  await page.reload();
  const card = page.locator('section.card', { has: page.getByRole('heading', { name: 'Notifications' }) });
  await expect(card.getByText('Test phone')).toBeVisible();
  // The kinds are shared settings.
  await card.getByLabel('Downloads that finish').uncheck();
  await expect.poll(async () => (await page.evaluate(async () => (await fetch('/api/settings')).json())).notifyDone).toBe(false);
  await card.getByLabel('Downloads that finish').check();
  await expect(card.getByLabel('Blacklisted tags in the library')).toBeChecked(); // on unless turned off
  await card.getByLabel('Blacklisted tags in the library').uncheck();
  await expect.poll(async () => (await page.evaluate(async () => (await fetch('/api/settings')).json())).notifyBlacklisted).toBe(false);
  await card.getByLabel('Blacklisted tags in the library').check();
  // Removing the other device.
  await card.getByRole('button', { name: 'Stop notifying Test phone' }).click();
  await expect(card.getByText('Test phone')).toHaveCount(0);
  const info = await page.evaluate(async () => (await fetch('/api/push')).json());
  expect(info.devices.length).toBe(0);
  expect(info.key.length).toBeGreaterThan(80);
});

test('downloads: max files stays empty (every post) when subscribing', async ({ page }) => {
  await page.goto('/downloads');
  const max = page.getByLabel(/^Max files/);
  await expect(max).toHaveValue('');
  await page.getByRole('button', { name: 'Repeat' }).click();
  await page.getByRole('option', { name: 'Every hour' }).click();
  await expect(page.getByLabel('Max files on the first check')).toHaveValue('');
  await expect(page.getByText(/The first check gets every post/)).toBeVisible();
  await page.getByRole('button', { name: 'Repeat' }).click();
  await page.getByRole('option', { name: 'Once' }).click();
  await expect(page.getByLabel('Max files')).toHaveValue('');
});

const addDownload = (page: import('@playwright/test').Page, site: string, query: string) =>
  page.evaluate(
    ([site, query]) =>
      fetch('/api/downloads', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ site, query, max_posts: 0 }) }).then((r) => r.json()),
    [site, query],
  );
const removeDownload = (page: import('@playwright/test').Page, id: number) =>
  page.evaluate((id) => fetch(`/api/downloads/${id}/remove`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' }), id);

test('downloads: changes made elsewhere show up live', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-webkit', 'one run is enough: the queue is on the server');
  await page.goto('/downloads');
  await page.waitForTimeout(1000); // the live stream is open
  // Added behind the page's back (as from another device): no polling for 20 s, so only the stream can show it.
  const d = await addDownload(page, 'test-slow', 'live_demo'); // (a slow site: it stays in the queue, the tab shown)
  await expect(page.locator('.stack .item', { hasText: 'live_demo' })).toBeVisible({ timeout: 1500 });
  await removeDownload(page, d.id);
  await expect(page.locator('.stack .item', { hasText: 'live_demo' })).toHaveCount(0, { timeout: 1500 });
});

test('downloads: a running search without an estimate has a moving striped bar; the speed shows, no per-file bar', async ({ page }) => {
  test.skip(test.info().project.name !== 'iphone', 'one run is enough: the test site keeps one search running at a time');
  await page.goto('/downloads');
  const d = await addDownload(page, 'test-hang', 'progress_demo'); // a site of its own: nothing else waits in its queue
  await page.reload(); // (shown live too - another test - but this one is about the bar)
  const row = page.locator('.stack .item', { hasText: 'progress_demo' });
  await expect(row.locator('.state')).toHaveText('Downloading');
  // The test site never answers: no total, no file limit - a striped bar, not an empty one.
  await expect(row.getByRole('progressbar', { name: 'Downloading (no estimate)' })).toBeVisible();
  await expect(row.locator('.bar.striped')).toHaveCSS('animation-name', /stripes/);
  // Files in flight (a made-up stream, every 300 ms, 2 MB more each time): only the speed shows.
  let read = 0;
  await page.route('**/api/events', (route) => {
    read += 2_000_000;
    const data = JSON.stringify([{ search: d.id, post: 9123456, read, total: 48_000_000 }]);
    route.fulfill({ status: 200, headers: { 'Content-Type': 'text/event-stream' }, body: `retry: 300

event: progress
data: ${data}

` });
  });
  await page.reload();
  await expect(row.locator('.stat.speed')).toHaveText(/\d.* MB\/s$/);
  await expect(row.locator('.now, .fbar')).toHaveCount(0);
  await removeDownload(page, d.id);
});

test("library: resting the mouse on a tile zooms the tile itself into a larger card in the file's shape (a video plays, a picture shows bigger); moving the mouse shrinks it back", async ({ page }) => {
  test.skip(isPhone(page), 'mouse only');
  await page.goto('/');
  const tile = page.locator('.tile[aria-label$="1280×720"]').first(); // the landscape video
  await expect(tile).toBeVisible();
  const tb = (await tile.boundingBox())!; // (before: zoomed, it is the card)
  // When the card and its video appear (the video waits until the card has grown: no stutter).
  await page.evaluate(() => {
    const w = window as any;
    new MutationObserver(() => {
      if (!w.__cardAt && document.querySelector('.tile.zoomed')) w.__cardAt = performance.now();
      if (!w.__videoAt && document.querySelector('.tile.zoomed video')) w.__videoAt = performance.now();
    }).observe(document.body, { childList: true, subtree: true, attributes: true, attributeFilter: ['class'] });
  });
  await tile.hover();
  const card = page.locator('.tile.zoomed');
  await page.waitForTimeout(300); // (it grows from its place)
  await expect(card).toHaveCount(1);
  expect(await card.getAttribute('data-i')).toBe(await tile.getAttribute('data-i')); // the tile itself, no copy
  await expect(card.locator('.zoom-media')).toHaveClass(/\bready\b/); // playing
  const video = card.locator('video');
  expect(await video.evaluate((v: HTMLVideoElement) => v.muted)).toBe(true);
  const [cardAt, videoAt] = await page.evaluate(() => [(window as any).__cardAt, (window as any).__videoAt]);
  expect(videoAt - cardAt).toBeGreaterThan(300); // the grow takes 340 ms
  // Larger than the tile, in the video's shape (16:9), inside the grid.
  const cb = (await card.boundingBox())!;
  expect(cb.width).toBeGreaterThan(tb.width * 1.2);
  expect(cb.width / cb.height).toBeCloseTo(16 / 9, 1);
  // A move on the card keeps it (it opens at once and stays while the mouse is on it)...
  await page.mouse.move(tb.x + tb.width / 2 + 12, tb.y + tb.height / 2 + 6);
  await page.waitForTimeout(200);
  await expect(card).toHaveCount(1);
  await expect(page.locator('.tile.shrinking')).toHaveCount(0);
  // ...off the grid, it shrinks back to its place; back on the tile, it opens again (no wait).
  // (its shrinking lasts 220 ms - a check under load can come after it: noted as it happens)
  await page.evaluate(() => {
    const w = window as any;
    w.__shrank = false;
    new MutationObserver(() => {
      if (document.querySelector('.tile.shrinking')) w.__shrank = true;
    }).observe(document.body, { subtree: true, attributes: true, attributeFilter: ['class'] });
  });
  await page.mouse.move(2, 2);
  await expect.poll(() => page.evaluate(() => (window as any).__shrank)).toBe(true);
  await expect(card).toHaveCount(0);
  await page.mouse.move(tb.x + tb.width / 2, tb.y + tb.height / 2);
  await expect(card).toHaveCount(1);
  // A picture: a card too (the tall one in its shape).
  await page.goto('/?q=system:type%3Dimage');
  const tall = page.locator('.tile[aria-label="png 400×3000"]').first();
  await tall.hover();
  await expect(card).toHaveCount(1);
  await expect(card.locator('img.zoom-media')).toHaveCount(1);
  await expect(card.locator('.zoom-media')).toHaveClass(/\bready\b/);
  const pb = (await card.boundingBox())!;
  expect(pb.height / pb.width).toBeGreaterThan(4); // 400 x 3000, fitted
  // "Play videos on hover" off in Settings (this device's copy; the setting is shared): pictures' cards still open.
  await page.evaluate(() => localStorage.setItem('vitrine.hoverPreview', 'false'));
  await page.route('**/api/settings', async (route) => {
    const r = await route.fetch();
    route.fulfill({ response: r, json: { ...(await r.json()), hoverPreview: false } });
  });
  await page.reload();
  await tall.hover();
  await expect(card).toHaveCount(1);
  await expect(card.locator('img.zoom-media')).toHaveClass(/\bready\b/);
});

test('details: a very long tag wraps inside the panel', async ({ page }, info) => {
  const long = 'tsuihou_sareta_tensei_juukishi_wa_game_chishiki_de_musou_suru';
  await page.goto('/');
  const id: number = await page.evaluate(async (long) => {
    const id = (await (await fetch('/api/search?q=&limit=1')).json()).ids[0];
    await fetch('/api/tags', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ids: [id], add: [long], remove: [] }) });
    return id;
  }, long);
  const panel = page.locator('.viewer aside.panel');
  await page.locator('.tile').first().click();
  if (!(await panel.isVisible())) await page.getByRole('button', { name: 'Details' }).click();
  const pill = panel.locator('li', { hasText: long });
  await expect(pill).toBeVisible();
  await page.waitForTimeout(400); // the panel has slid in (wide screens)
  const [p, b] = [await pill.boundingBox(), await panel.boundingBox()];
  expect(p!.x + p!.width).toBeLessThanOrEqual(b!.x + b!.width + 0.5);
  expect(await panel.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true);
  await panel.getByRole('button', { name: 'Edit tags' }).click(); // the × shows while editing
  await expect(pill.getByRole('button', { name: 'Remove ' + long })).toBeInViewport();
  expect(await panel.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true);
  await panel.getByRole('button', { name: 'Save tags' }).click(); // nothing changed: just done
  await page.waitForTimeout(400); // the panel slides in
  if (process.env.SHOT) await panel.screenshot({ path: `${process.env.SHOT}/longtag-${info.project.name}.png` });
  await page.evaluate(async ([id, long]) => {
    await fetch('/api/tags', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ids: [id], add: [], remove: [long] }) });
  }, [id, long] as const);
});

test('search: the filter menu adds filters as readable chips, and a tapped filter chip opens its form', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  const menu = page.getByRole('dialog', { name: /filter/i });
  const open = () => page.getByRole('button', { name: 'Filters', exact: true }).click();
  const chips = page.locator('.bar .chip');

  // Has audio: no form, added at once.
  await open();
  await menu.getByRole('button', { name: /^Has audio/ }).click();
  await expect(chips).toHaveText(['has audio']);
  await expect(page.getByText(/^1 file\b/)).toBeVisible(); // only the landscape clip has sound

  // Videos longer than 10 seconds.
  await page.getByRole('button', { name: 'Clear search' }).click();
  await open();
  await menu.getByRole('button', { name: /^Length/ }).click();
  await menu.getByLabel('Value').fill('10');
  await expect(menu.locator('.chip-preview')).toHaveText('longer than 10 seconds');
  await menu.getByRole('button', { name: 'Add filter' }).click();
  await expect(chips).toHaveText(['longer than 10 seconds']);
  await expect(page.getByText(/^1 file\b/)).toBeVisible();

  // Imported in the last 5 days, then edited to 2 hours by tapping the chip.
  await open();
  await menu.getByRole('button', { name: /^Imported/ }).click();
  await menu.getByLabel('How many').fill('5');
  await menu.getByLabel('How many').press('Enter');
  await expect(chips.nth(1)).toHaveText('imported in the last 5 days');
  await chips.nth(1).locator('.chip-main').click();
  await expect(menu).toBeVisible();
  await expect(menu.getByLabel('How many')).toHaveValue('5');
  await menu.getByLabel('How many').fill('2');
  await menu.getByRole('button', { name: 'Unit' }).click();
  await page.getByRole('option', { name: 'hours' }).click();
  await menu.getByRole('button', { name: 'Update' }).click();
  await expect(chips).toHaveText(['longer than 10 seconds', 'imported in the last 2 hours']);
  expect(page.url()).toContain(encodeURIComponent('system:imported<2h'));

  // Type: videos, and not videos.
  await page.getByRole('button', { name: 'Clear search' }).click();
  await open();
  await menu.getByRole('button', { name: /^Type/ }).click();
  await menu.getByRole('button', { name: 'Videos' }).click();
  await menu.getByRole('button', { name: 'Add filter' }).click();
  await expect(chips).toHaveText(['videos']);
  await expect(page.getByText(/^2 files/)).toBeVisible();
  await chips.first().locator('.chip-main').click();
  await menu.getByRole('tab', { name: 'Is not' }).click();
  await menu.getByRole('button', { name: 'Update' }).click();
  await expect(chips).toHaveText(['not videos']);
  await expect(page.getByText(/^24 files/)).toBeVisible();

  // Esc closes the menu.
  await open();
  await expect(menu).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(menu).toHaveCount(0);
});

test('filter menu: a dropdown in a form opens fully over the page', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'Filters', exact: true }).click();
  const menu = page.getByRole('dialog', { name: /filter/i });
  await menu.getByRole('button', { name: /^Imported/ }).click();
  for (const [dd, last] of [['Unit', 'years'], ['When', 'On a date']]) {
    await menu.getByRole('button', { name: dd }).click();
    await expect(page.getByRole('option', { name: last })).toBeInViewport({ ratio: 1 });
    await expect(menu.getByRole('heading', { name: 'Imported' })).toBeInViewport(); // the panel didn't scroll away
    expect(await menu.evaluate((el) => el.scrollTop)).toBe(0);
    await page.getByRole('option', { name: last }).click();
  }
  await expect(menu.getByLabel('Date')).toBeVisible(); // "On a date" picked
});

test('sort: the order opens a Sort by page, an order with two ways opens its own page (back arrows); Reset; a tap outside closes', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  await page.getByRole('button', { name: 'Filters', exact: true }).click();
  const pick = page.getByRole('button', { name: /^Sort by/ });
  await expect(pick).toContainText('Uploaded');
  await expect(pick).toContainText('Newest first');
  await expect(page.getByRole('button', { name: 'Reset to default' })).toHaveCount(0);
  // Sort by -> Imported -> Oldest first: applied, back on the menu (still open).
  await pick.click();
  const sortPage = page.getByRole('dialog', { name: 'Sort' });
  await expect(sortPage.getByRole('heading', { name: 'Sort by' })).toBeVisible();
  await sortPage.getByRole('button', { name: /^Imported/ }).click();
  await expect(sortPage.getByRole('heading', { name: 'Imported' })).toBeVisible();
  await sortPage.getByRole('button', { name: /^Oldest first/ }).click();
  await expect.poll(() => new URL(page.url()).searchParams.get('sort')).toBe('imported_asc');
  await expect(pick).toContainText('Oldest first');
  // The back arrows: from a direction page to the orders, then to the menu (nothing changes).
  await pick.click();
  await sortPage.getByRole('button', { name: /^Uploaded/ }).click();
  await sortPage.getByRole('button', { name: 'Back to the orders' }).click();
  await expect(sortPage.getByRole('heading', { name: 'Sort by' })).toBeVisible();
  await sortPage.getByRole('button', { name: 'Back to sort and filters' }).click();
  await expect(pick).toContainText('Oldest first');
  expect(new URL(page.url()).searchParams.get('sort')).toBe('imported_asc');
  // Reset goes back to the default order.
  await page.getByRole('button', { name: 'Reset to default' }).click();
  await expect.poll(() => new URL(page.url()).searchParams.get('sort')).toBeNull();
  await expect(page.getByRole('button', { name: 'Reset to default' })).toHaveCount(0);
  await page.mouse.click(20, 600); // outside the menu and the search bar
  await expect(pick).toHaveCount(0);
});

test('settings: install a site from the plugin list, update one, install from a file - no restart', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-webkit', 'one run is enough: plugins are on the server');
  await page.goto('/settings/sites');
  const card = page.locator('section.card', { has: page.getByRole('heading', { name: 'Sites', exact: true }) });

  // Update: test-down has 1.1.0 in the list.
  const down = card.locator('article', { has: page.getByRole('heading', { name: 'test-down.invalid' }) });
  await expect(down.getByText('Version 1.1.0 is available')).toBeVisible();
  await page.getByRole('button', { name: 'More for test-down.invalid' }).click();
  await expect(page.getByRole('menuitem', { name: 'Update to 1.1.0' })).toBeVisible(); // also in its menu
  await page.keyboard.press('Escape');
  await down.getByRole('button', { name: 'Update test-down.invalid' }).click();
  await expect(page.getByText('Updated test-down.invalid to 1.1.0')).toBeVisible();
  await expect(down.getByText('Version 1.1.0 is available')).toHaveCount(0);
  await expect(down.locator('.meta')).toContainText('v1.1.0');

  // Install from the list.
  await card.getByRole('button', { name: 'Add site' }).click();
  const panel = card.getByRole('region', { name: 'Add a site' });
  await expect(panel.getByText('test-extra.invalid')).toHaveCount(0); // an adult site: only when asked for
  await panel.getByRole('checkbox', { name: 'Adult (18+)' }).check();
  await expect(panel.getByText('test-extra.invalid')).toBeVisible();
  await expect(panel.getByText('test-down.invalid')).toHaveCount(0); // installed sites aren't offered again
  await expect(panel.getByText('adult', { exact: true })).toBeVisible();
  await panel.getByRole('button', { name: 'Install test-extra.invalid' }).click();
  await expect(page.getByText('Installed test-extra.invalid 1.0.0')).toBeVisible();
  await expect(card.locator('article').getByRole('heading', { name: 'test-extra.invalid' })).toBeVisible();
  // Usable at once: it's a site for new downloads.
  await page.goto('/downloads');
  await page.getByRole('button', { name: 'Site', exact: true }).click();
  await expect(page.getByRole('menuitemradio', { name: 'test-extra.invalid' })).toBeVisible();
  await page.keyboard.press('Escape');

  // Install from a file; a bad file is refused.
  await page.goto('/settings/sites');
  await card.getByRole('button', { name: 'Add site' }).click();
  await expect(panel.getByText('Every site in the plugin list is installed.')).toBeVisible();
  await panel.getByLabel('Plugin file').setInputFiles({ name: 'broken.yaml', mimeType: 'text/yaml', buffer: Buffer.from('id: broken\nengine: nope\n') });
  await expect(page.getByText(/not a valid plugin/)).toBeVisible();
  await panel.getByLabel('Plugin file').setInputFiles({
    name: 'test-file.yaml',
    mimeType: 'text/yaml',
    buffer: Buffer.from('id: test-file\nsite: test-file.invalid\nversion: 0.1.0\nengine: gelbooru-v02\nbase: https://127.0.0.1:1\npost_url: https://127.0.0.1:1/post/{id}\n'),
  });
  await expect(page.getByText('Installed test-file.invalid 0.1.0')).toBeVisible();
  await expect(card.getByRole('heading', { name: 'test-file.invalid' })).toBeVisible();

  // Remove: a confirmation that says what stays; then it's offered again in the list.
  const extra = card.locator('article', { has: page.getByRole('heading', { name: 'test-extra.invalid' }) });
  await rowMenu(page, 'test-extra.invalid', 'Remove site');
  const confirm = page.getByRole('alertdialog', { name: 'Remove test-extra.invalid?' });
  await expect(confirm).toContainText('files, tags and login stay');
  await confirm.getByRole('button', { name: 'Cancel' }).click();
  await expect(confirm).toHaveCount(0);
  await rowMenu(page, 'test-extra.invalid', 'Remove site');
  await confirm.getByRole('button', { name: 'Remove', exact: true }).click();
  await expect(page.getByText('Removed test-extra.invalid')).toBeVisible();
  await expect(card.getByRole('heading', { name: 'test-extra.invalid' })).toHaveCount(0);
  await card.getByRole('button', { name: 'Add site' }).click();
  await panel.getByRole('checkbox', { name: 'Adult (18+)' }).check();
  await expect(panel.getByText('test-extra.invalid')).toBeVisible(); // back in Add site
  await card.getByRole('button', { name: 'Close' }).click();
  // Built-in sites can be removed too (asked first - see the site on/off test).
  await page.getByRole('button', { name: 'More for safebooru.org' }).click();
  await expect(page.getByRole('menuitem', { name: /^Remove site/ })).toHaveCount(1);
  await page.keyboard.press('Escape');
  await rowMenu(page, 'test-file.invalid', 'Remove site');
  await confirmIn(page, 'Remove');
  await expect(card.getByRole('heading', { name: 'test-file.invalid' })).toHaveCount(0);
});

test('offline: the app, the last results and their thumbnails stay on the device', async ({ page, context }) => {
  test.skip(test.info().project.name !== 'desktop-chromium', 'service workers are tested in Chromium');
  const origin = ''; // (the tests run on the built app)
  await context.addInitScript(() => localStorage.setItem('vitrine.sw', 'on'));
  await page.goto(origin + '/');
  await page.waitForFunction(() => !!navigator.serviceWorker?.controller, null, { timeout: 15000 });
  await page.reload(); // now through the service worker
  await expect(page.locator('.tile img.in').first()).toBeVisible();
  const saved = () => page.evaluate(async () => (await (await caches.open('vitrine-thumbs-v1')).keys()).length);
  await expect.poll(saved).toBeGreaterThan(5);

  await context.setOffline(true);
  await page.reload();
  await expect(page.getByText(/Offline · showing saved results/)).toBeVisible();
  await expect(page.locator('.tile img.in').first()).toBeVisible();
  await expect(page.locator('.topbar .count')).toHaveText(/\d+ files/);

  // Back online: the note goes; Settings shows what's kept and clears it.
  await context.setOffline(false);
  await page.reload();
  await expect(page.getByText(/Offline · showing/)).toHaveCount(0);
  await page.goto(origin + '/settings/device');
  await expect(page.getByText(/\d+ thumbnails/)).toBeVisible();
  await page.locator('.saved').getByRole('button', { name: 'Clear' }).click();
  await expect(page.locator('.saved')).toContainText('Nothing yet');
  await page.evaluate(async () => {
    for (const r of await navigator.serviceWorker.getRegistrations()) await r.unregister();
  });
});

test('search chips: drag one onto another to make an OR group', async ({ page }) => {
  await page.goto('/?q=' + encodeURIComponent('blue_sky red limit:20'));
  await expect(page.getByText(/^0 files/)).toBeVisible(); // both tags: nothing has both
  const chips = page.locator('.bar .chip');
  await expect(chips).toHaveCount(3);
  const center = async (i: number) => {
    const b = (await chips.nth(i).locator('.chip-main').boundingBox())!;
    return { x: b.x + b.width / 2, y: b.y + b.height / 2 };
  };
  const from = await center(1); // red
  const to = await center(0); // blue_sky
  if (isPhone(page)) {
    // Touch: press and hold, then slide (synthetic pointer events - the hold starts the drag).
    const el = chips.nth(1).locator('.chip-main');
    const ev = (type: string, p: { x: number; y: number }) =>
      el.dispatchEvent(type, { pointerId: 7, pointerType: 'touch', isPrimary: true, button: 0, clientX: p.x, clientY: p.y, bubbles: true });
    await ev('pointerdown', from);
    await page.waitForTimeout(450);
    await ev('pointermove', { x: (from.x + to.x) / 2, y: to.y });
    await ev('pointermove', to);
    await expect(chips.nth(0)).toHaveClass(/target/);
    await ev('pointerup', to);
  } else {
    await page.mouse.move(from.x, from.y);
    await page.mouse.down();
    await page.mouse.move(from.x - 20, from.y, { steps: 3 });
    await page.mouse.move(to.x, to.y, { steps: 6 });
    await expect(page.locator('.drag-chip')).toBeVisible(); // the chip follows the pointer
    await expect(chips.nth(0)).toHaveClass(/target/);
    await page.mouse.up();
  }
  await expect(chips).toHaveCount(2);
  await expect(chips.nth(0)).toHaveText(/blue_sky\s*OR\s*red/);
  await expect(page.getByText(/^6 files/)).toBeVisible();
  expect(decodeURIComponent(new URL(page.url()).searchParams.get('q')!)).toContain('( blue_sky ~ red )');
  await expect(page.getByRole('dialog', { name: 'Edit search chip' })).toHaveCount(0); // the drop didn't open the editor

  // A tap still edits; a limit chip doesn't combine (no drag starts).
  await chips.nth(0).locator('.chip-main').click();
  await expect(page.getByRole('dialog', { name: 'Edit search chip' })).toBeVisible();
  await page.keyboard.press('Escape');
  if (!isPhone(page)) {
    const lim = await center(1);
    const grp = await center(0);
    await page.mouse.move(lim.x, lim.y);
    await page.mouse.down();
    await page.mouse.move(grp.x, grp.y, { steps: 6 });
    await expect(page.locator('.drag-chip')).toHaveCount(0);
    await page.mouse.up();
    await expect(chips).toHaveCount(2);
  }
});

test('subscriptions: a filter and an order once there are several (the order is remembered)', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-webkit', 'one run is enough: subscriptions are on the server');
  await page.goto('/downloads');
  const names = ['delta', 'alpha', 'zeta', 'echo', 'bravo', 'charlie'];
  const ids: number[] = await page.evaluate(async (names) => {
    const out: number[] = [];
    for (const q of names) {
      const r = await fetch('/api/subscriptions', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ site: 'test-down', query: 'list_' + q, interval: 604800, initial_max: 0 }) });
      out.push((await r.json()).id);
    }
    return out;
  }, names);
  await page.reload();
  await dlTab(page, 'Subscriptions'); // (remembered over the reloads below)
  const card = page.locator('section.card', { has: page.getByRole('heading', { name: 'Subscriptions' }) });
  const filter = card.getByLabel('Filter subscriptions');
  await expect(filter).toBeVisible();
  await filter.fill('zeta');
  await expect(card.locator('li .query')).toHaveText(['list_zeta']);
  await expect(card.getByText('Showing 1 of 6')).toBeVisible();
  await filter.fill('nothing_like_it');
  await expect(card.getByText('No subscriptions match "nothing_like_it"')).toBeVisible();
  await filter.fill('');
  await card.getByRole('button', { name: 'Order subscriptions by' }).click();
  await page.getByRole('option', { name: 'Name' }).click();
  await expect(card.locator('li .query').first()).toHaveText('list_alpha');
  await expect(card.locator('li .query').last()).toHaveText('list_zeta');
  await page.reload();
  await expect(card.getByRole('button', { name: 'Order subscriptions by' })).toHaveText(/Name/);
  await expect(card.locator('li .query').first()).toHaveText('list_alpha');
  await page.evaluate(async (ids) => {
    for (const id of ids) await fetch(`/api/subscriptions/${id}/remove`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' });
    localStorage.removeItem('vitrine.subSort');
  }, ids);
});

test('subscription groups: subscribe into a new or existing group, move, act on all, fold, rename, delete', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-webkit', 'one run is enough: subscriptions are on the server');
  await page.goto('/downloads');
  await dlTab(page, 'Subscriptions');
  const card = page.locator('section.card', { has: page.getByRole('heading', { name: 'Subscriptions' }) });
  const subscribe = async (q: string, group: string, newName = '') => {
    await page.getByRole('button', { name: 'Site', exact: true }).click();
    await page.getByRole('menuitemradio', { name: 'test-down.invalid' }).click();
    await page.getByRole('combobox', { name: 'Search on the site' }).fill(q);
    await page.getByRole('button', { name: 'Repeat' }).click();
    await page.getByRole('option', { name: 'Every week' }).click();
    await page.getByRole('button', { name: 'Group', exact: true }).click();
    await page.getByRole('option', { name: group }).click();
    if (newName) await page.getByLabel('New group name').fill(newName);
    await page.getByRole('button', { name: 'Subscribe', exact: true }).click();
  };
  await subscribe('grp_a', 'New group…', 'Artists');
  await expect(page.getByText('Subscribed to "grp_a" in "Artists"', { exact: false })).toBeVisible();
  const group = card.getByRole('region', { name: 'Group Artists' });
  // Groups start folded; unfolded, the device remembers.
  await expect(group.locator('li')).toHaveCount(0);
  await group.getByRole('button', { name: 'Unfold Artists' }).click();
  await expect(group.locator('li .query')).toHaveText(['grp_a']);
  // Its subscriptions are cards with round corners, like the ones outside a group.
  expect(await group.locator('li').first().evaluate((el) => getComputedStyle(el).borderTopLeftRadius)).toBe('12px');
  await subscribe('grp_b', 'Artists');
  await expect(group.locator('.count')).toHaveText('2');
  await subscribe('grp_c', 'No group');
  await expect(card.getByRole('heading', { name: 'Not in a group' })).toBeVisible();

  // Move grp_c into the group from its menu.
  await rowMenu(page, 'grp_c', 'Move to group', 'Artists');
  await expect(group.locator('.count')).toHaveText('3');
  await expect(card.getByRole('heading', { name: 'Not in a group' })).toHaveCount(0);

  // Act on everything in the group.
  await rowMenu(page, 'Artists', 'Pause all');
  await expect(group.locator('.group-head .state.paused')).toBeVisible();
  await rowMenu(page, 'Artists', 'Resume all');
  await expect(group.locator('.group-head .state.paused')).toHaveCount(0);
  await rowMenu(page, 'Artists', 'Schedule for all', 'Every 3 days');
  await expect(group.locator('li').filter({ hasText: 'Every 3 days' })).toHaveCount(3);

  // Check depth: set on the group, its subscriptions get it by default; one can set its own.
  await rowMenu(page, 'Artists', 'Check depth', '200 posts');
  await expect(async () => {
    await rowMenu(page, 'grp_a', 'Check depth');
    await expect(page.getByRole('menuitemradio', { name: 'Default (200 posts)' })).toHaveAttribute('aria-checked', 'true', { timeout: 1000 });
  }).toPass();
  await page.getByRole('menuitemradio', { name: '50 posts', exact: true }).click();
  await expect(async () => {
    await rowMenu(page, 'grp_a', 'Check depth');
    await expect(page.getByRole('menuitemradio', { name: '50 posts', exact: true })).toHaveAttribute('aria-checked', 'true', { timeout: 1000 });
  }).toPass();
  await page.getByRole('menuitemradio', { name: 'Default (200 posts)' }).click();

  // Fold and unfold (remembered on this device).
  await group.getByRole('button', { name: 'Fold Artists' }).click();
  await expect(group.locator('li')).toHaveCount(0);
  await page.reload();
  await expect(group.locator('li')).toHaveCount(0);
  await group.getByRole('button', { name: 'Unfold Artists' }).click();
  await expect(group.locator('li')).toHaveCount(3);

  // Rename, then delete: the subscriptions stay, in no group.
  await rowMenu(page, 'Artists', 'Rename');
  await group.getByLabel('New name for Artists').fill('Favorites');
  await group.getByRole('button', { name: 'Rename', exact: true }).click();
  const renamed = card.getByRole('region', { name: 'Group Favorites' });
  await expect(renamed).toBeVisible();
  await rowMenu(page, 'Favorites', 'Delete group');
  await expect(page.getByRole('alertdialog', { name: 'Delete the group "Favorites"?' })).toContainText('Its 3 subscriptions stay, in no group.');
  await confirmIn(page, 'Delete group');
  await expect(card.getByRole('region', { name: /^Group / })).toHaveCount(0);
  await expect(card.locator('li .query')).toHaveCount(3); // still subscribed, now in no group
  for (const q of ['grp_a', 'grp_b', 'grp_c']) {
    await rowMenu(page, q, 'Unsubscribe');
    await confirmIn(page, 'Unsubscribe');
    await expect(card.locator('li', { hasText: q })).toHaveCount(0);
  }
});

test('first run: the empty library welcomes you and points the way', async ({ page }) => {
  // An empty library (the test one has files): every search finds nothing.
  await page.route('**/api/search?*', (route) =>
    route.fulfill({ json: { result: 'empty', total: 0, matched: 0, ids: [], unknown: [], seed: 0, took_ms: 0, query: '', now: Date.now() } }),
  );
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Welcome to Vitrine' })).toBeVisible();
  await expect(page.getByText('Vitrine can download from safebooru.org', { exact: false })).toBeVisible();
  await expect(page.getByText('vitrine import /path/to/folder')).toBeVisible();
  await page.getByRole('button', { name: 'Start a download' }).click();
  await expect(page).toHaveURL(/\/downloads$/);
  await expect(page.getByRole('combobox', { name: 'Search on the site' })).toBeFocused();
  await page.goto('/');
  await page.getByRole('button', { name: 'Open Sites' }).click();
  await expect(page).toHaveURL(/\/settings\/sites$/);
  await expect(page.getByRole('heading', { name: 'Sites', exact: true })).toBeInViewport();
});

test('login: the password can be shown; a hint says how to reset it', async ({ page }) => {
  await page.goto('/');
  await page.evaluate(() => fetch('/api/logout', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' }));
  await page.goto('/');
  const pw = page.getByLabel('Password', { exact: true });
  await expect(pw).toHaveAttribute('type', 'password');
  await pw.fill('typo-check');
  await page.getByRole('button', { name: 'Show the password' }).click();
  await expect(pw).toHaveAttribute('type', 'text');
  await page.getByRole('button', { name: 'Hide the password' }).click();
  await expect(pw).toHaveAttribute('type', 'password');
  await expect(page.getByText(/Forgot it\?/)).toBeVisible();
  await expect(page.getByText('vitrine passwd')).toBeVisible();
});

test('downloads: the file limit says skipped posts do not count', async ({ page }) => {
  await page.goto('/downloads');
  await page.getByLabel('Max files').fill('1000');
  await expect(page.getByText('Stops once 1,000 files are in the library. Blacklisted and skipped posts do not count.')).toBeVisible();
});

const setSettings = (page: import('@playwright/test').Page, v: Record<string, unknown>) =>
  page.evaluate((v) => fetch('/api/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(v) }), v);

test('new files: first in the grid with NEW marks, whatever the order; the top bar pill marks them as seen', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-webkit', 'one run is enough: "seen" is a shared setting');
  // (Set before the app opens: on a fresh library its first start marks everything seen.)
  // "Seen" just before the 3 newest files (imported last when the test library was made).
  const { seenAt, newest, before } = await page.evaluate(async () => {
    const set = await (await fetch('/api/settings')).json();
    const ids: number[] = (await (await fetch('/api/search?q=&limit=26')).json()).ids;
    const files = (await (await fetch('/api/files?ids=' + ids.join(','))).json()).files;
    const times = files.map((f: { imported_at: number }) => f.imported_at).sort((a: number, b: number) => b - a);
    const cut = times.find((t: number) => times.filter((x: number) => x > t).length >= 3)!;
    return { seenAt: cut, newest: ids.slice(0, times.filter((x: number) => x > cut).length), before: set.seenAt };
  });
  await setSettings(page, { seenAt });
  await page.goto('/?sort=imported_asc'); // oldest first - the new ones still come first
  const pill = page.getByRole('button', { name: `Mark the ${newest.length} new files as seen` });
  await expect(pill).toHaveText(''); // (just the broom; how many in its label)
  await expect(page.locator('.tile .new-badge')).toHaveCount(newest.length);
  for (let i = 0; i < newest.length; i++) await expect(page.locator(`.tile[data-i="${i}"] .new-badge`)).toBeVisible(); // first, in the main grid
  expect(Number(await page.locator('.tile[data-i="0"]').getAttribute('data-id'))).toBe(newest[0]); // newest first
  await expect(page.getByRole('region', { name: 'New files' })).toHaveCount(0); // no section of their own
  // Open: the viewer marks it NEW too; past the new ones, no mark.
  await page.locator('.tile[data-i="0"]').click();
  await expect(page.locator('.viewer .top .chip.new')).toHaveText('NEW');
  for (let i = 0; i < newest.length; i++) await page.keyboard.press('ArrowRight');
  await expect(page.locator('.viewer .top .chip.new')).toHaveCount(0);
  await page.keyboard.press('Escape');
  await expect(page.locator('.viewer')).toHaveCount(0);
  // The pill: no pill, no marks; the setting moved on.
  await pill.click();
  await expect(pill).toHaveCount(0);
  await expect(page.locator('.tile .new-badge')).toHaveCount(0);
  await expect.poll(async () => (await page.evaluate(async () => (await fetch('/api/settings')).json())).seenAt).toBeGreaterThan(seenAt);
  await setSettings(page, { seenAt: before || Date.now() });
});

test('new files: the notification link shows the whole library', async ({ page }) => {
  // A "new files" notification opens /?new=1: the whole library, whatever search was open.
  await page.goto('/?q=blue_sky&new=1');
  await expect(page.locator('.bar .chip')).toHaveCount(0);
  await expect(page.getByText(/^26 files/)).toBeVisible();
  expect(new URL(page.url()).searchParams.get('new')).toBeNull();
});

test('settings: new files order is saved', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-webkit', 'one run is enough: shared settings');
  await page.goto('/settings/library');
  await page.getByRole('button', { name: 'New files order' }).click();
  await page.getByRole('menuitemradio', { name: /^Oldest first/ }).click();
  await expect.poll(async () => (await page.evaluate(async () => (await fetch('/api/settings')).json())).newOrder).toBe('imported_asc');
  await expect(page.getByRole('button', { name: 'New files order' })).toHaveText(/Oldest first/);
  await expect(page.getByRole('tab', { name: 'Automatically' })).toHaveCount(0); // new files are always added
  await setSettings(page, { newOrder: 'imported' });
});

test('settings: the default search is the library search bar - autocomplete and filters, no order or saved searches', async ({ page }) => {
  test.skip(test.info().project.name === 'desktop-chromium', 'phones and desktop WebKit are enough: shared settings');
  await page.goto('/settings/library');
  await setSettings(page, { defaultSearch: '' });
  await page.reload();
  const bar = page.getByRole('group', { name: 'Default search' });
  await expect(bar.getByRole('button', { name: /Saved searches/ })).toHaveCount(0);
  const input = bar.getByRole('combobox', { name: 'Default search' });
  await input.click();
  await input.pressSequentially('blue_s');
  await expect(page.getByRole('option', { name: /blue_sky/ })).toBeVisible();
  await input.pressSequentially('ky ');
  await expect(bar.locator('.chip')).toHaveText(['blue_sky']);

  await bar.getByRole('button', { name: 'Filters', exact: true }).click();
  const menu = page.getByRole('dialog', { name: 'Filters' });
  await expect(menu.getByRole('heading', { name: 'Filters' })).toBeVisible();
  await expect(menu.getByRole('button', { name: /^Sort by/ })).toHaveCount(0); // the order has its own setting
  await menu.getByRole('button', { name: /^Has audio/ }).click();
  await expect(bar.locator('.chip')).toHaveText(['blue_sky', 'has audio']);
  await expect
    .poll(async () => (await page.evaluate(async () => (await fetch('/api/settings')).json())).defaultSearch)
    .toBe('blue_sky system:has_audio');
  // A word left in the box counts when you leave it.
  await input.click();
  await page.keyboard.press('Escape'); // the filter menu, if still open
  await input.pressSequentially('-red');
  await page.getByText('When the app opens without one.').click();
  await expect(bar.locator('.chip')).toHaveText(['blue_sky', 'has audio', /−red/]);
  // Clean up once that is saved (a save still on its way would come back after).
  const server = () => page.evaluate(async () => (await (await fetch('/api/settings')).json()).defaultSearch);
  await expect.poll(server).toBe('blue_sky system:has_audio -red');
  await page.goto('/settings/account'); // away from the field: nothing more to save
  await setSettings(page, { defaultSearch: '' });
  await expect.poll(server).toBe('');
});

test('settings: the default sort is an action menu - a field, then its direction', async ({ page }) => {
  test.skip(test.info().project.name === 'desktop-chromium', 'phones (a sheet) and desktop (a menu) are enough');
  await page.goto('/settings/library');
  const field = page.getByRole('button', { name: 'Default sort' });
  await expect(field).toHaveText(/Newest uploaded/);
  await field.click();
  const menu = page.getByRole('menu', { name: 'Default sort' });
  await expect(menu.getByRole('menuitemradio', { name: /Uploaded/ })).toHaveAttribute('aria-checked', 'true');
  await expect(menu.getByRole('menuitemradio', { name: /Uploaded/ })).toContainText('Newest first');
  await menu.getByRole('menuitemradio', { name: /File size/ }).click();
  await page.getByRole('menu', { name: 'File size' }).getByRole('menuitemradio', { name: 'Smallest first' }).click();
  await expect(page.getByRole('menu')).toHaveCount(0);
  await expect(field).toHaveText(/File size - smallest first/);
  await expect.poll(async () => (await page.evaluate(async () => (await fetch('/api/settings')).json())).defaultSort).toBe('size_asc');
  await setSettings(page, { defaultSort: 'uploaded' });
});

test('details: a close button, Escape, a tap outside; phones: a sheet (the standard height) that drags down to close', async ({ page }) => {
  await page.goto('/');
  await page.locator('.tile').nth(3).click();
  const panel = page.getByRole('complementary', { name: 'Details' });
  const open = () => page.getByRole('button', { name: 'Details', exact: true }).click();
  await open();
  await expect(panel).toBeVisible();
  await expect(panel.getByText('Type')).toBeVisible();
  await expect(panel.getByText('Imported')).toBeVisible();
  // The close button.
  await panel.getByRole('button', { name: 'Close details' }).click();
  await expect(panel).toHaveCount(0);
  // Escape closes the details first, then the viewer.
  await open();
  await page.keyboard.press('Escape');
  await expect(panel).toHaveCount(0);
  await expect(page.locator('.viewer')).toBeVisible();
  // A tap on the picture (outside the details) closes them.
  await open();
  const vw = page.viewportSize()!;
  await page.mouse.click(vw.width / 4, 60 + 40);
  await expect(panel).toHaveCount(0);
  await expect(page.locator('.viewer')).toBeVisible();
  if (vw.width < 900) {
    await open();
    await page.waitForTimeout(400); // the sheet has slid up
    const box = (await panel.boundingBox())!;
    expect(box.height / vw.height).toBeCloseTo(0.7, 2); // the standard sheet height (--sheet-max-h)
    // Drag the header down.
    const head = (await panel.locator('.panel-head').boundingBox())!;
    const x = head.x + head.width / 3;
    await page.mouse.move(x, head.y + 20);
    await page.mouse.down();
    for (let i = 1; i <= 8; i++) await page.mouse.move(x, head.y + 20 + i * 25);
    await page.mouse.up();
    await expect(panel).toHaveCount(0);
  }
});

test('downloads on phones: rows keep their actions behind ⋯; numbers say what they are, on one line', async ({ page }) => {
  test.skip(test.info().project.name !== 'iphone', 'the phone layout');
  const now = Date.now();
  const s = { id: 1, site: 'safebooru', query: 'blue_sky', state: 'paused', max_posts: 0, site_total: 400, handled: 120, downloaded: 100, already: 20, deleted: 0, skipped: 0, blacklisted: 0, failed: 0, position: 0, created_at: now };
  const sub = { id: 7, site: 'safebooru', query: 'some_artist', interval: 43200, paused: false, initial_max: 100, checks: 3, downloaded: 50, last_new: 2, created_at: now, last_check_at: now - 3600_000, next_check_at: now + 3600_000 };
  await page.route('**/api/downloads', (r) => r.fulfill({ json: { sites: ['safebooru'], searches: [s], subscriptions: [sub], groups: [], refresh: [] } }));
  await page.goto('/downloads');
  const queue = page.locator('section.card', { has: page.getByRole('heading', { name: 'Queue' }) });
  await expect(queue.locator('.line-stats')).toContainText('120 of ~400 posts');
  await expect(queue.locator('.stat.good')).toHaveText('100 new');
  const counts = queue.locator('.counts');
  const one = await counts.evaluate((el) => {
    const kids = [...el.children].map((c) => c.getBoundingClientRect().top);
    return new Set(kids.map(Math.round)).size;
  });
  expect(one).toBe(1); // one line, never wrapping
  await page.getByRole('button', { name: 'More for the download blue_sky' }).click();
  await expect(page.getByRole('menuitem', { name: /^Resume/ })).toBeInViewport();
  await page.keyboard.press('Escape');
  // A subscription: its facts on one line; its actions behind ⋯ (no strip).
  await dlTab(page, 'Subscriptions');
  const row = page.locator('li[data-sub="7"]');
  await expect(row.locator('.facts')).toContainText('Every 12 hours');
  await expect(row.locator('.facts')).toContainText('50 files');
  await expect(row.locator('.acts')).toHaveCount(0);
  // The menu (a sheet on phones) is fully on screen, its schedule page too.
  await page.getByRole('button', { name: 'More for some_artist' }).click();
  await expect(page.getByRole('menuitem', { name: /^Unsubscribe/ })).toBeInViewport();
  await page.getByRole('menuitem', { name: /^Schedule/ }).click();
  await expect(page.getByRole('menuitemradio', { name: 'Every day' })).toBeInViewport();
  await expect(page.getByRole('menuitemradio', { name: 'Every 12 hours' })).toHaveAttribute('aria-checked', 'true');
});

test('downloads page: a stopped site at the top, a summary that links to its tabs, failed files by why (Dismiss), subscriptions that need a look, earlier finished ones folded', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-chromium', 'one run is enough: made-up lists');
  const now = Date.now();
  const H = 3600_000;
  const D = 24 * H;
  const search = (id: number, query: string, state: string, extra: object = {}) => ({
    id, site: 'safebooru', query, state, max_posts: 0, site_total: 0, handled: 0, downloaded: 0, already: 0, deleted: 0, skipped: 0, blacklisted: 0, position: id, failed: 0, created_at: now - H, ...extra,
  });
  const sub = (id: number, query: string, extra: object = {}) => ({
    id, site: 'safebooru', query, interval: 86400, paused: false, initial_max: 0, checks: 40, downloaded: 120, last_new: 2, created_at: now - 200 * D, last_check_at: now - 3 * H, next_check_at: now + 5 * H, depth: 0, last_found_at: now - 2 * D, ...extra,
  });
  let posts = [
    { site: 'safebooru', post: '101', query: 'blue_sky', error: 'HTTP 404 Not Found', at: now - H },
    { site: 'safebooru', post: '102', query: 'blue_sky', error: 'HTTP 404 Not Found', at: now - 2 * H },
    { site: 'safebooru', post: '103', query: 'sunset', error: 'read tcp: connection reset by peer', at: now - 3 * H },
  ];
  const dismissed: string[] = [];
  await page.route('**/api/failed-posts', (route) => route.fulfill({ json: { posts } }));
  await page.route('**/api/failed-posts/dismiss', (route) => {
    const gone: string[] = route.request().postDataJSON().posts.map((p: { post: string }) => p.post);
    dismissed.push(...gone);
    posts = posts.filter((p) => !gone.includes(p.post));
    return route.fulfill({ json: { dismissed: gone.length } });
  });
  await page.route('**/api/downloads', (route) =>
    route.fulfill({
      json: {
        sites: ['safebooru', 'example'],
        sites_paused: { example: { since: new Date(now - 60_000).toISOString(), until: new Date(now + 9 * 60_000).toISOString(), reason: 'connection refused', tries: 3 } },
        searches: [
          search(1, 'blue_sky', 'running', { handled: 10, site_total: 100, downloaded: 10 }),
          search(2, 'cloud', 'queued'),
          search(10, 'river', 'done', { handled: 6, site_total: 6, downloaded: 6, finished_at: now - 60_000 }),
          search(11, 'snow', 'done', { handled: 4, site_total: 4, downloaded: 4, finished_at: now - 3 * D }),
        ],
        subscriptions: [sub(1, 'blue_sky'), sub(2, 'old_tag', { last_found_at: now - 120 * D, last_new: 0 }), sub(3, 'broken_tag', { error: 'HTTP 403 Forbidden' })],
        groups: [],
        refresh: [{ site: 'safebooru', posts: 300, due: 12 }],
      },
    }),
  );
  await page.goto('/downloads');
  // The stopped site: a banner above everything, naming it, with Try now.
  const banner = page.locator('.page-inner > .paused-line');
  await expect(banner).toContainText("example isn't answering");
  await expect(banner.getByRole('button', { name: 'Try now' })).toBeVisible();
  // The summary: what's going on, zeros left out.
  const summary = page.locator('.summary');
  await expect(summary.getByRole('button', { name: '1 running' })).toBeVisible();
  await expect(summary.getByRole('button', { name: '1 waiting' })).toBeVisible();
  await expect(summary.getByRole('button', { name: '3 failed files' })).toBeVisible();
  await expect(summary.getByRole('button', { name: '2 subscriptions to look at' })).toBeVisible();
  await expect(summary.getByRole('button', { name: '12 posts to refresh' })).toBeVisible();
  // Failed files: to History, grouped by why.
  await summary.getByRole('button', { name: '3 failed files' }).click();
  await expect(page.getByRole('tab', { name: /^History/ })).toHaveAttribute('aria-selected', 'true');
  const failed = page.locator('section.card', { has: page.getByRole('heading', { name: 'Failed files' }) });
  const notFound = failed.locator('.reason-block', { hasText: 'Not on the site any more' });
  await expect(notFound.locator('.count')).toHaveText('2');
  await expect(failed.locator('.reason-block', { hasText: 'Connection problems' })).toBeVisible();
  // Dismiss all of one reason: gone from the list (and the summary).
  await notFound.getByRole('button', { name: 'Dismiss all' }).click();
  await expect(notFound).toHaveCount(0);
  expect(dismissed.sort()).toEqual(['101', '102']);
  await expect(summary.getByRole('button', { name: '1 failed file' })).toBeVisible();
  // Finished: today's shown, the earlier ones folded (remembered).
  const finished = page.locator('section.card', { has: page.getByRole('heading', { name: 'Finished' }) });
  await expect(finished.locator('.stack .item', { hasText: 'river' })).toBeVisible();
  await expect(finished.locator('.stack .item', { hasText: 'snow' })).toHaveCount(0);
  await finished.getByRole('button', { name: /^Earlier/ }).click();
  await expect(finished.locator('.stack .item', { hasText: 'snow' })).toBeVisible();
  // Subscriptions to look at: to Subscriptions, only those (a failing one, one quiet for months).
  await summary.getByRole('button', { name: '2 subscriptions to look at' }).click();
  await expect(page.getByRole('tab', { name: /^Subscriptions/ })).toHaveAttribute('aria-selected', 'true');
  const subs = page.locator('section.card', { has: page.getByRole('heading', { name: 'Subscriptions' }) });
  await expect(subs.getByRole('button', { name: /^Needs a look/ })).toHaveAttribute('aria-pressed', 'true');
  await expect(subs.locator('li .query')).toHaveText(['broken_tag', 'old_tag']);
  await expect(subs.locator('li', { hasText: 'old_tag' }).locator('.health.quiet')).toHaveAttribute('aria-label', 'Nothing new for months'); // (its health dot)
  await subs.getByRole('button', { name: /^Needs a look/ }).click();
  await expect(subs.locator('li .query')).toHaveCount(3);
  // The tab is remembered on this device.
  await page.reload();
  await expect(page.getByRole('tab', { name: /^Subscriptions/ })).toHaveAttribute('aria-selected', 'true');
});

test('new files: "Bulk downloads count as new" off leaves bulk files (imports, one-off downloads) unmarked', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-webkit', 'one run is enough: shared settings');
  // (Set before the app opens: on a fresh library its first start marks everything seen.)
  const before = await page.evaluate(async () => (await (await fetch('/api/settings')).json()).seenAt);
  await setSettings(page, { seenAt: 1 }); // everything is new...
  await page.goto('/');
  await expect(page.getByRole('button', { name: 'Mark the 26 new files as seen' })).toBeVisible();
  await expect(page.locator('.tile .new-badge').first()).toBeVisible();
  // ...but the test library was imported (bulk): with the setting off, nothing is.
  await page.goto('/settings/library');
  await page.getByText('Bulk downloads count as new').click();
  await expect.poll(async () => (await page.evaluate(async () => (await fetch('/api/settings')).json())).newFromBulk).toBe(false);
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  await expect(page.getByRole('button', { name: /^Mark the/ })).toHaveCount(0);
  await expect(page.locator('.tile .new-badge')).toHaveCount(0);
  expect((await page.evaluate(() => fetch('/api/new?q=&since=1&new_bulk=0').then((r) => r.json()))).new).toBe(0);
  await setSettings(page, { seenAt: before || Date.now(), newFromBulk: true });
});

test('search bar: chips stay on one line (scrolling sideways), suggestions are as wide as the box, clear sits in front', async ({ page }) => {
  await page.goto('/?q=' + encodeURIComponent('blue_sky landscape cloud -rain some_very_long_tag_name another_long_tag_name system:width>100'));
  const field = page.locator('.bar .field').first();
  const chips = field.locator('.chips');
  await expect(field.locator('.chip')).toHaveCount(7);
  const box = (await field.boundingBox())!;
  expect(box.height).toBeLessThan(64); // one line
  if ((page.viewportSize()?.width ?? 0) < 600) {
    expect(await chips.evaluate((el) => el.scrollWidth > el.clientWidth)).toBe(true); // the rest scrolls
  }
  // A swipe that starts on a chip's text scrolls the row too (not only one on its × or empty space).
  for (const sel of ['.chip-main', '.chip-x']) {
    const ta = await field.locator(sel).first().evaluate((el) => getComputedStyle(el).touchAction);
    expect(ta === 'auto' || ta === 'manipulation' || ta.includes('pan-x')).toBe(true);
  }
  // Clear sits where the magnifier was.
  await expect(field.locator('.lead.clear')).toBeVisible();
  // Suggestions under the whole box, as wide as it.
  const input = page.getByRole('combobox', { name: 'Search' });
  await input.click();
  await input.pressSequentially('re'); // (a tag not in the search: those already there aren't suggested)
  const list = page.locator('.list[data-floating]'); // (the search bar's suggestions: at the top of the page)
  await expect(list).toBeVisible();
  const lb = (await list.boundingBox())!;
  const fb = (await field.boundingBox())!;
  expect(Math.abs(lb.width - fb.width)).toBeLessThan(2);
  expect(Math.abs(lb.x - fb.x)).toBeLessThan(2);
  // Clearing empties it and the magnifier comes back.
  await field.locator('.lead.clear').click();
  await expect(field.locator('.chip')).toHaveCount(0);
  await expect(field.locator('.lead.clear')).toHaveCount(0);
});

test('search bar: suggestions show for the second tag too, with the chips overflowing', async ({ page }) => {
  await page.goto('/?q=' + encodeURIComponent('some_very_long_tag_name another_long_tag_name third_tag'));
  const input = page.getByRole('combobox', { name: 'Search' });
  const list = page.locator('.list[data-floating]'); // (the search bar's suggestions: at the top of the page)
  // Really seen: on top at its center, and nothing above it masks or clips it.
  const shown = () =>
    list.evaluate((el) => {
      const r = el.getBoundingClientRect();
      const top = document.elementFromPoint(r.left + r.width / 2, r.top + Math.min(20, r.height / 2));
      let masked = false;
      for (let a: Element | null = el; a; a = a.parentElement) {
        const cs = getComputedStyle(a);
        if ((cs.maskImage && cs.maskImage !== 'none') || (cs.webkitMaskImage && cs.webkitMaskImage !== 'none')) masked = true;
      }
      return !!top && el.contains(top) && !masked;
    });
  await input.click();
  await input.pressSequentially('blu');
  await expect(list).toBeVisible();
  expect(await shown()).toBe(true);
  await input.press('Enter'); // picks blue_sky
  await expect(page.locator('.bar .chip')).toHaveCount(4);
  await input.pressSequentially('land');
  await expect(list).toBeVisible();
  expect(await shown()).toBe(true);
  // The typing box stays small next to the chips (the first chips aren't pushed far away).
  const w = await page.locator('.bar .chips > .wrap').evaluate((el) => el.getBoundingClientRect().width);
  const fw = await page.locator('.bar .chips').evaluate((el) => el.clientWidth);
  expect(w).toBeLessThan(fw * 0.6);
});

test('new download: the search box suggests library tags; a pick goes in without its namespace', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-webkit', 'one run is enough: it tags a file on the server');
  await page.goto('/');
  const id: number = await page.evaluate(async () => {
    const id = (await (await fetch('/api/search?q=&limit=1')).json()).ids[0];
    await fetch('/api/tags', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ids: [id], add: ['creator:dl_suggest_artist'], remove: [] }) });
    return id;
  });
  await page.goto('/downloads');
  const box = page.getByRole('combobox', { name: 'Search on the site' });
  await box.click();
  await box.pressSequentially('-dl_sugg');
  const option = page.getByRole('option', { name: /creator:dl_suggest_artist/ });
  await expect(option).toBeVisible();
  await option.click();
  await expect(box).toHaveValue('-dl_suggest_artist '); // no "creator:", the minus kept
  // Enter (no suggestion picked) still starts the download - on an offline test site (never a real one:
  // the form starts on safebooru.org, and this used to download hundreds of its files).
  await page.getByRole('button', { name: 'Site', exact: true }).click();
  await page.getByRole('menuitemradio', { name: 'test-down.invalid' }).click();
  await box.fill('blue_sky');
  await page.waitForTimeout(150);
  await box.press('Escape');
  await box.press('Enter');
  // (it started: in the queue, or - the test site being down - already in History)
  await expect.poll(async () => (await (await page.request.get('/api/downloads')).json()).searches.some((x: { query: string }) => x.query === 'blue_sky')).toBe(true);
  await page.evaluate(async (id) => {
    await fetch('/api/tags', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ids: [id], add: [], remove: ['creator:dl_suggest_artist'] }) });
    const d = await (await fetch('/api/downloads')).json();
    for (const s of d.searches.filter((x: { query: string }) => x.query === 'blue_sky')) await fetch('/api/downloads/' + s.id + '/remove', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' });
  }, id);
});

test('search bar: a very long tag shows in full in its chip (the row scrolls)', async ({ page }) => {
  const tag = 'character:a_very_long_character_name_(with_a_long_series_name_in_brackets)';
  await page.goto('/?q=' + encodeURIComponent('blue_sky ' + tag));
  const chip = page.locator('.bar .chip').nth(1);
  await expect(chip).toBeVisible();
  await page.waitForTimeout(300); // (chips grow in)
  const [c, a] = [await chip.boundingBox(), await chip.locator('.atom').boundingBox()];
  expect(a!.x).toBeGreaterThanOrEqual(c!.x); // the text starts inside the chip
  expect(a!.x + a!.width).toBeLessThanOrEqual(c!.x + c!.width + 0.5);
  await expect(chip.getByRole('button', { name: 'Remove ' + tag + ' from the search' })).toBeVisible();
  expect(await chip.locator('.atom').evaluate((el) => el.scrollWidth <= el.clientWidth + 1)).toBe(true); // nothing cut off
  await expect(chip.locator('.atom')).toHaveText(tag.slice('character:'.length)); // the type shows as the color
  await expect(chip.locator('.atom')).toHaveCSS('color', 'rgb(166, 227, 161)'); // character: green
  expect(await chip.locator('.chip-main').getAttribute('title')).toContain(tag); // the whole tag on hover
});

test('backups: back up now, download, restore (staged until a restart) or cancel, restore from a file, delete, settings', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-webkit', 'one run is enough: backups are files on the server');
  await page.goto('/settings/backups');
  const card = page.locator('section.card', { has: page.getByRole('heading', { name: 'Backups' }) });
  await card.scrollIntoViewIfNeeded();
  const rows = card.getByRole('list', { name: 'Backups' }).locator('li');
  const before = await rows.count();
  await card.getByRole('button', { name: 'Back up now' }).click();
  await expect(page.getByText(/^Backed up \(/)).toBeVisible();
  await expect(rows).toHaveCount(before + 1);
  await expect(rows.first()).toContainText('Today at');
  await expect(rows.first()).toContainText('newest');
  // Download (from its ⋯ menu): a gzip file.
  const backupMenu = async (item: string) => {
    await rows.first().getByRole('button', { name: /^More for the backup/ }).click();
    await page.getByRole('menuitem', { name: new RegExp('^' + item) }).click();
  };
  const [dl] = await Promise.all([page.waitForEvent('download'), backupMenu('Download')]);
  const { readFileSync } = await import('node:fs');
  const bytes = { data: Array.from(readFileSync(await dl.path())) };
  expect(bytes.data.slice(0, 2)).toEqual([0x1f, 0x8b]);
  // Restore: confirm, then it waits for a restart; cancel it.
  await backupMenu('Restore');
  // It can't be undone: the focus starts on Cancel (a stray Enter does nothing).
  await expect(page.getByRole('alertdialog').getByRole('button', { name: 'Cancel' })).toBeFocused();
  await confirmIn(page, 'Restore');
  await expect(card.getByText('A restore is ready.')).toBeVisible();
  await expect(card.getByRole('button', { name: 'Restart now' })).toBeVisible();
  await card.getByRole('button', { name: 'Cancel restore' }).click();
  await expect(card.getByText('A restore is ready.')).toHaveCount(0);
  // From a file: something that isn't a backup is refused; the downloaded one is staged.
  const input = card.locator('input[type=file]');
  await input.setInputFiles({ name: 'nope.db.gz', mimeType: 'application/gzip', buffer: Buffer.from('not a backup') });
  await confirmIn(page, 'Restore');
  await expect(page.getByText(/not a database|not a Vitrine library/)).toBeVisible();
  await input.setInputFiles({ name: 'vitrine.db.gz', mimeType: 'application/gzip', buffer: Buffer.from(bytes.data) });
  await confirmIn(page, 'Restore');
  await expect(card.getByText('A restore is ready.')).toBeVisible();
  await card.getByRole('button', { name: 'Cancel restore' }).click();
  await expect(card.getByText('A restore is ready.')).toHaveCount(0);
  // Settings: kept on the server.
  await card.getByRole('button', { name: 'How many backups to keep' }).click();
  await page.getByRole('option', { name: 'Keep 14' }).click();
  await expect.poll(async () => (await page.evaluate(async () => (await fetch('/api/settings')).json())).backupKeep).toBe(14);
  await card.getByText('Back up every day').click();
  await expect.poll(async () => (await page.evaluate(async () => (await fetch('/api/settings')).json())).backupAuto).toBe(false);
  await page.evaluate(() => fetch('/api/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ backupAuto: true, backupKeep: 7 }) }));
  // Delete.
  await backupMenu('Delete');
  await confirmIn(page, 'Delete');
  await expect(rows).toHaveCount(before);
});

test('selection: the tag editor shows how many files have each tag; + gives one to all, × removes one from all', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-webkit', 'one run is enough: it tags files on the server');
  await page.goto('/');
  const ids: number[] = await page.evaluate(async () => {
    const ids = (await (await fetch('/api/search?q=&limit=2')).json()).ids;
    const tag = (ids: number[], add: string[]) =>
      fetch('/api/tags', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ids, add, remove: [] }) });
    await tag(ids, ['sel_both']);
    await tag([ids[0]], ['sel_one']);
    return ids;
  });
  await page.goto('/');
  await page.locator('.tile[data-i="0"]').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Select' }).click();
  await page.locator('.tile[data-i="1"]').click();
  await expect(page.getByText('2 selected')).toBeVisible();
  await page.getByRole('toolbar').getByRole('button', { name: 'Tags', exact: true }).click();
  const ed = page.getByRole('dialog', { name: 'Tags of the selected files' });
  await expect(ed.getByRole('heading', { name: 'Tags of 2 files' })).toBeVisible();
  // sel_one is on 1 of 2: give it to both. sel_both: remove from both.
  await ed.getByRole('button', { name: 'Add sel_one to all 2 files' }).click();
  await ed.getByRole('button', { name: 'Remove sel_both from all' }).click();
  await expect(ed.getByText('1 to add, 1 to remove')).toBeVisible();
  await ed.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByText(/Saved for 2 files/)).toBeVisible();
  const after = await page.evaluate(async (ids) => (await (await fetch('/api/tags/of', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ids }) })).json()).tags, ids);
  expect(after.find((t: { tag: string }) => t.tag === 'sel_one')?.count).toBe(2);
  expect(after.find((t: { tag: string }) => t.tag === 'sel_both')).toBeUndefined();
  await page.evaluate(async (ids) => {
    await fetch('/api/tags', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ids, add: [], remove: ['sel_one'] }) });
  }, ids);
});

test('sort menu: more orders - file size both ways, number of tags, random (the same shuffle on a refresh)', async ({ page }) => {
  await page.goto('/');
  // Nothing pinned as new (an earlier test may have moved "seen up to" on the server).
  await setSettings(page, { seenAt: Date.now() });
  await page.reload();
  await expect(page.getByText(/^26 files/)).toBeVisible();
  const sizes = () =>
    page.evaluate(async () => {
      const url = new URL(location.href);
      const sort = url.searchParams.get('sort') ?? '';
      const r = await (await fetch('/api/search?limit=26&q=' + encodeURIComponent(sort ? 'sort:' + sort : ''))).json();
      const files = (await (await fetch('/api/files?ids=' + r.ids.join(','))).json()).files as { id: number; size: number }[];
      const by = new Map(files.map((f) => [f.id, f.size]));
      return (r.ids as number[]).map((id) => by.get(id)!);
    });
  await page.getByRole('button', { name: 'Filters' }).click();
  await page.getByRole('button', { name: /^Sort by/ }).click();
  await page.getByRole('button', { name: /^File size/ }).click();
  await page.getByRole('button', { name: /^Largest first/ }).click();
  await expect(page).toHaveURL(/sort=size/);
  await expect(page.getByRole('button', { name: /^Sort by/ })).toContainText('Largest first');
  const desc = await sizes();
  expect(desc).toEqual([...desc].sort((a, b) => b - a));
  // The grid shows that order: the first tile is the largest file.
  const first = Number(await page.locator('.tile[data-i="0"]').getAttribute('data-id'));
  const largest = await page.evaluate(async () => (await (await fetch('/api/search?limit=1&q=sort:size')).json()).ids[0]);
  expect(first).toBe(largest);
  await page.getByRole('button', { name: /^Sort by/ }).click();
  await page.getByRole('button', { name: /^File size/ }).click();
  await page.getByRole('button', { name: /^Smallest first/ }).click();
  await expect(page).toHaveURL(/sort=size_asc/);
  const asc = await sizes();
  expect(asc).toEqual([...asc].sort((a, b) => a - b));
  // Number of tags is offered too.
  await page.getByRole('button', { name: /^Sort by/ }).click();
  await expect(page.getByRole('button', { name: /^Number of tags/ })).toBeVisible();
  await page.getByRole('button', { name: /^Random/ }).click(); // no directions: applied at once
  await expect(page).toHaveURL(/sort=random/);
  await expect(page.getByRole('button', { name: /^Sort by/ })).toHaveAttribute('data-dir', '');
  await page.keyboard.press('Escape');
  const order = () => page.locator('.tile').evaluateAll((els) => els.slice(0, 12).map((el) => el.getAttribute('data-id')));
  await expect.poll(async () => (await order()).filter(Boolean).length).toBe(12);
  const before = await order();
  await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange'))); // a check for new files, no change
  await page.waitForTimeout(300);
  expect(await order()).toEqual(before);
});

test('settings: sections - a list on phones (each opens with a back link), side by side on wide screens; listed in the menu', async ({ page }) => {
  await page.goto('/settings');
  const nav = page.getByRole('navigation', { name: 'Settings sections' });
  await expect(nav.getByRole('button')).toHaveCount(10);
  const wide = (page.viewportSize()?.width ?? 0) >= 900;
  if (wide) {
    // The first section shows beside the list.
    await expect(page.getByRole('heading', { name: 'Media', exact: true })).toBeVisible();
    await nav.getByRole('button', { name: /^Backups/ }).click();
    await expect(page).toHaveURL(/\/settings\/backups$/);
    await expect(nav).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Backups' })).toBeVisible();
  } else {
    await expect(page.getByRole('heading', { name: 'Media', exact: true })).toHaveCount(0); // just the list
    await nav.getByRole('button', { name: /^Download rules/ }).click();
    await expect(page).toHaveURL(/\/settings\/rules$/);
    await expect(nav).toHaveCount(0);
    await expect(page.getByLabel('Skip videos larger than (MB)')).toBeVisible();
    await page.getByRole('button', { name: 'Settings', exact: true }).click(); // the back link
    await expect(nav).toBeVisible();
  }
  // The menu lists the sections under Settings.
  await page.getByRole('button', { name: 'Menu' }).click();
  await unfold(page, 'settings');
  await page.getByRole('navigation').getByRole('button', { name: 'Notifications', exact: true }).click();
  await expect(page).toHaveURL(/\/settings\/notifications$/);
  await expect(page.getByRole('heading', { name: 'Notifications' })).toBeVisible();
});

test('viewer: the next still image is decoded in the background first (its slide shows the thumbnail until then)', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-chromium', 'one run is enough');
  await page.goto('/?q=system:type%3Dimage');
  await expect(page.locator('.tile[data-i="1"]')).toBeVisible();
  // Hold back the files (originals) for a moment: the next slide must wait for its decode.
  let release!: () => void;
  const gate = new Promise<void>((r) => (release = r));
  await page.route('**/files/**', async (route) => {
    await gate;
    await route.continue();
  });
  await page.locator('.tile[data-i="0"]').click();
  const nextImg = page.locator('.viewer .slide[data-slot="2"] img');
  await expect(nextImg).toHaveCount(1);
  expect(await nextImg.getAttribute('src')).toMatch(/^data:/); // not until decoded (a transparent pixel): the thumbnail shows
  await expect(nextImg).toHaveCSS('background-image', /thumbs/);
  release();
  await expect(nextImg).toHaveAttribute('src', /\/files\//); // decoded: the full picture
  await page.unroute('**/files/**');
});

test('startup: a repeat visit shows the app at once, even when the server is slow to answer', async ({ page }) => {
  await page.goto('/'); // first visit: the session and settings are kept on this device
  await expect(page.getByText(/^26 files/)).toBeVisible();
  const slow = async (route: import('@playwright/test').Route) => {
    await new Promise((r) => setTimeout(r, 2500));
    await route.continue();
  };
  await page.route('**/api/session', slow);
  await page.route('**/api/settings', slow);
  const t0 = Date.now();
  await page.reload();
  await expect(page.locator('.topbar')).toBeVisible({ timeout: 1500 }); // not waiting on the server
  expect(Date.now() - t0).toBeLessThan(2500);
  await expect(page.locator('#splash')).toHaveCount(0);
  await expect(page.getByText(/^26 files/)).toBeVisible({ timeout: 8000 }); // the library once the settings are read
  await page.unroute('**/api/session');
  await page.unroute('**/api/settings');
});

test('viewer: a big image shows at the screen\'s size; zooming in switches to the full size', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-chromium', 'one run is enough');
  await page.goto('/?q=' + encodeURIComponent('system:width>3000'));
  await page.locator('.tile').first().click();
  const img = page.locator('.viewer .slide[data-slot="1"] img');
  await expect(img).toHaveAttribute('src', /[?&]size=\d+/); // a screen-sized copy
  const box = (await page.locator('.stage').boundingBox())!;
  await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2); // a click zooms in (to 100 %)
  await expect(img).not.toHaveAttribute('src', /[?&]size=/); // the original, once decoded
});

// The test pictures are color-shifted copies of one pattern: alike pairs (as color variants).
test('duplicates: compare a pair (flip, side by side), keep one and undo, keep both with keys and undo', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('vitrine.dupeLevel', '6')); // (Very similar: the test library has no exact copies)
  const list = () => page.evaluate(() => fetch('/api/dupes?max=6&limit=100').then((r) => r.json()));
  await expect.poll(async () => (await list()).total).toBeGreaterThan(1);
  const before = await list();
  const first = before.pairs[0];
  await page.goto('/duplicates');
  await expect(page.getByRole('heading', { name: 'Duplicates' })).toBeVisible();
  await expect(page.getByText(`${before.total} to check`)).toBeVisible();
  await expect(page.getByText(/The colors differ/)).toBeVisible();

  // Flip: one picture at a time, a tap switches.
  await page.getByRole('tab', { name: 'Flip' }).click();
  const visible = page.locator('.layer:not(.hidden) img');
  await expect(visible).toHaveAttribute('alt', 'File A');
  await page.getByRole('button', { name: /Switch between A and B/ }).click();
  await expect(visible).toHaveAttribute('alt', 'File B');
  // Side by side: both.
  await page.getByRole('tab', { name: 'Side by side' }).click();
  await expect(page.locator('.stage.side .pane img')).toHaveCount(2);

  // Keep A: B is deleted (with its other pairs); Undo brings it back.
  await expect(page.getByRole('button', { name: 'Undo', exact: true })).toBeDisabled(); // nothing chosen yet
  await page.getByRole('button', { name: 'Keep A' }).click();
  await expect(page.locator('.toast')).toHaveCount(0); // no notice: the Undo button holds the last choice
  await expect.poll(async () => (await list()).pairs.some((p: { a: { id: number }; b: { id: number } }) => p.a.id === first.a.id && p.b.id === first.b.id)).toBe(false);
  await page.getByRole('button', { name: 'Undo: kept A, deleted B' }).click();
  await expect.poll(async () => (await list()).total).toBe(before.total);
  await expect(page.getByText(`${before.total} to check`)).toBeVisible();
  await expect(page.getByRole('button', { name: 'Undo', exact: true })).toBeDisabled();

  // Keys: K keeps both, Z takes it back.
  await page.keyboard.press('k');
  await expect(page.getByRole('button', { name: 'Undo: kept both' })).toBeEnabled();
  await expect(page.getByText(`${before.total - 1} to check`)).toBeVisible();
  await page.keyboard.press('z');
  await expect.poll(async () => (await list()).total).toBe(before.total);

  // Skip: the next pair, the count unchanged on the server.
  await page.getByRole('button', { name: /^Skip/ }).click();
  await expect(page.getByText(`${before.total - 1} to check`)).toBeVisible();
  expect((await list()).total).toBe(before.total);
});

test('duplicates: the menu shows how many wait', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('vitrine.dupeLevel', '6')); // (Very similar: the test library has no exact copies)
  await page.goto('/');
  await page.getByRole('button', { name: 'Menu' }).click();
  await expect(page.locator('#nav-drawer').getByRole('button', { name: /Duplicates \d+/ })).toBeVisible();
  await page.locator('#nav-drawer').getByRole('button', { name: /Duplicates/ }).click();
  await expect(page).toHaveURL(/\/duplicates$/);
});

test('duplicates: "Same picture" shows only pairs whose pictures match exactly', async ({ page }) => {
  const exact = await page.evaluate(() => fetch('/api/dupes?max=0&limit=100').then((r) => r.json()));
  expect(exact.pairs.every((p: { distance: number }) => p.distance === 0)).toBe(true);
  await page.goto('/duplicates');
  await expect(page.getByRole('button', { name: 'How alike' })).toContainText('Same picture'); // the default
  await page.getByRole('button', { name: 'How alike' }).click();
  await page.getByRole('option', { name: /Same picture/ }).click();
  if (exact.total > 0) await expect(page.getByText(`${exact.total} to check`)).toBeVisible();
  else await expect(page.getByRole('heading', { name: 'No duplicates to check' })).toBeVisible();
  // Back to the usual level for the other tests.
  await page.getByRole('button', { name: 'How alike' }).click();
  await page.getByRole('option', { name: /Very similar/ }).click();
});

test('video scrubbing with a mouse never starts a browser drag, even with text selected', async ({ page }, info) => {
  test.skip(info.project.name === 'iphone', 'mouse only');
  await page.goto('/?q=system:type%3Dvideo');
  await page.locator('.tile').nth(0).click();
  const track = page.getByRole('slider', { name: 'Position' });
  await expect(track).toBeVisible();
  const video = page.locator('.viewer .slide[data-slot="1"] video');
  await expect.poll(() => video.evaluate((v: HTMLVideoElement) => v.duration || 0)).toBeGreaterThan(1);
  await page.evaluate(() => {
    (window as unknown as { drags: number }).drags = 0;
    document.addEventListener('dragstart', () => (window as unknown as { drags: number }).drags++, true);
    // Text selected around the player (e.g. by a double-click), which the browser would drag.
    const r = document.createRange();
    r.selectNodeContents(document.querySelector('.viewer')!);
    getSelection()!.removeAllRanges();
    getSelection()!.addRange(r);
  });
  const box = (await track.boundingBox())!;
  const y = box.y + box.height / 2;
  await page.mouse.move(box.x + box.width * 0.2, y);
  await page.mouse.down();
  for (let x = 0.2; x <= 0.7; x += 0.05) await page.mouse.move(box.x + box.width * x, y);
  await page.mouse.up();
  expect(await page.evaluate(() => (window as unknown as { drags: number }).drags)).toBe(0);
  expect(await page.evaluate(() => getSelection()!.toString())).toBe('');
  const d = await video.evaluate((v: HTMLVideoElement) => v.duration);
  await expect.poll(() => video.evaluate((v: HTMLVideoElement) => v.currentTime)).toBeGreaterThan(d * 0.5);
  await expect(track).toBeFocused();
});

test('settings: FlareSolverr address - saved and checked, a clear error when it does not answer, empty turns it off', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-webkit', 'one run is enough: the address is on the server');
  await page.goto('/settings/sites');
  const box = page.getByRole('textbox', { name: 'FlareSolverr address' });
  await expect(page.getByText(/Not set up/)).toBeVisible();
  await box.fill('http://127.0.0.1:1');
  await page.getByRole('button', { name: 'Save', exact: true }).last().click();
  await expect(page.getByRole('status').filter({ hasText: /didn't answer/ })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Test', exact: true })).toBeVisible(); // saved: now a test button
  // A FlareSolverr that answers (stood in for here).
  await page.route('**/api/flaresolverr', (r) =>
    r.request().method() === 'POST' ? r.fulfill({ json: { url: 'http://127.0.0.1:1', version: '3.3.21' } }) : r.continue(),
  );
  await page.getByRole('button', { name: 'Test', exact: true }).click();
  await expect(page.getByText('Connected to FlareSolverr 3.3.21')).toBeVisible();
  await page.unroute('**/api/flaresolverr');
  // After a reload it's still set; emptied, it's off.
  await page.reload();
  await expect(box).toHaveValue('http://127.0.0.1:1');
  await box.fill('');
  await page.getByRole('button', { name: 'Save', exact: true }).last().click();
  await expect(page.getByText(/Not set up/)).toBeVisible();
  expect((await page.evaluate(() => fetch('/api/flaresolverr').then((r) => r.json()))).url).toBe('');
});

test('details: many sources - their own section, one shown, the rest folded (grouped by site)', async ({ page }) => {
  await page.route('**/api/files/*/tags', async (route) => {
    const res = await route.fetch();
    const body = await res.json();
    body.urls = [
      { url: 'https://safebooru.org/index.php?page=post&s=view&id=1', kind: 'post', site: 'safebooru' },
      { url: 'https://a.example.test/video/1/some-title', kind: 'source', site: 'safebooru' },
      { url: 'https://b.example.test/watch/2', kind: 'source', site: 'safebooru' },
      { url: 'https://a.example.test/video/3/another-title', kind: 'source', site: 'safebooru' },
      { url: 'https://c.example.test/v/4', kind: 'source', site: 'safebooru' },
    ];
    await route.fulfill({ response: res, json: body });
  });
  await page.goto('/');
  await page.locator('.tile').nth(3).click();
  await page.getByRole('button', { name: 'Details' }).click();
  await expect(page.getByRole('region', { name: "Where it's from" }).getByRole('link', { name: 'Open the source' })).toHaveCount(0);
  const srcs = page.getByRole('region', { name: 'Sources' });
  // Stacked: the first in front (the others behind it, hidden from the reader), "+3" unfolds them.
  const front = srcs.locator('.item:not(.behind) .row');
  await expect(front).toHaveCount(1);
  await expect(front.locator('.q')).toHaveText('a.example.test');
  await expect(front.locator('.path')).toHaveText('/video/1/some-title');
  if (process.env.SHOTS) await srcs.screenshot({ path: process.env.SHOTS + '/sources-folded-' + test.info().project.name + '.png' });
  await srcs.getByRole('button', { name: 'Show all 4 (sources)' }).click();
  // The same site's links together, in the order they came.
  await expect(srcs.locator('.item:not(.behind) .row .q')).toHaveText(['a.example.test', 'a.example.test', 'b.example.test', 'c.example.test']);
  await page.waitForTimeout(500);
  if (process.env.SHOTS) await srcs.screenshot({ path: process.env.SHOTS + '/sources-' + test.info().project.name + '.png' });
  await page.getByRole('button', { name: 'Show less' }).click();
  await expect(srcs.locator('.item:not(.behind) .row')).toHaveCount(1);
  await page.unrouteAll({ behavior: 'ignoreErrors' });
});

test('details: the post page and source links have copy buttons', async ({ page, context }, info) => {
  test.skip(info.project.name !== 'desktop-chromium', 'clipboard reading: Chromium');
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  await page.route('**/api/files/*/tags', async (route) => {
    const res = await route.fetch();
    const body = await res.json();
    body.urls = [
      { url: 'https://safebooru.org/index.php?page=post&s=view&id=1', kind: 'post', site: 'safebooru' },
      { url: 'https://example.test/some_artist/1', kind: 'source', site: 'safebooru' },
    ];
    await route.fulfill({ response: res, json: body });
  });
  // Site icons (the server fetches them from the sites: answered here instead).
  const favicon = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==', 'base64');
  await page.route('**/api/favicon?*', (route) =>
    new URL(route.request().url()).searchParams.get('host') === 'safebooru.org'
      ? route.fulfill({ contentType: 'image/png', body: favicon })
      : route.fulfill({ status: 404, json: { error: 'no icon' } }),
  );
  await page.goto('/');
  await page.locator('.tile').nth(3).click();
  await page.getByRole('button', { name: 'Details' }).click();
  const found = page.getByRole('region', { name: "Where it's from" });
  const srcs = page.getByRole('region', { name: 'Sources' });
  // The post page's row shows its site's icon; the source's site (its own section) has none: the link icon.
  await expect(found.locator('li.row').nth(0).locator('.ic.site img.shown')).toBeVisible();
  await expect(srcs.locator('.row').nth(0).locator('.ic svg')).toBeVisible();
  await expect(srcs.locator('.row').nth(0).locator('.ic img')).toHaveCount(0);
  await expect(srcs.locator('.unfold')).toHaveCount(0); // (one source: nothing stacked)
  if (process.env.SHOT) await found.screenshot({ path: process.env.SHOT.replace('.png', '-icons.png') });
  await found.getByRole('button', { name: 'Copy the post page link' }).click();
  await expect(page.getByText('Copied the post page link')).toBeVisible();
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe('https://safebooru.org/index.php?page=post&s=view&id=1');
  await srcs.getByRole('button', { name: 'Copy the source link' }).click();
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe('https://example.test/some_artist/1');
  // The links themselves still open the pages.
  await expect(found.getByRole('link', { name: 'Open the post page' })).toHaveAttribute('href', /safebooru\.org/);
  // Open looks and reacts like Copy: the same size, the same hover background.
  const open = srcs.getByRole('link', { name: 'Open the source' });
  const copy = srcs.getByRole('button', { name: 'Copy the source link' });
  const look = (l: typeof open) => l.evaluate((el) => {
    const s = getComputedStyle(el);
    return [el.getBoundingClientRect().width, el.getBoundingClientRect().height, s.borderRadius, s.color, s.backgroundColor].join(' ');
  });
  await open.hover();
  await page.waitForTimeout(250); // the hover fade
  const openHovered = await look(open);
  if (process.env.SHOT) await srcs.screenshot({ path: process.env.SHOT });
  await copy.hover();
  await page.waitForTimeout(250);
  expect(openHovered).toBe(await look(copy));
});

test('viewer: swiping down with the details open closes the viewer on wide screens (the details sheet first on phones)', async ({ page }, info) => {
  await page.goto('/');
  await page.locator('.tile').nth(3).click();
  await expect(page.getByText('4 / 26')).toBeVisible();
  await page.getByRole('button', { name: 'Details' }).click();
  await expect(page.getByRole('complementary', { name: 'Details' })).toBeVisible();
  await page.waitForTimeout(400);
  const box = (await page.locator('.stage').boundingBox())!;
  const x = box.x + box.width / 2;
  const y0 = box.y + box.height * 0.3; // on the picture (not the control bar along the top)
  await page.mouse.move(x, y0);
  await page.mouse.down();
  await page.mouse.move(x, y0 + 100, { steps: 5 });
  await page.mouse.move(x, y0 + 200, { steps: 5 });
  await page.mouse.up();
  if (isPhone(page)) {
    await expect(page.getByRole('complementary', { name: 'Details' })).toHaveCount(0); // the sheet went
    await expect(page.locator('.viewer')).toHaveCount(1);
  } else {
    await expect(page.locator('.viewer')).toHaveCount(0); // closed in one swipe
  }
  void info;
});

test('settings: top spacing is only offered on phones', async ({ page }, info) => {
  test.skip(info.project.name === 'iphone', 'desktop only');
  await page.goto('/settings/viewing');
  await expect(page.getByRole('checkbox', { name: /Start videos muted/ })).toBeVisible();
  await expect(page.getByRole('slider', { name: 'Top spacing' })).toHaveCount(0);
});

test('a press outside the filter menu only closes it (the tile under it does not open)', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  await page.getByRole('button', { name: 'Filters', exact: true }).click();
  await expect(page.getByRole('dialog', { name: 'Filters' })).toBeVisible();
  if (isPhone(page)) {
    // A sheet: the press lands on the dimmed strip above it (over the top bar - which must not react).
    await page.waitForTimeout(300);
    await page.mouse.click(page.viewportSize()!.width / 2, 10);
    await expect(page.getByRole('navigation', { name: 'Main' })).toHaveCount(0);
  } else {
    const tile = page.locator('.tile').last();
    await tile.scrollIntoViewIfNeeded();
    const box = (await tile.boundingBox())!;
    await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
  }
  await expect(page.getByRole('dialog', { name: 'Filters' })).toHaveCount(0);
  await page.waitForTimeout(500);
  await expect(page.locator('.viewer')).toHaveCount(0); // nothing opened
});

test('settings: a site can be switched off (not offered for downloads, its queue waits) and on again; the built-in one can be removed', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-webkit', 'one run is enough: sites are on the server');
  await page.goto('/settings/sites');
  const site = page.locator('article.site', { has: page.getByRole('heading', { name: 'safebooru.org' }) });
  await rowMenu(page, 'safebooru.org', 'Switch off');
  await expect(site.locator('.status')).toHaveText('Off');
  await expect(page.getByText(/^safebooru\.org is off/)).toBeVisible();
  await page.goto('/downloads');
  await page.getByRole('button', { name: 'Site', exact: true }).click();
  await expect(page.getByRole('menuitemradio', { name: 'safebooru.org' })).toHaveCount(0);
  await page.keyboard.press('Escape');
  const refused = await page.evaluate(async () => (await fetch('/api/downloads', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ site: 'safebooru', query: 'blue_sky', max_posts: 1 }) })).status);
  expect(refused).not.toBe(200);

  await page.goto('/settings/sites');
  await rowMenu(page, 'safebooru.org', 'Switch on');
  await expect(site.locator('.status')).not.toHaveText('Off');
  await page.goto('/downloads');
  await page.getByRole('button', { name: 'Site', exact: true }).click();
  await expect(page.getByRole('menuitemradio', { name: 'safebooru.org' })).toBeVisible();
  await page.keyboard.press('Escape');

  // The built-in site can be removed too (asked first; not done here).
  await page.goto('/settings/sites');
  await rowMenu(page, 'safebooru.org', 'Remove site');
  await expect(page.getByRole('alertdialog', { name: 'Remove safebooru.org?' })).toContainText('Add it again from Add a site');
  await page.getByRole('alertdialog').getByRole('button', { name: 'Cancel' }).click();
  await expect(page.getByRole('alertdialog')).toHaveCount(0);
});

test('downloads: a whole site folds (its subscriptions) - folded to start with, opened ones remembered on the device', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-webkit', 'one run is enough: subscriptions are on the server');
  await page.goto('/downloads');
  const ids: number[] = await page.evaluate(async () => {
    const out: number[] = [];
    for (const [site, q] of [['test-down', 'list_fold_a'], ['test-slow', 'list_fold_b']]) {
      const r = await fetch('/api/subscriptions', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ site, query: q, interval: 604800, initial_max: 0 }) });
      out.push((await r.json()).id);
    }
    return out;
  });
  await page.reload();
  await dlTab(page, 'Subscriptions');
  const card = page.locator('section.card', { has: page.getByRole('heading', { name: 'Subscriptions' }) });
  // Folded to start with (like the groups).
  await expect(card.getByRole('button', { name: 'Unfold test-down.invalid' })).toHaveAttribute('aria-expanded', 'false');
  await expect(card.getByText('list_fold_a')).toHaveCount(0);
  await card.getByRole('button', { name: 'Unfold test-down.invalid' }).click();
  await expect(card.getByText('list_fold_a')).toBeVisible();
  await expect(card.getByText('list_fold_b')).toHaveCount(0); // the other site stays folded
  // Opened: remembered.
  await page.reload();
  await expect(card.getByText('list_fold_a')).toBeVisible();
  await card.getByRole('button', { name: 'Fold test-down.invalid' }).click();
  await expect(card.getByText('list_fold_a')).toHaveCount(0);
  await page.evaluate(async (ids) => {
    for (const id of ids) await fetch(`/api/subscriptions/${id}/remove`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' });
    localStorage.removeItem('vitrine.openSubSites');
  }, ids);
});

test('details: the file ID shows, the same as in the address, with a copy button', async ({ page }) => {
  await page.goto('/');
  const tile = page.locator('.tile').nth(2);
  await expect(tile).toHaveAttribute('data-id', /^\d+$/);
  const id = await tile.getAttribute('data-id');
  await tile.click();
  await page.getByRole('button', { name: 'Details', exact: true }).click();
  const panel = page.getByRole('complementary', { name: 'Details' });
  await expect(panel.locator('dd.id')).toHaveText(id!);
  await expect.poll(() => new URL(page.url()).searchParams.get('f')).toBe(id);
  await expect(panel.getByRole('button', { name: 'Copy the file ID' })).toBeVisible();
});

// Many sites on several engines (faked): the picker lists the engines, each a page of its sites.
async function manySites(page: import('@playwright/test').Page) {
  const extra = [
    ['shimmie_a', 'shimmie-a.invalid', 'shimmie2'],
    ['shimmie_b', 'shimmie-b.invalid', 'shimmie2'],
    ['shimmie_c', 'shimmie-c.invalid', 'shimmie2'],
    ['masto_a', 'masto-a.invalid', 'mastodon'],
    ['html_a', 'html-a.invalid', 'gelbooru-v02-html'],
  ];
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
}

test('new download: with many sites on several engines, the site menu groups them by engine', async ({ page }) => {
  await manySites(page);
  await page.goto('/downloads');
  const field = page.getByRole('button', { name: 'Site', exact: true });
  await field.click();
  const menu = page.getByRole('menu', { name: 'Site' });
  await expect(menu.getByRole('menuitemradio', { name: /^Shimmie2/ })).toContainText('3 sites');
  await expect(menu.getByRole('menuitemradio', { name: /^masto-a\.invalid/ })).toContainText('Mastodon'); // alone: listed as itself
  await expect(menu.getByRole('menuitemradio', { name: /^Gelbooru 0\.2\b/ }).first()).toHaveAttribute('aria-checked', 'true'); // the current site's engine
  await menu.getByRole('menuitemradio', { name: /^Shimmie2/ }).click();
  await page.getByRole('menu', { name: 'Shimmie2' }).getByRole('menuitemradio', { name: 'shimmie-b.invalid' }).click();
  await expect(field).toHaveText(/shimmie-b\.invalid/);
  await field.click(); // at once, while the last menu is still fading out
  await expect(page.getByRole('menu', { name: 'Site' }).getByRole('menuitemradio', { name: /^Shimmie2/ })).toContainText('shimmie-b.invalid');
  await page.keyboard.press('Escape');
});

test('duplicates: two videos play in step, one bar scrubs both, flipping keeps the moment', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('vitrine.dupeLevel', '6')); // (Very similar: the test library has no exact copies)
  await page.goto('/');
  const vids = await page.evaluate(async () => {
    const ids = (await (await fetch('/api/search?limit=26&q=')).json()).ids as number[];
    const files = (await (await fetch('/api/files?ids=' + ids.join(','))).json()).files as { mime: string }[];
    return files.filter((f) => f.mime.startsWith('video/')).map((f) => ({ ...f, tags: 0, sites: null }));
  });
  expect(vids.length).toBeGreaterThanOrEqual(2);
  await page.route('**/api/dupes?*', (route) =>
    route.fulfill({ json: { total: 1, pairs: [{ a: vids[0], b: vids[1], distance: 3 }], progress: { checked: 1, total: 1 } } }),
  );
  await page.goto('/duplicates');
  await page.getByRole('tab', { name: 'Side by side' }).click();
  const times = () => page.locator('.stage video').evaluateAll((vs) => vs.map((v) => (v as HTMLVideoElement).currentTime));
  const bar = page.getByRole('slider', { name: 'Position (both videos)' });
  await expect(bar).toBeVisible();
  await expect.poll(async () => (await times())[0]).toBeGreaterThan(0.5); // playing
  await expect.poll(() => times().then(([p, q]) => Math.abs(p - q) < 0.3)).toBe(true); // in step

  // Scrubbing moves both.
  await bar.fill('4');
  await expect.poll(async () => (await times()).every((t) => Math.abs(t - 4) < 0.6)).toBe(true);
  // Pause pauses both.
  await page.getByRole('button', { name: 'Pause both' }).click();
  await expect.poll(() => page.locator('.stage video').evaluateAll((vs) => vs.every((v) => (v as HTMLVideoElement).paused))).toBe(true);
  await page.getByRole('button', { name: 'Play both' }).click();

  // Flip: the hidden one keeps up.
  await page.getByRole('tab', { name: 'Flip' }).click();
  // (flip makes new video elements: they join at the moment on show, then keep in step - read once a second after
  // the flip, a busy machine sometimes caught them still settling)
  await expect.poll(() => times().then(([p, q]) => Math.abs(p - q) < 0.3), { timeout: 6000 }).toBe(true);
});

test('duplicates: on a slow connection both videos still play, together, with nothing pressed (the slower one froze, seeking every frame)', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-chromium', 'a throttled network: Chrome only');
  test.setTimeout(60000);
  await page.addInitScript(() => localStorage.setItem('vitrine.dupeLevel', '6'));
  await page.goto('/');
  const vids = await page.evaluate(async () => {
    const ids = (await (await fetch('/api/search?limit=26&q=')).json()).ids as number[];
    const files = (await (await fetch('/api/files?ids=' + ids.join(','))).json()).files as { mime: string }[];
    return files.filter((f) => f.mime.startsWith('video/')).map((f) => ({ ...f, tags: 0, sites: null }));
  });
  await page.route('**/api/dupes?*', (route) => route.fulfill({ json: { total: 1, pairs: [{ a: vids[0], b: vids[1], distance: 3 }], progress: { checked: 1, total: 1 } } }));
  const cdp = await page.context().newCDPSession(page);
  await cdp.send('Network.enable');
  await cdp.send('Network.emulateNetworkConditions', { offline: false, latency: 150, downloadThroughput: 300_000, uploadThroughput: 300_000 });
  await page.goto('/duplicates');
  await page.getByRole('tab', { name: 'Side by side' }).click();
  const times = () => page.locator('.stage video').evaluateAll((vs) => vs.map((v) => (v as HTMLVideoElement).currentTime));
  await expect.poll(async () => Math.min(...(await times())), { timeout: 40000 }).toBeGreaterThan(1.5); // both moving
  const [x, y] = await times();
  expect(Math.abs(x - y)).toBeLessThan(0.5); // and together
  // Not stuck seeking (its clock moved with each seek, but no picture came).
  let seeking = 0;
  for (let k = 0; k < 10; k++) {
    if (await page.locator('.stage video').evaluateAll((vs) => vs.some((v) => (v as HTMLVideoElement).seeking))) seeking++;
    await page.waitForTimeout(300);
  }
  expect(seeking).toBeLessThanOrEqual(3);
});

test('phones: action sheets sit edge to edge like the details and drag down to close', async ({ page }) => {
  test.skip(test.info().project.name !== 'iphone', 'phones only (wider screens get a menu at the button)');
  await page.goto('/settings/sites');
  await page.getByRole('button', { name: 'More for safebooru.org', exact: true }).click();
  const menu = page.getByRole('menu', { name: 'More for safebooru.org' });
  await expect(menu).toBeVisible();
  await page.waitForTimeout(400); // slid in
  const box = (await menu.boundingBox())!;
  const vw = page.viewportSize()!;
  expect(Math.round(box.x)).toBe(0); // edge to edge
  expect(Math.round(box.width)).toBe(vw.width);
  expect(Math.round(box.y + box.height)).toBe(vw.height); // down to the bottom
  // A short drag springs back; a long one closes it.
  const grab = menu.locator('.grab-zone');
  const g = (await grab.boundingBox())!;
  await page.mouse.move(g.x + g.width / 2, g.y + g.height / 2);
  await page.mouse.down();
  await page.mouse.move(g.x + g.width / 2, g.y + g.height / 2 + 15, { steps: 3 }); // a nudge
  await page.mouse.up();
  await expect(menu).toBeVisible();
  await page.waitForTimeout(300);
  await page.mouse.move(g.x + g.width / 2, g.y + g.height / 2);
  await page.mouse.down();
  await page.mouse.move(g.x + g.width / 2, g.y + 200, { steps: 8 });
  await page.mouse.up();
  await expect(page.getByRole('menu')).toHaveCount(0);
});

test('tag rules: a parent\'s "add children" box stays put above its children, with an Add button; the blacklist has one too', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-webkit', 'one run is enough: tag rules are on the server');
  await page.goto('/tags/rules');
  await page.evaluate(() => fetch('/api/relations', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ kind: 'parent', action: 'add', from: 'red', to: 'rule_parent' }) }));
  await page.reload();
  const group = page.locator('.group', { hasText: 'rule_parent' });
  await expect(group.locator('.pill')).toHaveCount(1);
  const box = group.getByRole('combobox', { name: 'Add a child to rule_parent' });
  const add = group.getByRole('button', { name: 'Add', exact: true });
  await expect(add).toBeDisabled(); // nothing typed
  await page.waitForTimeout(400); // (the lists slide in after loading)
  const y0 = (await box.boundingBox())!.y;
  await box.fill('blue_sky landscape');
  await add.click();
  await expect(group.locator('.pill')).toHaveCount(3);
  await expect(box).toHaveValue('');
  expect((await box.boundingBox())!.y).toBe(y0); // still in the same place
  for (const c of ['red', 'blue_sky', 'landscape']) await group.getByRole('button', { name: `Remove ${c} from rule_parent` }).click();
  await expect(page.locator('.group', { hasText: 'rule_parent' })).toHaveCount(0);

  // The blacklist: an Add button beside its box.
  const panel = page.locator('.blacklist');
  await panel.getByRole('combobox', { name: 'Add to the blacklist' }).fill('nothing_like_this');
  await panel.getByRole('button', { name: 'Add', exact: true }).click();
  await expect(panel.locator('.pill')).toHaveCount(1);
  await panel.getByRole('button', { name: 'Remove nothing_like_this from the blacklist' }).click();
  await expect(panel.locator('.pill')).toHaveCount(0);
});

test('settings: a change made just before leaving the page is still saved', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-webkit', 'one run is enough: settings are on the server');
  await page.goto('/settings/viewing');
  const muted = page.getByRole('checkbox', { name: /Start videos muted/ });
  await expect(muted).toBeChecked();
  await muted.uncheck();
  await page.reload(); // at once - before the usual short wait to group changes
  await expect.poll(async () => (await page.evaluate(async () => (await fetch('/api/settings')).json())).startMuted).toBe(false);
  await setSettings(page, { startMuted: true });
});

test('video sound: double-tap the middle toggles it; a volume slider (where the browser allows) remembers the level on this device', async ({ page }) => {
  await page.goto('/?q=system:type%3Dvideo%20system:has_audio');
  await page.locator('.tile').first().click();
  const video = page.locator('.viewer video');
  await expect(video).toHaveCount(1);
  const muted = () => video.evaluate((v: HTMLVideoElement) => v.muted);
  await expect.poll(muted).toBe(true); // starts muted (the setting)
  const box = (await page.locator('.stage').boundingBox())!;
  const mouseUI = test.info().project.name !== 'iphone';
  // Touch: double-tap the middle; a mouse: M (a click plays/pauses there).
  const toggleSound = () => (mouseUI ? page.keyboard.press('m') : page.mouse.dblclick(box.x + box.width / 2, box.y + box.height / 2));
  await toggleSound();
  await expect.poll(muted).toBe(false);
  if (!mouseUI) await expect(page.locator('.flash.mid svg')).toBeVisible(); // (the text is for screen readers)
  await page.waitForTimeout(400);
  await toggleSound();
  await expect.poll(muted).toBe(true);

  const settable = await page.evaluate(() => {
    const v = document.createElement('video');
    v.volume = 0.5;
    return v.volume === 0.5;
  });
  const slider = page.getByRole('slider', { name: 'Volume' });
  if (!settable || isPhone(page)) {
    if (isPhone(page)) {
      await expect(slider).toBeHidden(); // phones: the volume buttons
      return;
    }
    await expect(slider).toHaveCount(0); // iOS: the hardware buttons decide
    return;
  }
  await page.getByRole('button', { name: 'Unmute' }).hover();
  await slider.fill('0.3');
  await expect.poll(muted).toBe(false); // moving it turns the sound on
  expect(await video.evaluate((v: HTMLVideoElement) => v.volume)).toBeCloseTo(0.3, 2);
  // Muted and unmuted again: back to that level; also on the next video / after a reload.
  await page.getByRole('button', { name: 'Mute' }).click();
  await page.getByRole('button', { name: 'Unmute' }).click();
  expect(await video.evaluate((v: HTMLVideoElement) => v.volume)).toBeCloseTo(0.3, 2);
  await page.reload();
  await expect(page.locator('.viewer video')).toHaveCount(1);
  await page.getByRole('button', { name: 'Unmute' }).click();
  expect(await page.locator('.viewer video').evaluate((v: HTMLVideoElement) => v.volume)).toBeCloseTo(0.3, 2);
  await page.evaluate(() => localStorage.removeItem('vitrine.volume'));
});

test('phones: the fast scroller moves to the side the thumb scrolls on', async ({ page }) => {
  test.skip(test.info().project.name !== 'iphone', 'phones only');
  await page.setViewportSize({ width: 393, height: 430 }); // a short window: the scroller shows
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  const fast = page.locator('.fast');
  await expect(fast).toBeAttached();
  // A tap on that side of the grid (it opens the file there; closed again).
  const touch = async (side: 'left' | 'right') => {
    const r = (await page.locator('#grid-scroller').boundingBox())!;
    await page.touchscreen.tap(side === 'left' ? r.x + 40 : r.x + r.width - 80, r.y + r.height - 100); // (on a tile: the top is under the bars)
    await page.getByRole('button', { name: 'Close' }).click();
    await expect(page.locator('.viewer')).toHaveCount(0);
  };
  await expect(fast).not.toHaveClass(/left/); // on the right to begin with
  await touch('left');
  await expect(fast).toHaveClass(/left/);
  const box = (await fast.boundingBox())!;
  expect(Math.round(box.x)).toBeLessThan(5); // against the left edge
  await touch('right');
  await expect(fast).not.toHaveClass(/left/);
});

test('phones: tapping a suggestion picks it and keeps the keyboard up (the box keeps focus)', async ({ page }) => {
  test.skip(test.info().project.name !== 'iphone', 'touch');
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  const input = page.locator('.field input');
  await input.tap();
  await input.pressSequentially('blue_s');
  const option = page.getByRole('option', { name: /blue_sky/ });
  await expect(option).toBeVisible();
  const b = (await option.boundingBox())!;
  await page.touchscreen.tap(b.x + b.width / 2, b.y + b.height / 2);
  await expect(page.locator('.chip')).toHaveText(/blue_sky/);
  await expect(page.getByText(/^4 files/)).toBeVisible();
  expect(await input.evaluate((el) => el === document.activeElement)).toBe(true);
});

test('a tag added to the search from the details: the search line shows its end (the new chip, the typing box)', async ({ page }) => {
  test.skip(test.info().project.name !== 'iphone', 'a narrow search line');
  await page.goto('/?q=blue_sky%20-nothing_like_aaaa%20-nothing_like_bbbb%20-nothing_like_cccc');
  await expect(page.getByText(/^4 files/)).toBeVisible();
  const chips = page.locator('.bar .chips');
  await chips.evaluate((el) => (el.scrollLeft = 0)); // looking at the start
  await page.locator('.tile').first().click();
  await page.getByRole('button', { name: 'Details', exact: true }).click();
  const panel = page.getByRole('complementary', { name: 'Details' });
  await panel.getByRole('button', { name: 'landscape', exact: true }).click();
  await page.getByRole('menu').getByRole('menuitem', { name: 'Add to search' }).click();
  await expect(page).toHaveURL(/landscape/);
  await expect
    .poll(() => chips.evaluate((el) => Math.round(el.scrollWidth - el.clientWidth - el.scrollLeft)))
    .toBeLessThanOrEqual(1);
});

test('grid: a thumbnail still on its way shows a spinner after a moment; one that failed is asked again', async ({ page }) => {
  let failed = false;
  let release: () => void = () => {};
  const held = new Promise<void>((r) => (release = r));
  await page.route('**/thumbs/**', async (route) => {
    if (!failed) {
      failed = true;
      return route.fulfill({ status: 503, body: '' }); // the first one fails once
    }
    await held; // the rest wait
    await route.continue();
  });
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  const waiting = page.locator('.tile .waiting').first();
  await expect.poll(() => waiting.evaluate((el) => getComputedStyle(el).opacity)).toBe('1'); // after half a second
  release();
  await expect.poll(() => page.locator('.tile img:not(.in)').count(), { timeout: 10_000 }).toBe(0); // all in, the failed one too
  await expect(page.locator('.tile .waiting').first()).toBeHidden();
});

test('phones: suggestions never reach under the keyboard (and the bar iOS puts above it)', async ({ page }) => {
  test.skip(test.info().project.name !== 'iphone', 'an on-screen keyboard');
  // The keyboard up: the visible part of the page ends 360 px down.
  await page.addInitScript(() => {
    const fake = { height: 360, offsetTop: 0, width: innerWidth, addEventListener() {}, removeEventListener() {} };
    Object.defineProperty(window, 'visualViewport', { get: () => fake });
  });
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  const input = page.locator('.field input');
  await input.tap();
  await input.pressSequentially('b');
  const list = page.getByRole('listbox');
  await expect(list).toBeVisible();
  const top = (await list.boundingBox())!.y;
  const max = await list.evaluate((el) => parseFloat(getComputedStyle(el).maxHeight));
  expect(top + max).toBeLessThanOrEqual(360 - 8 + 1);
});

test('phones: a zoomed picture keeps gliding after a flick, then stops', async ({ page }) => {
  test.skip(test.info().project.name !== 'iphone', 'touch');
  await page.goto('/?q=system:type%3Dimage');
  await page.locator('.tile').first().click();
  const box = (await page.locator('.stage').boundingBox())!;
  await page.mouse.dblclick(box.x + box.width / 2, box.y + box.height / 2);
  await expect.poll(() => imageScale(page)).toBeGreaterThan(1.5);
  await page.waitForTimeout(400); // the zoom animation done
  const tx = () =>
    page.locator('.viewer .slide[data-slot="1"] img').evaluate((img) => new DOMMatrix(getComputedStyle(img).transform).e);
  // A quick flick to the right with one finger.
  await page.locator('.stage').evaluate(async (stage) => {
    const r = stage.getBoundingClientRect();
    const y = r.top + r.height / 2;
    const fire = (type: string, x: number) =>
      stage.dispatchEvent(new PointerEvent(type, { pointerId: 21, pointerType: 'touch', clientX: x, clientY: y, button: 0, bubbles: true }));
    const x0 = r.left + r.width / 2 - 60;
    fire('pointerdown', x0);
    for (let i = 1; i <= 6; i++) {
      await new Promise((res) => setTimeout(res, 16));
      fire('pointermove', x0 + i * 12);
    }
    fire('pointerup', x0 + 72);
  });
  const atRelease = await tx();
  await page.waitForTimeout(300);
  const later = await tx();
  expect(later).toBeGreaterThan(atRelease + 5); // still moving after the finger left
  await page.waitForTimeout(1500);
  const settled = await tx();
  await page.waitForTimeout(300);
  expect(await tx()).toBeCloseTo(settled, 0); // and it stops
});

test('pinching smaller than the fit gives a little, then springs back', async ({ page }) => {
  test.skip(test.info().project.name !== 'iphone', 'touch');
  await page.goto('/?q=system:type%3Dimage');
  await page.locator('.tile').first().click();
  await expect(page.locator('.viewer .slide[data-slot="1"] img')).toBeVisible();
  const stage = page.locator('.stage');
  const pinch = (from: number, to: number, up: boolean) =>
    stage.evaluate(
      (el, [from, to, up]) => {
        const r = el.getBoundingClientRect();
        const cx = r.left + r.width / 2;
        const cy = r.top + r.height / 2;
        const fire = (type: string, id: number, x: number) =>
          el.dispatchEvent(new PointerEvent(type, { pointerId: id, pointerType: 'touch', clientX: x, clientY: cy, button: 0, bubbles: true }));
        fire('pointerdown', 31, cx - (from as number));
        fire('pointerdown', 32, cx + (from as number));
        for (let d = from as number; d >= (to as number); d -= 10) {
          fire('pointermove', 31, cx - d);
          fire('pointermove', 32, cx + d);
        }
        if (up) {
          fire('pointerup', 31, cx - (to as number));
          fire('pointerup', 32, cx + (to as number));
        }
      },
      [from, to, up] as const,
    );
  await pinch(120, 40, false); // fingers together: smaller than fitted, with resistance
  const squeezed = await imageScale(page);
  expect(squeezed).toBeLessThan(1);
  expect(squeezed).toBeGreaterThan(0.5); // resistance: not a third of the size
  await stage.evaluate((el) => {
    const r = el.getBoundingClientRect();
    for (const id of [31, 32])
      el.dispatchEvent(new PointerEvent('pointerup', { pointerId: id, pointerType: 'touch', clientX: r.left + r.width / 2, clientY: r.top + r.height / 2, button: 0, bubbles: true }));
  });
  await expect.poll(() => imageScale(page)).toBe(1); // sprung back
});

test('video scrub bar: pressed, it shows no focus outline (Tab still does)', async ({ page }) => {
  await page.goto('/?q=system:type%3Dvideo');
  await page.locator('.tile').first().click();
  const track = page.getByRole('slider', { name: 'Position' });
  await expect(track).toBeVisible();
  const b = (await track.boundingBox())!;
  await page.mouse.click(b.x + b.width / 2, b.y + b.height / 2);
  await expect(track).toBeFocused();
  expect(await track.evaluate((el) => getComputedStyle(el).outlineStyle)).toBe('none');
});

test('a mouse on a zoomed picture: the wheel zooms (down to the fit, not into the next file); the middle button pans without closing', async ({ page }) => {
  test.skip(test.info().project.name === 'iphone', 'a mouse');
  await page.goto('/?q=system:type%3Dimage');
  await page.locator('.tile').nth(1).click();
  await expect(page.getByText(/^2 \//)).toBeVisible();
  const stage = page.locator('.stage');
  const b = (await stage.boundingBox())!;
  const cx = b.x + b.width / 2;
  const cy = b.y + b.height / 2;
  await page.mouse.click(cx, cy); // zoom to 100 %
  await expect.poll(() => imageScale(page)).toBeGreaterThan(1);
  await page.waitForTimeout(300);
  const at100 = await imageScale(page);
  await page.mouse.wheel(0, -200); // in
  await expect.poll(() => imageScale(page)).toBeGreaterThan(at100);

  // Middle button held: pans, and doesn't close.
  const tx = () => page.locator('.viewer .slide[data-slot="1"] img').evaluate((img) => new DOMMatrix(getComputedStyle(img).transform).e);
  const before = await tx();
  await page.mouse.move(cx, cy);
  await page.mouse.down({ button: 'middle' });
  await page.mouse.move(cx + 80, cy, { steps: 4 });
  await page.mouse.up({ button: 'middle' });
  expect(Math.abs((await tx()) - before)).toBeGreaterThan(20);
  await expect(page.locator('.viewer')).toBeVisible();

  // Wheel out to the fit, and on: still this file.
  for (let i = 0; i < 12; i++) await page.mouse.wheel(0, 400);
  await expect.poll(() => imageScale(page)).toBe(1);
  await expect(page.getByText(/^2 \//)).toBeVisible();
  // After a rest the wheel moves between files again.
  await page.waitForTimeout(600);
  await page.mouse.wheel(0, 200);
  await expect(page.getByText(/^3 \//)).toBeVisible();
  // Not zoomed: a middle click closes.
  await page.mouse.click(cx, cy, { button: 'middle' });
  await expect(page.locator('.viewer')).toHaveCount(0);
});

test('pinching near an edge: the picture follows the fingers smoothly, then settles inside the edges', async ({ page }) => {
  test.skip(test.info().project.name !== 'iphone', 'touch');
  await page.goto('/?q=system:type%3Dimage');
  await page.locator('.tile').first().click();
  await expect(page.locator('.viewer .slide[data-slot="1"] img')).toBeVisible();
  const stage = page.locator('.stage');
  // Fingers spread around a point near the picture's edge on the side where it
  // leaves bars (it's smaller than the screen there), step by step. After each
  // step: where that point is across the picture (0 = one edge, 1 = the other).
  const spots = await stage.evaluate(async (el) => {
    const r = el.getBoundingClientRect();
    const img = el.querySelector('.slide[data-slot="1"] img') as HTMLImageElement;
    // The picture itself inside the element (it's fitted in, with bars).
    const pic = () => {
      const b = img.getBoundingClientRect();
      const k = Math.min(b.width / img.naturalWidth, b.height / img.naturalHeight);
      const w = img.naturalWidth * k;
      const h = img.naturalHeight * k;
      return { left: b.left + (b.width - w) / 2, top: b.top + (b.height - h) / 2, width: w, height: h };
    };
    const f = pic();
    const vertical = f.height < r.height - 1; // bars above and below
    const at = vertical ? { x: r.left + r.width / 2, y: f.top + 20 } : { x: f.left + 20, y: r.top + r.height / 2 };
    const fire = (type: string, id: number, d: number) =>
      el.dispatchEvent(
        new PointerEvent(type, {
          pointerId: id,
          pointerType: 'touch',
          clientX: vertical ? at.x : at.x + d,
          clientY: vertical ? at.y + d : at.y,
          button: 0,
          bubbles: true,
        }),
      );
    fire('pointerdown', 41, -20);
    fire('pointerdown', 42, 20);
    const out: number[] = [];
    for (let d = 22; d <= 50; d += 2) {
      fire('pointermove', 41, -d);
      fire('pointermove', 42, d);
      await new Promise(requestAnimationFrame); // the page updates after the event
      const b = pic();
      out.push(vertical ? (at.y - b.top) / b.height : (at.x - b.left) / b.width);
    }
    return out;
  });
  // It grows around the fingers: the same spot stays under them all along
  // (held in the middle until it filled the screen, it slid, then jumped).
  for (const spot of spots) expect(spot).toBeCloseTo(spots[0], 2);
  expect(await imageScale(page)).toBeGreaterThan(1.5);
  await stage.evaluate((el) => {
    for (const id of [41, 42]) el.dispatchEvent(new PointerEvent('pointerup', { pointerId: id, pointerType: 'touch', button: 0, bubbles: true }));
  });
  // Let go: back inside the edges - each way, centered if smaller than the
  // screen, else covering it.
  await expect
    .poll(() =>
      page.locator('.viewer .slide[data-slot="1"] img').evaluate((img: HTMLImageElement) => {
        const e = img.getBoundingClientRect();
        const k = Math.min(e.width / img.naturalWidth, e.height / img.naturalHeight);
        const b = { width: img.naturalWidth * k, height: img.naturalHeight * k, left: 0, top: 0 };
        b.left = e.left + (e.width - b.width) / 2;
        b.top = e.top + (e.height - b.height) / 2;
        const s = img.closest('.stage')!.getBoundingClientRect();
        const fits = (lo: number, size: number, slo: number, ssize: number) =>
          size < ssize ? Math.abs(lo + size / 2 - (slo + ssize / 2)) < 1 : lo <= slo + 0.5 && lo + size >= slo + ssize - 0.5;
        return fits(b.left, b.width, s.left, s.width) && fits(b.top, b.height, s.top, s.height);
      }),
    )
    .toBe(true);
});

test('swiping down to close: the controls fade away with the drag', async ({ page }) => {
  test.skip(test.info().project.name !== 'iphone', 'touch');
  await page.goto('/?q=system:type%3Dvideo');
  await page.locator('.tile').first().click();
  const top = page.locator('.viewer .top');
  const bottom = page.locator('.viewer .bottom');
  await expect(bottom).toBeVisible();
  await expect.poll(() => top.evaluate((el) => getComputedStyle(el).opacity)).toBe('1');
  const stage = page.locator('.stage');
  const drag = (dy: number, upToo: boolean) =>
    stage.evaluate(
      (el, [dy, upToo]) => {
        const r = el.getBoundingClientRect();
        const x = r.left + r.width / 2;
        const y = r.top + r.height / 3;
        const fire = (type: string, py: number) =>
          el.dispatchEvent(new PointerEvent(type, { pointerId: 51, pointerType: 'touch', clientX: x, clientY: py, button: 0, bubbles: true }));
        fire('pointerdown', y);
        for (let d = 0; d <= (dy as number); d += 10) fire('pointermove', y + d);
        if (upToo) fire('pointerup', y);
      },
      [dy, upToo] as const,
    );
  await drag(200, false); // held well down: the controls are gone
  await expect.poll(() => top.evaluate((el) => Number(getComputedStyle(el).opacity))).toBeLessThan(0.05);
  await expect.poll(() => bottom.evaluate((el) => Number(getComputedStyle(el).opacity))).toBeLessThan(0.05);
  await stage.evaluate((el) => {
    const r = el.getBoundingClientRect();
    // back up before letting go: it stays open, the controls come back
    for (let d = 200; d >= 0; d -= 20)
      el.dispatchEvent(new PointerEvent('pointermove', { pointerId: 51, pointerType: 'touch', clientX: r.left + r.width / 2, clientY: r.top + r.height / 3 + d, button: 0, bubbles: true }));
    el.dispatchEvent(new PointerEvent('pointerup', { pointerId: 51, pointerType: 'touch', clientX: r.left + r.width / 2, clientY: r.top + r.height / 3, button: 0, bubbles: true }));
  });
  await expect(page.locator('.viewer')).toBeVisible();
  await expect.poll(() => top.evaluate((el) => getComputedStyle(el).opacity)).toBe('1');
});

test("the grid behind the viewer follows the file being viewed - closing doesn't jump", async ({ page }) => {
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  const grid = page.locator('#grid-scroller');
  await page.locator('.tile[data-i="0"]').click();
  await expect(page.locator('.viewer')).toBeVisible();
  for (let i = 0; i < 25; i++) await page.keyboard.press('ArrowRight');
  await expect(page.getByText('26 / 26', { exact: true })).toBeVisible();
  // Still open: the last file's tile is already on screen behind it.
  await expect
    .poll(() =>
      grid.evaluate((el) => {
        const t = el.querySelector('.tile[data-i="25"]');
        if (!t) return false;
        const a = t.getBoundingClientRect();
        const b = el.getBoundingClientRect();
        return a.top >= b.top - 1 && a.bottom <= b.bottom + 1;
      }),
    )
    .toBe(true);
  const before = await grid.evaluate((el) => el.scrollTop);
  if (isPhone(page)) expect(before).toBeGreaterThan(0); // (on desktop all 26 fit)
  await page.keyboard.press('Escape');
  await expect(page.locator('.viewer')).toHaveCount(0);
  expect(await grid.evaluate((el) => el.scrollTop)).toBe(before); // no jump on closing
});

test('opening a file: the controls fade in once, above the opening animation (never covered by it)', async ({ page }) => {
  await page.goto('/?q=system:type%3Dimage');
  await expect(page.locator('.tile img.in').first()).toBeVisible();
  // Every frame from the tap on: the controls' opacity only ever goes up, and
  // while the animation runs, what's drawn at the close button is the button.
  const { seen, covered } = await page.evaluate(async () => {
    const out: number[] = [];
    const covered: string[] = [];
    (document.querySelector('.tile') as HTMLElement).click();
    const t0 = performance.now();
    while (performance.now() - t0 < 700) {
      await new Promise(requestAnimationFrame);
      const top = document.querySelector('.viewer .top');
      if (top) out.push(Number(getComputedStyle(top).opacity));
      const layer = document.querySelector<HTMLElement>('.hero-layer');
      const close = document.querySelector('.viewer .top button');
      if (layer && close) {
        layer.style.pointerEvents = 'auto'; // so the hit test sees it (it ignores pointer-events: none)
        const b = close.getBoundingClientRect();
        const at = document.elementFromPoint(b.left + b.width / 2, b.top + b.height / 2);
        layer.style.pointerEvents = '';
        if (!close.contains(at)) covered.push(String((at as HTMLElement)?.className));
      }
    }
    return { seen: out, covered };
  });
  expect(covered).toEqual([]);
  expect(seen.length).toBeGreaterThan(5);
  expect(seen[0]).toBeLessThan(0.5);
  for (let i = 1; i < seen.length; i++) expect(seen[i]).toBeGreaterThanOrEqual(seen[i - 1] - 0.001);
  expect(seen[seen.length - 1]).toBe(1);
});

test('sidebar: the hover moves at once from item to item (never two lit, no fade)', async ({ page }) => {
  test.skip(test.info().project.name === 'iphone', 'a mouse');
  await page.goto('/');
  await page.getByRole('button', { name: 'Menu' }).click();
  const tabs = page.locator('#nav-drawer .tab:not(.active)');
  await expect(tabs.nth(1)).toBeVisible();
  await page.waitForTimeout(300); // the drawer slides in
  const a = (await tabs.nth(0).boundingBox())!;
  const b = (await tabs.nth(1).boundingBox())!;
  await page.mouse.move(a.x + 40, a.y + a.height / 2);
  await page.mouse.move(b.x + 40, b.y + b.height / 2);
  // One frame later (the browser updates hover with the next frame).
  const bg = await tabs.evaluateAll(async (els) => {
    await new Promise(requestAnimationFrame);
    await new Promise(requestAnimationFrame);
    return els.slice(0, 2).map((e) => getComputedStyle(e).backgroundColor);
  });
  expect(bg[0]).toBe('rgba(0, 0, 0, 0)');
  expect(bg[1]).toBe('rgba(49, 50, 68, 0.5)');
});

test('notices go where their kind is set: Copied on screen (not the bell), a delete with Undo pops up and goes to the bell; each says its kind', async ({ page, context }, info) => {
  test.skip(info.project.name !== 'desktop-chromium', 'clipboard: Chromium; the notices are the same everywhere');
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  await page.goto('/?q=system:type%3Dimage');
  await clearBell(page);
  await page.reload();
  await page.locator('.tile').nth(5).click();
  await page.getByRole('button', { name: 'Details' }).click();
  await page.getByRole('button', { name: 'Copy the file ID' }).click();
  // Copied: on screen a moment (its default), not to the bell.
  await expect(page.locator('.toast.done', { hasText: 'Copied the file ID' })).toBeVisible();
  // Delete is in the details now (not among the viewer's buttons), and asks first.
  await expect(page.locator('.viewer .top').getByRole('button', { name: 'Delete' })).toHaveCount(0);
  await page.locator('.viewer').getByRole('button', { name: 'Delete file' }).click();
  await expect(page.getByRole('alertdialog', { name: 'Delete this file?' }).locator('.thumbs img')).toHaveCount(1);
  await confirmIn(page, 'Delete');
  // With Undo: it pops up (a bin) - and goes to the bell (its default).
  const removed = page.locator('.toast.removed', { hasText: 'Deleted' });
  await expect(removed.locator('.badge svg')).toBeVisible();
  await removed.getByRole('button', { name: 'Undo' }).click(); // (the file comes back: covered by the delete tests)
  await expect(removed).toHaveCount(0);
  // (the details, then the viewer: the top bar shows)
  await expect(async () => {
    await page.keyboard.press('Escape');
    await expect(page.locator('.viewer')).toHaveCount(0, { timeout: 500 });
  }).toPass();
  const bell = page.getByRole('button', { name: /^Notifications, \d+ unread$/ });
  await expect(bell).toBeVisible();
  await bell.click();
  const panel = page.getByRole('dialog', { name: 'Notifications' });
  await expect(panel.locator('li.removed', { hasText: 'Deleted' }).locator('.badge svg')).toBeVisible();
  await expect(panel.locator('li', { hasText: 'Copied' })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Notifications', exact: true })).toBeVisible(); // (opened: read)
  await panel.getByRole('button', { name: 'Clear' }).click();
  await expect(panel).toHaveCount(0); // (cleared: the menu closes too)
  await page.getByRole('button', { name: 'Notifications', exact: true }).click();
  await expect(panel.getByRole('img', { name: 'No notifications' })).toBeVisible(); // (a quiet check)
});

/** Empties the bell (it's the server's: shared by every device and so by the tests on one server). */
async function clearBell(page: Page) {
  await page.evaluate(async () => {
    const { notices } = await (await fetch('/api/notices')).json();
    const upTo = Math.max(0, ...notices.map((n: { id: number }) => n.id));
    if (upTo) await fetch('/api/notices/clear', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ up_to: upTo }) });
  });
}

test('the bell rings for what the downloader did: a subscription check that failed, worded here, leading to Downloads', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop-webkit', 'one run is enough: the server is shared');
  await page.goto('/');
  const sub = await page.evaluate(() =>
    fetch('/api/subscriptions', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ site: 'test-down', query: 'bell_sub', interval: 86400, initial_max: 0 }) }).then((r) => r.json()),
  );
  // (live: the check fails - the test site is down - and the bell rings with it)
  const bell = page.getByRole('button', { name: /^Notifications, \d+ unread$/ });
  await expect(bell).toBeVisible({ timeout: 15000 });
  await bell.click();
  const item = page.getByRole('dialog', { name: 'Notifications' }).locator('li.error', { hasText: 'bell_sub' });
  await expect(item).toContainText('Subscription check failed');
  await expect(item).toContainText('test-down.invalid');
  await item.getByRole('button').click();
  await expect(page).toHaveURL(/\/downloads/);
  await page.evaluate((id) => fetch('/api/subscriptions/' + id + '/remove', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' }), sub.id);
});

test('search chips have a menu like a tag in the details: edit, exclude, new search, copy, remove (phones: a sheet)', async ({ page }) => {
  await page.goto('/?q=blue_sky%20landscape');
  await expect(page.locator('.chip')).toHaveCount(2);
  const menu = page.getByRole('dialog', { name: 'Edit search chip' });
  await page.locator('.chip-main').nth(1).click();
  await expect(menu).toBeVisible();
  await expect(menu.locator('.head .name')).toHaveText('landscape');
  await expect(menu.getByRole('button', { name: /^Included: exclude it/ })).toHaveCount(0); // one tag: Exclude below does it
  await page.waitForTimeout(350); // it has risen / popped in
  if (isPhone(page)) {
    // Edge to edge along the bottom.
    const b = (await menu.boundingBox())!;
    expect(b.x).toBeLessThanOrEqual(0.5);
    expect(Math.abs(b.x + b.width - page.viewportSize()!.width)).toBeLessThan(1);
    expect(Math.abs(b.y + b.height - page.viewportSize()!.height)).toBeLessThan(1);
  } else {
    // Under its chip.
    const [m, c] = [(await menu.boundingBox())!, (await page.locator('.chip').nth(1).boundingBox())!];
    expect(m.y).toBeGreaterThan(c.y + c.height);
    expect(Math.abs(m.x - c.x)).toBeLessThan(2);
  }
  // Exclude: the chip becomes -landscape, and the menu says so.
  await menu.getByRole('button', { name: 'Exclude from search' }).click();
  await expect(page).toHaveURL(/q=blue_sky(\+|%20)-landscape/);
  await expect(menu.getByRole('button', { name: 'Include again' })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(menu).toHaveCount(0);
  // New search: only this chip.
  await page.locator('.chip-main').nth(0).click();
  await menu.getByRole('button', { name: 'New search' }).click();
  await expect(menu).toHaveCount(0);
  await expect(page.locator('.chip')).toHaveCount(1);
  await expect(page).toHaveURL(/q=blue_sky$/);
  // Remove from search.
  await page.locator('.chip-main').nth(0).click();
  await menu.getByRole('button', { name: 'Remove from search' }).click();
  await expect(page.locator('.chip')).toHaveCount(0);
});

test('library: the downloads menu - a counter, New download, what is downloading now (running first), to the Downloads page', async ({ page }) => {
  const search = (id: number, state: string, extra = {}) => ({
    id, site: 'test-down', query: `menu_${id}`, state, max_posts: 10, site_total: 0, handled: 0, downloaded: 0, already: 0, deleted: 0, skipped: 0,
    blacklisted: 0, position: id, failed: 0, created_at: 0, ...extra,
  });
  let searches = [search(1, 'queued'), search(2, 'done'), search(3, 'paused'), search(4, 'running', { downloaded: 4, label: 'A running one' })];
  await page.route('**/api/downloads', async (r) => {
    if (r.request().method() !== 'GET') return r.continue();
    const res = await r.fetch();
    await r.fulfill({ response: res, json: { ...(await res.json()), searches, sites_paused: {} } });
  });
  await page.goto('/');
  const button = page.getByRole('button', { name: /^Downloads/ });
  await expect(button).toHaveAccessibleName('Downloads, 2 going on'); // running and waiting (not paused, not done)
  await expect(button.locator('.counter')).toHaveText('2');
  await button.click();
  const menu = page.getByRole('dialog', { name: 'Downloads' });
  const rows = menu.locator('li');
  await expect(rows).toHaveCount(3);
  await expect(rows.nth(0)).toContainText('A running one');
  await expect(rows.nth(0)).toContainText('Downloading · 4 files');
  await expect(rows.nth(0).locator('.fill')).toHaveAttribute('style', /width: 40%/);
  await expect(rows.nth(1)).toContainText('menu_1');
  await expect(rows.nth(1)).toContainText('Waiting');
  await expect(rows.nth(2)).toContainText('Paused');
  await rows.nth(0).click();
  await expect(page).toHaveURL(/\/downloads/);
  await expect(menu).toHaveCount(0);
  // Nothing going on: no counter, a quiet line.
  searches = [search(2, 'done')];
  await page.goto('/');
  await expect(button).toHaveAccessibleName('Downloads');
  await expect(button.locator('.counter')).toHaveCount(0);
  await button.click();
  await expect(menu.getByRole('img', { name: 'Nothing downloading' })).toBeVisible(); // (a quiet check, like the bell's)
  await expect(menu.getByRole('button', { name: 'New download' })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(menu).toHaveCount(0);
  await page.unrouteAll({ behavior: 'ignoreErrors' });
});

test('library: the download button - this search or another, the site, the search written its way, once or a subscription', async ({ page }) => {
  const posted: { site: string; query: string; max_posts: number }[] = [];
  await page.route('**/api/downloads', (r) => (r.request().method() === 'POST' ? (posted.push(r.request().postDataJSON()), r.fulfill({ json: { id: 1 } })) : r.continue()));
  await page.goto('/?q=' + encodeURIComponent('blue_sky ( landscape ~ portrait ) system:type=image'));
  await openNewDownload(page);
  const dlg = page.getByRole('dialog');
  // The search in the bar (in its chips' words), on a site with an OR search: its tags that way, the filter left out.
  await expect(dlg.getByRole('button', { name: /This search/ })).toContainText('blue_sky · landscape or portrait · images');
  await dlg.getByRole('button', { name: /This search/ }).click();
  await dlg.getByRole('button', { name: 'test-down.invalid' }).click();
  await expect(dlg.getByRole('combobox', { name: 'Search on the site' })).toHaveValue('blue_sky ( landscape ~ portrait )');
  await expect(dlg).toContainText('left out (the site can’t search them): images'.replace(/^l/, 'L'));
  // Back, and another site: written that one's way - no OR search there: the group split, a search for each.
  await dlg.getByRole('button', { name: 'Back' }).click();
  await dlg.getByRole('button', { name: 'safebooru.org' }).click();
  await expect(dlg.getByRole('combobox', { name: 'Search on the site' })).toHaveValue('blue_sky');
  await expect(dlg).toContainText('safebooru.org has no OR search: 2 searches, one for each combination.');
  await dlg.getByRole('button', { name: /Next/ }).click();
  await expect(dlg.locator('.what')).toContainText('2 searches');
  await expect(dlg.locator('.split-list li')).toHaveText(['blue_sky landscape', 'blue_sky portrait']);
  // Once (the default), at most 50 files: both searches start.
  await expect(dlg.getByRole('tab', { name: /Once/ })).toHaveAttribute('aria-selected', 'true');
  await dlg.getByRole('spinbutton').fill('50');
  await dlg.getByRole('button', { name: 'Download', exact: true }).click();
  await expect.poll(() => posted).toMatchObject([
    { site: 'safebooru', query: 'blue_sky landscape', max_posts: 50 },
    { site: 'safebooru', query: 'blue_sky portrait', max_posts: 50 },
  ]);
  await expect(dlg).toHaveCount(0);
  // No tags in the search: straight to the site, then a new search, as a subscription.
  let subscribed: { site: string; query: string; interval: number } | null = null;
  await page.route('**/api/subscriptions', (r) => (r.request().method() === 'POST' ? ((subscribed = r.request().postDataJSON()), r.fulfill({ json: { id: 1 } })) : r.continue()));
  await page.goto('/?q=system:type%3Dimage');
  await openNewDownload(page);
  await expect(dlg.getByRole('heading', { name: 'Which site?' })).toBeVisible();
  await dlg.getByRole('button', { name: 'safebooru.org' }).click();
  const box = dlg.getByRole('combobox', { name: 'Search on the site' });
  await expect(box).toHaveValue('');
  await expect(dlg.getByRole('button', { name: /Next/ })).toBeDisabled();
  await box.fill('landscape');
  await box.press('Enter');
  await dlg.getByRole('tab', { name: /Subscribe/ }).click();
  // How often: its list over the dialog, nothing of it cut off or under it.
  await dlg.getByRole('button', { name: 'Repeat' }).click();
  const list = page.getByRole('listbox', { name: 'Repeat' });
  const last = list.getByRole('option', { name: 'Every week' });
  await expect(last).toBeVisible();
  expect(await dlg.evaluate((el) => el.scrollTop)).toBe(0); // (the dialog made room for it by scrolling: its title went)
  await expect(dlg.getByRole('heading')).toBeInViewport();
  expect(await last.evaluate((el) => {
    const r = el.getBoundingClientRect();
    return el.contains(document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2));
  })).toBe(true);
  await list.getByRole('option', { name: 'Every day' }).click();
  await expect(dlg).toBeVisible(); // (picking closes the list, not the dialog)
  await dlg.getByRole('button', { name: 'Subscribe', exact: true }).click();
  await expect.poll(() => subscribed).toMatchObject({ site: 'safebooru', query: 'landscape', interval: 86400 });
});

test('suggestions: found inside a name (not only its start), what was typed marked; in the download dialog too, over it', async ({ page }) => {
  await page.goto('/');
  const box = page.getByRole('combobox', { name: 'Search' });
  await box.fill('sky');
  const option = page.getByRole('option', { name: /blue_sky/ });
  await expect(option).toBeVisible();
  await expect(option.locator('mark')).toHaveText('sky');
  await box.fill('');
  await page.keyboard.press('Escape');
  // The dialog's search box: its list over the dialog, on screen whole; a pick goes in.
  await openNewDownload(page);
  const dlg = newDl(page);
  await dlg.getByRole('button', { name: 'safebooru.org' }).click();
  const sbox = dlg.getByRole('combobox', { name: 'Search on the site' });
  await sbox.click();
  await sbox.pressSequentially('sky');
  const pick = page.getByRole('option', { name: /blue_sky/ });
  await expect(pick).toBeVisible();
  const r = (await pick.boundingBox())!;
  expect(r.y).toBeGreaterThanOrEqual(0);
  expect(r.y + r.height).toBeLessThanOrEqual(page.viewportSize()!.height);
  expect(await pick.evaluate((el) => {
    const b = el.getBoundingClientRect();
    return el.contains(document.elementFromPoint(b.left + b.width / 2, b.top + b.height / 2));
  })).toBe(true);
  await pick.click();
  await expect(sbox).toHaveValue('blue_sky ');
});

test('search: a suggestion picked while the typed word is highlighted becomes a chip (it went in front of it)', async ({ page }) => {
  await page.goto('/');
  const box = page.getByRole('combobox', { name: 'Search' });
  await box.fill('blu');
  const option = page.getByRole('option', { name: /blue_sky/ });
  await expect(option).toBeVisible();
  await box.evaluate((el: HTMLInputElement) => el.setSelectionRange(0, el.value.length));
  await option.click();
  await expect(page.locator('.bar .chip-main', { hasText: 'blue_sky' })).toBeVisible();
  await expect(box).toHaveValue('');
  await expect.poll(() => new URL(page.url()).searchParams.get('q')).toBe('blue_sky');
});

test('suggestions: a long list scrolls under the app\'s floating scrollbar (not the browser\'s)', async ({ page }, info) => {
  test.skip(isPhone(page) || info.project.name === 'desktop-webkit', 'the floating scrollbar: with a mouse');
  await page.route('**/api/autocomplete?*', (r) =>
    r.fulfill({ json: { suggestions: Array.from({ length: 12 }, (_, k) => ({ tag: `scroll_tag_${k}`, count: 100 - k })) } }),
  );
  await page.setViewportSize({ width: 1280, height: 420 }); // (short: the list has to scroll)
  await page.goto('/');
  await page.getByRole('combobox', { name: 'Search' }).fill('scroll');
  const list = page.locator('.list[data-floating]');
  await expect(list.getByRole('option')).toHaveCount(12);
  const scroller = list.locator('ul.scroll');
  await expect(scroller).toHaveClass(/oscroll-host/);
  expect(await scroller.evaluate((el) => el.scrollHeight > el.clientHeight)).toBe(true);
  await scroller.hover();
  await page.mouse.wheel(0, 120);
  await expect(list.locator('.oscroll.on')).toBeVisible();
  await page.waitForTimeout(300); // (its fade in)
  if (process.env.SHOTS) await page.screenshot({ path: process.env.SHOTS + '/sugg-scroll.png', clip: { x: 0, y: 0, width: 1280, height: 420 } });
});

test('suggestions: a tag already in the search isn\'t one; marked ones go in together - all of them, or any of them (keys: arrows, Space, Enter)', async ({ page }) => {
  await page.goto('/?q=blue_sky');
  const box = page.getByRole('combobox', { name: 'Search' });
  await box.fill('land');
  await expect(page.getByRole('option', { name: /landscape/ })).toBeVisible();
  await box.fill('sky');
  await page.waitForTimeout(400);
  await expect(page.getByRole('option', { name: /blue_sky/ })).toHaveCount(0);
  // Marked from two words typed one after another, then any of them: one OR chip.
  await page.goto('/');
  await box.fill('sky');
  await page.getByRole('option', { name: /blue_sky/ }).getByRole('checkbox').click();
  await expect(page.getByRole('option', { name: /blue_sky/ }).getByRole('checkbox')).toHaveAttribute('aria-checked', 'true');
  await box.fill('land');
  await page.getByRole('option', { name: /landscape/ }).getByRole('checkbox').click();
  await expect(page.locator('.mtag')).toHaveText(['blue_sky', 'landscape']);
  await page.getByRole('button', { name: 'Any of them' }).click();
  await expect.poll(() => new URL(page.url()).searchParams.get('q')).toBe('( blue_sky ~ landscape )');
  await expect(box).toHaveValue('');
  await expect(box).toBeFocused();
  // The keys: down to a suggestion, Space marks it, Enter puts the marked ones in (all of them).
  await page.goto('/');
  await box.fill('sky');
  await expect(page.getByRole('option', { name: /blue_sky/ })).toBeVisible();
  await page.keyboard.press('ArrowDown');
  await page.keyboard.press('Space');
  await expect(page.getByRole('option', { name: /blue_sky/ }).getByRole('checkbox')).toHaveAttribute('aria-checked', 'true');
  await expect(box).toHaveValue('sky'); // (no space typed)
  await box.fill('land');
  await expect(page.getByRole('option', { name: /landscape/ })).toBeVisible();
  await page.keyboard.press('ArrowDown');
  await page.keyboard.press('Space');
  await page.keyboard.press('Enter');
  await expect.poll(() => new URL(page.url()).searchParams.get('q')).toBe('blue_sky landscape');
});

test('library: the download dialog with a very long search never scrolls sideways', async ({ page }) => {
  const long = ['blue_sky', ...Array.from({ length: 14 }, (_, k) => `some_rather_long_tag_number_${k}`), 'a_single_tag_' + 'x'.repeat(120), 'system:type=image'].join(' ');
  await page.goto('/?q=' + encodeURIComponent(long));
  await openNewDownload(page);
  const dlg = page.getByRole('dialog');
  const sideways = () => dlg.evaluate((el) => el.scrollWidth - el.clientWidth);
  const pageSideways = () => page.evaluate(() => document.documentElement.scrollWidth - innerWidth);
  await page.waitForTimeout(400);
  expect(await sideways()).toBeLessThanOrEqual(0);
  await dlg.getByRole('button', { name: /This search/ }).click();
  await dlg.getByRole('button', { name: 'test-down.invalid' }).click();
  await page.waitForTimeout(400);
  expect(await sideways()).toBeLessThanOrEqual(0);
  await dlg.getByRole('combobox', { name: 'Search on the site' }).press('Enter');
  await page.waitForTimeout(400);
  expect(await sideways()).toBeLessThanOrEqual(0);
  expect(await pageSideways()).toBeLessThanOrEqual(0);
});

test('dialogs open above the on-screen keyboard (iOS: it covers the bottom of the page, the middle of the screen is under it)', async ({ page }) => {
  await page.goto('/?q=system:type%3Dimage');
  const kb = Math.round(page.viewportSize()!.height * 0.5); // (as lib/viewport reports an open keyboard: about half a phone's screen)
  await page.evaluate((h) => document.documentElement.style.setProperty('--kb', `${h}px`), kb);
  const above = async (loc: ReturnType<typeof page.locator>) => {
    const box = (await loc.boundingBox())!;
    expect(box.y).toBeGreaterThanOrEqual(0);
    expect(box.y + box.height).toBeLessThanOrEqual(page.viewportSize()!.height - kb + 1);
  };
  await openNewDownload(page);
  await page.waitForTimeout(400);
  await above(page.getByRole('dialog'));
  await page.keyboard.press('Escape');
  if (isPhone(page)) return;
  // The delete confirmation.
  await page.locator('.tile[data-i="0"]').click({ modifiers: ['Control'] });
  await page.getByRole('button', { name: 'Delete selected' }).click();
  await page.waitForTimeout(300);
  await above(page.getByRole('alertdialog'));
});

test('an OR chip\'s download: the site\'s OR search with its tags, or a search for each where it has none', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('vitrine.lastSite', 'test-down'));
  await page.goto('/?q=' + encodeURIComponent('( blue_sky ~ landscape ) system:type=image'));
  const menu = page.getByRole('dialog', { name: 'Edit search chip' });
  // A filter chip: nothing to download.
  await page.locator('.chip-main').nth(1).click();
  await expect(menu.getByRole('button', { name: /^Download/ })).toHaveCount(0);
  await page.keyboard.press('Escape');
  // The OR group: any of them, as the site's OR search.
  await page.locator('.chip-main').nth(0).click();
  await menu.getByRole('button', { name: 'Download any of them' }).click();
  // The New download dialog: what goes; a site with an OR search writes it that way.
  const dlg = newDl(page);
  await expect(dlg).toContainText('blue_sky or landscape');
  await dlg.getByRole('button', { name: 'test-down.invalid' }).click();
  await expect(dlg.getByRole('combobox', { name: 'Search on the site' })).toHaveValue('( blue_sky ~ landscape )');
  // One without: a search for each tag, as subscriptions together in a group named after the search.
  const subs: { query: string; group_name?: string }[] = [];
  await page.route('**/api/subscriptions', (r) => (r.request().method() === 'POST' ? (subs.push(r.request().postDataJSON()), r.fulfill({ json: { id: subs.length } })) : r.continue()));
  await dlg.getByRole('button', { name: 'Back' }).click();
  await dlg.getByRole('button', { name: 'safebooru.org' }).click();
  await expect(dlg.getByRole('combobox', { name: 'Search on the site' })).toHaveValue('');
  await dlg.getByRole('button', { name: /Next/ }).click();
  await expect(dlg.locator('.split-list li')).toHaveText(['blue_sky', 'landscape']);
  await dlg.getByRole('tab', { name: /Subscribe/ }).click();
  await expect(dlg.locator('.new-group')).toHaveValue('blue_sky or landscape');
  await dlg.getByRole('button', { name: 'Subscribe', exact: true }).click();
  await expect.poll(() => subs).toMatchObject([
    { query: 'blue_sky', group_name: 'blue_sky or landscape' },
    { query: 'landscape', group_name: 'blue_sky or landscape' },
  ]);
});

test('a chip\'s New download: the tag written the site\'s way (#tag, @creator on a Mastodon server), again when the site changes', async ({ page }) => {
  // The built-in test site searches like a Mastodon server here.
  await page.route('**/api/sites', async (route) => {
    const res = await route.fetch();
    const body = await res.json();
    for (const s of body.sites) if (s.id === 'safebooru') s.tag_query = { creator: '@{tag}', '*': '#{tag}' };
    await route.fulfill({ response: res, json: body });
  });
  await page.goto('/?q=creator%3Asome_artist%20blue_sky');
  const menu = page.getByRole('dialog', { name: 'Edit search chip' });
  const dlg = newDl(page);
  const box = dlg.getByRole('combobox', { name: 'Search on the site' });
  await page.locator('.chip-main').nth(0).click();
  await menu.getByRole('button', { name: 'New download' }).click();
  await dlg.getByRole('button', { name: 'safebooru.org' }).click();
  await expect(box).toHaveValue('@some_artist');
  // Another site (a booru): the plain tag.
  await dlg.getByRole('button', { name: 'Back' }).click();
  await dlg.getByRole('button', { name: 'test-down.invalid' }).click();
  await expect(box).toHaveValue('some_artist');
  // A general tag: a hashtag.
  await page.keyboard.press('Escape');
  await expect(dlg).toHaveCount(0);
  await page.locator('.chip-main').nth(1).click();
  await menu.getByRole('button', { name: 'New download' }).click();
  await dlg.getByRole('button', { name: 'safebooru.org' }).click();
  await expect(box).toHaveValue('#blue_sky');
  await page.keyboard.press('Escape');
  // An excluded chip or a filter has no New download.
  await page.goto('/?q=-blue_sky');
  await page.locator('.chip-main').nth(0).click();
  await expect(menu.getByRole('button', { name: 'New download' })).toHaveCount(0);
});

test('phones: every sheet has the same maximum height, and slides away (from where a drag left it) when closed', async ({ page }) => {
  test.skip(test.info().project.name !== 'iphone', 'phone sheets');
  const vh = page.viewportSize()!.height;
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  // The filter sheet (long: it scrolls inside) stops at 70 % of the screen.
  await page.getByRole('button', { name: 'Filters', exact: true }).click();
  const panel = page.locator('.panel[data-sheet]');
  await page.waitForTimeout(350);
  const b = (await panel.boundingBox())!;
  expect(b.height / vh).toBeLessThanOrEqual(0.701);
  expect(b.height / vh).toBeGreaterThan(0.69);
  expect(await panel.evaluate((el) => el.scrollHeight > el.clientHeight)).toBe(true);
  // Dragged down and let go: it carries on down from there, then it's gone.
  const grab = panel.locator('.grab-zone');
  const g = (await grab.boundingBox())!;
  await page.mouse.move(g.x + g.width / 2, g.y + g.height / 2);
  await page.mouse.down();
  await page.mouse.move(g.x + g.width / 2, g.y + 150, { steps: 6 });
  await page.mouse.up();
  const tops: number[] = await page.evaluate(async () => {
    const out: number[] = [];
    for (let k = 0; k < 8; k++) {
      const el = document.querySelector('.panel[data-sheet]');
      if (!el) break;
      out.push(el.getBoundingClientRect().top);
      await new Promise(requestAnimationFrame);
    }
    return out;
  });
  expect(tops.length).toBeGreaterThan(2); // it slides (doesn't vanish at once)
  expect(tops[0]).toBeGreaterThan(b.y + 100); // starting where the finger left it
  for (let i = 1; i < tops.length; i++) expect(tops[i]).toBeGreaterThanOrEqual(tops[i - 1] - 0.5); // only ever down
  await expect(panel).toHaveCount(0);
});

test('deleting asks first (one dialog for every destructive action): Del in the viewer, Escape cancels, Enter deletes; phones: a sheet', async ({ page }) => {
  await page.goto('/?q=system:type%3Dimage');
  await expect(page.getByText(/^24 files/)).toBeVisible();
  await page.locator('.tile').nth(2).click();
  await expect(page.locator('.viewer')).toBeVisible();
  const dialog = page.getByRole('alertdialog', { name: 'Delete this file?' });
  if (isPhone(page)) {
    // No keyboard: from the details.
    await page.getByRole('button', { name: 'Details' }).click();
    await page.getByRole('button', { name: 'Delete file' }).click();
    await page.waitForTimeout(300);
    const b = (await dialog.boundingBox())!;
    expect(b.x).toBeLessThanOrEqual(0.5); // a sheet, edge to edge
    expect(Math.abs(b.y + b.height - page.viewportSize()!.height)).toBeLessThan(1);
    await dialog.getByRole('button', { name: 'Cancel' }).click();
    await expect(dialog).toHaveCount(0);
    await expect(page.getByText(/^24 files/)).toBeVisible(); // nothing deleted
    return;
  }
  await page.keyboard.press('Delete');
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText('You can still undo it.');
  // Escape: only the dialog closes (the viewer stays), nothing deleted.
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
  await expect(page.locator('.viewer')).toBeVisible();
  // Keys don't reach the viewer while it's open (no move to the next file).
  await page.keyboard.press('Delete');
  await page.keyboard.press('ArrowRight');
  await expect(page.getByText(/^3 \/ 24/)).toBeVisible();
  // Enter: it can be undone, so the action has the focus.
  await page.keyboard.press('Enter');
  await expect(dialog).toHaveCount(0);
  await expect(page.getByText(/^1 \/ 23|^3 \/ 23/)).toBeVisible();
  await page.locator('.toast.removed').getByRole('button', { name: 'Undo' }).click();
  await expect(page.getByText(/ \/ 24$/)).toBeVisible();
});

test('search chips: pointing at a chip or its × colors the round pill, never a square inside it', async ({ page }) => {
  test.skip(isPhone(page), 'a mouse');
  await page.goto('/?q=blue_sky');
  const chip = page.locator('.bar .chip').first();
  const plain = await chip.evaluate((el) => getComputedStyle(el).backgroundColor);
  await chip.locator('.chip-x').hover();
  await page.waitForTimeout(250);
  expect(await chip.locator('.chip-x').evaluate((el) => getComputedStyle(el).backgroundImage)).toBe('none');
  const onX = await chip.evaluate((el) => getComputedStyle(el).backgroundColor);
  expect(onX).not.toBe(plain); // the pill takes the red tint
  await chip.locator('.chip-main').hover();
  await page.waitForTimeout(250);
  expect(await chip.locator('.chip-main').evaluate((el) => getComputedStyle(el).backgroundImage)).toBe('none');
  expect(await chip.evaluate((el) => getComputedStyle(el).backgroundColor)).not.toBe(onX);
});

test('phones: a sheet drags down by its header (not only the grab bar); the details sheet slides away from where it was let go (no jump up)', async ({ page }) => {
  test.skip(test.info().project.name !== 'iphone', 'phone sheets');
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  const drag = async (from: { x: number; y: number }, dy: number) => {
    await page.mouse.move(from.x, from.y);
    await page.mouse.down();
    await page.mouse.move(from.x, from.y + dy, { steps: 8 });
    await page.mouse.up();
  };
  // The filter sheet: dragged by its title.
  await page.getByRole('button', { name: 'Filters', exact: true }).click();
  const panel = page.locator('.panel[data-sheet]');
  await page.waitForTimeout(350);
  const h = (await panel.locator('.head h2').boundingBox())!;
  await drag({ x: h.x + h.width / 2, y: h.y + h.height / 2 }, 160);
  await expect(panel).toHaveCount(0);

  // The details sheet: every frame after letting go, it's only ever lower.
  await page.locator('.tile').nth(1).click();
  await page.getByRole('button', { name: 'Details' }).click();
  const details = page.locator('.viewer aside.panel');
  await page.waitForTimeout(400);
  const head = (await details.locator('.panel-head h2').boundingBox())!;
  const top0 = (await details.boundingBox())!.y;
  await page.mouse.move(head.x + 10, head.y + head.height / 2);
  await page.mouse.down();
  await page.mouse.move(head.x + 10, head.y + head.height / 2 + 150, { steps: 8 });
  const tops = await page.evaluate(async () => {
    const out: number[] = [];
    const el = document.querySelector('.viewer aside.panel')!;
    out.push(el.getBoundingClientRect().top); // held down
    return out;
  });
  await page.mouse.up();
  const after = await page.evaluate(async () => {
    const out: number[] = [];
    for (let k = 0; k < 10; k++) {
      const el = document.querySelector('.viewer aside.panel');
      if (!el) break;
      out.push(el.getBoundingClientRect().top);
      await new Promise(requestAnimationFrame);
    }
    return out;
  });
  expect(tops[0]).toBeGreaterThan(top0 + 100); // it followed the finger
  expect(after.length).toBeGreaterThan(1);
  expect(after[0]).toBeGreaterThanOrEqual(tops[0] - 1); // no jump back up
  for (let i = 1; i < after.length; i++) expect(after[i]).toBeGreaterThanOrEqual(after[i - 1] - 0.5);
  await expect(details).toHaveCount(0);
});

test('duplicates: switching flip / side by side after the videos started keeps them playing, at the same moment', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('vitrine.dupeLevel', '6')); // (Very similar: the test library has no exact copies)
  await page.goto('/');
  const vids = await page.evaluate(async () => {
    const ids = (await (await fetch('/api/search?limit=26&q=')).json()).ids as number[];
    const files = (await (await fetch('/api/files?ids=' + ids.join(','))).json()).files as { mime: string }[];
    return files.filter((f) => f.mime.startsWith('video/')).map((f) => ({ ...f, tags: 0, sites: null }));
  });
  await page.route('**/api/dupes?*', (route) =>
    route.fulfill({ json: { total: 1, pairs: [{ a: vids[0], b: vids[1], distance: 3 }], progress: { checked: 1, total: 1 } } }),
  );
  await page.goto('/duplicates');
  const playing = () => page.locator('.stage video').evaluateAll((vs) => vs.map((v) => !(v as HTMLVideoElement).paused && (v as HTMLVideoElement).currentTime));
  await expect.poll(async () => (await playing()).some((t) => t !== false && t > 0.8), { timeout: 10_000 }).toBe(true); // started
  await page.getByRole('tab', { name: 'Side by side' }).click();
  // The new videos join in: playing, not back at the start.
  await expect.poll(async () => { const p = await playing(); return p.length === 2 && p.every((t) => t !== false && t > 0.8); }, { timeout: 10_000 }).toBe(true);
});

test('a tag can be put in another namespace: from the details (its tag menu) and from All tags (on every file)', async ({ page }) => {
  await page.goto('/?q=blue_sky');
  await expect(page.getByText(/^4 files/)).toBeVisible();
  await page.locator('.tile').first().click();
  await page.getByRole('button', { name: 'Details' }).click();
  const panel = page.locator('.viewer aside.panel');
  await panel.getByRole('button', { name: 'blue_sky', exact: true }).click();
  const menu = page.getByRole('menu', { name: /^blue_sky/ });
  await menu.getByRole('menuitem', { name: /^Namespace/ }).click();
  await expect(menu.getByRole('menuitemradio', { name: 'General' })).toHaveAttribute('aria-checked', 'true');
  await menu.getByRole('menuitemradio', { name: 'Series' }).click();
  await expect(page.getByText('blue_sky is series now (4 files)')).toBeVisible();
  // The details show it as a series tag; every file has it there.
  await expect(panel.getByRole('listitem', { name: /^series:blue_sky/ })).toBeVisible(); // (it moves there: the old one fades out of General meanwhile)
  expect((await page.evaluate(() => fetch('/api/search?q=series:blue_sky&limit=10').then((r) => r.json()))).ids.length).toBe(4);
  await page.keyboard.press('Escape');

  // All tags: back to general from its ⋯ menu.
  await page.goto('/tags/all');
  await rowMenu(page, 'series:blue_sky', 'Namespace', 'General');
  await expect(page.getByText('blue_sky is general now (4 files)')).toBeVisible();
  await expect(page.getByRole('button', { name: 'More for blue_sky', exact: true })).toBeVisible();
  expect((await page.evaluate(() => fetch('/api/search?q=series:blue_sky&limit=10').then((r) => r.json()))).ids.length).toBe(0);
});

test('menus with pages all move between them the same way (a slide while the height follows): the tag menu too', async ({ page }) => {
  await page.goto('/?q=blue_sky');
  await page.locator('.tile').first().click();
  await page.getByRole('button', { name: 'Details' }).click();
  await page.locator('.viewer aside.panel').getByRole('button', { name: 'blue_sky', exact: true }).click();
  const menu = page.getByRole('menu', { name: /^blue_sky/ });
  await expect(menu.locator('.mpages .mpage')).toHaveCount(1);
  // Pressed: for a moment both pages are there (the old going, the new coming) and the height animates.
  const during = await menu.evaluate(async (m) => {
    const ns = [...m.querySelectorAll<HTMLButtonElement>('[role=menuitem]')].find((b) => b.textContent?.includes('Namespace'))!;
    ns.click();
    await new Promise(requestAnimationFrame);
    await new Promise(requestAnimationFrame);
    return { pages: m.querySelectorAll('.mpage').length, animating: !!m.querySelector('.mpages.animating') };
  });
  if (!(await page.evaluate(() => matchMedia('(prefers-reduced-motion: reduce)').matches))) {
    expect(during.pages).toBe(2);
    expect(during.animating).toBe(true);
  }
  await expect(menu.getByRole('menuitemradio', { name: 'General' })).toBeVisible();
  await expect(menu.locator('.mpage')).toHaveCount(1); // settled
  await expect(menu.locator('.mpages.animating')).toHaveCount(0);
});

test('the viewer controls fade after a while untouched; the mouse moving or a tap brings them back; not while a video is paused or the pointer is on them', async ({ page }) => {
  test.setTimeout(45_000);
  await page.goto('/?q=system:type%3Dimage');
  await page.locator('.tile').first().click();
  const top = page.locator('.viewer .top');
  await expect(top).not.toHaveClass(/hide/);
  await expect(top).toHaveClass(/hide/, { timeout: 6000 }); // left alone: gone
  if (isPhone(page)) {
    const box = (await page.locator('.stage').boundingBox())!;
    await page.locator('.stage').tap({ position: { x: box.width / 2, y: box.height / 2 } });
    await expect(top).not.toHaveClass(/hide/);
    return;
  }
  await expect(page.locator('.viewer')).toHaveClass(/idle/); // the cursor hides too
  await page.mouse.move(300, 300);
  await page.mouse.move(320, 310);
  await expect(top).not.toHaveClass(/hide/);
  // The pointer on the controls: they stay.
  const close = (await page.locator('.viewer .top').getByRole('button', { name: 'Close' }).boundingBox())!;
  await page.mouse.move(close.x + close.width / 2, close.y + close.height / 2);
  await page.waitForTimeout(4000);
  await expect(top).not.toHaveClass(/hide/);
  // A paused video: they stay.
  await page.goto('/?q=system:type%3Dvideo');
  await page.locator('.tile').first().click();
  const video = page.locator('.viewer .slide[data-slot="1"] video');
  await expect.poll(() => video.evaluate((v: HTMLVideoElement) => !v.paused)).toBe(true);
  await page.keyboard.press(' '); // pause
  await expect.poll(() => video.evaluate((v: HTMLVideoElement) => v.paused)).toBe(true);
  await page.mouse.move(700, 450);
  await page.waitForTimeout(4000);
  await expect(page.locator('.viewer .top')).not.toHaveClass(/hide/);
});

test('full screen: our viewer, not the browser\'s player - a button in the video controls (and the top bar for a picture), F; it ends with the viewer', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop-chromium', 'the Fullscreen API: Chromium');
  await page.goto('/?q=system:type%3Dvideo');
  await page.locator('.tile').first().click();
  const fsEl = () => page.evaluate(() => document.fullscreenElement?.className ?? '');
  const btn = page.locator('.viewer .controls').getByRole('button', { name: 'Full screen' });
  await btn.click();
  await expect.poll(fsEl).toContain('viewer'); // the viewer (our player and controls), not the video element
  await expect(page.locator('.viewer .controls').getByRole('button', { name: 'Exit full screen' })).toBeVisible();
  await page.keyboard.press('f');
  await expect.poll(fsEl).toBe('');
  // A picture: the button is in the top bar; F too; closing the viewer ends it.
  await page.goto('/?q=system:type%3Dimage');
  await page.locator('.tile').first().click();
  await page.locator('.viewer .top').getByRole('button', { name: 'Full screen' }).click();
  await expect.poll(fsEl).toContain('viewer');
  await page.locator('.viewer .top').getByRole('button', { name: 'Close' }).click();
  await expect.poll(fsEl).toBe('');
});

test('the top bar slides away when the page scrolls down and comes back when it scrolls up (always at the top)', async ({ page }) => {
  await page.setViewportSize({ width: 400, height: 700 }); // a long grid
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  const bar = page.locator('header.topbar');
  const grid = page.locator('#grid-scroller');
  const shown = async () => ((await bar.boundingBox())!.y + (await bar.boundingBox())!.height) > 10;
  expect(await shown()).toBe(true);
  // Down: away - sliding over the grid, which doesn't move (only the scrolling moves it).
  for (const y of [100, 300, 500]) {
    await grid.evaluate((el, y) => (el.scrollTop = y), y);
    await page.waitForTimeout(60);
  }
  const y0 = (await page.locator('.tile').nth(12).boundingBox())!.y;
  await expect(bar).toHaveClass(/away/);
  await expect.poll(shown).toBe(false);
  expect((await page.locator('.tile').nth(12).boundingBox())!.y).toBeCloseTo(y0, 0);
  // Up a little (a moment later, as a hand would): back.
  await page.waitForTimeout(400);
  await grid.evaluate((el) => (el.scrollTop -= 80));
  await expect(bar).not.toHaveClass(/away/);
  // Down again, then all the way to the top: back, and it stays.
  await page.waitForTimeout(400);
  await grid.evaluate((el) => (el.scrollTop += 200));
  await page.waitForTimeout(60);
  await grid.evaluate((el) => (el.scrollTop += 200));
  await expect(bar).toHaveClass(/away/);
  await grid.evaluate((el) => (el.scrollTop = 0));
  await expect(bar).not.toHaveClass(/away/);
  await expect.poll(shown).toBe(true);
});

test('videos loop seamlessly: sent back to the start just before the end (never left to end and restart)', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop-chromium', 'frame timing: Chromium (requestVideoFrameCallback)');
  await page.goto('/?q=system:type%3Dvideo%20system:has_audio');
  await page.locator('.tile').first().click();
  const video = page.locator('.viewer .slide[data-slot="1"] video');
  await expect.poll(() => video.evaluate((v: HTMLVideoElement) => v.duration || 0)).toBeGreaterThan(1);
  const res = await video.evaluate(async (v: HTMLVideoElement) => {
    let last = 0;
    let atSeek = -1;
    let ended = false;
    const track = (_: number, m: { mediaTime: number }) => {
      last = m.mediaTime;
      (v as any).requestVideoFrameCallback(track);
    };
    (v as any).requestVideoFrameCallback(track);
    v.addEventListener('seeking', () => atSeek < 0 && last > 1 && (atSeek = last));
    v.addEventListener('ended', () => (ended = true));
    v.currentTime = v.duration - 0.6;
    await new Promise((r) => v.addEventListener('seeked', r, { once: true }));
    atSeek = -1;
    if (v.paused) await v.play();
    await new Promise((r) => setTimeout(r, 1500));
    return { atSeek, duration: v.duration, ended, now: v.currentTime };
  });
  expect(res.ended).toBe(false);
  expect(res.atSeek).toBeGreaterThan(res.duration - 0.3); // it got to the end...
  expect(res.atSeek).toBeLessThan(res.duration - 0.04); // ...and went back before its last frame (the browser's loop waits for that)
  expect(res.now).toBeLessThan(2); // playing from the start again
});

test('with a mouse, pages scroll with a thin floating bar: no room taken (the page never shifts), shown while scrolling, then gone', async ({ page }, info) => {
  test.skip(info.project.name === 'iphone', 'touch screens keep their own');
  await page.setViewportSize({ width: 1000, height: 420 }); // a page longer than the window
  await page.goto('/settings/library');
  const pageEl = page.locator('.page').first();
  await expect(pageEl).toHaveClass(/oscroll-host/);
  // The browser's bar takes no room.
  expect(await pageEl.evaluate((el) => el.offsetWidth - el.clientWidth)).toBe(0);
  const thumb = page.locator('.oscroll');
  await expect(thumb).not.toHaveClass(/\bon\b/);
  await pageEl.evaluate((el) => (el.scrollTop = 200));
  await expect(thumb).toHaveClass(/\bon\b/);
  const box = (await thumb.boundingBox())!;
  const pb = (await pageEl.boundingBox())!;
  expect(box.x + box.width).toBeGreaterThan(pb.x + pb.width - 6); // at the right edge, over the content
  expect(box.width).toBeLessThanOrEqual(10);
  await expect(thumb).not.toHaveClass(/\bon\b/, { timeout: 3000 }); // fades when idle
  // Dragging the thumb scrolls.
  await page.mouse.move(pb.x + pb.width - 4, pb.y + 40);
  await expect(thumb).toHaveClass(/\bon\b/);
  await pageEl.evaluate((el) => (el.scrollTop = 0));
  await page.waitForTimeout(50);
  const t = (await thumb.boundingBox())!;
  const before = await pageEl.evaluate((el) => el.scrollTop);
  await page.mouse.move(t.x + t.width / 2, t.y + 5);
  await page.mouse.down();
  await page.mouse.move(t.x + t.width / 2, t.y + 60, { steps: 4 });
  await page.mouse.up();
  expect(await pageEl.evaluate((el) => el.scrollTop)).toBeGreaterThan(before + 30);
});

test('the home-screen app has a launch screen for each iPhone size (the splash, drawn by the server), so a start is never blank', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop-chromium', 'a page and an image: one browser is enough');
  await page.goto('/');
  const links = page.locator('link[rel="apple-touch-startup-image"]');
  expect(await links.count()).toBeGreaterThan(10);
  const href = await page.locator('link[rel="apple-touch-startup-image"][media*="393px"]').getAttribute('href');
  expect(href).toBe('/startup.png?w=1179&h=2556&top=177&scale=3&v=2'); // (v: raised when the logo changes)
  const size = await page.evaluate(async (href) => {
    const r = await fetch(href!, { credentials: 'omit' }); // iOS asks without the login
    const b = new DataView(await r.arrayBuffer());
    return { type: r.headers.get('content-type'), w: b.getUint32(16), h: b.getUint32(20) }; // PNG header
  }, href);
  expect(size).toEqual({ type: 'image/png', w: 1179, h: 2556 });
  expect((await page.request.get('/startup.png?w=99999&h=1&top=0&scale=3')).status()).toBe(400);
});

test('grid: a change under the same search moves tiles by file - the deleted ones fade out where they were, the rest slide up into place', async ({ page }) => {
  test.skip(isPhone(page), 'desktop only');
  await page.goto('/?q=system:type%3Dimage');
  await expect(page.getByText(/^24 files/)).toBeVisible();
  const idAt = (i: number) => page.locator(`.tile[data-i="${i}"]`).getAttribute('data-id');
  const third = await idAt(2);
  // Watch the animations on the tiles.
  await page.evaluate(() => {
    const w = window as any;
    w.__slides = [];
    const orig = Element.prototype.animate;
    Element.prototype.animate = function (this: Element, k: any, o: any) {
      if (this.classList.contains('tile')) w.__slides.push({ id: (this as HTMLElement).dataset.id ?? null, ghost: this.classList.contains('ghost'), from: k?.[0]?.transform ?? '' });
      return orig.call(this, k, o);
    };
  });
  await page.locator('.tile[data-i="0"]').click({ modifiers: ['Control'] });
  await page.locator('.tile[data-i="1"]').click({ modifiers: ['Control'] });
  await page.getByRole('button', { name: 'Delete selected' }).click();
  await confirmIn(page, 'Delete 2');
  await expect(page.getByText(/^22 files/)).toBeVisible();
  await expect(page.locator('.sizer > .ghost')).toHaveCount(0); // faded out, and gone
  expect(await idAt(0)).toBe(third);
  const slides = await page.evaluate(() => (window as any).__slides as { id: string | null; ghost: boolean; from: string }[]);
  expect(slides.filter((s) => s.ghost)).toHaveLength(2);
  // The third file slid from its old place (third in the row) to the first.
  expect(slides.find((s) => s.id === third && !s.ghost)?.from).toMatch(/^translate\((?!0px)/);
  await page.getByRole('button', { name: 'Undo' }).click();
  await expect(page.getByText(/^24 files/)).toBeVisible();
  expect(await idAt(2)).toBe(third);
});

test('a click on a file with its hover card open: the viewer opens from the card (not from the small tile), and the card is gone', async ({ page }) => {
  test.skip(isPhone(page), 'mouse only');
  await page.goto('/?q=system:type%3Dimage');
  await expect(page.getByText(/^24 files/)).toBeVisible();
  await page.evaluate(() => {
    const w = window as any;
    const orig = Element.prototype.animate;
    Element.prototype.animate = function (this: Element, k: any, o: any) {
      if (this.classList.contains('hero-frame') && !w.__heroFrom) w.__heroFrom = k[0].transform;
      return orig.call(this, k, o);
    };
  });
  const tile = page.locator('.tile[aria-label="png 4000×500"]').first();
  await tile.scrollIntoViewIfNeeded();
  const b = (await tile.boundingBox())!;
  await page.mouse.move(b.x + b.width / 2 - 5, b.y + b.height / 2);
  await page.mouse.move(b.x + b.width / 2, b.y + b.height / 2);
  const card = page.locator('.tile.zoomed');
  await expect(card).toHaveCount(1);
  await page.waitForTimeout(500); // fully open
  const cb = (await card.boundingBox())!;
  await page.mouse.down();
  await page.mouse.up();
  await expect(page.locator('.viewer')).toBeVisible();
  const from = await page.evaluate(() => (window as any).__heroFrom as string);
  const [x, y] = from.match(/translate\(([-\d.]+)px, ([-\d.]+)px\)/)!.slice(1).map(Number);
  expect(Math.abs(x - cb.x)).toBeLessThan(3); // the card's place...
  expect(Math.abs(y - cb.y)).toBeLessThan(3);
  expect(Math.abs(x - b.x)).toBeGreaterThan(20); // ...not the tile's
  await expect(card).toHaveCount(0);
});

test('Settings > Stats: the library, imports, downloads, storage and the server at a glance', async ({ page }) => {
  await page.goto('/settings/stats');
  const library = page.locator('section.card', { has: page.getByRole('heading', { name: 'Media' }) });
  await expect(library.locator('.tile').first()).toContainText('26');
  await expect(library.locator('.legend')).toContainText('Images');
  await expect(library.locator('.legend')).toContainText('Videos');
  // Today's bar holds today's imports (the whole test library).
  const imports = page.locator('section.card', { has: page.getByRole('heading', { name: 'Imports' }) });
  await expect(imports.locator('.col')).toHaveCount(30);
  await imports.locator('.col').last().hover();
  await expect(imports.locator('.readout')).toContainText('26 on');
  const storage = page.locator('section.card', { has: page.getByRole('heading', { name: 'Storage' }) });
  await expect(storage.getByRole('meter').first()).toBeVisible();
  await expect(storage).toContainText(/free of/);
  const server = page.locator('section.card', { has: page.getByRole('heading', { name: 'Server' }) });
  await expect(server).toContainText('Running for');
  await expect(server.locator('dd').filter({ hasText: 'Not found' })).toHaveCount(0); // ffmpeg and libvips are there (the tests need them)
  // Nothing wider than the screen.
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('problem report: errors in the app reach the server log; the report shows, copies and downloads it, without private values', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-chromium', 'one run is enough: the report is made on the server');
  await page.goto('/settings/stats');
  await expect(page.getByRole('heading', { name: 'Problem report' })).toBeVisible();
  // An error in the app (as a bug would throw), and a private search in the address.
  await page.evaluate(() => setTimeout(() => { throw new Error('boom from the test'); }));
  await page.waitForTimeout(500);
  const card = page.locator('section.card', { has: page.getByRole('heading', { name: 'Problem report' }) });
  await card.getByRole('button', { name: 'Show' }).click();
  const pre = card.getByLabel('The problem report');
  await expect(pre).toContainText('Vitrine problem report');
  await expect(pre).toContainText('web app error');
  await expect(pre).toContainText('boom from the test');
  await expect(pre).toContainText('where=/settings/stats');
  await expect(pre).not.toContainText('test-down'); // a site's name
  await card.getByRole('button', { name: 'Hide' }).click();
  await expect(pre).toHaveCount(0);
  const download = page.waitForEvent('download');
  await card.getByRole('button', { name: 'Download' }).click();
  expect((await download).suggestedFilename()).toMatch(/^vitrine-report-\d{4}-\d\d-\d\d\.txt$/);
});

test('grid: removing a chip from the search animates too - files in both lists slide from where they were, the others fade in', async ({ page }) => {
  test.skip(isPhone(page), 'desktop: the whole library fits on screen');
  await page.goto('/?q=red');
  await expect(page.getByText(/^2 files/)).toBeVisible();
  const first = await page.locator('.tile[data-i="0"]').getAttribute('data-id');
  await page.evaluate(() => {
    const w = window as any;
    w.__anims = [];
    const orig = Element.prototype.animate;
    Element.prototype.animate = function (this: Element, k: any, o: any) {
      if (this.classList.contains('tile')) w.__anims.push({ id: (this as HTMLElement).dataset.id ?? null, from: k?.[0]?.transform ?? '', fade: k?.[0]?.opacity === 0 });
      return orig.call(this, k, o);
    };
  });
  await page.getByRole('button', { name: 'Remove red from the search' }).click();
  await expect(page.getByText(/^26 files/)).toBeVisible();
  await expect.poll(() => page.evaluate(() => (window as any).__anims.length)).toBeGreaterThan(10);
  const anims = await page.evaluate(() => (window as any).__anims as { id: string | null; from: string; fade: boolean }[]);
  const at = Number(await page.locator(`.tile[data-id="${first}"]`).getAttribute('data-i'));
  expect(at).toBeGreaterThan(0); // it moved...
  expect(anims.find((a) => a.id === first)?.from).toBe('translate(0px, 0px)'); // ...sliding from its old place (the first)
  expect(anims.filter((a) => a.fade).length).toBeGreaterThan(10); // the newcomers fade in
});

test('hover backgrounds change at once everywhere (no fade: two neighbors crossfading flickered)', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-chromium', 'styles: one browser is enough');
  // Every clickable thing on each screen: its background must not be animated.
  const fading = () =>
    page.evaluate(() => {
      const out: string[] = [];
      for (const el of document.querySelectorAll<HTMLElement>('button, a, [role="option"], [role="menuitem"], [role="tab"], .tab, .chip, li')) {
        const cs = getComputedStyle(el);
        const props = cs.transitionProperty.split(',').map((p) => p.trim());
        const durs = cs.transitionDuration.split(',').map((d) => parseFloat(d));
        const bad = props.some((p, i) => (p === 'all' || p.startsWith('background')) && (durs[i] ?? durs[0]) > 0);
        if (bad) out.push(`${el.tagName.toLowerCase()}.${[...el.classList].join('.')} "${(el.textContent ?? '').trim().slice(0, 20)}"`);
      }
      return out;
    });
  for (const path of ['/?q=blue_sky', '/downloads', '/settings/library', '/settings/stats', '/tags/all', '/duplicates']) {
    await page.goto(path);
    await page.waitForTimeout(500);
    expect(await fading(), path).toEqual([]);
  }
  // The viewer, with its details open.
  await page.goto('/');
  await page.locator('.tile').first().click();
  await page.keyboard.press('i');
  await page.waitForTimeout(500);
  expect(await fading(), 'viewer').toEqual([]);
  // The search bar's buttons: moving from one to the next, only the second is lit a frame later.
  await page.keyboard.press('Escape');
  await page.keyboard.press('Escape');
  await page.goto('/?q=blue_sky');
  const filter = page.getByRole('button', { name: /Filters?/ }).first();
  const star = page.getByRole('button', { name: /Saved|star/i }).first();
  const a = (await filter.boundingBox())!;
  const b = (await star.boundingBox())!;
  await page.mouse.move(a.x + a.width / 2, a.y + a.height / 2);
  await page.waitForTimeout(100);
  await page.mouse.move(b.x + b.width / 2, b.y + b.height / 2);
  const bg = await page.evaluate(async ([f, s]) => {
    await new Promise(requestAnimationFrame);
    await new Promise(requestAnimationFrame);
    const at = (x: number, y: number) => getComputedStyle(document.elementFromPoint(x, y)!.closest('button')!).backgroundColor;
    return [at(f[0], f[1]), at(s[0], s[1])];
  }, [[a.x + a.width / 2, a.y + a.height / 2], [b.x + b.width / 2, b.y + b.height / 2]]);
  expect(bg[0]).toBe('rgba(0, 0, 0, 0)');
  expect(bg[1]).not.toBe('rgba(0, 0, 0, 0)');
});

test('favorites: the heart in the viewer (and L), a heart on the tile, the file menu, the selection bar; the menu opens them, newest first', async ({ page }) => {
  test.skip(isPhone(page), 'mouse and keys (phones: the same heart in the viewer)');
  await page.goto('/?q=system:type%3Dimage');
  await expect(page.getByText(/^24 files/)).toBeVisible();
  const idOf = (i: number) => page.locator(`.tile[data-i="${i}"]`).getAttribute('data-id');
  const [first, third] = [await idOf(0), await idOf(2)];
  // The viewer's heart.
  await page.locator('.tile[data-i="2"]').click();
  const heart = page.locator('.viewer').getByRole('button', { name: 'Favorite' });
  await expect(heart).toHaveAttribute('aria-pressed', 'false');
  await heart.click();
  await expect(heart).toHaveAttribute('aria-pressed', 'true');
  await page.keyboard.press('ArrowLeft');
  await page.keyboard.press('ArrowLeft');
  await page.keyboard.press('l'); // the first file, by key
  await expect(heart).toHaveAttribute('aria-pressed', 'true');
  await page.keyboard.press('Escape');
  await expect(page.locator(`.tile[data-id="${third}"] .fav-badge`)).toBeVisible();
  await expect(page.locator(`.tile[data-id="${first}"] .fav-badge`)).toBeVisible();
  // The menu's Favorites: both, the newest favorite first (the first file, by key).
  await page.getByRole('button', { name: 'Menu' }).click();
  await unfold(page, 'library');
  await page.locator('#nav-drawer').getByRole('button', { name: 'Favorites' }).click();
  await expect(page.getByText(/^2 files/)).toBeVisible();
  // A page of its own: no "favorite" chip in the search bar, its own placeholder,
  // its own default order (newest favorite first) - not marked as changed.
  await expect(page).toHaveURL(/\/favorites$/);
  await expect(page.locator('.library header .chip')).toHaveCount(0);
  await expect(page.getByPlaceholder('Favorites tag search')).toBeVisible();
  await expect(page.locator('.library header .changed')).toHaveCount(0);
  await expect(page.locator('.library header .filt .dot')).toHaveCount(0); // no "not the default order" dot
  expect(await idOf(0)).toBe(first);
  expect(await idOf(1)).toBe(third);
  await page.getByRole('button', { name: 'Menu' }).click();
  await expect(page.locator('#nav-drawer').getByRole('button', { name: 'Favorites' })).toHaveAttribute('aria-current', 'page');
  await expect(page.locator('#nav-drawer').getByRole('button', { name: 'Everything' })).not.toHaveAttribute('aria-current', 'page');
  await page.keyboard.press('Escape');
  // A tag typed here searches within the favorites (the library has four blue_sky files; these two aren't).
  await page.getByPlaceholder('Favorites tag search').fill('blue_sky ');
  await expect(page.getByText('Nothing matches this search.')).toBeVisible();
  await page.getByRole('button', { name: 'Remove blue_sky from the search' }).click();
  await expect(page.getByText(/^2 files/)).toBeVisible();
  // The file menu: removed from favorites, the list follows.
  await page.locator('.tile[data-i="0"]').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Remove from favorites' }).click();
  await expect(page.getByText(/^1 file\b/)).toBeVisible();
  // The selection bar: back again (from the whole library).
  await page.goto('/?q=system:type%3Dimage');
  await page.locator(`.tile[data-id="${first}"]`).click({ modifiers: ['Control'] });
  await page.locator(`.tile[data-id="${third}"]`).click({ modifiers: ['Control'] });
  await page.getByRole('button', { name: 'Add selected to favorites' }).click();
  await expect(page.locator('.fav-badge')).toHaveCount(2);
  // Both favorites now: the selection's heart removes them.
  await page.getByRole('button', { name: 'Remove selected from favorites' }).click();
  await expect(page.locator('.fav-badge')).toHaveCount(0);
});

test('video controls: the volume sits beside play; its slider opens to the right, moving only the time (the buttons on the right stay put)', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-chromium', 'a mouse, and a browser that lets a page set the volume');
  await page.goto('/?q=system:type%3Dvideo%20system:has_audio');
  await page.locator('.tile').first().click();
  const bar = page.locator('.viewer .bottom');
  await expect(bar).toBeVisible();
  const play = bar.getByRole('button', { name: /^(Play|Pause)$/ });
  const speaker = bar.getByRole('button', { name: /^(Mute|Unmute)$/ });
  const loop = bar.getByRole('button', { name: 'Loop' });
  const time = bar.locator('.time');
  const [p, s, t] = [(await play.boundingBox())!, (await speaker.boundingBox())!, (await time.boundingBox())!];
  expect(s.x).toBeGreaterThan(p.x); // play, then the speaker, then the time
  expect(t.x).toBeGreaterThan(s.x);
  const loopBefore = (await loop.boundingBox())!.x;
  await speaker.hover();
  const slider = bar.getByRole('slider', { name: 'Volume' });
  await expect.poll(async () => (await slider.boundingBox())?.width ?? 0).toBeGreaterThan(60);
  await page.waitForTimeout(250); // the opening
  expect((await time.boundingBox())!.x).toBeGreaterThan(t.x + 50); // the time moved along...
  expect((await loop.boundingBox())!.x).toBe(loopBefore); // ...the loop button didn't
});

test('Favorites keeps its own search and order: its default order is a setting; switching to Everything and back keeps each page\'s search', async ({ page }) => {
  test.skip(isPhone(page), 'the menu is the same on phones; one run is enough');
  const ids: number[] = await page.evaluate(async () => (await (await fetch('/api/search?q=system:type%3Dimage&limit=2')).json()).ids);
  await page.evaluate(async (ids) => {
    for (const id of ids) {
      await fetch('/api/favorite', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ids: [id], on: true }) });
      await new Promise((r) => setTimeout(r, 20)); // (distinct times)
    }
  }, ids);
  // Settings > Library: Favorites sort, oldest favorite first.
  await page.goto('/settings/library');
  await page.getByRole('button', { name: 'Favorites sort' }).click();
  await expect(page.getByRole('button', { name: 'Favorites sort' })).toHaveText(/Favorited - newest first/); // the default
  await page.getByRole('menu', { name: 'Favorites sort' }).getByRole('menuitemradio', { name: /Favorited/ }).click();
  await page.getByRole('menu', { name: 'Favorited' }).getByRole('menuitemradio', { name: 'Oldest first' }).click();
  await expect(page.getByRole('button', { name: 'Favorites sort' })).toHaveText(/Favorited - oldest first/);
  await page.goto('/?q=red');
  await expect(page.getByText(/^2 files/)).toBeVisible();
  await page.getByRole('button', { name: 'Menu' }).click();
  await unfold(page, 'library');
  await page.locator('#nav-drawer').getByRole('button', { name: 'Favorites' }).click();
  await expect(page.getByText(/^2 files/)).toBeVisible();
  await expect(page.locator('.tile[data-i="0"]')).toHaveAttribute('data-id', String(ids[0])); // the oldest favorite first
  await expect(page.locator('.library header .changed')).toHaveCount(0); // that's this page's default
  await expect(page.locator('.library header .filt .dot')).toHaveCount(0);
  // The sort menu agrees: this page's order is its default (no Reset offered).
  await page.getByRole('button', { name: 'Filters' }).click();
  await expect(page.getByText(/^Favorited/).first()).toBeVisible();
  await expect(page.getByRole('button', { name: 'Reset to default' })).toHaveCount(0);
  await page.getByRole('button', { name: 'Filters' }).click(); // closed again
  await expect(page.getByRole('button', { name: 'Filters' })).toHaveAttribute('aria-expanded', 'false');
  // Everything comes back to its own search (red), Favorites to its own (none).
  await page.getByRole('button', { name: 'Menu' }).click();
  await page.locator('#nav-drawer').getByRole('button', { name: 'Everything' }).click();
  await expect(page.locator('.library header .chip', { hasText: 'red' })).toBeVisible();
  await expect(page.getByText(/^2 files/)).toBeVisible();
  await page.goBack();
  await expect(page).toHaveURL(/\/favorites/);
  await expect(page.locator('.library header .chip')).toHaveCount(0);
  await page.evaluate((ids) => fetch('/api/favorite', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ids, on: false }) }), ids);
  await page.evaluate(() => fetch('/api/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ favoritesSort: 'favorite' }) }));
});

test('Favorites: unfavoriting in the viewer moves on to the next favorite; the last one closes the viewer; Undo brings it back', async ({ page }) => {
  test.skip(isPhone(page), 'keys; the same on phones with the heart');
  const ids: number[] = await page.evaluate(async () => (await (await fetch('/api/search?q=system:type%3Dimage&limit=2')).json()).ids);
  await page.evaluate((ids) => fetch('/api/favorite', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ids, on: true }) }), ids);
  await page.goto('/favorites');
  await expect(page.getByText(/^2 files/)).toBeVisible();
  await page.locator('.tile[data-i="0"]').click();
  await expect(page.getByText(/^1 \/ 2/)).toBeVisible();
  await page.keyboard.press('l');
  await expect(page.getByText(/^1 \/ 1/)).toBeVisible(); // the other favorite now
  await expect(page.locator('.viewer').getByRole('button', { name: 'Favorite' })).toHaveAttribute('aria-pressed', 'true');
  await page.keyboard.press('l'); // the last one
  await expect(page.locator('.viewer')).toHaveCount(0);
  await expect(page.getByText('No favorites yet. Tap the heart on a file to add it.')).toBeVisible();
  await page.getByRole('button', { name: 'Undo' }).last().click();
  await expect(page.getByText(/^1 file\b/)).toBeVisible();
  await page.evaluate((ids) => fetch('/api/favorite', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ids, on: false }) }), ids);
});

test('the viewer closed while a favorite change is on its way stays closed (it used to open again when the answer came)', async ({ page }) => {
  test.skip(isPhone(page), 'keys');
  await page.goto('/?q=system:type%3Dimage');
  await page.locator('.tile[data-i="0"]').click();
  await expect(page.locator('.viewer')).toBeVisible();
  let release!: () => void;
  const held = new Promise<void>((r) => (release = r));
  await page.route('**/api/favorite', async (route) => {
    await held;
    await route.continue();
  });
  await page.keyboard.press('l');
  await page.keyboard.press('Escape');
  await expect(page.locator('.viewer')).toHaveCount(0);
  release();
  await page.waitForResponse('**/api/favorite');
  await page.waitForTimeout(300);
  await expect(page.locator('.viewer')).toHaveCount(0);
  const id = Number(await page.locator('.tile[data-i="0"]').getAttribute('data-id'));
  await page.evaluate((id) => fetch('/api/favorite', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ids: [id], on: false }) }), id);
});

test('starting the app: the empty-library welcome (with its download form) never flashes before the first search has answered', async ({ page }) => {
  // A device that has synced before starts on its saved settings and waits up to 1.5 s for the
  // server's copy before the first search: a first visit, then a start with the settings slow to
  // come (held in the page itself - the service worker answers the page's requests on a reload).
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  await page.addInitScript(() => {
    const orig = window.fetch;
    window.fetch = (input, init) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
      if (url.includes('/api/settings') && (!init?.method || init.method === 'GET')) return new Promise((r) => setTimeout(r, 1200)).then(() => orig(input, init));
      return orig(input, init);
    };
  });
  // Anything that ever shows the welcome or a 0 count during the start is noted.
  await page.addInitScript(() => {
    new MutationObserver(() => {
      const w = window as any;
      if (document.body?.textContent?.includes('Welcome to Vitrine')) w.__welcome = true;
      if (/^0 files/.test(document.querySelector('.count')?.textContent ?? '')) w.__zero = true;
    }).observe(document, { childList: true, subtree: true, characterData: true });
  });
  await page.reload();
  await expect(page.getByText(/^26 files/)).toBeVisible();
  expect(await page.evaluate(() => [(window as any).__welcome ?? false, (window as any).__zero ?? false])).toEqual([false, false]);
});

test('saved searches menu: NEW pills on saved searches and group (parent) tags with files since they were last opened; the star shows a dot; opening one marks its new files NEW', async ({ page }) => {
  // (one browser is enough; it cleans up after itself - the saved-searches test only looks at its own anyway)
  test.skip(test.info().project.name !== 'desktop-chromium', 'one run is enough (the same menu everywhere)');
  const post = (url: string, body: unknown) =>
    page.evaluate(async ([url, body]) => (await fetch(url as string, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })).json(), [url, body]);
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  const { id } = await post('/api/saved', { name: 'Sky', query: 'blue_sky', sort: '' });
  await post('/api/relations', { kind: 'parent', action: 'add', from: 'landscape', to: 'favorite_artists' });
  // Last opened long ago: their files all count as new.
  await post('/api/seen', { key: 'saved:' + id, at: 1 });
  await post('/api/seen', { key: 'tag:favorite_artists', at: 1 });
  await page.reload();
  const star = page.getByRole('button', { name: /^Saved searches/ });
  await expect(star.locator('.dot')).toBeVisible();
  await star.click();
  const panel = page.getByRole('dialog', { name: 'Saved searches' });
  await expect(panel.getByRole('button', { name: /Sky/ }).locator('.new-pill')).toHaveText('4 new');
  const group = panel.getByRole('button', { name: /favorite artists|favorite_artists/ });
  await expect(group.locator('.new-pill')).toHaveText('4 new');
  await expect(group).toContainText('1 tag');
  // Opening Sky: its four files are NEW in the grid; its pill is gone, the group's stays (the star's dot too).
  await panel.getByRole('button', { name: /Sky/ }).click();
  await expect(page.getByText(/^4 files/)).toBeVisible();
  await expect(page.locator('.tile .new-badge')).toHaveCount(4);
  await star.click();
  await expect(panel.getByRole('button', { name: /Sky/ }).locator('.new-pill')).toHaveCount(0);
  await expect(group.locator('.new-pill')).toHaveText('4 new');
  // The group: the same, then nothing new anywhere - no dot.
  await group.click();
  await expect(page.locator('.tile .new-badge')).toHaveCount(4);
  await expect(star.locator('.dot')).toHaveCount(0);
  // Another search: the usual NEW files again (none here).
  await page.getByRole('button', { name: 'Remove favorite_artists from the search' }).or(page.getByRole('button', { name: /^Remove .* from the search$/ })).first().click();
  await expect(page.locator('.tile .new-badge')).toHaveCount(0);
  await post('/api/relations', { kind: 'parent', action: 'remove', from: 'landscape', to: 'favorite_artists' });
  await post(`/api/saved/${id}/remove`, {});
});

test('desktop: the search bar is exactly as wide as the thumbnail grid', async ({ page }) => {
  test.skip(isPhone(page), 'desktop');
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  const bar = (await page.getByRole('group', { name: 'Search' }).boundingBox())!;
  const tiles = await page.locator('.tile').evaluateAll((els) => els.map((e) => e.getBoundingClientRect()).map((r) => [r.left, r.right]));
  const left = Math.min(...tiles.map((t) => t[0]));
  const right = Math.max(...tiles.map((t) => t[1]));
  expect(Math.abs(bar.x - left)).toBeLessThan(1);
  expect(Math.abs(bar.x + bar.width - right)).toBeLessThan(1);
});

test('new download (wide screens): option labels above their fields; a long group name is cut short, never over the Subscribe button', async ({ page }) => {
  test.skip(isPhone(page), 'the wide layout (phones stack the options already)');
  await page.setViewportSize({ width: 900, height: 800 });
  const g = await page.evaluate(async () =>
    (await fetch('/api/sub-groups', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ site: 'safebooru', name: 'A very long group name that goes on and on and on' }) })).json(),
  );
  await page.goto('/downloads');
  const form = page.locator('form.add');
  // A schedule and the group (the site the form starts on: safebooru.org).
  await expect(form).toContainText('safebooru.org');
  await form.getByRole('button', { name: 'Repeat' }).click();
  await page.getByRole('option', { name: 'Every hour' }).click();
  await form.getByRole('button', { name: 'Group' }).click();
  await page.getByRole('option', { name: /A very long group name/ }).click();
  const groupBtn = (await form.getByRole('button', { name: 'Group' }).boundingBox())!;
  const go = (await form.getByRole('button', { name: /Subscribe/ }).boundingBox())!;
  expect(groupBtn.x + groupBtn.width).toBeLessThanOrEqual(go.x); // never over the button
  // The labels sit above their fields.
  const label = (await form.locator('.group-opt .opt-label').boundingBox())!;
  expect(label.y + label.height).toBeLessThanOrEqual(groupBtn.y + 1);
  await page.evaluate((id) => fetch(`/api/sub-groups/${id}/remove`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' }), g.id);
});

test('downloads: failed files are listed (post, site, search, why); a retry runs in the queue of its site, then says how it went', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-chromium', 'one run is enough');
  let posts = [
    { site: 'safebooru', post: '111', url: 'https://safebooru.org/index.php?page=post&s=view&id=111', query: 'blue_sky', error: 'store: copy: unexpected EOF', at: Date.now() - 120_000 },
    { site: 'safebooru', post: '1152003385209828810', query: 'blue_sky', error: 'the post has no file URL', at: Date.now() - 7_200_000 },
  ];
  type Job = { site: string; post: string; query: string; started: number; finished?: number; outcome?: string; error?: string };
  let retries: Job[] = [];
  const retried: string[] = [];
  await page.route('**/api/failed-posts', (route) => route.fulfill({ json: { posts } }));
  await page.route('**/api/downloads', async (route) => {
    const json = await (await route.fetch()).json();
    route.fulfill({ json: { ...json, retries } });
  });
  // The server tries it in the background: here it has already finished by the next look.
  await page.route('**/api/failed-posts/retry', async (route) => {
    const { post } = route.request().postDataJSON();
    retried.push(post);
    const job: Job = { site: 'safebooru', post, query: 'blue_sky', started: Date.now(), finished: Date.now() };
    if (post === '111') {
      posts = posts.filter((p) => p.post !== '111');
      job.outcome = 'downloaded';
    } else {
      job.outcome = 'failed';
      job.error = 'still broken';
    }
    retries = [...retries.filter((r) => r.post !== post), job];
    return route.fulfill({ status: 202, json: { started: true } });
  });
  await page.goto('/downloads');
  const card = page.locator('section.card', { has: page.getByRole('heading', { name: 'Failed files' }) });
  const queue = page.locator('section.card', { has: page.getByRole('heading', { name: 'Queue' }) });
  await dlTab(page, 'History');
  // (grouped by why: a connection problem, and something else)
  await expect(card.locator('.reason-block')).toHaveCount(2);
  await expect(card.locator('.stack .item')).toHaveCount(2);
  await unfoldStacks(card);
  await expect(card.getByRole('link', { name: 'Post 111' })).toHaveAttribute('href', /id=111/);
  await expect(card).toContainText('blue_sky');
  await expect(card).toContainText('unexpected EOF');
  await card.getByRole('button', { name: 'Retry post 111' }).click();
  // In the queue with its result (Downloaded), gone from the failed files.
  await dlTab(page, 'Queue');
  await expect(queue.locator('.stack .item', { hasText: 'Post 111' }).locator('.state')).toHaveText('Downloaded');
  await dlTab(page, 'History');
  await expect(card.locator('.stack .item')).toHaveCount(1);
  await card.getByRole('button', { name: 'Retry post 1152003385209828810' }).click(); // a Mastodon-sized ID: sent back exactly (as text)
  await expect(page.getByText(/Post 1152003385209828810 failed again: still broken/)).toBeVisible(); // (an error: it pops up)
  await expect(card.locator('.stack .item')).toHaveCount(1);
  expect(retried).toEqual(['111', '1152003385209828810']);
});

test('"I came" button (off by default): counted in the viewer with Undo; Rankings shows the total, days, files, tags; "See all" sorts the library by count', async ({ page }) => {
  test.skip(isPhone(page), 'keys; the same button on phones');
  const set = (v: boolean) =>
    page.evaluate((v) => fetch('/api/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ cameButton: v }) }), v);
  // Off: no button, no Rankings in the menu.
  await page.goto('/?q=blue_sky');
  await page.locator('.tile[data-i="0"]').click();
  await expect(page.locator('.viewer').getByRole('button', { name: /^I came/ })).toHaveCount(0);
  await page.keyboard.press('Escape');
  await page.getByRole('button', { name: 'Menu' }).click();
  await expect(page.locator('#nav-drawer').getByRole('button', { name: 'I came', exact: true })).toHaveCount(0);
  await page.keyboard.press('Escape');
  // On (Settings > Viewing, shared). Every press is noted, to take them back at the end.
  await set(true);
  const presses: number[] = [];
  page.on('response', async (res) => {
    if (res.url().endsWith('/api/climax') && res.request().method() === 'POST') presses.push((await res.json()).id);
  });
  await page.goto('/?q=blue_sky');
  const id = await page.locator('.tile[data-i="0"]').getAttribute('data-id');
  await page.locator('.tile[data-i="0"]').click();
  const came = page.locator('.viewer').getByRole('button', { name: /^I came/ });
  await came.click();
  await expect(came).toHaveAccessibleName('I came (1 so far)');
  await page.keyboard.press('c');
  await expect(came).toHaveAccessibleName('I came (2 so far)');
  await page.getByRole('button', { name: 'Undo' }).last().click();
  await expect(came).toHaveAccessibleName('I came (1 so far)');
  // The details: the count, with Remove one and Reset.
  await page.keyboard.press('c');
  await page.keyboard.press('i');
  const row = page.locator('.came-row');
  await expect(row).toContainText('2 times');
  await row.getByRole('button', { name: 'Remove one' }).click(); // (an icon button: its label for screen readers)
  await expect(row).toContainText('1 time');
  await expect(came).toHaveAccessibleName('I came (1 so far)');
  await page.keyboard.press('Escape');
  await page.keyboard.press('Escape');
  // Rankings.
  await page.getByRole('button', { name: 'Menu' }).click();
  await page.locator('#nav-drawer').getByRole('button', { name: 'I came', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'I came' })).toBeVisible();
  await expect(page.locator('.tiles .tile').first()).toContainText('1');
  await expect(page.locator('ol.files li')).toHaveCount(1);
  await expect(page.locator('ol.bars').first()).toContainText('blue sky');
  await page.getByRole('radio', { name: 'All time' }).click();
  await expect(page.getByRole('radio', { name: 'All time' })).toHaveAttribute('aria-checked', 'true');
  await page.getByRole('button', { name: 'See all' }).click();
  await expect(page.getByText(/^1 file\b/)).toBeVisible();
  await expect(page.locator('.tile[data-i="0"]')).toHaveAttribute('data-id', id!);
  // Clear all (the I came page): asks first, then nothing counted.
  await page.goto('/rankings');
  await page.getByRole('button', { name: 'Clear all' }).click();
  await page.getByRole('alertdialog').getByRole('button', { name: 'Clear all' }).click();
  await expect(page.getByText('Nothing counted yet')).toBeVisible();
  // Clean up: the presses taken back, the button off again.
  for (const p of presses) await page.evaluate((id) => fetch('/api/climax/undo', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ id }) }), p);
  await set(false);
});

test('desktop: the fast scroller sits at the very right edge of the page (past the grid\'s side gap)', async ({ page }) => {
  test.skip(isPhone(page), 'desktop');
  await page.setViewportSize({ width: 1300, height: 300 }); // a grid over three screens tall: the fast scroller shows
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  const fast = page.locator('.fast');
  await expect(fast).toHaveCount(1);
  const r = await fast.evaluate((el) => el.getBoundingClientRect().right);
  expect(Math.abs(r - 1300)).toBeLessThan(1);
});

test('live updates come back by themselves after a server restart (the stream refused for a while: opened again, the page catches up)', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-chromium', 'one browser is enough');
  let refused = 0;
  let opened = 0;
  await page.route('**/api/events', async (route) => {
    if (refused < 2) {
      refused++;
      return route.fulfill({ status: 502, body: 'Bad Gateway' }); // (the proxy while the server is down)
    }
    opened++;
    return route.continue();
  });
  await page.goto('/downloads');
  // Refused twice - the browser itself gives up after an error answer - then opened again (1 s, 2 s later).
  await expect.poll(() => opened, { timeout: 10_000 }).toBeGreaterThan(0);
  expect(refused).toBe(2);
  // And it works: a change on the server shows without polling's 20 s wait.
  await page.waitForTimeout(500);
  const d = await page.evaluate(async () => (await (await fetch('/api/downloads', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ site: 'test-slow', query: 'live_again', max_posts: 0 }) })).json()));
  await expect(page.locator('.stack .item', { hasText: 'live_again' }).first()).toBeVisible({ timeout: 3000 });
  await page.evaluate((id) => fetch(`/api/downloads/${id}/remove`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' }), d.id);
});

test('the menu scrolls when it is taller than the window', async ({ page }) => {
  await page.setViewportSize({ width: 800, height: 360 }); // too short for every item
  await page.goto('/');
  await page.getByRole('button', { name: 'Menu' }).click();
  const drawer = page.locator('#nav-drawer');
  await expect(drawer).toBeVisible();
  for (const s of ['library', 'tags', 'settings']) await unfold(page, s);
  await page.waitForTimeout(250); // (they slide open)
  expect(await drawer.evaluate((el) => el.scrollHeight > el.clientHeight)).toBe(true);
  const last = drawer.getByRole('button', { name: 'Account' });
  if (isPhone(page)) await drawer.evaluate((el) => el.scrollBy(0, 2000)); // (a finger: no wheel here)
  else {
    await drawer.hover();
    await page.mouse.wheel(0, 2000);
  }
  await expect.poll(() => drawer.evaluate((el) => el.scrollTop)).toBeGreaterThan(0);
  await expect(last).toBeInViewport();
  await last.click();
  await expect(page).toHaveURL(/\/settings\/account/);
});

test('details: pick several tags, then search all of them, any of them, add them to the search, start a new download or blacklist them (the usual menu); a tag menu blacklists it', async ({ page }) => {
  const openPanel = async (q: string) => {
    await page.goto('/?q=' + encodeURIComponent(q));
    await page.locator('.tile[data-i="0"]').click();
    await page.getByRole('button', { name: 'Details', exact: true }).click();
    return page.getByRole('region', { name: 'Tags' });
  };
  // Pressing and holding a tag starts picking with it; taps pick more.
  const hold = async (tag: ReturnType<typeof page.getByRole>) => {
    await tag.waitFor();
    await page.waitForTimeout(500); // (the panel settled: its tags loaded and drawn)
    const b = (await tag.boundingBox())!;
    await page.mouse.move(b.x + b.width / 2, b.y + b.height / 2);
    await page.mouse.down();
    await page.waitForTimeout(650);
    await page.mouse.up();
  };
  const pick = async (tags: ReturnType<typeof page.getByRole>) => {
    await hold(tags.getByRole('button', { name: 'blue_sky', exact: true }));
    await expect(page.getByRole('menu')).toHaveCount(0); // (the hold isn't also a tap: no tag menu)
    await tags.getByRole('button', { name: 'landscape', exact: true }).click();
    await expect(tags.getByText('2 picked')).toBeVisible();
    await expect(tags.getByRole('button', { name: 'blue_sky', exact: true })).toHaveAttribute('aria-pressed', 'true');
  };
  // The bar's Use: the usual menu (a sheet on phones).
  const use = async (tags: ReturnType<typeof page.getByRole>, item: string) => {
    await tags.getByRole('button', { name: 'Use the picked tags' }).click();
    await page.getByRole('menuitem', { name: item }).click();
  };
  // The bar's × ends picking (the tags open their menus again); so does unpicking the last one; a right-click starts it too.
  let tags = await openPanel('blue_sky');
  await pick(tags);
  await tags.getByRole('button', { name: 'landscape', exact: true }).click();
  await tags.getByRole('button', { name: 'blue_sky', exact: true }).click();
  await expect(tags.getByRole('toolbar', { name: 'Picked tags' })).toHaveCount(0);
  await tags.getByRole('button', { name: 'landscape', exact: true }).click({ button: 'right' });
  await expect(tags.getByText('1 picked')).toBeVisible();
  await tags.getByRole('button', { name: 'Stop picking' }).click();
  await expect(tags.getByRole('toolbar', { name: 'Picked tags' })).toHaveCount(0);
  await pick(tags);
  await tags.getByRole('button', { name: 'Stop picking' }).click();
  await expect(tags.getByRole('toolbar', { name: 'Picked tags' })).toHaveCount(0);
  await expect(tags.getByRole('button', { name: 'blue_sky', exact: true })).not.toHaveAttribute('aria-pressed');
  // Any of them: one OR group.
  await pick(tags);
  await use(tags, 'Search any of them');
  await expect.poll(() => new URL(page.url()).searchParams.get('q')).toBe('( blue_sky ~ landscape )');
  // All of them, as a new search.
  tags = await openPanel('blue_sky');
  await pick(tags);
  await use(tags, 'Search all of them');
  await expect.poll(() => new URL(page.url()).searchParams.get('q')).toBe('blue_sky landscape');
  // Added to the current search (blue_sky, already a chip there, is replaced - not doubled).
  tags = await openPanel('system:type=image blue_sky');
  await pick(tags);
  await use(tags, 'Add them to the search');
  await expect.poll(() => new URL(page.url()).searchParams.get('q')).toBe('system:type=image blue_sky landscape');
  // A new download: both, written the site's way (the form is filled in; nothing starts).
  tags = await openPanel('blue_sky');
  await pick(tags);
  await use(tags, 'Download all of them');
  const dlg = newDl(page);
  await dlg.getByRole('button', { name: 'safebooru.org' }).click();
  await expect(dlg.getByRole('combobox', { name: 'Search on the site' })).toHaveValue('blue_sky landscape');
  await page.keyboard.press('Escape');
  // Any of them, as a download: the site's OR search (written its way) - a site without one: a search each.
  tags = await openPanel('blue_sky');
  await pick(tags);
  await use(tags, 'Download any of them');
  await dlg.getByRole('button', { name: 'safebooru.org' }).click();
  await expect(dlg).toContainText('2 searches, one for each combination');
  await dlg.getByRole('button', { name: 'Back' }).click();
  await dlg.getByRole('button', { name: 'test-down.invalid' }).click();
  await expect(dlg.getByRole('combobox', { name: 'Search on the site' })).toHaveValue('( blue_sky ~ landscape )');
  await page.keyboard.press('Escape');

  // The blacklist in a tap (mocked: shared by every test): a tag's menu adds it (Undo takes it off), the picked ones too.
  let black: string[] = [];
  await page.route('**/api/blacklist', async (r) => {
    if (r.request().method() === 'POST') black = r.request().postDataJSON().tags;
    await r.fulfill({ json: { tags: black } });
  });
  tags = await openPanel('blue_sky');
  await tags.getByRole('button', { name: 'blue_sky', exact: true }).click();
  await page.getByRole('menuitem', { name: 'Add to blacklist' }).click();
  await expect(page.getByText("blue_sky won’t be downloaded any more")).toBeVisible();
  expect(black).toEqual(['blue_sky']);
  await page.getByRole('button', { name: 'Undo' }).click();
  await expect.poll(() => black).toEqual([]);
  await pick(tags);
  await use(tags, 'Add them to the blacklist');
  await expect(page.getByText("2 tags won’t be downloaded any more")).toBeVisible();
  expect(black).toEqual(['blue_sky', 'landscape']);
  await expect(tags.getByRole('button', { name: 'landscape, on the download blacklist' })).toBeVisible(); // (marked: a crossed-out eye)
  await tags.getByRole('button', { name: 'landscape, on the download blacklist' }).click();
  await expect(page.getByRole('menuitem', { name: 'Remove from blacklist' })).toBeVisible();
  await page.keyboard.press('Escape');
  await page.unrouteAll({ behavior: 'ignoreErrors' });
});

test('masonry grid: justified rows - tiles in their files\' shapes, the order reading left to right, row by row; opening one still works', async ({ page }) => {
  await page.goto('/settings/viewing');
  await page.getByRole('radio', { name: 'Rows' }).click();
  await expect(page.getByRole('radio', { name: 'Rows' })).toBeChecked();
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  // Varied widths (the files' shapes); each row one height.
  await expect
    .poll(async () => new Set(await page.locator('.tile').evaluateAll((ts) => ts.map((t) => Math.round(t.getBoundingClientRect().width)))).size)
    .toBeGreaterThan(2);
  const tiles = await page.locator('.library .tile').evaluateAll((ts) =>
    ts
      .map((t) => {
        const r = t.getBoundingClientRect();
        return { i: Number((t as HTMLElement).dataset.i), x: r.left, y: Math.round(r.top), w: r.width, h: Math.round(r.height), right: r.right };
      })
      .sort((a, b) => a.i - b.i),
  );
  const rows = new Map<number, typeof tiles>();
  for (const t of tiles) rows.set(t.y, [...(rows.get(t.y) ?? []), t]);
  expect(rows.size).toBeGreaterThan(1);
  for (const row of rows.values()) expect(new Set(row.map((t) => t.h)).size).toBe(1);
  // In order: each next file to the right in the same row, or in the row below.
  for (let k = 1; k < tiles.length; k++) {
    const a = tiles[k - 1];
    const b = tiles[k];
    expect(b.y > a.y || (b.y === a.y && b.x > a.x), `file ${b.i} after file ${a.i}`).toBe(true);
  }
  // Full rows reach the same right edge (the last row may stop short).
  const ys = [...rows.keys()].sort((a, b) => a - b);
  const edges = ys.slice(0, -1).map((y) => Math.round(Math.max(...rows.get(y)!.map((t) => t.right))));
  expect(Math.max(...edges) - Math.min(...edges)).toBeLessThanOrEqual(1);
  // Scrolled down, a tile opens the file it shows.
  await page.locator('#grid-scroller').evaluate((el) => el.scrollBy(0, 400));
  const id = await page.locator('.tile').nth(10).getAttribute('data-i');
  await page.locator(`.library .tile[data-i="${id}"]`).click(); // (by its position: the grid re-renders as it scrolls)
  await expect(page.getByText(`${Number(id) + 1} / 26`)).toBeVisible();
});

test('a new download from a tag: the sites it came from first (most files first), then the others', async ({ page }) => {
  // blue_sky came from two sites (test-slow more often); red from one.
  await page.route('**/api/tags/sites?*', (route) => {
    const tags = new URL(route.request().url()).searchParams.getAll('t');
    const from: Record<string, { site: string; count: number }[]> = {
      blue_sky: [{ site: 'test-slow', count: 3 }, { site: 'test-down', count: 1 }],
      red: [{ site: 'test-down', count: 2 }],
    };
    route.fulfill({ json: { tags: Object.fromEntries(tags.map((t) => [t, from[t] ?? []])) } });
  });
  await page.goto('/?q=blue_sky%20red');
  const chipMenu = page.getByRole('dialog', { name: 'Edit search chip' });
  const dlg = newDl(page);
  await page.locator('.chip-main').nth(0).click();
  await chipMenu.getByRole('button', { name: 'New download' }).click();
  await expect(dlg.getByRole('heading', { name: 'Which site?' })).toBeVisible(); // (straight to the site: the tag is chosen)
  const rows = dlg.locator('.item.site');
  await expect(rows.nth(0)).toContainText('test-slow.invalid');
  await expect(rows.nth(0)).toContainText('3 files');
  await expect(rows.nth(1)).toContainText('test-down.invalid');
  await expect(dlg.getByRole('heading', { name: 'Other sites' })).toBeVisible(); // (safebooru, ...)
  if (!isPhone(page)) await expect(rows.nth(0)).toBeFocused(); // Enter takes the first
  await rows.nth(1).click();
  await expect(dlg.getByRole('combobox', { name: 'Search on the site' })).toHaveValue('blue_sky');
  // Canceled: nothing happens.
  await page.keyboard.press('Escape');
  await expect(dlg).toHaveCount(0);
  await expect(page).not.toHaveURL(/\/downloads/);
});

test('first steps: what you\'ll use (and safe for work), storage, sites (adult ones behind a switch), how it looks, the app, notifications, what never to download, a first download, then the good-to-knows; each skippable', async ({ page }, info) => {
  const shot = async (name: string) => {
    if (process.env.SHOTS) await page.screenshot({ path: `${process.env.SHOTS}/fs-${info.project.name}-${name}.png` });
  };
  try {
    await page.goto('/settings/account');
    await page.getByRole('button', { name: 'Show them again' }).click();
    const next = page.getByRole('button', { name: 'Continue' });
    // What you'll use: Media and Comics, both on to start with.
    await expect(page.getByRole('heading', { name: 'What will you use?', level: 1 })).toBeVisible();
    await expect(page.getByText('Step 1 of 9')).toBeVisible();
    await expect(page.getByRole('checkbox', { name: /^Media/ })).toHaveAttribute('aria-checked', 'true');
    await expect(page.getByRole('checkbox', { name: /^Comics/ })).toHaveAttribute('aria-checked', 'true');
    await expect(page.getByRole('checkbox', { name: /^Safe for work/ })).not.toBeChecked();
    await shot('0-sections');
    await next.click();
    // Storage: the folders and the tools.
    await expect(page.getByRole('heading', { name: 'Storage', level: 1 })).toBeVisible();
    await expect(page.getByText('Step 2 of 9')).toBeVisible();
    await expect(page.getByText('Your files')).toBeVisible();
    await expect(page.getByText('Video tool (ffmpeg)')).toBeVisible();
    await shot('1-storage');
    // Carries on where it was left (a reload, another device).
    await page.reload();
    await expect(page.getByRole('heading', { name: 'Storage', level: 1 })).toBeVisible();
    await expect(page.locator('#splash')).toHaveCount(0); // (the start-up splash goes for it too)
    await next.click();
    // Sites: the plugin list to install from; adult sites only when asked for.
    await expect(page.getByRole('heading', { name: 'Sites', level: 1 })).toBeVisible();
    const extra = page.getByRole('button', { name: 'Install test-extra.invalid' });
    await expect(extra).toHaveCount(0);
    await page.getByRole('checkbox', { name: 'Adult (18+)' }).check();
    await expect(extra).toBeVisible();
    await shot('2-sites');
    await expect(page.getByText('Cloudflare')).toHaveCount(0);
    await next.click();
    // How it looks: square tiles or masonry (this device), and videos' sound.
    await expect(page.getByRole('heading', { name: 'How it looks', level: 1 })).toBeVisible();
    await page.getByRole('radio', { name: /^Rows/ }).click();
    await expect(page.getByRole('radio', { name: /^Rows/ })).toHaveAttribute('aria-checked', 'true');
    await page.getByRole('radio', { name: /^Squares/ }).click();
    await shot('2b-look');
    await next.click();
    await expect(page.getByRole('heading', { name: 'Install the app', level: 1 })).toBeVisible();
    await shot('3-app');
    await next.click();
    await expect(page.getByRole('heading', { name: 'Notifications', level: 1 })).toBeVisible();
    await shot('4-notify');
    // Back goes back.
    await page.getByRole('button', { name: 'Back' }).click();
    await expect(page.getByRole('heading', { name: 'Install the app', level: 1 })).toBeVisible();
    await next.click();
    await next.click();
    // Before downloading: the blacklist (common tags a tap away) and the size limits.
    await expect(page.getByRole('heading', { name: 'Before downloading', level: 1 })).toBeVisible();
    await expect(page.getByRole('button', { name: /watermark/ })).toBeVisible();
    await expect(page.getByRole('spinbutton', { name: 'Skip videos larger than (MB)' })).toBeVisible();
    await shot('4b-rules');
    await next.click();
    await expect(page.getByRole('heading', { name: 'Start your library', level: 1 })).toBeVisible();
    await shot('5-library');
    // The first download: the New download dialog itself (its site, search and every option); closed, the step stays.
    await page.getByRole('button', { name: 'Start a download' }).click();
    const dlg = newDl(page);
    await expect(dlg.getByRole('button', { name: 'test-down.invalid' })).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(dlg).toHaveCount(0);
    await expect(page.getByRole('heading', { name: 'Start your library', level: 1 })).toBeVisible();
    // Done in it: the steps go on (once, the newest 25 - answered here: nothing is downloaded).
    let added: { query: string; max_posts: number } | null = null;
    await page.route('**/api/downloads', (r) => (r.request().method() === 'POST' ? ((added = r.request().postDataJSON()), r.fulfill({ json: { id: 1 } })) : r.continue()));
    await page.getByRole('button', { name: 'Start a download' }).click();
    await dlg.getByRole('button', { name: 'test-down.invalid' }).click();
    const search = dlg.getByRole('combobox', { name: 'Search on the site' });
    await search.fill('landscape');
    await search.press('Enter');
    await dlg.getByRole('button', { name: 'Download', exact: true }).click();
    await expect.poll(() => added).toMatchObject({ query: 'landscape' });
    await expect(page.getByRole('heading', { name: "You're all set", level: 1 })).toBeVisible();
    await page.unroute('**/api/downloads');
    await expect(page.getByText('Your first download is running')).toBeVisible();
    await expect(page.getByText('Daily backups')).toBeVisible();
    await shot('6-done');
    await page.getByRole('button', { name: 'See the download' }).click();
    await expect(page).toHaveURL(/\/downloads/);
    await expect.poll(async () => (await page.evaluate(async () => (await fetch('/api/settings')).json())).welcome).toBe('finished'); // (saved a moment later)
  } finally {
    await page.evaluate(() => fetch('/api/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ welcome: 'finished' }) }));
  }
});

test('the tour after the first steps: hints on the library, one at a time, the thing lit up; Skip ends it for good on this device', async ({ page }) => {
  await page.addInitScript(() => {
    if (!sessionStorage.getItem('toured')) localStorage.setItem('vitrine.tour', 'pending'); // (as the first steps leave it)
  });
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  const card = page.locator('.hint-card');
  await expect(card).toBeVisible();
  await expect(card.getByRole('heading', { name: 'Search' })).toBeVisible();
  await expect(card).toContainText(/1 of [45]/);
  await expect(page.locator('.spot')).toBeVisible();
  await card.getByRole('button', { name: 'Next' }).click();
  await expect(card.getByRole('heading', { name: 'Downloads' })).toBeVisible();
  // Skip: gone, and not again after a reload.
  await card.getByRole('button', { name: 'Skip tour' }).click();
  await expect(card).toHaveCount(0);
  await page.evaluate(() => sessionStorage.setItem('toured', '1'));
  await page.reload();
  await expect(page.getByText(/^26 files/)).toBeVisible();
  await page.waitForTimeout(900);
  await expect(card).toHaveCount(0);
  expect(await page.evaluate(() => localStorage.getItem('vitrine.tour'))).toBe('done');
});

test('phones: Back closes what is open on top (the menu, a chip, the details) and stays on the page; closed by a tap, Back leaves as usual', async ({ page }) => {
  test.skip(!isPhone(page), 'touch screens (Android back button / gesture)');
  await page.goto('/settings/viewing');
  await page.goto('/?q=blue_sky');
  await expect(page.getByText(/^4 files/)).toBeVisible();
  // The menu.
  await page.getByRole('button', { name: 'Menu' }).click();
  const drawer = page.locator('#nav-drawer');
  await expect(drawer).toBeVisible();
  await page.goBack();
  await expect(drawer).toHaveCount(0);
  await expect(page).toHaveURL(/q=blue_sky/);
  // A chip's editor.
  await page.locator('.chip-main').first().click();
  const chip = page.getByRole('dialog', { name: 'Edit search chip' });
  await expect(chip).toBeVisible();
  await page.goBack();
  await expect(chip).toHaveCount(0);
  await expect(page).toHaveURL(/q=blue_sky/);
  // The viewer's details over the viewer: Back closes the details, then the viewer.
  await page.locator('.tile').first().click();
  await expect(page.locator('.viewer')).toBeVisible();
  await page.getByRole('button', { name: 'Details', exact: true }).click();
  await expect(page.getByRole('complementary', { name: 'Details' })).toBeVisible();
  await page.goBack();
  await expect(page.getByRole('complementary', { name: 'Details' })).toHaveCount(0);
  await expect(page.locator('.viewer')).toBeVisible();
  await page.goBack();
  await expect(page.locator('.viewer')).toHaveCount(0);
  // Closed by a tap (no entry left behind): Back goes to the page before.
  await page.getByRole('button', { name: 'Menu' }).click();
  await expect(drawer).toBeVisible();
  await page.locator('.backdrop').click({ position: { x: 380, y: 400 } });
  await expect(drawer).toHaveCount(0);
  await page.waitForTimeout(100);
  await page.goBack();
  await expect(page).toHaveURL(/\/settings\/viewing/);
});

test('menu: items with sub-pages fold (folded to begin with); the arrow unfolds them, and it is remembered', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'Menu' }).click();
  const nav = page.locator('#nav-drawer');
  const arrow = nav.locator('.fold[aria-describedby="menu-settings"]');
  await expect(arrow).toHaveAttribute('aria-expanded', 'false');
  await expect(nav.getByRole('button', { name: 'Backups' })).toHaveCount(0);
  await arrow.click();
  await expect(arrow).toHaveAttribute('aria-expanded', 'true');
  await expect(nav.getByRole('button', { name: 'Backups' })).toBeVisible();
  await expect(page).not.toHaveURL(/settings/); // the arrow only unfolds
  await page.reload();
  await page.getByRole('button', { name: 'Menu' }).click();
  await expect(nav.getByRole('button', { name: 'Backups' })).toBeVisible(); // remembered on this device
  await arrow.click();
  await expect(nav.getByRole('button', { name: 'Backups' })).toHaveCount(0);
});

test('new download: My favorites (a heart in the search box) fills in fav:me where the login says who you are', async ({ page }) => {
  await page.route('**/api/sites', async (route) => {
    const r = await route.fetch();
    const b = await r.json();
    for (const s of b.sites) if (s.id === 'test-down') s.fav_me = true;
    await route.fulfill({ response: r, json: b });
  });
  await page.goto('/downloads');
  const site = page.getByRole('button', { name: 'Site', exact: true });
  await site.click();
  await page.getByRole('menuitemradio', { name: 'test-slow.invalid' }).click();
  await expect(page.getByRole('button', { name: 'My favorites' })).toHaveCount(0); // no user ID saved
  await site.click();
  await page.getByRole('menuitemradio', { name: 'test-down.invalid' }).click();
  await expect(page.getByRole('checkbox', { name: 'Add to my favorites' })).toHaveCount(0); // (only for your own favorites)
  await page.getByRole('button', { name: 'My favorites' }).click();
  await expect(page.getByRole('combobox', { name: 'Search on the site' })).toHaveValue('fav:me');
  // They can become favorites here too (on to begin with), sent with the download.
  const add = page.getByRole('checkbox', { name: 'Add to my favorites' });
  await expect(add).toBeChecked();
  const sent = page.waitForRequest((r) => r.url().endsWith('/api/downloads') && r.method() === 'POST');
  await page.getByRole('button', { name: 'Download', exact: true }).click();
  expect((await sent).postDataJSON()).toMatchObject({ query: 'fav:me', favorite: true });
  // A subscription to favorites goes in the site's own Favorites group: no group to pick.
  await expect(page.getByRole('combobox', { name: 'Search on the site' })).toHaveValue(''); // (the download went: the box empties)
  await page.getByRole('combobox', { name: 'Search on the site' }).fill('fav:me');
  await page.getByRole('button', { name: 'Repeat' }).click();
  await page.getByRole('option', { name: 'Every day' }).click();
  await expect(page.getByRole('button', { name: 'Group', exact: true })).toHaveCount(0);
  await page.getByRole('combobox', { name: 'Search on the site' }).fill('blue_sky');
  await expect(page.getByRole('button', { name: 'Group', exact: true })).toBeVisible();
});

test('languages: Settings > Viewing picks one for every device; English (UK) spells it the British way', async ({ page }) => {
  await page.goto('/settings/viewing');
  const picker = page.getByRole('button', { name: 'Language', exact: true });
  await expect(picker).toContainText('Automatic: English (US)'); // the browser's language
  try {
    await picker.click();
    await page.getByRole('menuitemradio', { name: 'English (UK)' }).click();
    await expect(page.locator('html')).toHaveAttribute('lang', 'en-GB');
    await page.goto('/');
    await page.getByRole('button', { name: 'Menu' }).click();
    await unfold(page, 'library');
    await expect(page.locator('#nav-drawer').getByRole('button', { name: 'Favourites' })).toBeVisible();
    await page.keyboard.press('Escape');
    // Shared: kept on the server (another device gets it too).
    const shared = await page.evaluate(async () => (await (await fetch('/api/settings')).json()).language);
    expect(shared).toBe('en-GB');
  } finally {
    // Back to Automatic for the other tests (the setting is shared).
    await page.evaluate(() =>
      fetch('/api/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ language: 'auto' }) }),
    );
  }
  await page.goto('/settings/viewing');
  await expect(page.locator('html')).toHaveAttribute('lang', 'en-US');
});

test('hover zoom: resting again on a tile still shrinking back opens the card in the same place (no jump to the side)', async ({ page }) => {
  test.skip(isPhone(page), 'mouse only');
  await page.goto('/');
  const tile = page.locator('.tile').first(); // at the edge: its card is pushed inside the grid
  await expect(tile).toBeVisible();
  const tb = (await tile.boundingBox())!;
  const card = page.locator('.tile.zoomed');
  // Grown: until the card stops moving (a fixed wait measured it mid-grow on a busy machine).
  const settled = async () => {
    await expect(card).toHaveCount(1);
    let last = '';
    await expect
      .poll(async () => {
        const b = await card.boundingBox();
        const now = b ? [b.x, b.y, b.width, b.height].map(Math.round).join(',') : '';
        const still = now !== '' && now === last;
        last = now;
        return still;
      }, { intervals: [120] })
      .toBe(true);
    return (await card.boundingBox())!;
  };
  await page.mouse.move(tb.x + tb.width / 2, tb.y + tb.height / 2);
  const first = await settled();
  // Off it and straight back: the tile is mid-way through shrinking when the next card opens.
  // (off the grid - up in the top bar: "off" on the tile below opened that tile's card when a busy machine stretched the
  // moment out)
  await page.mouse.move(tb.x + tb.width / 2, 4);
  await page.waitForTimeout(60);
  await page.mouse.move(tb.x + tb.width / 2, tb.y + tb.height / 2);
  const again = await settled();
  expect(Math.abs(again.x - first.x)).toBeLessThan(2);
  expect(Math.abs(again.y - first.y)).toBeLessThan(2);
});

test('hover zoom: the card never gets smaller than its tile either way while it grows (a wide picture\'s card came out shorter: the picture shrank inside a growing card - a wobble)', async ({ page }) => {
  test.skip(isPhone(page), 'mouse only');
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  // The widest picture among the first tiles (its card is wide).
  const tiles = page.locator('.tile:not(:has(.badge))');
  await expect(tiles.first()).toBeVisible();
  const n = Math.min(8, await tiles.count());
  let pick = 0;
  let widest = 0;
  for (let k = 0; k < n; k++) {
    const label = (await tiles.nth(k).getAttribute('aria-label')) ?? '';
    const m = label.match(/(\d+)×(\d+)/);
    if (m && +m[1] / +m[2] > widest) {
      widest = +m[1] / +m[2];
      pick = k;
    }
  }
  expect(widest).toBeGreaterThan(1.5);
  const tile = tiles.nth(pick);
  const tb = (await tile.boundingBox())!;
  await page.evaluate(() => {
    const w = window as any;
    w.sizes = [];
    const tick = () => {
      const z = document.querySelector('.tile.zoomed');
      if (z) {
        const r = z.getBoundingClientRect();
        w.sizes.push([r.width, r.height]);
      }
      if (w.sizes.length < 40) requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  });
  await page.mouse.move(tb.x + tb.width / 2, tb.y + tb.height / 2);
  await expect(page.locator('.tile.zoomed')).toHaveCount(1);
  await page.waitForTimeout(600);
  const sizes: [number, number][] = await page.evaluate(() => (window as any).sizes);
  expect(sizes.length).toBeGreaterThan(5);
  for (const [w, h] of sizes) {
    expect(w).toBeGreaterThanOrEqual(tb.width - 1);
    expect(h).toBeGreaterThanOrEqual(tb.height - 1);
  }
});

test('grid: switching between Comics and the library, the tiles appear (the same motion as any new tiles), not just show', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  await expect(page.locator('.library .tile img.in').first()).toBeVisible();
  const appearing = (scope: string) =>
    page.evaluate(
      (scope) =>
        document.getAnimations().filter((a) => {
          const t = (a.effect as KeyframeEffect | null)?.target as Element | null;
          return !!t?.matches?.(`${scope} .tile`) && a.playState === 'running';
        }).length,
      scope,
    );
  const go = async (name: string) => {
    await page.getByRole('button', { name: 'Menu' }).click();
    await page.locator('#nav-drawer').getByRole('button', { name, exact: true }).click();
  };
  // (the app's first results just show)
  expect(await appearing('.library')).toBe(0);
  // Comics, opened for the first time: its tiles appear.
  await go('Comics');
  await expect(page.locator('.comics .tile').first()).toBeVisible();
  await expect.poll(() => appearing('.comics')).toBeGreaterThan(0);
  // Back to the library: its tiles appear again.
  await page.waitForTimeout(700);
  await go('Media');
  await expect.poll(() => appearing('.library')).toBeGreaterThan(0);
});

test('hover zoom: a video opened from its playing card carries on in the viewer from where the card was; the card\'s copy is let go of at once', async ({ page }) => {
  test.skip(isPhone(page), 'mouse only');
  await page.goto('/');
  const tile = page.locator('.tile[aria-label$="1280×720"]').first(); // the landscape video (15 s)
  await expect(tile).toBeVisible();
  const tb = (await tile.boundingBox())!;
  await page.mouse.move(tb.x + tb.width / 2, tb.y + tb.height / 2);
  const card = page.locator('.tile.zoomed');
  await expect(card.locator('.zoom-media')).toHaveClass(/\bready\b/);
  await page.waitForTimeout(1500); // (playing a while)
  const at = await card.locator('video').evaluate((v: HTMLVideoElement) => v.currentTime);
  expect(at).toBeGreaterThan(0.5);
  const cardVideo = await card.locator('video').elementHandle();
  await card.click();
  // The card's copy: let go of (no file, paused) - not decoding beside the viewer's.
  expect(await cardVideo!.evaluate((v: HTMLVideoElement) => v.paused && !v.getAttribute('src'))).toBe(true);
  // The viewer's: from about where the card was (not from the start).
  const viewerVideo = page.locator('.viewer video.shown').first();
  await expect(viewerVideo).toHaveCount(1);
  await expect.poll(() => viewerVideo.evaluate((v: HTMLVideoElement) => v.currentTime)).toBeGreaterThan(at - 0.3);
});

test('hover zoom: a mouse sweeping across tiles opens no card on the way, only on the tile it arrives at (no cards popping out and back)', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop-chromium', 'mouse; timing-sensitive (a busy machine spreads the moves out: a sweep looks slow) - one engine is enough');
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  const a = (await page.locator('.library .tile[data-i="0"]').boundingBox())!;
  const c = (await page.locator('.library .tile[data-i="2"]').boundingBox())!;
  await page.mouse.move(a.x + a.width / 2, a.y + a.height / 2 + 200); // (below the first row: nothing open)
  await page.mouse.move(a.x + a.width / 2, a.y + a.height / 2);
  await page.evaluate(() => {
    const w = window as any;
    w.opened = new Set();
    const tick = () => {
      for (const z of document.querySelectorAll<HTMLElement>('.tile.zoomed')) w.opened.add(z.dataset.i);
      requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  });
  await page.mouse.move(c.x + c.width / 2, c.y + c.height / 2, { steps: 20 }); // a quick sweep over tile 1
  await expect(page.locator('.tile.zoomed[data-i="2"]')).toHaveCount(1); // arrived: open
  expect(await page.evaluate(() => [...(window as any).opened])).not.toContain('1'); // passed over: never
});

test('hover zoom: while the grid still scrolls (a wheel\'s glide) no card opens; once it\'s still, the tile under the mouse opens - never opened and closed in the glide', async ({ page }) => {
  test.skip(isPhone(page), 'mouse only');
  await page.setViewportSize({ width: 1000, height: 450 }); // (it scrolls)
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  await page.mouse.move(2, 2); // (off the grid: nothing open)
  await page.evaluate(() => {
    const w = window as any;
    w.seen = { zoomedWhileScrolling: false, shrank: false };
    w.scrolling = true;
    const sc = document.getElementById('grid-scroller')!;
    const t0 = performance.now();
    const step = () => {
      if (performance.now() - t0 < 400) {
        sc.scrollTop += 3; // (a glide: small steps every frame)
        if (document.querySelector('.tile.zoomed')) w.seen.zoomedWhileScrolling = true;
        requestAnimationFrame(step);
      } else w.scrolling = false;
    };
    requestAnimationFrame(step);
    const watch = () => {
      if (document.querySelector('.tile.shrinking')) w.seen.shrank = true;
      requestAnimationFrame(watch);
    };
    requestAnimationFrame(watch);
  });
  await page.waitForTimeout(80);
  await page.mouse.move(520, 310, { steps: 3 }); // onto a tile, mid-glide
  await expect.poll(() => page.evaluate(() => (window as any).scrolling)).toBe(false);
  const card = page.locator('.tile.zoomed');
  await expect(card).toHaveCount(1); // once still: the tile under the mouse
  const cb = (await card.boundingBox())!;
  expect(520 >= cb.x && 520 <= cb.x + cb.width && 310 >= cb.y && 310 <= cb.y + cb.height).toBe(true);
  expect(await page.evaluate(() => (window as any).seen)).toEqual({ zoomedWhileScrolling: false, shrank: false });
});

test('hover zoom: a list change elsewhere while a card is open leaves the card alone (it grew by card/tile more, then back)', async ({ page }) => {
  test.skip(isPhone(page), 'mouse only');
  let changed = false;
  await page.route('**/api/search?*', async (route) => {
    const res = await route.fetch();
    const body = await res.json();
    if (changed) body.ids = [...body.ids.slice(0, -2), body.ids.at(-1), body.ids.at(-2)]; // (the last two swap)
    await route.fulfill({ response: res, json: body });
  });
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  const tb = (await page.locator('.library .tile[data-i="3"]').boundingBox())!;
  await page.mouse.move(tb.x + tb.width / 2, tb.y + tb.height / 2);
  const card = page.locator('.tile.zoomed[data-i="3"]');
  await expect(card).toHaveCount(1);
  await page.waitForTimeout(500); // (grown)
  const open = (await card.boundingBox())!;
  await page.evaluate(() => {
    const w = window as any;
    w.widest = 0;
    const tick = () => {
      const z = document.querySelector('.tile.zoomed');
      if (z) w.widest = Math.max(w.widest, z.getBoundingClientRect().width);
      requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  });
  // A refresh of the list (the files arriving check, say) - a change away from the card.
  changed = true;
  await page.route('**/api/new?*', (route) => route.fulfill({ json: { new: 2 } }));
  const searched = page.waitForResponse((r) => r.url().includes('/api/search?'));
  await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')));
  await searched;
  await page.waitForTimeout(600);
  await expect(card).toHaveCount(1); // (still open: its file didn't move)
  expect(await page.evaluate(() => (window as any).widest)).toBeLessThanOrEqual(open.width + 1);
});

test('grid: the width stays the same whether the list scrolls or not - no browser scrollbar coming and going (the tiles shifted sideways after a long list gave way to a short one)', async ({ page }) => {
  test.skip(isPhone(page), 'desktop scrollbars (phones overlay theirs)');
  await page.setViewportSize({ width: 1100, height: 520 }); // (26 files scroll a little)
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  const grid = page.locator('#grid-scroller');
  const scrolls = () => grid.evaluate((el) => el.scrollHeight > el.clientHeight + 1);
  // (headless browsers overlay their scrollbars - no width to lose - so: the browser's own is off, both ways)
  const at = async () => ({
    w: await grid.evaluate((el) => el.clientWidth),
    x: Math.round((await page.locator('.library .tile[data-i="0"]').boundingBox())!.x),
    bar: await grid.evaluate((el) => getComputedStyle(el).scrollbarWidth),
  });
  expect(await scrolls()).toBe(true);
  const long = await at();
  expect(long.bar).toBe('none');
  await page.goto('/?q=system:type%3Dvideo'); // (a short list: no scrolling)
  await expect(page.locator('.library .tile').first()).toBeVisible();
  expect(await scrolls()).toBe(false);
  await page.waitForTimeout(500);
  expect(await at()).toEqual(long);
});

test('hover zoom: a hand drifting a few px while the card grows doesn\'t close it (it used to shrink and grow again: a wobble)', async ({ page }) => {
  test.skip(isPhone(page), 'mouse only');
  await page.goto('/');
  const tile = page.locator('.tile').first();
  await expect(tile).toBeVisible();
  const tb = (await tile.boundingBox())!;
  const cx = tb.x + tb.width / 2;
  const cy = tb.y + tb.height / 2;
  await page.mouse.move(cx, cy);
  await expect(page.locator('.tile.zoomed')).toHaveCount(1); // (growing)
  await page.evaluate(() => {
    (window as any).shrank = false;
    const watch = () => {
      if (document.querySelector('.tile.shrinking')) (window as any).shrank = true;
      if (document.querySelector('.tile.zoomed')) requestAnimationFrame(watch);
    };
    requestAnimationFrame(watch);
  });
  await page.mouse.move(cx + 5, cy, { steps: 2 });
  await page.waitForTimeout(500);
  expect(await page.evaluate(() => (window as any).shrank)).toBe(false);
  await expect(page.locator('.tile.zoomed')).toHaveCount(1);
});

test('hover zoom: with "Play videos on hover" off the cards still open - a video\'s just doesn\'t play (the setting once turned every card off)', async ({ page }) => {
  test.skip(isPhone(page), 'mouse only');
  const set = (v: boolean) => page.evaluate((v) => fetch('/api/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ hoverPreview: v }) }), v);
  await page.goto('/');
  await set(false);
  try {
    await page.reload();
    for (const sel of ['.tile:has(.badge)', '.tile:not(:has(.badge))']) {
      const tile = page.locator(sel).first();
      await expect(tile).toBeVisible();
      await page.mouse.move(2, 2);
      await expect(page.locator('.tile.zoomed')).toHaveCount(0);
      const tb = (await tile.boundingBox())!;
      await page.mouse.move(tb.x + tb.width / 2, tb.y + tb.height / 2);
      await expect(page.locator('.tile.zoomed')).toHaveCount(1);
    }
    await page.mouse.move(2, 2);
    const video = page.locator('.tile:has(.badge)').first();
    const vb = (await video.boundingBox())!;
    await page.mouse.move(vb.x + vb.width / 2, vb.y + vb.height / 2);
    await expect(page.locator('.tile.zoomed')).toHaveCount(1);
    await page.waitForTimeout(800);
    await expect(page.locator('.tile.zoomed video')).toHaveCount(0);
  } finally {
    await set(true);
  }
});

test('hover zoom: a card waits while the tiles slide (the first row\'s gaps closing after a scroll to the top) - it never jumps to the side, then grows', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop-chromium', 'mouse; slowed down through Chromium (CDP)');
  await page.setViewportSize({ width: 1000, height: 450 }); // (it scrolls)
  const arrive = await arriveTwo(page);
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  const grid = page.locator('#grid-scroller');
  await grid.evaluate((el) => (el.scrollTop = 400));
  await page.waitForTimeout(300);
  await arrive(); // (scrolled down: they wait behind gaps in the first row)
  await page.mouse.move(400, 250);
  await page.evaluate(() => {
    const w = window as any;
    w.zoomedWhileSliding = false;
    const tick = () => {
      const sliding = document.getAnimations().some((a) => a.playState === 'running' && !(a instanceof CSSTransition) && ((a.effect as KeyframeEffect)?.target as Element)?.classList.contains('tile'));
      if (sliding && document.querySelector('.tile.zoomed')) w.zoomedWhileSliding = true;
      requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  });
  // (animations at a quarter speed: the slide outlasts the card's wait, as it can with a slow machine or scroll)
  const cdp = await page.context().newCDPSession(page);
  await cdp.send('Animation.enable');
  await cdp.send('Animation.setPlaybackRate', { playbackRate: 0.25 });
  await page.mouse.wheel(0, -500); // (to the top: the gaps close, the tiles slide along)
  await page.waitForTimeout(40);
  await page.mouse.move(406, 250, { steps: 2 }); // a twitch mid-slide
  const card = page.locator('.tile.zoomed');
  await expect(card).toHaveCount(1); // (once the tiles are in place)
  // Grown around the tile now under the mouse (where it's drawn), not elsewhere.
  const id = await card.getAttribute('data-id');
  const cb = (await card.boundingBox())!;
  await page.waitForTimeout(500);
  expect(await page.evaluate(() => (window as any).zoomedWhileSliding)).toBe(false);
  expect(Math.abs(cb.x + cb.width / 2 - 406)).toBeLessThan(120);
  await page.mouse.move(2, 2);
  await expect(card).toHaveCount(0);
  await expect(page.locator('.tile.shrinking')).toHaveCount(0);
  const under = await page.evaluate(() => document.elementFromPoint(406, 250)?.closest<HTMLElement>('.tile')?.dataset.id);
  expect(under).toBe(id);
});

test('hover zoom: files arriving under a resting mouse never set a hidden copy of the card\'s video playing with sound (the grid\'s move animation copies the tiles)', async ({ page }) => {
  test.skip(isPhone(page), 'mouse only');
  // Every video the page copies, and whether one ever plays (off the page: sound with nothing to see or stop).
  await page.addInitScript(() => {
    const w = window as any;
    w.copied = [];
    w.copyPlayed = false;
    const clone = Node.prototype.cloneNode;
    Node.prototype.cloneNode = function (deep?: boolean) {
      const c = clone.call(this, deep) as Element;
      if (c.querySelectorAll) w.copied.push(...c.querySelectorAll('video'));
      return c;
    };
    setInterval(() => {
      for (const v of w.copied) if (!v.paused) w.copyPlayed = true;
    }, 50);
  });
  const arrive = await arriveTwo(page);
  await page.goto('/');
  await page.evaluate(() => localStorage.setItem('vitrine.gridLayout', JSON.stringify('masonry'))); // (its arrivals animate by copies)
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  const tile = page.locator('.tile:has(.badge)').first();
  const b = (await tile.boundingBox())!;
  await page.mouse.move(b.x + b.width / 2, b.y + b.height / 2);
  await expect(page.locator('.tile.zoomed video')).toHaveCount(1);
  await page.waitForTimeout(600); // (playing, muted)
  await arrive();
  await page.waitForTimeout(1200);
  expect(await page.evaluate(() => (window as any).copyPlayed)).toBe(false);
  // (and nothing on the page plays with sound)
  expect(await page.evaluate(() => [...document.querySelectorAll('video')].some((v) => !v.paused && !v.muted))).toBe(false);
  await page.evaluate(() => localStorage.removeItem('vitrine.gridLayout'));
});

test('hover zoom: files arriving while a card is open close it - it never slides off with the tiles or shows another file', async ({ page }) => {
  test.skip(isPhone(page), 'mouse only');
  const arrive = await arriveTwo(page);
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  const tile = page.locator('.tile[data-i="3"]');
  const id = await tile.getAttribute('data-id');
  const tb = (await tile.boundingBox())!;
  await page.mouse.move(tb.x + tb.width / 2, tb.y + tb.height / 2);
  const card = page.locator('.tile.zoomed');
  await expect(card).toHaveCount(1);
  expect(await card.getAttribute('data-id')).toBe(id);
  await arrive(); // at the top: every tile moves two places along
  await expect(page.locator('.tile[data-i="0"]')).not.toHaveAttribute('data-id', String(await page.locator('.tile[data-i="2"]').getAttribute('data-id')));
  await expect(card).toHaveCount(0);
});

// Subscriptions on the test site: its section unfolded (sites start folded). With one site its header shows only
// while the site is paused - the test site never answers, so that comes and goes: tried until the cards show.
async function unfoldTestSite(page: Page) {
  await expect(async () => {
    const unfold = page.getByRole('button', { name: 'Unfold test-down.invalid' });
    if (await unfold.count()) await unfold.click({ timeout: 1000 });
    await expect(page.locator('li[data-sub]').first()).toBeVisible({ timeout: 1000 });
  }).toPass({ timeout: 15000 });
}

test('downloads: a subscription to an OR search shows the group as one chip - "( a ~ b )" and "~a ~b" both', async ({ page }) => {
  await page.goto('/');
  const p = test.info().project.name.replace(/\W/g, '_');
  const make = (q: string) =>
    page.evaluate(
      (q) => fetch('/api/subscriptions', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ site: 'test-down', query: q, interval: 86400, initial_max: 0 }) }).then((r) => r.json()),
      q,
    ) as Promise<{ id: number }>;
  const bracket = await make(`( or_a_${p} ~ or_b_${p} ) or_c_${p}`);
  const tilde = await make(`~or_x_${p} or_z_${p} ~or_y_${p}`);
  await page.goto('/downloads');
  await page.getByRole('tab', { name: /Subscriptions/ }).click();
  await unfoldTestSite(page);
  const chips = (id: number) => page.locator(`li[data-sub="${id}"] .qchip`);
  await expect(chips(bracket.id)).toHaveText([`or_a_${p}ORor_b_${p}`, `or_c_${p}`]);
  await expect(chips(bracket.id).first().locator('.or')).toHaveCount(1);
  await expect(chips(tilde.id)).toHaveText([`or_x_${p}ORor_y_${p}`, `or_z_${p}`]);
  // (the subscriptions go again: the test server is shared - another test counts the ungrouped ones)
  for (const s of [bracket, tilde]) await page.evaluate((id) => fetch(`/api/subscriptions/${id}/remove`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' }), s.id);
});

test('downloads: subscriptions as cards - health, the search as chips, the next check, the newest files, "+N" new opens them; phones swipe for actions', async ({ page }) => {
  await page.goto('/');
  const q = `cards_sky_${test.info().project.name} -cards_cloud`;
  const sub = await page.evaluate(
    (q) => fetch('/api/subscriptions', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ site: 'test-down', query: q, interval: 86400, initial_max: 0 }) }).then((r) => r.json()),
    q,
  );
  const files = await page.evaluate(async () => {
    const ids = (await (await fetch('/api/search?q=system:type%3Dimage&limit=4')).json()).ids as number[];
    return (await (await fetch('/api/files?ids=' + ids.join(','))).json()).files as { id: number; thumb: string; width: number; height: number }[];
  });
  await page.route('**/api/subscriptions/looks?*', (r) =>
    r.fulfill({ json: { looks: { [sub.id]: { recent: files.map((f) => ({ id: f.id, thumb: f.thumb, w: f.width, h: f.height })), fresh: 3 } } } }),
  );
  let checked = false;
  await page.route(`**/api/subscriptions/${sub.id}/check`, (r) => ((checked = true), r.fulfill({ json: { ok: true } })));
  await page.goto('/downloads');
  await page.getByRole('tab', { name: /Subscriptions/ }).click();
  await unfoldTestSite(page);
  const card = page.locator(`li[data-sub="${sub.id}"]`);
  await expect(card.locator('.health')).toBeVisible();
  await expect(card.locator('.qchip').first()).toHaveText(`cards_sky_${test.info().project.name}`);
  await expect(card.locator('.qchip.neg')).toHaveText('cards_cloud'); // (excluded: struck through)
  await expect(card.locator('.ring')).toBeVisible();
  // The health dot in the very middle of the ring.
  const off = await card.locator('.status').evaluate((el) => {
    const d = el.querySelector('.health')!.getBoundingClientRect(); // (both at once: the tab may still be sliding in)
    const r = el.querySelector('.ring svg')!.getBoundingClientRect();
    return Math.max(Math.abs(d.x + d.width / 2 - (r.x + r.width / 2)), Math.abs(d.y + d.height / 2 - (r.y + r.height / 2)));
  });
  expect(off).toBeLessThanOrEqual(0.5);
  await expect(card.locator('.pics img')).toHaveCount(4); // its newest files
  await expect(card.locator('.facts')).toContainText('Every day');
  // Phones: a swipe to the left shows Check now (and Pause).
  if (isPhone(page)) {
    const body = card.locator('.card-body');
    const box = (await body.boundingBox())!;
    await body.evaluate((el, b) => {
      const fire = (type: string, x: number) =>
        el.dispatchEvent(new PointerEvent(type, { bubbles: true, pointerId: 7, pointerType: 'touch', clientX: x, clientY: b.y + b.height / 2 }));
      fire('pointerdown', b.x + b.width - 20);
      for (let k = 1; k <= 8; k++) fire('pointermove', b.x + b.width - 20 - k * 25);
      fire('pointerup', b.x + b.width - 220);
    }, box);
    await card.getByRole('button', { name: 'Check now' }).click();
    await expect.poll(() => checked).toBe(true);
  }
  // "+3": those files, as a library search (found by this subscription).
  await card.getByRole('button', { name: /3 new files since you last looked/ }).click();
  await expect(page).toHaveURL(new RegExp(`found_by(%3D|=)${sub.id}`));
  await expect(page.locator('.bar .chip').first()).toContainText('found by “cards_sky'); // (named by its search)
  await page.evaluate((id) => fetch(`/api/subscriptions/${id}/remove`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' }), sub.id);
});

test('downloads: a gallery subscription shows its title, not its link (the link on hover)', async ({ page }) => {
  test.skip(test.info().project.name !== 'desktop-chromium', 'one run is enough: it subscribes on the server');
  await page.goto('/');
  const link = 'https://127.0.0.1:1/g/777/abcdef0123/';
  const res = await page.evaluate(
    (link) => fetch('/api/subscriptions', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ site: 'test-down', query: link, interval: 86400, initial_max: 0, label: 'A Labeled Comic' }) }).then(async (r) => ({ status: r.status, id: (await r.json()).id as number })),
    link,
  );
  expect(res.status).toBe(200);
  await page.goto('/downloads');
  await page.getByRole('tab', { name: /Subscriptions/ }).click();
  const row = page.locator('li[data-sub] .query', { hasText: 'A Labeled Comic' }).first();
  await expect(row).toBeVisible();
  await expect(row).toHaveAttribute('title', link);
  await expect(page.getByText(link, { exact: true })).toHaveCount(0);
  // (gone again: a later test on this server - or this one run again - subscribes from scratch)
  await page.evaluate((id) => fetch('/api/subscriptions/' + id + '/remove', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' }), res.id);
});

test('comics: Browse - a new search calls off the one on its way (its request ends; the new one doesn\'t wait for it)', async ({ page }) => {
  const gallery = (id: number, title: string) => ({ id, token: 'abcdef0123', url: `https://g.test/g/${id}/abcdef0123/`, title, category: 'Manga', pages: 10, tags: [] });
  await page.route('**/api/browse/sites', (r) => r.fulfill({ json: { sites: ['gallery-site'], sorts: { 'gallery-site': [] } } }));
  await page.route('**/api/browse?*', async (r) => {
    const q = new URL(r.request().url()).searchParams.get('q') ?? '';
    const quick = q.includes('quick'); // (the second search adds its tag to the first's)
    if (!quick) await new Promise((res) => setTimeout(res, 4000)); // (a site taking its time)
    await r.fulfill({ json: { galleries: [gallery(quick ? 1 : 2, quick ? 'Quick result' : 'Slow result')], total: 1, next: 0 } }).catch(() => {});
  });
  const failed: string[] = [];
  page.on('requestfailed', (req) => req.url().includes('/api/browse?') && failed.push(new URL(req.url()).searchParams.get('q') ?? ''));
  await page.addInitScript(() => localStorage.removeItem('vitrine.browseSite'));
  await page.goto('/browse');
  const box = page.locator('.browse').getByRole('combobox', { name: 'Search' });
  await box.fill('slow_tag');
  await box.press('Enter');
  await page.waitForTimeout(300);
  const t0 = Date.now();
  await box.fill('quick_tag');
  await box.press('Enter');
  await expect(page.locator('.browse .tile', { hasText: 'Quick result' })).toBeVisible();
  expect(Date.now() - t0).toBeLessThan(2500); // (not after the slow one's 4 s)
  await expect.poll(() => failed.some((q) => q.includes('slow') && !q.includes('quick'))).toBe(true); // its request called off
  await page.waitForTimeout(4200);
  await expect(page.locator('.browse .tile', { hasText: 'Slow result' })).toHaveCount(0); // and never shown
});

test('comics: Browse - a gallery site\'s galleries as covers (marked when in the library or downloading), more as it scrolls; a preview with tags and page thumbnails; Download, a tag\'s search or subscription', async ({ page }) => {
  // A made-up site (the test server has no gallery site): its answers mocked.
  const now = Date.now();
  const gallery = (id: number, extra: object = {}) => ({
    id, token: 'abcdef0123', url: `https://gallery.test/g/${id}/abcdef0123/`, title: `Some Comic ${id}`, category: 'Manga', pages: 24, posted: now - id * 3600_000,
    cover: `https://img.gallery.test/c/${id}.webp`, rating: 4.5, uploader: 'someone', tags: [{ ns: 'creator', name: 'some_artist' }, { ns: 'female', name: 'blue_sky' }, { name: 'landscape' }, { ns: 'female', name: 'blue_sky' }], ...extra, // (a tag twice: the panel still opens)
  });
  const thumb = await page.evaluate(async () => {
    const ids = (await (await fetch('/api/search?q=system:type%3Dimage&limit=1')).json()).ids;
    return (await (await fetch('/api/files?ids=' + ids[0])).json()).files[0].thumb as string;
  }).catch(() => '');
  await page.route('**/api/browse/sites', (r) => r.fulfill({ json: { sites: ['gallery-site', 'other-site'], sorts: { 'gallery-site': ['popular', 'top-day'], 'other-site': [] } } }));
  await page.route('**/api/browse/tags?*', (r) => r.fulfill({ json: { suggestions: [{ tag: 'female:blue_sky', count: 0 }] } }));
  const pages: string[] = [];
  const sorts: string[] = [];
  const siteOf: string[] = [];
  await page.route('**/api/browse?*', (r) => {
    const u = new URL(r.request().url());
    pages.push(`${u.searchParams.get('q')}|${u.searchParams.get('before')}`);
    sorts.push(u.searchParams.get('sort') ?? '');
    siteOf.push(u.searchParams.get('site') ?? '');
    const before = Number(u.searchParams.get('before'));
    if (before === 0) return r.fulfill({ json: { galleries: [gallery(30, { comic: 7 }), gallery(29, { queued: true }), ...Array.from({ length: 18 }, (_, k) => gallery(28 - k))], total: 23, next: u.searchParams.get('q') ? 5 : 11 } });
    return r.fulfill({ json: { galleries: [gallery(10), gallery(9), gallery(8)], total: 23, next: 0 } });
  });
  await page.route('**/api/browse/gallery?*', (r) => {
    const p = Number(new URL(r.request().url()).searchParams.get('p'));
    const thumbs = Array.from({ length: 4 }, (_, k) => ({ page: p * 4 + k, url: 'https://img.gallery.test/t/sprite.webp', x: k * 200, y: 0, w: 200, h: 280 }));
    return r.fulfill({ json: { thumbs, pages: 2 } });
  });
  await page.route('**/api/browse/image?*', async (r) => (thumb ? r.fulfill({ response: await page.request.get(thumb) }) : r.fulfill({ status: 404 })));
  let subscribed: { query: string; label: string } | null = null;
  await page.route('**/api/subscriptions', async (r) => {
    if (r.request().method() !== 'POST') return r.continue();
    subscribed = r.request().postDataJSON();
    return r.fulfill({ json: { id: 999 } });
  });
  // Comics > Browse in the menu: a page of its own.
  await page.goto('/');
  await page.getByRole('button', { name: 'Menu' }).click();
  const drawer = page.locator('#nav-drawer');
  if (!(await drawer.getByRole('button', { name: 'Browse' }).isVisible())) await drawer.locator('.tab-row', { hasText: 'Comics' }).getByRole('button', { name: 'Unfold' }).click();
  await drawer.getByRole('button', { name: 'Browse' }).click();
  await expect(page).toHaveURL(/\/browse$/);
  // The library's own search bar and grid (the same tiles), the count in the top bar.
  await expect(page.locator('.browse').getByRole('combobox', { name: 'Search' })).toBeVisible();
  const cards = page.locator('.browse .tile');
  await expect(cards.nth(19)).toBeVisible(); // (the first page; the next may come at once, near its end)
  await expect(page.getByText('23 galleries')).toBeVisible();
  await expect(cards.nth(0).getByRole('img', { name: 'In library' })).toBeVisible(); // (icons, no words)
  await expect(cards.nth(1).getByRole('img', { name: 'Downloading' })).toBeVisible();
  // More as it scrolls (the next page, from where the last ended).
  await page.locator('#browse-scroller').evaluate((el) => el.scrollTo({ top: el.scrollHeight }));
  // (the next page asked for from where the first ended; the grid draws only the rows near the screen - its last
  // gallery, not a count of tiles)
  await expect.poll(() => pages, { timeout: 10000 }).toContain('|11');
  await page.locator('#browse-scroller').evaluate((el) => el.scrollTo({ top: el.scrollHeight }));
  await expect(cards.filter({ hasText: 'Some Comic 8' })).toBeVisible({ timeout: 10000 });
  // A gallery's details: the app's details panel, laid out like a comic's - facts, its gallery page, its tags (the
  // same Tags section, nothing to edit), its pages' thumbnails (more of them on request).
  await page.locator('#browse-scroller').evaluate((el) => el.scrollTo({ top: 0 }));
  await page.waitForTimeout(500); // (a touch right after scrolling only stops it, as on iOS)
  await cards.nth(2).click();
  const panel = page.getByRole('complementary', { name: 'Details' });
  await expect(panel.getByRole('heading', { name: 'Some Comic 28' })).toBeVisible();
  await expect(panel.locator('.facts')).toContainText('24');
  await expect(panel.getByRole('button', { name: 'some_artist', exact: true })).toBeVisible(); // (as the library keeps tags)
  await expect(panel.getByRole('button', { name: 'blue_sky', exact: true })).toBeVisible();
  await expect(panel.getByRole('heading', { name: /^Female/ })).toBeVisible(); // (the site's own namespaces, as a download keeps them)
  await expect(panel.getByRole('button', { name: 'Edit tags' })).toHaveCount(0);
  // Its pages: one row to swipe through (a carousel), the next ones loading as its end comes near.
  const strip = panel.locator('.thumbs');
  await expect(panel.locator('.thumb:not(.placeholder)').first()).toBeVisible();
  await strip.evaluate((el) => el.scrollTo({ left: el.scrollWidth }));
  await expect(panel.locator('.thumb:not(.placeholder)')).toHaveCount(8);
  expect(await panel.locator('.thumb').evaluateAll((ts) => new Set(ts.map((t) => Math.round(t.getBoundingClientRect().top))).size)).toBe(1);
  expect(await strip.evaluate((el) => el.scrollWidth > el.clientWidth)).toBe(true);
  if (!isPhone(page)) {
    // With a mouse: arrows at its ends.
    await strip.evaluate((el) => el.scrollTo({ left: 0 }));
    await expect(panel.getByRole('button', { name: 'Earlier pages' })).toHaveCount(0);
    await panel.getByRole('button', { name: 'Later pages' }).click();
    await expect.poll(() => strip.evaluate((el) => el.scrollLeft)).toBeGreaterThan(0);
  }
  // Download (an icon in the panel's header): once or a subscription (an ongoing gallery's newer versions come in) -
  // the New download dialog, named by the gallery's title (in the lists too); then it says so.
  await page.getByRole('button', { name: 'Download', exact: true }).click();
  const dl = newDl(page);
  await expect(dl.getByRole('heading', { name: 'Download once or subscribe?' })).toBeVisible();
  await expect(dl).toContainText('Some Comic 28');
  await dl.getByRole('tab', { name: /Subscribe/ }).click();
  await dl.getByRole('button', { name: 'Subscribe', exact: true }).click();
  await expect.poll(() => subscribed).toMatchObject({ query: 'https://gallery.test/g/28/abcdef0123/', label: 'Some Comic 28' });
  await expect(page.getByRole('img', { name: 'Downloading' }).first()).toBeVisible();
  // A tag: a new search for it here (the tag's menu), in the site's own syntax.
  await panel.getByRole('button', { name: 'landscape', exact: true }).click();
  await page.getByRole('menuitem', { name: /^New search/ }).click();
  await expect.poll(() => pages.some((p) => p.startsWith('"landscape$"|0') || p.startsWith('landscape|0'))).toBe(true);
  await page.waitForTimeout(300);
  expect(pages.filter((p) => p.endsWith('|11') && !p.startsWith('|'))).toEqual([]); // (never the first search's cursor)
  // Its category: a search of that category.
  await cards.nth(0).click();
  await panel.getByRole('button', { name: 'Manga' }).click();
  await expect.poll(() => pages).toContain('category:manga|0');
  // The filter menu: the site's own filters (chips, sent in the site's terms)...
  const bar = page.locator('.browse .bar');
  await bar.getByRole('button', { name: 'Filters' }).click();
  const menu = page.getByRole('dialog', { name: 'Filters' });
  await menu.getByRole('button', { name: 'Has a torrent' }).click();
  await expect.poll(() => pages).toContain('category:manga filter:torrent|0');
  await expect(bar.getByText('has a torrent').first()).toBeVisible();
  await bar.getByRole('button', { name: 'Filters' }).click();
  await menu.getByRole('button', { name: 'Number of pages' }).click();
  await page.getByRole('dialog').getByRole('spinbutton').fill('20');
  await page.getByRole('dialog').getByRole('button', { name: 'Add filter' }).click();
  await expect.poll(() => pages).toContain('category:manga filter:torrent pages:21-|0');
  // ... and its orders: one of its own lists clears the search (it has none).
  await bar.getByRole('button', { name: 'Filters' }).click();
  await page.getByRole('dialog').locator('.sort-pick').first().click();
  await page.getByRole('dialog').getByRole('button', { name: /Popular now/ }).click();
  await expect.poll(() => sorts.at(-1)).toBe('popular');
  expect(pages).toContain('|0');
  // A title search ("/words"), as plain words; a search goes back to the newest first.
  await page.keyboard.press('Escape');
  const box = page.locator('.browse').getByRole('combobox', { name: 'Search' });
  await box.fill('/some words');
  await box.press('Enter');
  await expect.poll(() => pages).toContain('some words|0');
  expect(sorts.at(-1)).toBe('');
  // The site's own tags as suggestions (not only the library's).
  await box.fill('blu');
  await page.getByRole('option', { name: /blue/ }).click();
  await expect.poll(() => pages.at(-1)).toMatch(/blue.sky/);
  // The site: picked in the filter menu too.
  await bar.getByRole('button', { name: 'Filters' }).click();
  await page.getByRole('dialog').getByRole('button', { name: /^Site:/ }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'other-site' }).click();
  await expect.poll(() => siteOf.at(-1)).toBe('other-site');
});

test('comics: their own page, never in the library; the reader turns pages, remembers the place, two pages and right to left', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible(); // (the comics' 15 pages aren't in the library)
  // (from the first page: the test library is shared by the tests of a run)
  await page.evaluate(async () => {
    for (const c of (await (await fetch("/api/comics?q=")).json()).comics)
      await fetch(`/api/comics/${c.id}`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ read_page: 0 }) });
  });
  await page.goto('/comics');
  await expect(page.getByText('2 comics')).toBeVisible();
  await page.getByRole('button', { name: 'Blue Sky Comic', exact: true }).click();
  await expect(page).toHaveURL(/\/comics\/\d+$/);
  const reader = page.getByRole('dialog', { name: 'Blue Sky Comic' });
  await expect(reader).toBeVisible();
  await expect(reader.getByText('1 of 12')).toBeVisible();
  // Its details from the reader (over it); Escape closes them, not the reader.
  await reader.getByRole('button', { name: 'Details' }).click();
  await expect(page.getByRole('complementary', { name: 'Details' }).getByRole('heading', { name: 'Blue Sky Comic' })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('complementary', { name: 'Details' })).toHaveCount(0);
  await expect(reader).toBeVisible();
  await page.keyboard.press('ArrowRight');
  await page.keyboard.press('ArrowRight');
  await expect(reader.getByText('3 of 12')).toBeVisible();
  await page.keyboard.press('ArrowLeft');
  await expect(reader.getByText('2 of 12')).toBeVisible();
  // Closed: it's first in the list, marked as being read (a book turning), its place kept.
  await page.keyboard.press('Escape');
  await expect(page).toHaveURL(/\/comics$/);
  await expect(page.locator('.comics .tile[data-i="0"]')).toHaveAccessibleName('Blue Sky Comic');
  await expect(page.locator('.comics .tile[data-i="0"]').getByRole('img', { name: 'Reading' })).toBeVisible();
  // A tap opens it where it stopped (being read); closed and opened again: still there.
  await page.getByRole('button', { name: 'Blue Sky Comic', exact: true }).click();
  await expect(reader.getByText('2 of 12')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page).toHaveURL(/\/comics$/);
  await page.getByRole('button', { name: 'Blue Sky Comic', exact: true }).click();
  await expect(reader.getByText('2 of 12')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page).toHaveURL(/\/comics$/);
  // Its details: Continue at page 2 opens it there too.
  await page.getByRole('button', { name: 'Blue Sky Comic', exact: true }).click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Details' }).click();
  await page.getByRole('complementary', { name: 'Details' }).getByRole('button', { name: 'Continue at page 2' }).click();
  await expect(reader.getByText('2 of 12')).toBeVisible();
  // The reading menu (its bars hide after a while: a tap in the middle brings them back).
  const settings = reader.getByRole('button', { name: 'Reading' });
  const choose = async (item: RegExp, choice: string) => {
    await expect(async () => {
      if (!(await settings.isVisible())) await page.mouse.click(page.viewportSize()!.width / 2, page.viewportSize()!.height / 2);
      await settings.click({ timeout: 1500 });
      await page.getByRole('menuitem', { name: item }).click({ timeout: 1500 });
    }).toPass();
    await page.getByRole('menuitemradio', { name: choice }).click();
  };
  // Two pages: the cover alone, then pairs.
  await page.keyboard.press('Home');
  await expect(reader.getByText('1 of 12')).toBeAttached();
  await choose(/^Layout/, 'Two pages');
  await page.keyboard.press('ArrowRight');
  await expect(reader.getByText('Pages 2–3 of 12')).toBeAttached();
  // Right to left (manga): the left arrow goes forward.
  await choose(/^Direction/, 'Right to left (manga)');
  await page.keyboard.press('ArrowLeft');
  await expect(reader.getByText('Pages 4–5 of 12')).toBeAttached();
  // Back to one page for the other tests (a device setting).
  await page.evaluate(() => localStorage.removeItem('vitrine.readerMode'));
});

test('comics: Mark as > Read / Unread in a comic\'s menu (more than one page)', async ({ page }) => {
  await page.goto('/comics');
  const tile = page.getByRole('button', { name: 'Short Story', exact: true });
  const markAs = async (state: string) => {
    await tile.click({ button: 'right' });
    await page.getByRole('menuitem', { name: /^Mark as/ }).click();
    await page.getByRole('menuitemradio', { name: state, exact: true }).click();
    await expect(page.getByRole('menu')).toHaveCount(0);
  };
  await markAs('Read');
  await expect(tile.getByRole('img', { name: 'Finished' })).toBeVisible();
  // The menu says so (its state on the right, ticked inside); back with the arrow.
  await tile.click({ button: 'right' });
  await expect(page.getByRole('menuitem', { name: /^Mark as/ })).toContainText('Read');
  await page.getByRole('menuitem', { name: /^Mark as/ }).click();
  await expect(page.getByRole('menuitemradio', { name: 'Read', exact: true })).toHaveAttribute('aria-checked', 'true');
  await page.getByRole('button', { name: 'Back' }).click();
  await expect(page.getByRole('menuitem', { name: 'Details' })).toBeVisible();
  await page.keyboard.press('Escape');
  await markAs('Unread');
  await expect(tile.getByRole('img', { name: 'Finished' })).toHaveCount(0);
});

test('comics: the reader handles like the viewer - i, L, a swipe up shows the details, a swipe down closes', async ({ page }, info) => {
  await page.goto('/comics');
  await page.evaluate(() => localStorage.removeItem('vitrine.readerMode'));
  await page.getByRole('button', { name: 'Short Story', exact: true }).click();
  const reader = page.getByRole('dialog', { name: 'Short Story' });
  await expect(reader).toBeVisible();
  const details = page.getByRole('complementary', { name: 'Details' });
  // The page fits the screen at its own shape (Safari once made it wider than a phone's screen: cut off).
  const vp0 = page.viewportSize()!;
  await expect.poll(async () => {
    const b = await reader.locator('.slide').nth(1).locator('.page').first().boundingBox();
    return !!b && b.x >= -1 && b.y >= -1 && b.x + b.width <= vp0.width + 1 && b.y + b.height <= vp0.height + 1;
  }).toBe(true);
  // i: its details (over it; i again hides them), the page keys still turn pages with them open.
  await page.keyboard.press('i');
  await expect(details).toBeVisible();
  await expect(reader.getByRole('button', { name: 'Details' })).toHaveAttribute('aria-pressed', 'true');
  await page.keyboard.press('i');
  await expect(details).toHaveCount(0);
  // L: the heart (on the reader's bar, as the viewer's), and back.
  const heart = reader.getByRole('button', { name: 'Favorite' });
  const was = await heart.getAttribute('aria-pressed');
  await page.keyboard.press('l');
  await expect(heart).not.toHaveAttribute('aria-pressed', was!);
  await page.keyboard.press('l');
  await expect(heart).toHaveAttribute('aria-pressed', was!);
  // The wheel turns as fast as it turns (as the viewer's): two quick clicks, two pages; back again.
  if (!info.project.name.startsWith('iphone')) {
    await page.mouse.move(page.viewportSize()!.width / 2, page.viewportSize()!.height / 2);
    await page.keyboard.press('Home');
    for (let i = 0; i < 2; i++) await page.mouse.wheel(0, 100);
    await expect(reader.getByText('3 of 3')).toBeVisible();
    for (let i = 0; i < 2; i++) await page.mouse.wheel(0, -100);
    await expect(reader.getByText('1 of 3')).toBeVisible();
  }
  // A drag: up shows the details, down (once they're gone) closes it.
  const vp = page.viewportSize()!;
  const drag = async (dy: number) => {
    await page.mouse.move(vp.width / 2 - 40, vp.height / 2);
    await page.mouse.down();
    await page.mouse.move(vp.width / 2 - 40, vp.height / 2 + dy, { steps: 8 });
    await page.mouse.up();
  };
  await drag(-120);
  await expect(details).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(details).toHaveCount(0);
  await drag(180);
  await expect(page).toHaveURL(/\/comics$/);
});

test('comics: the library\'s search, grid and selection - tags or title, filters, details, read state, favorites, delete with undo', async ({ page }) => {
  await page.goto('/comics');
  await page.evaluate(async () => {
    for (const id of (await (await fetch('/api/search?scope=comics&q=')).json()).ids) {
      await fetch('/api/comics/do/read', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ids: [id], read: false }) });
      await fetch('/api/comics/' + id, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ tags: [] }) }); // (no tags of the user's: a run before added one)
    }
  });
  await page.reload();
  await expect(page.getByText('2 comics')).toBeVisible();
  const tile = (title: string) => page.getByRole('button', { name: title, exact: true });
  await expect(tile('Short Story')).toBeVisible();
  await expect(tile('Blue Sky Comic')).toBeVisible();
  // A word that isn't a tag searches the titles; a chip like the library's.
  const search = page.getByRole('combobox', { name: 'Search' });
  await search.fill('short');
  await search.press('Enter');
  await expect(page.getByText('1 comic', { exact: true })).toBeVisible();
  await expect(tile('Blue Sky Comic')).toHaveCount(0);
  await page.getByRole('button', { name: /^Remove .*short/ }).click();
  await expect(page.getByText('2 comics')).toBeVisible();
  await expect(tile('Blue Sky Comic')).toBeVisible();
  // "/" searches the titles only, spaces and all (Enter ends it): one chip, in quotes.
  await search.pressSequentially('/sky comic');
  await expect(search).toHaveValue('/sky comic'); // (a space didn't make a chip)
  await search.press('Enter');
  await expect(page.getByText('1 comic', { exact: true })).toBeVisible();
  await expect(tile('Short Story')).toHaveCount(0);
  await expect(page.getByRole('button', { name: /^Remove .*sky comic/ })).toBeVisible();
  await expect(page.getByText('“sky comic”')).toBeVisible();
  await page.getByRole('button', { name: /^Remove .*sky comic/ }).click();
  await expect(page.getByText('2 comics')).toBeVisible();
  // Details (the menu: right-click, or long-press on a phone): a tag of the user's, then a search by it.
  await tile('Short Story').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Details' }).click();
  // (the same details panel as a library file's: the Tags section's pencil, then a tag's menu)
  const details = page.getByRole('complementary', { name: 'Details' });
  await expect(details.getByRole('heading', { name: 'Short Story' })).toBeVisible();
  await details.getByRole('button', { name: 'Edit tags' }).click();
  const addTag = details.getByRole('combobox', { name: 'Add tags' });
  await addTag.pressSequentially('some_artist');
  await addTag.press('Enter');
  await details.getByRole('button', { name: 'Save tags' }).click();
  await expect(page.getByText('Tags saved (added 1)')).toBeVisible();
  await details.getByRole('button', { name: 'some_artist', exact: true }).click();
  await expect(page.getByRole('menuitem', { name: /^Remove from this comic/ })).toBeVisible();
  await page.getByRole('menuitem', { name: /^New search/ }).click(); // a tag searches the comics
  await expect(page.getByText('1 comic', { exact: true })).toBeVisible();
  await expect(tile('Short Story')).toBeVisible();
  await page.getByRole('button', { name: /^Remove .*some.artist/ }).click();
  // The search suggests the comics' tags.
  await search.pressSequentially('some_a');
  await expect(page.getByRole('option', { name: /some_artist/ })).toBeVisible();
  await search.fill('');
  await search.press('Escape');
  // Filters: the comics' own (read state, pages).
  await page.getByRole('button', { name: 'Filters', exact: true }).click();
  await page.getByRole('button', { name: /^Number of pages/ }).click();
  await page.getByRole('spinbutton', { name: 'Value' }).fill('5');
  await page.getByRole('button', { name: 'Add filter' }).click();
  await expect(page.getByText('1 comic', { exact: true })).toBeVisible();
  await expect(tile('Blue Sky Comic')).toBeVisible();
  await page.getByRole('button', { name: /^Remove .*pages/ }).click();
  await expect(page.getByText('2 comics')).toBeVisible();
  // Selection: mark as read, a favorite; the filters find them.
  await tile('Short Story').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Select' }).click();
  await expect(page.getByText('1 selected')).toBeVisible();
  await page.getByRole('button', { name: 'Mark as read' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Marked 1 comic as read' })).toBeAttached(); // (to the bell)
  await page.getByRole('button', { name: 'Add selected to favorites' }).click();
  await page.getByRole('button', { name: 'Clear selection' }).click();
  await page.getByRole('button', { name: 'Filters', exact: true }).click();
  await page.getByRole('button', { name: /^Read$/ }).click();
  await expect(page.getByText('1 comic', { exact: true })).toBeVisible();
  await expect(tile('Short Story')).toBeVisible();
  await expect(tile('Short Story').getByRole('img', { name: 'Finished' })).toBeVisible(); // (a book with a check)
  await page.getByRole('button', { name: /^Remove .*read/ }).click();
  await page.goto('/comics/favorites'); // (the Favorites page, like the library's)
  await expect(page.getByText('1 comic', { exact: true })).toBeVisible();
  await expect(tile('Short Story')).toBeVisible();
  // Delete, then undo.
  await page.goto('/comics');
  await expect(page.getByText('2 comics')).toBeVisible();
  await tile('Short Story').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Delete' }).click();
  await confirmIn(page, 'Delete');
  await expect(page.getByText('1 comic', { exact: true })).toBeVisible();
  await page.locator('.toasts').getByRole('button', { name: 'Undo' }).click();
  await expect(page.getByText('2 comics')).toBeVisible();
  // (back as it was for the other tests)
  await page.evaluate(async () => {
    const ids = (await (await fetch('/api/search?scope=comics&q=')).json()).ids;
    const post = (path: string, body: object) => fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
    await post('/api/comics/do/read', { ids, read: false });
    await post('/api/comics/do/favorite', { ids, on: false });
  });
});

test('comics: a swipe turns the page, a double tap zooms; scrolling down reads like a webtoon and reaches the end', async ({ page }, info) => {
  await page.goto('/comics');
  await page.evaluate(async () => {
    for (const c of (await (await fetch('/api/comics?q=')).json()).comics)
      await fetch(`/api/comics/${c.id}`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ read_page: 0 }) });
  });
  await page.reload();
  await page.getByRole('button', { name: 'Short Story', exact: true }).click();
  const reader = page.getByRole('dialog', { name: 'Short Story' });
  await expect(reader.getByText('1 of 3')).toBeAttached();
  const { width: w, height: h } = page.viewportSize()!;
  // A swipe to the left: the next page.
  await page.mouse.move(w * 0.7, h / 2);
  await page.mouse.down();
  await page.mouse.move(w * 0.45, h / 2, { steps: 6 });
  await page.mouse.move(w * 0.2, h / 2, { steps: 6 });
  await page.mouse.up();
  await expect(reader.getByText('2 of 3')).toBeAttached();
  // A double tap (a mouse: a click in the middle) zooms to the page's own pixels - the zoom level says 100 % - and back.
  const zoomTap = () => (info.project.name.startsWith('iphone') ? page.mouse.dblclick(w / 2, h / 2) : page.mouse.click(w / 2, h / 2));
  const pct = reader.locator('.pct');
  await expect(pct).not.toHaveText('100 %');
  await zoomTap();
  await expect(reader.locator('.slide .spread[style*="scale"]')).toHaveCount(1);
  await expect(pct).toHaveText('100 %');
  await expect(reader.getByRole('status', { name: 'Loading the picture' })).toHaveCount(0); // (its full-size picture came)
  await zoomTap();
  await expect(reader.locator('.slide .spread[style*="scale"]')).toHaveCount(0);
  // Past the last page: the end page, a page of its own - a swipe back from it shows the last page, one on, the end again.
  await page.keyboard.press('ArrowRight');
  await page.keyboard.press('ArrowRight');
  const end = reader.locator('.end-page');
  await expect(end).toContainText('End of Short Story');
  const swipe = async (from: number, to: number) => {
    await page.mouse.move(w * from, h / 2);
    await page.mouse.down();
    await page.mouse.move(w * ((from + to) / 2), h / 2, { steps: 6 });
    await page.mouse.move(w * to, h / 2, { steps: 6 });
    await page.mouse.up();
  };
  await swipe(0.2, 0.8);
  await expect(reader.getByText('3 of 3')).toBeAttached();
  await expect(end).not.toBeInViewport(); // (the page beside it now)
  await expect(reader.locator('.track.animating')).toHaveCount(0); // (a turn under way takes no swipe)
  await swipe(0.8, 0.2);
  await expect(end).toBeInViewport();
  await expect(end.getByRole('button', { name: /^Up next/ })).toHaveCount(0); // (not in a collection)
  await reader.getByRole('button', { name: 'Read again' }).click();
  await expect(reader.getByText('1 of 3')).toBeAttached();
  // Scroll down: the pages one under another; the counter follows.
  await page.evaluate(() => localStorage.setItem('vitrine.readerMode', '"scroll"'));
  await page.keyboard.press('Escape');
  await page.getByRole('button', { name: 'Short Story', exact: true }).click();
  await expect(reader.locator('.strip')).toHaveCount(3);
  await reader.locator('.scroller').evaluate((el) => el.scrollTo(0, el.scrollHeight));
  await expect(reader.getByText('3 of 3')).toBeAttached();
  await expect(reader.locator('.strip-end')).toContainText('End of Short Story');
  await page.evaluate(() => localStorage.removeItem('vitrine.readerMode'));
});

test('the top bar covers no screen: each screen starts below it', async ({ page }) => {
  for (const p of ['/duplicates', '/comics', '/downloads', '/tags', '/tags/all', '/settings', '/settings/library', '/rankings']) {
    await page.goto(p);
    const title = page.locator('main :is(h1, button.back, .card-head h2, .comics header):visible').first(); // (a phone's section page: its back link; Comics: its search bar)
    await expect(title).toBeVisible();
    const bar = (await page.locator('header.topbar').boundingBox())!;
    const top = (await title.boundingBox())!.y;
    expect(top, `${p}: the title starts under the top bar`).toBeGreaterThanOrEqual(bar.y + bar.height);
  }
});

test('comics: a title of your own (the site title kept) and a cropped cover', async ({ page }) => {
  await page.goto('/comics');
  await expect(page.getByText('2 comics')).toBeVisible();
  const tile = (title: string) => page.getByRole('button', { name: title, exact: true });
  // Details open with its reader, over it (as a file's with the viewer).
  await tile('Short Story').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Details' }).click();
  await expect(page.getByRole('dialog', { name: 'Short Story' })).toBeAttached();
  const details = page.getByRole('complementary', { name: 'Details' });
  // Rename (a pop-up): the site's title stays (and is still searched); Original title brings it back.
  await details.getByRole('button', { name: 'Rename' }).click();
  const rename = page.getByRole('dialog', { name: 'Rename' });
  await expect(rename.getByRole('button', { name: 'Original title' })).toHaveCount(0); // (the site's is the title now)
  await rename.getByRole('textbox', { name: 'Title' }).fill('A Tidier Title');
  await rename.getByRole('button', { name: 'Save' }).click();
  await expect(details.getByRole('heading', { name: 'A Tidier Title' })).toBeVisible();
  await page.keyboard.press('Escape'); // the details
  await page.keyboard.press('Escape'); // the reader
  await expect(tile('A Tidier Title')).toBeVisible();
  // Crop cover (the tile's menu): the frame's corner dragged in; the tile takes the crop's shape.
  const before = (await tile('A Tidier Title').boundingBox())!;
  await tile('A Tidier Title').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Crop cover' }).click();
  const dialog = page.getByRole('dialog', { name: 'Crop the cover' });
  await expect(dialog.locator('img')).toHaveJSProperty('complete', true);
  const grip = dialog.locator('.grip.se');
  await grip.hover(); // (waits until it's still: the dialog has opened)
  await page.waitForTimeout(400);
  const g = (await grip.boundingBox())!;
  await page.mouse.move(g.x + g.width / 2, g.y + g.height / 2);
  await page.mouse.down();
  await page.mouse.move(g.x + g.width / 2, g.y - 120, { steps: 6 });
  await page.mouse.up();
  await dialog.getByRole('button', { name: 'Save' }).click();
  await expect(dialog).toHaveCount(0);
  const ratio = async () => {
    const b = (await tile('A Tidier Title').boundingBox())!;
    return b.height / b.width;
  };
  await expect.poll(ratio).toBeLessThan(before.height / before.width - 0.1);
  // Back as it was: the whole cover, the site's title.
  await tile('A Tidier Title').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Crop cover' }).click();
  await dialog.getByRole('button', { name: 'All of it' }).click();
  await dialog.getByRole('button', { name: 'Save' }).click();
  await expect.poll(ratio).toBeGreaterThan(before.height / before.width - 0.02);
  await tile('A Tidier Title').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Details' }).click();
  await details.getByRole('button', { name: 'Rename' }).click();
  await rename.getByRole('button', { name: 'Original title' }).click();
  await expect(details.getByRole('heading', { name: 'Short Story' })).toBeVisible();
  await page.keyboard.press('Escape');
  await page.keyboard.press('Escape');
  await expect(tile('Short Story')).toBeVisible();
});


// (a sections test - passed or failed halfway - leaves Media and Comics on: switched off, the next tests on that
// server opened on Comics)
test.afterEach(async ({ page }, info) => {
  if (!info.title.startsWith('sections:') && !info.title.startsWith('first steps')) return;
  await page.unrouteAll({ behavior: 'ignoreErrors' });
  await page.evaluate(() => fetch('/api/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ mediaOff: false, comicsOff: false }) })).catch(() => {});
});

// (a collections test that failed halfway leaves none behind: the next tests on that server expect the comics on their own)
test.afterEach(async ({ page }, info) => {
  if (!info.title.startsWith('comics: collections')) return;
  await page.evaluate(async () => {
    for (const k of (await (await fetch('/api/comics/collections')).json()).collections ?? [])
      await fetch('/api/comics/collections/' + k.id + '/remove', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' });
  });
});

test('comics: collections - a collection\'s hover card keeps its mosaic (no single cover over it)', async ({ page }) => {
  test.skip(isPhone(page), 'mouse only');
  await page.goto('/comics');
  await page.evaluate(async () => {
    const r = await (await fetch('/api/search?scope=comics&q=')).json();
    await fetch('/api/comics/collections', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: 'Some Series', ids: r.ids }) });
  });
  await page.reload();
  const tile = page.getByRole('button', { name: 'Some Series', exact: true });
  await expect(tile.locator('.mosaic img')).toHaveCount(2);
  const b = (await tile.boundingBox())!;
  await page.mouse.move(b.x + b.width / 2, b.y + b.height / 2);
  await expect(page.locator('.tile.zoomed')).toHaveCount(1);
  await page.waitForTimeout(600); // (grown: a file's card would load its bigger picture now)
  await expect(page.locator('.tile.zoomed .mosaic img')).toHaveCount(2);
  await expect(page.locator('.tile.zoomed .zoom-media')).toHaveCount(0);
});

test('comics: collections - several selected comics into a new one (the selection bar), then out of it on its page', async ({ page }) => {
  await page.goto('/comics');
  const tile = (title: string) => page.getByRole('button', { name: title, exact: true });
  await tile('Short Story').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Select' }).click();
  await tile('Blue Sky Comic').click();
  await page.getByRole('button', { name: 'Add to collection' }).click();
  await page.getByRole('menuitem', { name: 'New collection…' }).click();
  const ask = page.getByRole('dialog', { name: 'New collection' });
  await ask.getByRole('textbox', { name: 'Name' }).fill('Some Series');
  await ask.getByRole('button', { name: 'Create' }).click();
  await expect(tile('Some Series')).toContainText('2 comics');
  await expect(page.getByRole('button', { name: 'Add to collection' })).toHaveCount(0); // (the selection is done)
  // On its page: both selected, out of it - it's gone, they're on their own again.
  await tile('Some Series').click();
  await tile('Short Story').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Select' }).click();
  await page.getByRole('button', { name: 'Select all' }).click();
  await page.getByRole('button', { name: 'Remove from collection' }).click();
  await expect(page).toHaveURL(/\/comics$/);
  await expect(tile('Short Story')).toBeVisible();
  await expect(tile('Blue Sky Comic')).toBeVisible();
});

test('comics: collections - made from a tile\'s menu, one stacked tile, its own page, renamed, taken apart', async ({ page }) => {
  await page.goto('/comics');
  await expect(page.getByText('2 comics')).toBeVisible();
  const tile = (title: string) => page.getByRole('button', { name: title, exact: true });
  const addTo = async (title: string, choice: string) => {
    await tile(title).click({ button: 'right' });
    await page.getByRole('menuitem', { name: /^Add to collection/ }).click();
    await page.getByRole('menuitemradio', { name: choice }).click();
  };
  // A new one (its name asked), then the other comic added to it: one tile, stacked, "2 comics".
  await addTo('Short Story', 'New collection…');
  const ask = page.getByRole('dialog', { name: 'New collection' });
  await ask.getByRole('textbox', { name: 'Name' }).fill('Some Series');
  await ask.getByRole('button', { name: 'Create' }).click();
  await expect(tile('Some Series')).toBeVisible();
  await expect(tile('Short Story')).toHaveCount(0);
  await addTo('Blue Sky Comic', 'Some Series');
  await expect(tile('Blue Sky Comic')).toHaveCount(0);
  await expect(tile('Some Series')).toContainText('2 comics');
  // The count in the top bar: the comics, each of a collection's counted (not the tiles: "1 comic").
  await expect(page.locator('span.count[aria-live="polite"]')).toHaveText('2 comics');
  // Its page: both comics by title, Back to all comics.
  await tile('Some Series').click();
  await expect(page).toHaveURL(/\/comics\/collection\/\d+/);
  await expect(page.getByRole('heading', { name: 'Some Series' })).toBeVisible();
  await expect(tile('Blue Sky Comic')).toBeVisible();
  await expect(tile('Short Story')).toBeVisible();
  // At the end of one, Up next: the collection's next comic (A to Z) in the reader's place; Back closes it.
  await tile('Blue Sky Comic').click();
  const reader = page.getByRole('dialog', { name: 'Blue Sky Comic' });
  await expect(reader.getByText(/of 12/)).toBeAttached();
  for (let i = 0; i < 12; i++) await page.keyboard.press('ArrowRight');
  const upNext = reader.locator('.end-page').getByRole('button', { name: /^Up next\s*Short Story/ });
  await expect(upNext).toBeVisible();
  await upNext.click();
  await expect(page.getByRole('dialog', { name: 'Short Story' })).toBeVisible();
  await expect(page.getByRole('dialog', { name: 'Short Story' }).getByText(/of 3/)).toBeAttached();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect(page).toHaveURL(/\/comics\/collection\/\d+/);
  // One taken out: back on its own outside.
  await tile('Short Story').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Remove from collection' }).click();
  await expect(tile('Short Story')).toHaveCount(0);
  await page.getByRole('button', { name: 'Back to all comics' }).click();
  await expect(page).toHaveURL(/\/comics$/);
  await expect(tile('Short Story')).toBeVisible();
  await expect(tile('Some Series')).toContainText('1 comic');
  // Renamed from the tile's menu; taken apart: its comic stays.
  await tile('Some Series').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Rename collection' }).click();
  const rename = page.getByRole('dialog', { name: 'Rename collection' });
  await rename.getByRole('textbox', { name: 'Name' }).fill('Other Series');
  await rename.getByRole('button', { name: 'Save' }).click();
  await expect(tile('Other Series')).toBeVisible();
  await tile('Other Series').click({ button: 'right' });
  await page.getByRole('menuitem', { name: /^Take apart/ }).click();
  await expect(tile('Other Series')).toHaveCount(0);
  await expect(tile('Blue Sky Comic')).toBeVisible();
  await expect(tile('Short Story')).toBeVisible();
});

test('comics: collections - deleted with its comics from the tile\'s menu (asked first), Undo brings them back', async ({ page }) => {
  await page.goto('/comics');
  await expect(page.getByText('2 comics')).toBeVisible();
  const tile = (title: string) => page.getByRole('button', { name: title, exact: true });
  for (const [title, choice] of [['Short Story', 'New collection…'], ['Blue Sky Comic', 'Doomed Series']]) {
    await tile(title).click({ button: 'right' });
    await page.getByRole('menuitem', { name: /^Add to collection/ }).click();
    await page.getByRole('menuitemradio', { name: choice }).click();
    if (choice.endsWith('…')) {
      const ask = page.getByRole('dialog', { name: 'New collection' });
      await ask.getByRole('textbox', { name: 'Name' }).fill('Doomed Series');
      await ask.getByRole('button', { name: 'Create' }).click();
    }
  }
  await expect(tile('Doomed Series')).toContainText('2 comics');
  await tile('Doomed Series').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Delete collection' }).click();
  const dlg = page.getByRole('alertdialog');
  await expect(dlg).toContainText('Delete "Doomed Series" and its comics?');
  await expect(dlg).toContainText('Its 2 comics go to the trash');
  await dlg.getByRole('button', { name: 'Delete' }).click();
  await expect(tile('Doomed Series')).toHaveCount(0);
  await expect(tile('Blue Sky Comic')).toHaveCount(0);
  await expect(tile('Short Story')).toHaveCount(0);
  // Undo: the comics, and their collection, back.
  await page.locator('.toast', { hasText: 'Deleted "Doomed Series" and its comics' }).getByRole('button', { name: 'Undo' }).click();
  await expect(tile('Doomed Series')).toContainText('2 comics');
});

test('comics: collections - its own order (Reorder: drag or arrow keys, A to Z), its page follows; its tile can\'t be selected; the bar\'s buttons say what they do', async ({ page }) => {
  await page.goto('/comics');
  await expect(page.getByText('2 comics')).toBeVisible();
  const tile = (title: string) => page.getByRole('button', { name: title, exact: true });
  for (const [title, choice] of [['Short Story', 'New collection…'], ['Blue Sky Comic', 'Some Series']]) {
    await tile(title).click({ button: 'right' });
    await page.getByRole('menuitem', { name: /^Add to collection/ }).click();
    await page.getByRole('menuitemradio', { name: choice }).click();
    if (choice.endsWith('…')) {
      const ask = page.getByRole('dialog', { name: 'New collection' });
      await ask.getByRole('textbox', { name: 'Name' }).fill('Some Series');
      await ask.getByRole('button', { name: 'Create' }).click();
    }
  }
  await expect(tile('Some Series')).toContainText('2 comics');
  // Reorder from the tile's menu: A to Z at first; the arrow keys move one; Save.
  await tile('Some Series').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Reorder' }).click();
  const dlg = page.getByRole('dialog', { name: 'Order of "Some Series"' });
  await expect(dlg.locator('li .title')).toHaveText(['Blue Sky Comic', 'Short Story']);
  await dlg.getByRole('button', { name: /^Move Blue Sky Comic/ }).press('ArrowDown');
  await expect(dlg.locator('li .title')).toHaveText(['Short Story', 'Blue Sky Comic']);
  await dlg.getByRole('button', { name: 'Save' }).click();
  await expect(dlg).toHaveCount(0);
  // Its page follows.
  await tile('Some Series').click();
  await expect(page).toHaveURL(/\/comics\/collection\/\d+/);
  await expect(page.locator('.comics .tile')).toHaveCount(2);
  await expect(page.locator('.comics .tile').first()).toHaveAccessibleName('Short Story');
  // A to Z puts it back (from its page's menu).
  await page.getByRole('button', { name: 'More for the collection Some Series' }).click();
  await page.getByRole('menuitem', { name: 'Reorder' }).click();
  await dlg.getByRole('button', { name: 'A to Z' }).click();
  await expect(dlg.locator('li .title')).toHaveText(['Blue Sky Comic', 'Short Story']);
  await dlg.getByRole('button', { name: 'Save' }).click();
  await expect(page.locator('.comics .tile').first()).toHaveAccessibleName('Blue Sky Comic');
  await page.getByRole('button', { name: 'Back to all comics' }).click();
  // Its tile can't be selected (a collection never goes into another).
  if (!isPhone(page)) {
    await tile('Some Series').click({ modifiers: ['ControlOrMeta'] });
    await expect(page.getByRole('toolbar', { name: 'Selected comics' })).toHaveCount(0);
  }
  await page.goto('/');
  // With room, the selection bar's buttons have their words; else just icons - the first never under the soft edge.
  const first = page.locator('.grid-scroller .tile').first();
  if (isPhone(page)) {
    await first.click({ button: 'right' }).catch(() => {});
    const sel = page.getByRole('menuitem', { name: 'Select' });
    if (await sel.isVisible()) await sel.click();
  } else await first.click({ modifiers: ['ControlOrMeta'] });
  const bar = page.getByRole('toolbar', { name: 'Selected files' });
  await expect(bar).toBeVisible();
  await expect(bar.locator('.actions')).not.toHaveClass(/fade-l/);
  await expect(bar.getByRole('button', { name: 'Select all' })).toContainText('Select all'); // (phones too)
  if (!isPhone(page)) await expect(bar.getByRole('button', { name: 'Select all' })).toContainText('Select all');
});

test('new download on a phone with the keyboard up: the tag suggestions are part of the sheet, right above the box (floating over the page, they hung far above it on an iPhone)', async ({ page }) => {
  test.skip(!isPhone(page), 'phones');
  await page.route('**/api/autocomplete?*', (r) => r.fulfill({ json: { suggestions: [{ tag: 'blue_sky', count: 3 }, { tag: 'blue_hair', count: 2 }, { tag: 'blueberry', count: 1 }], took_ms: 0 } }));
  await page.goto('/');
  await openNewDownload(page);
  const dlg = newDl(page);
  await dlg.getByRole('button', { name: 'safebooru.org' }).click();
  const box = dlg.getByRole('combobox', { name: 'Search on the site' });
  // (a short screen: the room the keyboard leaves - little below the box, more above it)
  await page.setViewportSize({ width: 393, height: 380 });
  await box.fill('blue');
  const list = dlg.locator('.wrap.above .list'); // (part of the sheet: nothing floats)
  await expect(list).toBeVisible();
  const gap = () =>
    page.evaluate(() => {
      const l = document.querySelector('.wrap.above .list')!.getBoundingClientRect();
      const b = (document.activeElement as HTMLElement).getBoundingClientRect();
      return b.top - l.bottom;
    });
  await expect.poll(async () => { const g = await gap(); return g >= 0 && g <= 16; }).toBe(true); // (4px above its frame; it was a keyboard's height)
  // The keyboard rising (the sheet moves up by --kb, as on an iPhone): the list stays on the box.
  await page.evaluate(() => document.documentElement.style.setProperty('--kb', '120px'));
  await expect.poll(async () => { const g = await gap(); return g >= 0 && g <= 16; }).toBe(true); // (once the sheet has settled)
});

test('browse: several galleries selected (Select, or Ctrl-click) and downloaded at once - those the library has can\'t be picked', async ({ page }) => {
  const gallery = (id: number, extra: Record<string, unknown> = {}) => ({
    id, token: 'abcdef0123', url: `https://gallery.test/g/${id}/abcdef0123/`, title: `Some Comic ${id}`, category: 'Manga', pages: 24, posted: 1700000000000,
    cover: '', rating: 4, uploader: 'someone', tags: [], ...extra,
  });
  await page.route('**/api/browse/sites', (r) => r.fulfill({ json: { sites: ['gallery-site'], sorts: { 'gallery-site': [] } } }));
  await page.route('**/api/browse?*', (r) => r.fulfill({ json: { galleries: [gallery(30, { comic: 7 }), gallery(29), gallery(28), gallery(27)], total: 4, next: 0 } }));
  await page.route('**/api/browse/image?*', (r) => r.fulfill({ status: 404 }));
  const added: { query: string; label: string; site: string }[] = [];
  await page.route('**/api/downloads', async (r) => {
    if (r.request().method() !== 'POST') return r.continue();
    added.push(r.request().postDataJSON());
    return r.fulfill({ json: { id: 900 + added.length } });
  });
  await page.goto('/browse');
  const cards = page.locator('.browse .tile');
  await expect(cards).toHaveCount(4);
  // Select (in the top bar): taps select instead of opening; the bar counts them.
  await page.getByRole('button', { name: 'Select', exact: true }).click();
  await cards.nth(0).click({ force: true }); // (in the library already: dimmed, not picked)
  await expect(cards.nth(0)).toHaveAttribute('aria-disabled', 'true');
  await cards.nth(1).click();
  await cards.nth(2).click();
  await expect(page.getByRole('complementary', { name: 'Details' })).toHaveCount(0);
  const bar = page.getByRole('toolbar', { name: /selected/i });
  await expect(bar).toContainText('2');
  // Select all: every gallery but the one the library has.
  await bar.getByRole('button', { name: /select all/i }).click();
  await expect(bar).toContainText('3');
  await cards.nth(3).click();
  await expect(bar).toContainText('2');
  await bar.getByRole('button', { name: 'Download' }).click();
  await expect.poll(() => added.map((a) => a.query)).toEqual(['https://gallery.test/g/29/abcdef0123/', 'https://gallery.test/g/28/abcdef0123/']);
  expect(added[0]).toMatchObject({ site: 'gallery-site', label: 'Some Comic 29' });
  await expect(page.getByText('Downloading 2 galleries from')).toBeVisible();
  await expect(bar).toHaveCount(0);
  await expect(cards.nth(1).getByRole('img', { name: 'Downloading' })).toBeVisible();
  // A tap opens again.
  await cards.nth(3).click();
  await expect(page.getByRole('complementary', { name: 'Details' })).toBeVisible();
  if (!isPhone(page)) {
    await page.keyboard.press('Escape');
    // With a mouse: Ctrl-click selects without Select.
    await cards.nth(3).click({ modifiers: ['ControlOrMeta'] });
    await expect(bar).toContainText('1');
  }
});

test('tag suggestions keep to the sites picked: system:site= in the search, the chosen site in New download', async ({ page }) => {
  const asked: URL[] = [];
  await page.route('**/api/autocomplete?*', (route) => {
    asked.push(new URL(route.request().url()));
    return route.fulfill({ json: { suggestions: [{ tag: 'blue_sky', count: 3 }], took_ms: 0 } });
  });
  const last = () => asked[asked.length - 1];
  // No site picked: every site's tags.
  await page.goto('/');
  const box = page.locator('.field input');
  await box.fill('blu');
  await expect.poll(() => asked.length).toBeGreaterThan(0);
  expect(last().searchParams.get('sites')).toBeNull();
  // A site picked in the search: only its files' tags.
  await page.goto('/?q=system:site%3Dsafebooru');
  const n = asked.length;
  await box.fill('blu');
  await expect.poll(() => asked.length).toBeGreaterThan(n);
  expect(last().searchParams.get('sites')).toBe('safebooru');
  // New download: the chosen site's.
  await page.goto('/downloads');
  const m = asked.length;
  await page.getByRole('combobox', { name: 'Search on the site' }).fill('blu');
  await expect.poll(() => asked.length).toBeGreaterThan(m);
  expect(last().searchParams.get('sites')).toBeTruthy();
});

test('phone sheets drag down to close from anywhere on them - a menu item where it starts isn\'t chosen; the details panel from its body', async ({ page }) => {
  test.skip(!isPhone(page), 'phone sheets');
  // A menu sheet (the new download's site picker), dragged down from one of its items.
  await page.goto('/downloads');
  const site = page.getByRole('button', { name: 'Site', exact: true });
  await expect(site).toContainText('.'); // (the sites are in)
  const before = await site.textContent();
  await site.click();
  const item = page.getByRole('menuitemradio', { name: 'test-down.invalid' });
  await expect(item).toBeVisible();
  await page.waitForTimeout(300); // (in place)
  const ib = (await item.boundingBox())!;
  await page.mouse.move(ib.x + ib.width / 2, ib.y + ib.height / 2);
  await page.mouse.down();
  await page.mouse.move(ib.x + ib.width / 2, ib.y + ib.height / 2 + 60, { steps: 6 });
  await page.mouse.move(ib.x + ib.width / 2, ib.y + ib.height / 2 + 200, { steps: 6 });
  await page.mouse.up();
  await expect(page.getByRole('menu')).toHaveCount(0);
  expect(await site.textContent()).toBe(before); // (not chosen)
  // A short drag springs back; a tap still chooses.
  await site.click();
  await expect(item).toBeVisible();
  await page.waitForTimeout(300);
  const ib2 = (await item.boundingBox())!;
  await page.mouse.move(ib2.x + ib2.width / 2, ib2.y + ib2.height / 2);
  await page.mouse.down();
  await page.mouse.move(ib2.x + ib2.width / 2, ib2.y + ib2.height / 2 + 30, { steps: 4 });
  await page.mouse.up();
  await expect(page.getByRole('menu')).toBeVisible();
  await item.click();
  await expect(site).toContainText('test-down.invalid');

  // The viewer's details panel, dragged down from its body.
  await page.goto('/');
  await page.locator('.tile').first().click();
  await page.getByRole('button', { name: 'Details', exact: true }).click();
  const panel = page.locator('.viewer aside.panel');
  await expect(panel).toBeVisible();
  await page.waitForTimeout(400);
  const pb = (await panel.boundingBox())!;
  const x = pb.x + pb.width / 2;
  const y = pb.y + pb.height / 2;
  await page.mouse.move(x, y);
  await page.mouse.down();
  await page.mouse.move(x, y + 60, { steps: 6 });
  await page.mouse.move(x, y + 220, { steps: 6 });
  await page.mouse.up();
  await expect(panel).toHaveCount(0);
});

test('privacy: text fields keep the keyboard from suggesting or learning', async ({ page }) => {
  test.skip(!isPhone(page), 'phones');
  // A text field (the library's search): no suggestions, autocorrect, capitals or spellcheck.
  await page.goto('/');
  const search = page.getByRole('combobox', { name: 'Search' });
  for (const [attr, value] of [['autocorrect', 'off'], ['autocapitalize', 'off'], ['spellcheck', 'false'], ['autocomplete', 'off']])
    await expect(search).toHaveAttribute(attr, value);
});

test('the bell is one for every device: read and cleared on one, the other follows at once; a cleared notice stays gone', async ({ page, browser }, info) => {
  test.skip(info.project.name !== 'desktop-chromium', 'one run is enough: the bell is the server\'s');
  await page.goto('/');
  await clearBell(page);
  // Another device (its own browser, the same account).
  const other = await browser.newContext({ baseURL: `http://127.0.0.1:${8100 + info.parallelIndex}` });
  const page2 = await other.newPage();
  await login(page2);
  await page2.goto('/');
  // Something done on this one that goes to the bell (a saved search deleted, say): both bells have it.
  await page.evaluate(() =>
    fetch('/api/notices', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ text: 'Deleted "some search"', tone: 'removed', category: 'savedSearches' }) }),
  );
  const unread = (p: Page) => p.getByRole('button', { name: /^Notifications, 1 unread$/ });
  await expect(unread(page)).toBeVisible();
  await expect(unread(page2)).toBeVisible();
  // Read here: read there too.
  await unread(page).click();
  await expect(page.getByRole('dialog', { name: 'Notifications' }).locator('li', { hasText: 'some search' })).toBeVisible();
  await expect(page2.getByRole('button', { name: 'Notifications', exact: true })).toBeVisible();
  // Cleared here: gone there, and it stays gone (reloads catch up from the server).
  await page.getByRole('dialog', { name: 'Notifications' }).getByRole('button', { name: 'Clear' }).click();
  await page2.getByRole('button', { name: 'Notifications', exact: true }).click();
  await expect(page2.getByRole('dialog', { name: 'Notifications' }).getByRole('img', { name: 'No notifications' })).toBeVisible();
  for (let i = 0; i < 2; i++) {
    await page.reload();
    await expect(page.getByRole('button', { name: 'Notifications', exact: true })).toBeVisible();
    await page.waitForTimeout(400);
    await expect(page.getByRole('button', { name: /unread/ })).toHaveCount(0);
  }
  await other.close();

});

test('trash: its own page in the menu - its files, Restore and Delete forever (asked first), Empty trash after typing a word', async ({ page }, info) => {
  test.skip(isPhone(page), 'right-click and Ctrl+click: a mouse');
  // (the test library is shared: the trash's answers are made up here - nothing is deleted)
  const ids = (await page.request.get('/api/search?q=system:type%3Dimage&limit=2').then((r) => r.json())).ids as number[];
  const calls: { path: string; body: unknown }[] = [];
  await page.route('**/api/trash', (r) => r.fulfill({ json: { files: 2, bytes: 3_500_000, days: 0 } }));
  await page.route('**/api/search?*', async (r) => {
    if (!r.request().url().includes('scope=trash')) return r.continue();
    await r.fulfill({ json: { result: 'trash-test', total: 2, matched: 2, ids, unknown: [], seed: 0, took_ms: 1, query: '', now: Date.now() } });
  });
  for (const p of ['undelete', 'trash/purge', 'trash/empty']) {
    await page.route(`**/api/${p}`, async (r) => {
      calls.push({ path: p, body: r.request().postDataJSON() });
      await r.fulfill({ json: { restored: 1, purged: 1 } });
    });
  }
  await page.goto('/');
  await page.getByRole('button', { name: 'Menu' }).click();
  await unfold(page, 'library');
  await page.locator('#nav-drawer').getByRole('button', { name: 'Trash' }).click();
  await expect(page).toHaveURL(/\/trash$/);
  await expect(page.getByRole('combobox', { name: 'Search' })).toHaveAttribute('placeholder', 'Trash tag search');
  await expect(page.locator('.trash-facts')).toHaveText('2 files · 3.3 MB · kept until you empty it');
  await expect(page.locator('.tile')).toHaveCount(2);
  // A tile's menu: Restore (no favorite), Delete forever.
  await page.locator('.tile[data-i="0"]').click({ button: 'right' });
  await expect(page.getByRole('menuitem', { name: 'Add to favorites' })).toHaveCount(0);
  await page.getByRole('menuitem', { name: 'Restore' }).click();
  await expect.poll(() => calls.find((c) => c.path === 'undelete')?.body).toEqual({ ids: [ids[0]] });
  // The selection bar: Restore and Delete forever (asked first; it can't be undone).
  await page.locator('.tile[data-i="1"]').click({ modifiers: ['Control'] });
  await expect(page.getByRole('button', { name: 'Restore' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Tags' })).toHaveCount(0);
  await page.getByRole('button', { name: 'Delete forever' }).click();
  const dlg = page.getByRole('alertdialog');
  await expect(dlg).toContainText('Delete 1 file forever?');
  await expect(dlg.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await dlg.getByRole('button', { name: 'Delete forever' }).click();
  await expect.poll(() => calls.find((c) => c.path === 'trash/purge')?.body).toEqual({ ids: [ids[1]] });
  // Empty trash: only after typing the word.
  await page.getByRole('button', { name: 'Empty trash' }).click();
  await expect(dlg).toContainText('2 files (3.3 MB) are deleted forever');
  const go = dlg.getByRole('button', { name: 'Empty trash' });
  await expect(go).toBeDisabled();
  await dlg.getByRole('textbox').fill('nope');
  await expect(go).toBeDisabled();
  await dlg.getByRole('textbox').fill('delete');
  if (process.env.SHOTS) await page.screenshot({ path: `${process.env.SHOTS}/trash-empty-${info.project.name}.png` });
  await go.click();
  await expect.poll(() => calls.some((c) => c.path === 'trash/empty')).toBe(true);
  await page.unrouteAll({ behavior: 'ignoreErrors' });
});

test('SFW mode: the "I came" button, its Rankings, sort and setting go, and adult sites are never offered', async ({ page }) => {
  // (settings are shared: the server's answer is changed here, nothing is saved)
  await page.route('**/api/settings', async (r) => {
    if (r.request().method() !== 'GET') return r.continue();
    const res = await r.fetch();
    await r.fulfill({ response: res, json: { ...(await res.json()), cameButton: true, sfwMode: true } });
  });
  await page.route('**/api/plugins', (r) =>
    r.fulfill({ json: { plugins: [{ id: 'sfw_site', site: 'sfw.example', name: 'Sfw', version: '1.0.0' }, { id: 'adult_site', site: 'adult.example', name: 'Adult', version: '1.0.0', nsfw: true }] } }),
  );
  await page.goto('/?q=blue_sky');
  await page.locator('.tile[data-i="0"]').click();
  await expect(page.locator('.viewer .tools, .viewer')).toBeVisible();
  await expect(page.locator('.viewer').getByRole('button', { name: /^I came/ })).toHaveCount(0);
  await page.goto('/');
  await page.getByRole('button', { name: 'Menu' }).click();
  await expect(page.locator('#nav-drawer').getByRole('button', { name: 'I came', exact: true })).toHaveCount(0);
  await page.keyboard.press('Escape');
  // Its page's address: Media instead.
  await page.goto('/rankings');
  await expect(page).toHaveURL(/\/$/);
  // Settings > Viewing: SFW on; the "I came" setting not there.
  await page.goto('/settings/viewing');
  await expect(page.getByRole('checkbox', { name: /Safe for work/ })).toBeChecked();
  await expect(page.getByRole('checkbox', { name: /I came/ })).toHaveCount(0);
  // Adding a site: no adult ones, no switch to show them.
  await page.goto('/settings/sites');
  await page.getByRole('button', { name: 'Add site', exact: true }).click();
  const adding = page.getByRole('region', { name: 'Add a site' });
  await expect(adding).toContainText('sfw.example');
  await expect(adding).not.toContainText('adult.example');
  await expect(page.getByText('Adult (18+)')).toHaveCount(0);
  await page.unrouteAll({ behavior: 'ignoreErrors' });
});

test('Add site with a long plugin list: the first eight A to Z, Show all, a search, kinds and the adult switch', async ({ page }) => {
  const kinds = ['booru', 'booru', 'booru', 'gallery', 'social'];
  const plugins = Array.from({ length: 12 }, (_, i) => ({
    id: `many_${i}`,
    site: `site-${String.fromCharCode(108 - i)}.example`, // l..a: listed A to Z, not in this order
    name: `Site ${i}`,
    version: '1.0.0',
    description: i === 3 ? 'Comic galleries and doujinshi' : 'Images',
    kind: kinds[i % kinds.length],
    installed: false,
    update: false,
  }));
  plugins.push({ ...plugins[0], id: 'many_adult', site: 'adult-gallery.example', name: 'Adult', kind: 'gallery', nsfw: true } as (typeof plugins)[number]);
  await page.route('**/api/plugins', (r) => r.fulfill({ json: { plugins } }));
  await page.goto('/settings/sites');
  await page.getByRole('button', { name: 'Add site', exact: true }).click();
  const panel = page.getByRole('region', { name: 'Add a site' });
  const rows = panel.locator('ul.available > li');
  await expect(rows).toHaveCount(8);
  await expect(rows.first()).toContainText('site-a.example');
  await panel.getByRole('button', { name: 'Show all 12 sites' }).click();
  await expect(rows).toHaveCount(12);
  // A search: name, address or description.
  await panel.getByRole('searchbox', { name: 'Search sites' }).fill('doujin');
  await expect(rows).toHaveCount(1);
  await expect(rows).toContainText('site-i.example');
  await panel.getByRole('searchbox', { name: 'Search sites' }).fill('nothing-like-this');
  await expect(panel.getByText('No sites match.')).toBeVisible();
  await panel.getByRole('searchbox', { name: 'Search sites' }).fill('adult');
  await expect(panel.getByText(/Some adult sites do/)).toBeVisible();
  await panel.getByRole('checkbox', { name: 'Adult (18+)' }).check();
  await expect(rows).toHaveCount(1);
  await expect(rows).toContainText('adult-gallery.example');
  await panel.getByRole('searchbox', { name: 'Search sites' }).fill('');
  // Kinds.
  await panel.getByRole('radio', { name: 'Galleries' }).click();
  await expect(rows).toHaveCount(3); // two galleries and the adult one
  await panel.getByRole('radio', { name: 'Social' }).click();
  await expect(rows).toHaveCount(2);
  await panel.getByRole('radio', { name: 'All', exact: true }).click();
  await expect(rows).toHaveCount(13);
  await page.unrouteAll({ behavior: 'ignoreErrors' });
});

test('"I came" in the comic reader: counted on the page showing; the I-came page shows that page and opens the comic on it', async ({ page }) => {
  test.skip(isPhone(page), 'keys; the same button on phones');
  const set = (v: boolean) =>
    page.evaluate((v) => fetch('/api/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ cameButton: v }) }), v);
  await set(true);
  const presses: number[] = [];
  page.on('response', async (res) => {
    if (res.url().endsWith('/api/climax') && res.request().method() === 'POST') presses.push((await res.json()).id);
  });
  try {
    await page.goto('/comics');
    await page.getByRole('button', { name: 'Blue Sky Comic', exact: true }).click();
    const reader = page.getByRole('dialog', { name: 'Blue Sky Comic' });
    await expect(reader).toBeVisible();
    await page.keyboard.press('ArrowRight'); // (page 2)
    const came = reader.getByRole('button', { name: /^I came/ });
    await came.click();
    await expect(came).toHaveAccessibleName('I came (1 so far)');
    const url = new URL(page.url());
    const comic = url.pathname.split('/').pop();
    // The I-came page: the comic's page, opening the comic there.
    await page.goto('/rankings?period=all');
    const pages = page.locator('section.card', { has: page.getByRole('heading', { name: 'Comic pages' }) });
    const tile = pages.getByRole('button', { name: /Blue Sky Comic, page \d+/ });
    await expect(tile).toHaveCount(1);
    const n = Number((await tile.getAttribute('aria-label'))!.match(/page (\d+)/)![1]);
    await tile.click();
    await expect.poll(() => { const u = new URL(page.url()); return u.pathname + u.search; }).toBe(`/comics/${comic}?page=${n}`);
    await expect(page.getByRole('dialog', { name: 'Blue Sky Comic' }).getByRole('button', { name: 'I came (1 so far)' })).toBeVisible();
  } finally {
    for (const id of presses) await page.evaluate((id) => fetch('/api/climax/undo', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ id }) }), id);
    await set(false);
  }
});

test('trash: Menu > Trash has Media and Comics - the comics trash restores, deletes forever and empties comics', async ({ page }) => {
  test.skip(isPhone(page), 'right-click and Ctrl+click: a mouse');
  // (made-up answers, as for the media trash: nothing is deleted)
  const comics = (await page.request.get('/api/comics?q=').then((r) => r.json())).comics as { id: number }[];
  const ids = comics.map((c) => c.id);
  const calls: { path: string; body: unknown }[] = [];
  await page.route('**/api/trash', (r) => r.fulfill({ json: { files: 0, bytes: 0, comics: ids.length, comic_bytes: 2_000_000, days: 30 } }));
  await page.route('**/api/search?*', async (r) => {
    if (!r.request().url().includes('scope=comics_trash')) return r.continue();
    // (the covers' shapes too: a square each - the grid lays comics out by them)
    const shapes = Buffer.from(ids.flatMap(() => [0, 4])).toString('base64');
    await r.fulfill({ json: { result: 'ctrash-test', total: ids.length, matched: ids.length, ids, shapes, unknown: [], seed: 0, took_ms: 1, query: '', now: Date.now() } });
  });
  // (its result's slices and dates, as the server would keep them)
  await page.route('**/api/results/ctrash-test*', (r) => r.fulfill({ json: { total: ids.length, offset: 0, ids, dates: [] } }));
  for (const p of ['comics/undelete', 'trash/purge-comics', 'trash/empty-comics']) {
    await page.route(`**/api/${p}`, async (r) => {
      calls.push({ path: p, body: r.request().postDataJSON() });
      await r.fulfill({ json: { done: 1, purged: 1 } });
    });
  }
  await page.goto('/');
  await page.getByRole('button', { name: 'Menu' }).click();
  await unfold(page, 'trash');
  const drawer = page.locator('#nav-drawer');
  await expect(drawer.locator('.subs').getByRole('button', { name: 'Media' })).toBeVisible();
  await drawer.locator('.subs').getByRole('button', { name: 'Comics' }).click();
  await expect(page).toHaveURL(/\/comics\/trash$/);
  await expect(page.getByRole('combobox', { name: 'Search' }).last()).toHaveAttribute('placeholder', 'Trash tag search');
  await expect(page.locator('.comics .trash-facts')).toHaveText(`${ids.length} comics · 1.9 MB · kept 30 days`);
  const tile = page.locator('.comics .tile[data-i="0"]');
  await tile.click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Restore' }).click();
  await expect.poll(() => calls.find((c) => c.path === 'comics/undelete')?.body).toEqual({ ids: [ids[0]] });
  await page.locator('.comics .tile[data-i="1"]').click({ modifiers: ['Control'] });
  await page.getByRole('button', { name: 'Delete forever' }).click();
  await expect(page.getByRole('alertdialog')).toContainText('Delete 1 comic forever?');
  await page.getByRole('alertdialog').getByRole('button', { name: 'Delete forever' }).click();
  await expect.poll(() => calls.find((c) => c.path === 'trash/purge-comics')?.body).toEqual({ ids: [ids[1]] });
  await page.locator('.comics').getByRole('button', { name: 'Empty trash' }).click();
  await expect(page.getByRole('alertdialog')).toContainText('Empty the comics trash?');
  await page.getByRole('alertdialog').getByRole('textbox').fill('delete');
  await page.getByRole('alertdialog').getByRole('button', { name: 'Empty trash' }).click();
  await expect.poll(() => calls.some((c) => c.path === 'trash/empty-comics')).toBe(true);
  await page.unrouteAll({ behavior: 'ignoreErrors' });
});

test('downloads tabs: the lit pill covers its tab\'s count, "99+" too', async ({ page }) => {
  // (120 subscriptions, made up in the answer: the count reads 99+)
  await page.route('**/api/downloads', async (r) => {
    if (r.request().method() !== 'GET') return r.continue();
    const res = await r.fetch();
    const body = await res.json();
    body.subscriptions = Array.from({ length: 120 }, (_, i) => ({ id: 9000 + i, site: 'test-down', query: 'pill_' + i, interval: 86400, paused: false, initial_max: 0, checks: 1, downloaded: 0, last_new: 0, created_at: 0, next_check_at: Date.now() + 1e7, depth: 0 }));
    await r.fulfill({ response: res, json: body });
  });
  await page.goto('/downloads');
  await page.getByRole('tab', { name: /Subscriptions/ }).click();
  await expect(page.locator('#tab-subs .tab-count')).toHaveText('99+');
  await expect.poll(() =>
    page.evaluate(() => {
      const pill = document.querySelector('.tab-pill')!.getBoundingClientRect();
      const c = document.querySelector('#tab-subs .tab-count')!.getBoundingClientRect();
      return pill.left <= c.left && pill.right >= c.right;
    }),
  ).toBe(true);
  await page.unrouteAll({ behavior: 'ignoreErrors' });
});

test('sections: switching Media off asks first, then whether to delete it all (a word to type); it leaves the menu, its pages open Comics; the last one on can\'t go', async ({ page }) => {
  // (the server's settings stay as they are: what's saved goes to a record here, and reading them gives the server's
  // with that record on top - one consistent world; nothing is switched off for the tests after this one)
  const calls: { path: string; body: unknown }[] = [];
  const saved: Record<string, unknown> = {};
  await page.route('**/api/settings', async (r) => {
    if (r.request().method() !== 'POST') {
      const real = await (await r.fetch()).json();
      return r.fulfill({ json: { ...real, ...saved } });
    }
    const body = r.request().postDataJSON() as Record<string, unknown>;
    calls.push({ path: 'settings', body });
    Object.assign(saved, body);
    await r.fulfill({ json: {} });
  });
  await page.route('**/api/sections', (r) =>
    r.fulfill({ json: { media: { items: 26, bytes: 14_000_000, subscriptions: 3, downloads: 1 }, comics: { items: 2, bytes: 1_000_000, subscriptions: 0, downloads: 0 } } }),
  );
  await page.route('**/api/sections/media/off', async (r) => {
    calls.push({ path: 'media/off', body: r.request().postDataJSON() });
    await r.fulfill({ json: { subscriptions: 3, downloads: 1, deleting: false } });
  });
  await page.goto('/settings/viewing');
  const media = page.getByRole('checkbox', { name: /^Media/ });
  await expect(media).toBeChecked();
  await media.click();
  const dlg = page.getByRole('alertdialog');
  await expect(dlg).toContainText('Switch off Media?');
  await expect(dlg).toContainText('Its 3 subscriptions and 1 downloads are removed');
  await dlg.getByRole('button', { name: 'Switch off' }).click();
  // Then: delete it all? (a word to type) - Cancel keeps the files.
  await expect(dlg).toContainText('Also delete everything in Media?');
  await expect(dlg.getByRole('button', { name: 'Delete everything' })).toBeDisabled();
  await dlg.getByRole('button', { name: 'Cancel' }).click();
  await expect.poll(() => calls.find((c) => c.path === 'media/off')?.body).toEqual({ delete: false });
  await expect(media).not.toBeChecked();
  // (its save sent before leaving the page: one still waiting would go as the page closes - past the routes)
  await expect.poll(() => saved.mediaOff).toBe(true);
  // Comics is the last one on: it can't be switched off.
  await expect(page.getByRole('checkbox', { name: /^Comics/ })).toBeDisabled();
  // Media's gone from the menu, and its pages open Comics.
  await page.getByRole('button', { name: 'Menu' }).click();
  await expect(page.locator('#nav-drawer').getByRole('button', { name: 'Media', exact: true })).toHaveCount(0);
  await expect(page.locator('#nav-drawer').getByRole('button', { name: 'Duplicates' })).toHaveCount(0);
  await page.keyboard.press('Escape');
  await page.goto('/');
  await expect(page).toHaveURL(/\/comics$/);
  // On again: no question.
  await page.goto('/settings/viewing');
  await page.getByRole('checkbox', { name: /^Media/ }).click();
  await expect(page.getByRole('checkbox', { name: /^Media/ })).toBeChecked();
  await expect.poll(() => calls.filter((c) => c.path === 'settings').some((c) => (c.body as Record<string, unknown>).mediaOff === false)).toBe(true);
  await page.unrouteAll({ behavior: 'ignoreErrors' });
  // (the switch-off's save can leave as the page does - past the routes, to this worker's real server: Media stayed
  // off there and every later test opened on Comics)
  await page.evaluate(() => fetch('/api/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ mediaOff: false, comicsOff: false }) }));
});

test("settings: What's new is a page of its own with the changelog, newest first (built into the app)", async ({ page }) => {
  await page.goto('/settings/account');
  await page.locator('.about-links').getByRole('button', { name: 'What’s new' }).click(); // (also in the section list)
  await expect(page).toHaveURL(/\/settings\/changelog$/);
  await expect(page.getByRole('heading', { name: /^1\.0\.0/ })).toBeVisible();
  await expect(page.getByText('The first public release.')).toBeVisible();
  const heads = await page.locator('.log h2').allTextContents();
  expect(heads[heads.length - 1]).toMatch(/^1\.0\.0/); // (the oldest last)
});

test('the blacklist clean-up: deleted files leave the library behind it at once (no reload)', async ({ page }) => {
  // (mocked: nothing is really deleted - the library is shared by every test)
  await page.route('**/api/blacklist', (r) => r.fulfill({ json: { tags: ['blue_sky'] } }));
  await page.route('**/api/blacklist/matches', (r) => r.fulfill({ json: { total: 1, size: 1000, tags: [{ tag: 'blue_sky', files: 1 }] } }));
  await page.route('**/api/blacklist/delete', (r) => r.fulfill({ json: { deleted: 1, ids: [1] } }));
  await page.goto('/');
  await expect(page.getByText(/^26 files/)).toBeVisible();
  // To Tag rules inside the app (the library stays loaded behind it).
  await page.evaluate(() => {
    history.pushState({}, '', '/tags/rules');
    dispatchEvent(new PopStateEvent('popstate'));
  });
  await page.getByRole('button', { name: 'Delete them…' }).click();
  const searched = page.waitForRequest((r) => r.url().includes('/api/search'));
  await page.getByRole('alertdialog').getByRole('button', { name: /^Delete/ }).click();
  await searched; // the library's search ran again
  await page.unrouteAll({ behavior: 'ignoreErrors' });
});
