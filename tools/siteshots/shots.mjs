// The website's screenshots (vitrine.software): Vitrine - this checkout, built - on a copy of the demo library
// (tools/demolib), every screen at desktop and phone widths, into the site's src/assets/shots. Run from web/ (it
// uses its Playwright):  node ../tools/siteshots/shots.mjs [demo work folder] [site folder]
// A throwaway account on 127.0.0.1 (generated per run, never shown); SFW mode on (no "I came", no ratings) and the
// first steps done; the Downloads page from made-up downloads (no site is contacted).
import { execFileSync, spawn } from 'node:child_process';
import { cpSync, mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { randomBytes } from 'node:crypto';
import { tmpdir } from 'node:os';
import { createRequire } from 'node:module';
import { join, resolve } from 'node:path';

const root = resolve(import.meta.dirname, '../..');
const { chromium } = createRequire(join(root, 'web', 'package.json'))('playwright'); // (the web app's)
const demo = resolve(process.argv[2] ?? join(process.env.LOCALAPPDATA ?? tmpdir(), 'VitrineDemo'));
const out = resolve(process.argv[3] ?? join(root, '..', 'vitrine.software', 'src', 'assets', 'shots'));
const work = join(tmpdir(), 'vitrine-siteshots');
const bin = join(work, process.platform === 'win32' ? 'vitrine.exe' : 'vitrine');
const config = join(work, 'config.toml'); // (never the machine's own: an installed Vitrine's)
const env = { ...process.env, VITRINE_CONFIG: config };
const port = 8095;
const base = `http://127.0.0.1:${port}`;

const run = (cmd, args, cwd = root) => execFileSync(cmd, args, { stdio: ['ignore', 'ignore', 'inherit'], cwd, env, shell: process.platform === 'win32' && cmd === 'npm' });

rmSync(work, { recursive: true, force: true });
mkdirSync(work, { recursive: true });
writeFileSync(config, '');
run('npm', ['run', 'build', '--silent'], join(root, 'web'));
// (the latest release's version: Settings > Updates shows it, not "dev")
const version = execFileSync('git', ['describe', '--tags', '--abbrev=0', '--match', 'v*'], { cwd: root }).toString().trim();
run('go', ['build', '-ldflags', `-X main.version=${version}`, '-o', bin, './cmd/vitrine']);
const data = join(work, 'data');
const files = join(work, 'files');
cpSync(join(demo, 'library', 'data'), data, { recursive: true });
cpSync(join(demo, 'library', 'files'), files, { recursive: true });
const dirs = ['-data-dir', data, '-files-dir', files];
for (const [k, v] of [['welcome', '"finished"'], ['sfwMode', 'true'], ['cameButton', 'false']]) run(bin, ['setting', ...dirs, k, v]);
const server = spawn(bin, ['serve', ...dirs, '-listen', `127.0.0.1:${port}`], { env, stdio: 'ignore' });
for (let i = 0; ; i++) {
  try {
    if ((await fetch(`${base}/api/health`)).ok) break;
  } catch {}
  if (i > 100) throw new Error('vitrine did not start');
  await new Promise((r) => setTimeout(r, 200));
}

const user = 'demo';
const password = randomBytes(18).toString('base64url');
mkdirSync(out, { recursive: true });
const browser = await chromium.launch();
const views = {
  desktop: { viewport: { width: 1920, height: 1080 }, deviceScaleFactor: 1 },
  phone: { viewport: { width: 390, height: 792 }, deviceScaleFactor: 2, isMobile: true, hasTouch: true },
};
try {
  for (const [kind, v] of Object.entries(views)) {
    const ctx = await browser.newContext(v);
    const page = await ctx.newPage();
    await page.goto(`${base}/api/health`);
    const status = await page.evaluate(
      async ([name, password]) => {
        const s = await (await fetch('/api/session')).json();
        if (s.logged_in) return 200;
        return (await fetch(s.setup_needed ? '/api/setup' : '/api/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name, password }) })).status;
      },
      [user, password],
    );
    if (status !== 200) throw new Error(`login: ${status}`);
    const shot = async (name) => {
      await page.waitForTimeout(900);
      await page.screenshot({ path: join(out, `${kind}-${name}.png`) });
      console.log(`${kind}-${name}`);
    };
    const settle = async () => {
      await page.waitForLoadState('networkidle');
      await page.waitForTimeout(600);
    };

    // The library in Rows, newest first.
    await page.goto(`${base}/`);
    await page.getByText(/files?$/).first().waitFor();
    await settle();
    await shot('library');
    // A search: chips in the bar.
    await page.goto(`${base}/?q=${encodeURIComponent('woodblock_print -portraits')}`);
    await settle();
    await shot('search');
    // Squares.
    await page.evaluate(() => localStorage.setItem('vitrine.gridLayout', '"squares"'));
    await page.goto(`${base}/`);
    await settle();
    await shot('squares');
    await page.evaluate(() => localStorage.setItem('vitrine.gridLayout', '"masonry"'));
    // The viewer with its details: a painting.
    await page.goto(`${base}/?q=${encodeURIComponent('creator:vincent_van_gogh')}`);
    await settle();
    await page.locator('.tile').first().click();
    await page.waitForTimeout(2500); // (the picture loaded before the details cover part of it)
    await shot('picture');
    await page.getByRole('button', { name: /details/i }).first().click();
    await settle();
    await page.waitForTimeout(1500);
    await shot('viewer');
    await page.keyboard.press('Escape');
    await page.keyboard.press('Escape');
    // A clip in the viewer, paused mid-scene with its controls showing.
    await page.goto(`${base}/?q=${encodeURIComponent('big_buck_bunny')}&sort=imported_asc`);
    await settle();
    await page.locator('.tile').nth(1).click();
    await page.waitForTimeout(2000);
    await page.evaluate(() => {
      const v = document.querySelector('.viewer video');
      if (v) {
        v.pause();
        v.currentTime = 3;
      }
    });
    await page.mouse.move(v.viewport.width / 2, v.viewport.height / 2);
    await page.mouse.move(v.viewport.width / 2 + 20, v.viewport.height / 2 + 10);
    await page.waitForTimeout(500);
    await page.screenshot({ path: join(out, `${kind}-video.png`) });
    console.log(`${kind}-video`);
    // Comics and the reader.
    await page.goto(`${base}/comics`);
    await settle();
    await shot('comics');
    await page.locator('.tile', { hasText: 'Nemo' }).first().click(); // (its scans are the sharpest)
    await settle();
    await shot('reader');
    // Tags.
    await page.goto(`${base}/tags/all`);
    await settle();
    await shot('tags');
    // Downloads, from made-up downloads (nothing is contacted).
    const search = (id, query, state, extra = {}) => ({
      id, site: 'safebooru', query, state, max_posts: 0, site_total: 0, handled: 0, downloaded: 0, already: 0, deleted: 0, skipped: 0, blacklisted: 0,
      position: id, failed: 0, created_at: Date.now() - id * 60000, ...extra,
    });
    const searches = [
      search(1, 'landscape', 'running', { site_total: 1840, handled: 512, downloaded: 498, already: 14 }),
      search(2, 'blue_sky', 'running', { site_total: 3000, handled: 1284, downloaded: 1260, already: 20, blacklisted: 4 }),
      search(3, 'cherry_blossoms', 'queued', { site_total: 640 }),
      search(4, 'mountain', 'done', { site_total: 220, handled: 220, downloaded: 214, already: 6 }),
      search(5, 'ocean', 'done', { site_total: 95, handled: 95, downloaded: 95 }),
    ];
    await page.route('**/api/downloads?active=1', (r) => r.fulfill({ json: { searches: searches.filter((s) => s.state !== 'done'), sites_paused: {} } }));
    await page.route('**/api/downloads', (r) =>
      r.request().method() === 'GET' ? r.fulfill({ json: { sites: ['safebooru'], searches, subscriptions: [], groups: [], refresh: [] } }) : r.continue(),
    );
    await page.goto(`${base}/downloads`);
    await settle();
    await shot('downloads');
    await page.unrouteAll({ behavior: 'ignoreErrors' });
    // Settings > Updates.
    await page.goto(`${base}/settings/updates`);
    await settle();
    await shot('updates');
    await ctx.close();
  }
} finally {
  await browser.close();
  server.kill();
}
