import { defineConfig, devices } from '@playwright/test';

const CI = !!process.env.CI;
// Not Vite's default 4173: another project's preview may own that port on a dev box.
const PORT = 4620;
const BASE = `http://127.0.0.1:${PORT}`;

/**
 * P6 verification suite (docs/plans/2026-07-cosmic-frontend-redesign.md §6).
 *
 * The suite runs against the production build served by `vite preview`, with
 * every /api/v1 call answered by the fixtures in e2e/fixtures/api.ts — no
 * backend, no network. Deep links such as /pipelines/gates/edit work because
 * vite.config.ts sets no `appType`, so preview keeps Vite's SPA history
 * fallback; if that ever changes, this suite is the first thing to break.
 *
 * Projects: `chromium` is the must-pass CI gate; `perf` holds the local-only
 * open-time / CLS budget checks the spec lists as non-CI browser checks.
 */
export default defineConfig({
  testDir: 'e2e',
  timeout: 30_000,
  expect: { timeout: 5_000 },
  fullyParallel: true,
  retries: CI ? 1 : 0,
  workers: CI ? 2 : undefined,
  reporter: [['list'], ['html', { open: 'never' }]],
  use: {
    baseURL: BASE,
    colorScheme: 'dark',
    trace: 'retain-on-failure',
  },
  webServer: {
    // CI has already run `npm run build`; locally build first so the suite
    // never asserts against a stale dist/.
    command: CI ? `npm run preview -- --host 127.0.0.1 --port ${PORT} --strictPort` : `npm run build && npm run preview -- --host 127.0.0.1 --port ${PORT} --strictPort`,
    url: BASE,
    // Never adopt a stranger on the port — a foreign server here once made
    // every assertion fail against someone else's app. Fail loudly instead.
    reuseExistingServer: false,
    timeout: 180_000,
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 } },
      testIgnore: /perf\.spec\.ts$/,
    },
    {
      name: 'perf',
      use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 } },
      testMatch: /perf\.spec\.ts$/,
    },
  ],
});
