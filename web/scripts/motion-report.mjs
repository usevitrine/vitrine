// Reads the motion recordings (e2e/motion.spec.ts) and says, per recording: how long until the screen settled, a
// contact sheet of its frames (sheet.png), and frames that look wrong:
//   late step    - the motion had slowed, then a frame moved more again (a last-moment pixel drop, a snap at the end)
//   long tail    - still changing well after most of the motion (something lingering)
//   jump         - most of the change in one frame (a pop rather than a motion; fine for a color, not for a move)
// Needs ffmpeg on the PATH. Usage: node scripts/motion-report.mjs <folder> [name filter]
import fs from 'node:fs';
import path from 'node:path';
import { execFileSync } from 'node:child_process';

const dir = process.argv[2];
const filter = process.argv[3] ?? '';
if (!dir) {
  console.error('usage: node scripts/motion-report.mjs <folder> [name filter]');
  process.exit(1);
}
const W = 360; // analysed at this width (gray)

function frames(rec) {
  const files = fs.readdirSync(rec).filter((f) => /^f_\d+\.png$/.test(f)).sort();
  if (!files.length) return null;
  const probe = execFileSync('ffmpeg', ['-v', 'error', '-i', path.join(rec, files[0]), '-vf', `scale=${W}:-2`, '-f', 'rawvideo', '-pix_fmt', 'gray', '-'], { maxBuffer: 1 << 26 });
  const H = probe.length / W;
  const raw = execFileSync('ffmpeg', ['-v', 'error', '-f', 'image2', '-i', path.join(rec, 'f_%04d.png'), '-vf', `scale=${W}:${H}`, '-f', 'rawvideo', '-pix_fmt', 'gray', '-'], { maxBuffer: 1 << 30 });
  const n = raw.length / (W * H);
  return { n, H, at: (k) => raw.subarray(k * W * H, (k + 1) * W * H) };
}
const diff = (a, b) => {
  let s = 0;
  for (let i = 0; i < a.length; i++) {
    const d = Math.abs(a[i] - b[i]);
    if (d > 3) s += d; // (fainter than that: the last steps of a fade, rounding - not to be seen)
  }
  return s / a.length;
};

const rows = [];
for (const name of fs.readdirSync(dir).sort()) {
  const rec = path.join(dir, name);
  if (!fs.statSync(rec).isDirectory() || (filter && !name.startsWith(filter))) continue;
  const err = fs.existsSync(path.join(rec, 'error.txt')) ? fs.readFileSync(path.join(rec, 'error.txt'), 'utf8').split('\n')[0] : '';
  const f = frames(rec);
  if (!f || f.n < 2) {
    rows.push({ name, note: err ? `step failed: ${err}` : 'no motion recorded' });
    continue;
  }
  const times = JSON.parse(fs.readFileSync(path.join(rec, 'times.json'), 'utf8'));
  const ms = (k) => Math.round((times[k] - times[0]) * 1000);
  const last = f.at(f.n - 1);
  const energy = [0];
  const toEnd = [];
  for (let k = 0; k < f.n; k++) {
    if (k) energy.push(diff(f.at(k), f.at(k - 1)));
    toEnd.push(diff(f.at(k), last));
  }
  const total = energy.reduce((a, b) => a + b, 0) || 1;
  const moving = energy.map((e, k) => (e > 0.05 ? k : -1)).filter((k) => k > 0);
  const start = moving[0] ?? 0;
  const end = moving.at(-1) ?? 0;
  const issues = [];
  // late step: after the peak, a frame moving at least 1.8x the one before, once the motion had slowed below 25% of the peak
  const peak = energy.indexOf(Math.max(...energy));
  for (let k = peak + 2; k <= end; k++) {
    if (energy[k - 1] < energy[peak] * 0.25 && energy[k] > energy[k - 1] * 1.8 && energy[k] > 0.15) issues.push(`late step at ${ms(k)} ms (frame ${k}: ${energy[k].toFixed(2)} after ${energy[k - 1].toFixed(2)})`);
  }
  // long tail: 95% of the change done, then still changing for over 250 ms
  let acc = 0;
  let k95 = start;
  for (let k = 1; k < f.n; k++) {
    acc += energy[k];
    if (acc >= total * 0.95) {
      k95 = k;
      break;
    }
  }
  if (ms(end) - ms(k95) > 250 && energy.slice(k95 + 1).some((e) => e > 0.1)) issues.push(`long tail: 95% done at ${ms(k95)} ms, still changing until ${ms(end)} ms`);
  // jump: one frame carries over 60% of all the change (and there was more than one frame of motion)
  const big = energy.indexOf(Math.max(...energy));
  if (moving.length > 2 && energy[big] / total > 0.6) issues.push(`jump: ${Math.round((energy[big] / total) * 100)}% of the change in frame ${big} (${ms(big)} ms)`);
  if (err) issues.unshift(`step failed: ${err}`);
  // the contact sheet
  try {
    const cols = 6;
    const pick = Math.min(f.n, 36);
    const step = Math.max(1, Math.floor(f.n / pick));
    execFileSync('ffmpeg', ['-v', 'error', '-y', '-f', 'image2', '-i', path.join(rec, 'f_%04d.png'), '-vf', `select='not(mod(n\\,${step}))',scale=320:-2,tile=${cols}x${Math.ceil(pick / cols)}`, '-frames:v', '1', path.join(rec, 'sheet.png')]);
  } catch {
    /* no sheet */
  }
  rows.push({ name, frames: f.n, settled: ms(end), note: issues.join('; ') || 'ok' });
}

const report = ['| recording | frames | settled | notes |', '|---|---|---|---|', ...rows.map((r) => `| ${r.name} | ${r.frames ?? ''} | ${r.settled != null ? r.settled + ' ms' : ''} | ${r.note} |`)].join('\n');
fs.writeFileSync(path.join(dir, 'report.md'), report + '\n');
console.log(report);
