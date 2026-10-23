import { defineConfig, devices } from '@playwright/test';
import { cpus } from 'node:os';

// End-to-end tests. iOS is a first-class target, so the main project is an
// emulated iPhone on WebKit (Safari's engine) with touch. `npm run e2e`.
// Tests run side by side: each worker has its own test server and library
// (e2e/setup.ts), so they can't trip over each other.
export default defineConfig({
  testDir: './e2e',
  // Room for a busy machine (six browsers at once): a wait for something to show is 10 s, a test 45 s - a real
  // failure still fails, a slow moment no longer does (the 5 s default timed out under load now and then).
  timeout: 45_000,
  expect: { timeout: 10_000 },
  fullyParallel: true,
  // Each worker drives a browser (more than one core): half as many workers as
  // cores to use, and at most 3/4 of the machine (12 of 16 cores: 6 workers).
  // E2E_WORKERS overrides.
  workers: Number(process.env.E2E_WORKERS) || Math.max(2, Math.floor((cpus().length * 3) / 4 / 2)),
  // One retry: a test that fails only when the machine is busy passes on its
  // second try and is reported as "flaky" (listed, never hidden) instead of
  // failing the run; one that fails twice is a real failure.
  retries: 1,
  globalSetup: './e2e/setup.ts',
  reporter: [['list']],
  use: {
    trace: 'retain-on-failure',
    // American English (en-US, the main language file). One test switches to en-GB
    // in Settings and checks the British spelling.
    locale: 'en-US',
  },
  projects: [
    { name: 'iphone', use: { ...devices['iPhone 15'] } },
    { name: 'desktop-webkit', use: { ...devices['Desktop Safari'], viewport: { width: 1440, height: 900 } } },
    // Chrome, Brave and Edge run Chromium.
    // Muted: nothing from the tests comes out of the speakers (the test video's sound track is silent too).
    { name: 'desktop-chromium', use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 }, launchOptions: { args: ['--mute-audio'] } } },
  ],
});
