// Test backends for the end-to-end tests, made once per run: builds the web
// app and Vitrine (the built app is what's tested - what ships), fills a
// throwaway library with generated images and videos (needs Go and ffmpeg),
// then starts one `vitrine serve` per worker, each on its own copy of that
// library and its own port (PORT0 + worker number) - so tests run side by side
// without touching each other's files, tags or settings. Logs: <work>/w<n>.log.
import { execFileSync, spawn, type ChildProcess } from 'node:child_process';
import { cpSync, existsSync, mkdirSync, openSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import type { FullConfig } from '@playwright/test';
import { PORT0 } from './fixtures';

const root = resolve(import.meta.dirname, '../..');
const web = resolve(import.meta.dirname, '..');
const work = join(tmpdir(), 'vitrine-e2e');
const media = join(work, 'media'); // kept between runs (the same every time)
const template = join(work, 'template');
const bin = join(work, process.platform === 'win32' ? 'vitrine.exe' : 'vitrine');

const run = (cmd: string, args: string[], cwd = root) =>
  execFileSync(cmd, args, { stdio: ['ignore', 'ignore', 'inherit'], cwd, shell: process.platform === 'win32' && cmd === 'npm' });

function makeMedia() {
  if (existsSync(join(media, 'portrait.mp4'))) return;
  mkdirSync(media, { recursive: true });
  const ffmpeg = (args: string[], out: string) => run('ffmpeg', ['-v', 'error', '-y', ...args, join(media, out)]);
  // 24 images of assorted shapes, including a very tall and a very wide one.
  const sizes = ['800x600', '600x800', '1024x1024', '1920x1080', '400x3000', '4000x500'];
  for (let i = 0; i < 24; i++) {
    const size = sizes[i % sizes.length];
    ffmpeg(['-f', 'lavfi', '-i', `testsrc2=size=${size}`, '-frames:v', '1', '-vf', `hue=h=${i * 15}`], `img-${i}.png`);
  }
  // Videos: landscape with sound, portrait without.
  ffmpeg(
    ['-f', 'lavfi', '-i', 'testsrc2=size=1280x720:rate=30:duration=15', '-f', 'lavfi', '-i', 'anullsrc=channel_layout=stereo:sample_rate=44100',
      '-c:v', 'libx264', '-pix_fmt', 'yuv420p', '-c:a', 'aac', '-shortest', '-movflags', '+faststart'],
    'clip.mp4',
  );
  ffmpeg(['-f', 'lavfi', '-i', 'mandelbrot=size=720x1280:rate=25', '-t', '8', '-c:v', 'libx264', '-pix_fmt', 'yuv420p'], 'portrait.mp4');
}

// Two comics (their pages are never in the library: it still has 26 files): a
// 12-page one and a 3-page one, portrait pages.
const comics = join(work, 'comics');
function makeComics() {
  if (existsSync(join(comics, 'Short Story', 'p3.png'))) return;
  const ffmpeg = (args: string[], out: string) => run('ffmpeg', ['-v', 'error', '-y', ...args, out]);
  for (const [title, n] of [['Blue Sky Comic', 12], ['Short Story', 3]] as const) {
    const dir = join(comics, title);
    mkdirSync(dir, { recursive: true });
    for (let i = 1; i <= n; i++) {
      ffmpeg(['-f', 'lavfi', '-i', 'testsrc2=size=800x1200', '-frames:v', '1', '-vf', `hue=h=${i * 25 + n}`], join(dir, `p${i}.png`));
    }
  }
}

function makeTemplate() {
  const data = join(template, 'data');
  rmSync(template, { recursive: true, force: true });
  mkdirSync(data, { recursive: true });
  run(bin, ['import', '-data-dir', data, media]);
  // IDs follow import order (file names sorted: clip, img-0.., portrait).
  run(bin, ['tag', '-data-dir', data, '-add', 'blue_sky', '-add', 'landscape', '2', '3', '4', '5']);
  run(bin, ['tag', '-data-dir', data, '-add', 'red', '6', '7']);
  makeComics();
  run(bin, ['import-comic', '-data-dir', data, join(comics, 'Blue Sky Comic'), join(comics, 'Short Story')]);
  // The test sites below never answer: they'd pause after a few errors (their searches waiting instead of failing).
  // Off here; the test of pausing turns it on for itself.
  run(bin, ['setting', '-data-dir', data, 'pauseAfter', '0']);

  // Two test sites that never deliver anything (no real site is contacted by the
  // download tests): test-down refuses at once (searches fail), test-slow never
  // answers (192.0.2.1 is reserved and unrouted), so a search stays running and
  // the next one waits in the queue.
  mkdirSync(join(data, 'plugins'), { recursive: true });
  for (const [id, base] of [
    ['test-down', 'https://127.0.0.1:1'],
    ['test-slow', 'https://192.0.2.1'],
    ['test-hang', 'https://192.0.2.2'], // never answers either: for one test alone (its queue is never taken)
  ]) {
    // (test-down has an OR search, like some Gelbooru 0.2 sites; the others don't.)
    const or = id === 'test-down' ? ', or: "( {tags} )"' : '';
    writeFileSync(join(data, 'plugins', `${id}.yaml`), `id: ${id}\nsite: ${id}.invalid\nengine: gelbooru-v02\nbase: ${base}\npost_url: ${base}/post/{id}\nsearch: {per_page: 10${or}}\n`);
  }
  // A site that needs an account (Settings > Sites tests): down like test-down.
  writeFileSync(
    join(data, 'plugins', 'test-auth.yaml'),
    `id: test-auth\nname: Test Auth\nsite: test-auth.invalid\nengine: gelbooru-v02\nbase: https://127.0.0.1:1\nauth: {fields: [api_key, user_id], required: true, help: Copy them from your account options.}\n`,
  );

  // A local plugin list (Settings > Sites > Add site): a new site to install and
  // a newer test-down (the installed one has no version).
  const index = join(template, 'plugin-index');
  mkdirSync(join(index, 'plugins'), { recursive: true });
  writeFileSync(
    join(index, 'plugins', 'test-extra.yaml'),
    `id: test-extra\nname: Test Extra\nsite: test-extra.invalid\nversion: 1.0.0\nengine: gelbooru-v02\nbase: https://127.0.0.1:1\npost_url: https://127.0.0.1:1/post/{id}\n`,
  );
  writeFileSync(
    join(index, 'plugins', 'test-down.yaml'),
    `id: test-down\nsite: test-down.invalid\nversion: 1.1.0\nengine: gelbooru-v02\nbase: https://127.0.0.1:1\npost_url: https://127.0.0.1:1/post/{id}\nsearch: {per_page: 10, or: "( {tags} )"}\n`, // (still with its OR search: tests running after the update rely on it)
  );
  writeFileSync(
    join(index, 'index.json'),
    JSON.stringify({
      plugins: [
        { id: 'test-extra', name: 'Test Extra', site: 'test-extra.invalid', version: '1.0.0', file: 'plugins/test-extra.yaml', description: 'A test site.', nsfw: true },
        { id: 'test-down', name: 'test-down', site: 'test-down.invalid', version: '1.1.0', file: 'plugins/test-down.yaml' },
      ],
    }),
  );
}

async function healthy(port: number, server: ChildProcess) {
  const until = Date.now() + 60_000;
  while (Date.now() < until) {
    if (server.exitCode !== null) throw new Error(`test server on ${port} exited (see ${work}/w${port - PORT0}.log)`);
    try {
      if ((await fetch(`http://127.0.0.1:${port}/api/health`)).ok) return;
    } catch {
      /* not up yet */
    }
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error(`test server on ${port} didn't start`);
}

export default async function setup(config: FullConfig) {
  const workers = config.workers;
  mkdirSync(work, { recursive: true });
  run('npm', ['run', 'build', '--silent'], web); // the app, into internal/server/web (embedded)
  run('go', ['build', '-o', bin, './cmd/vitrine']);
  makeMedia();
  makeTemplate();

  const servers: ChildProcess[] = [];
  for (let i = 0; i < workers; i++) {
    const dir = join(work, `w${i}`);
    rmSync(dir, { recursive: true, force: true });
    cpSync(join(template, 'data'), dir, { recursive: true });
    const log = openSync(join(work, `w${i}.log`), 'w');
    servers.push(
      spawn(bin, ['serve', '-data-dir', dir, '-listen', `127.0.0.1:${PORT0 + i}`], {
        stdio: ['ignore', log, log],
        cwd: root,
        env: { ...process.env, VITRINE_PLUGIN_INDEX: join(template, 'plugin-index'), VITRINE_RELEASES_URL: 'off' }, // (no update checks against GitHub)
      }),
    );
  }
  await Promise.all(servers.map((s, i) => healthy(PORT0 + i, s)));
  return async () => {
    for (const s of servers) s.kill();
  };
}
