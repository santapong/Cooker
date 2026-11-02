import { defineConfig, devices } from '@playwright/test';
// Build this directory first; browser checks reuse that exact build.
const port = 4648, output = process.env.COOKER_NATIVE_UI_BUILD_DIR ?? 'native-runtime-dist';
export default defineConfig({
  testDir: 'e2e', testMatch: /(?:native-runtime|runtime-logs)\.spec\.ts$/, timeout: 30000,
  expect: { timeout: 5000 }, workers: 2, retries: 0,
  reporter: [['list']], outputDir: 'test-results/native-runtime',
  use: { baseURL: `http://127.0.0.1:${port}`, trace: 'off', screenshot: 'off' },
  webServer: {
    command: `npm run preview -- --outDir ${output} --host 127.0.0.1 --port ${port} --strictPort`,
    env: { VITE_LOCAL_AUTH_ENABLED: 'true', VITE_OIDC_ENABLED: 'false' },
    url: `http://127.0.0.1:${port}`, reuseExistingServer: false, timeout: 180000,
  },
  projects: [{ name: 'native-runtime-chromium', use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 } } }],
});
