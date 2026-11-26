// The website's demo (vitrine.software/demo): `npm run build:demo [snapshot folder]`. Builds the web app in demo mode
// into dist-demo (vite --mode demo: a pretend server in the page, src/demo) with beside it the search engine - this
// program's index compiled to WebAssembly (cmd/demowasm) - and Go's loader for it; with a snapshot folder
// (tools/demosnap's: snapshot.json and media/), that too, so dist-demo is the whole demo. Needs Go.
import { execFileSync } from 'node:child_process';
import { cpSync, existsSync } from 'node:fs';
import { join, resolve } from 'node:path';

const web = resolve(import.meta.dirname, '..');
const root = resolve(web, '..');
const out = join(web, 'dist-demo');
const snapshot = process.argv[2] ? resolve(process.argv[2]) : '';
const run = (cmd, args, opts = {}) => execFileSync(cmd, args, { stdio: 'inherit', shell: process.platform === 'win32' && cmd === 'npx', ...opts });

run('npx', ['vite', 'build', '--mode', 'demo'], { cwd: web });
run('go', ['build', '-trimpath', '-ldflags', '-s -w', '-o', join(out, 'demo.wasm'), './cmd/demowasm'], {
  cwd: root,
  env: { ...process.env, GOOS: 'js', GOARCH: 'wasm' },
});
const goroot = execFileSync('go', ['env', 'GOROOT'], { cwd: root }).toString().trim();
const loader = [join(goroot, 'lib', 'wasm', 'wasm_exec.js'), join(goroot, 'misc', 'wasm', 'wasm_exec.js')].find(existsSync);
if (!loader) throw new Error('wasm_exec.js not found in GOROOT');
cpSync(loader, join(out, 'wasm_exec.js'));
if (snapshot) {
  cpSync(join(snapshot, 'snapshot.json'), join(out, 'snapshot.json'));
  cpSync(join(snapshot, 'media'), join(out, 'media'), { recursive: true });
}
console.log(`demo: ${out}${snapshot ? ' (with its library)' : ' (no library: add one with a snapshot folder)'}`);
