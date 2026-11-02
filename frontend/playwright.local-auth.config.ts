import { defineConfig, devices } from '@playwright/test';

// Separate build and port: a local-only authentication build must never replace
// the ordinary development-mode fixture bundle or a user's running preview.
const port = 4621;
const output = 'node_modules/.cache/cooker-local-auth-dist';

export default defineConfig({
  testDir: 'e2e',
  testMatch: /local-auth\.spec\.ts$/,
  timeout: 30_000,
  expect: { timeout: 5_000 },
  workers: 2,
  reporter: [['list']],
  outputDir: 'test-results/local-auth',
  use: { baseURL: `http://127.0.0.1:${port}`, trace: 'retain-on-failure' },
  webServer: {
    command: `npm run build -- --outDir ${output} && npm run preview -- --outDir ${output} --host 127.0.0.1 --port ${port} --strictPort`,
    env: { VITE_LOCAL_AUTH_ENABLED: 'true', VITE_OIDC_ENABLED: 'false' },
    url: `http://127.0.0.1:${port}`,
    reuseExistingServer: false,
    timeout: 180_000,
  },
  projects: [{ name: 'local-auth-chromium', use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 } } }],
});
