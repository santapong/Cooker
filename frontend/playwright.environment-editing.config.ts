import { defineConfig, devices } from '@playwright/test';
const port = 4623;
export default defineConfig({
  testDir: 'e2e', testMatch: /environment-editing\.spec\.ts$/, timeout: 30000, expect: { timeout: 5000 }, workers: 2,
  reporter: [['list']], outputDir: 'environment-editing-test-results',
  use: { baseURL: `http://127.0.0.1:${port}`, trace: 'retain-on-failure' },
  webServer: { command: 'npm run build -- --outDir environment-editing-dist && npm run preview -- --outDir environment-editing-dist --host 127.0.0.1 --port 4623 --strictPort', env: { VITE_LOCAL_AUTH_ENABLED: 'true', VITE_OIDC_ENABLED: 'false' }, url: `http://127.0.0.1:${port}`, reuseExistingServer: false, timeout: 180000 },
  projects: [{ name: 'configuration-chromium', use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 1000 } } }],
});
