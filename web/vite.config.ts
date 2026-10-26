import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

// The build goes into the Go server's embedded web folder, so `go build`
// after `npm run build` produces one binary with the web app inside.
// `npm run dev` serves the app with hot reload and forwards API, file and
// thumbnail requests to a Vitrine server on :8080 (`vitrine serve`).
const backend = process.env.VITRINE_BACKEND ?? 'http://127.0.0.1:8080';
// The server refuses changes whose Origin isn't its own host, so proxied
// requests present the backend's host and origin.
const toBackend = { target: backend, changeOrigin: true, headers: { origin: backend } };

// `npm run build:demo` (mode demo): the website's demo instead - the same app with a pretend server in the page
// (src/demo), into dist-demo for the website (its base: where the site serves it, DEMO_BASE).
export default defineConfig(({ mode }) => ({
  plugins: [svelte()],
  base: mode === 'demo' ? (process.env.DEMO_BASE ?? '/demo/app/') : '/',
  build: {
    outDir: mode === 'demo' ? 'dist-demo' : '../internal/server/web/app',
    emptyOutDir: true,
    target: 'es2022',
    // The bundled packages' licenses (served at /licenses with the Go side's: internal/server/licenses.go).
    license: { fileName: 'licenses.md' },
  },
  server: {
    fs: { allow: ['..'] }, // the language files in ../locales
    proxy: {
      '/api': toBackend,
      '/files': toBackend,
      '/thumbs': toBackend,
      '/sprites': toBackend,
    },
  },
}));
