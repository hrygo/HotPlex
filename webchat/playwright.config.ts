import { defineConfig, devices } from '@playwright/test';

const chromiumExecutablePath = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH;
const playwrightPort = /^\d+$/.test(process.env.PLAYWRIGHT_PORT ?? '')
  ? process.env.PLAYWRIGHT_PORT
  : '3000';
const baseURL = `http://127.0.0.1:${playwrightPort}`;

export default defineConfig({
  testDir: './e2e',
  timeout: 30_000,
  expect: { timeout: 10_000 },
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [['html', { open: 'never' }], ['list']] : 'list',
  use: {
    baseURL,
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
  },
  projects: [
    {
      name: 'chromium',
      use: {
        ...devices['Desktop Chrome'],
        launchOptions: chromiumExecutablePath
          ? { executablePath: chromiumExecutablePath }
          : undefined,
      },
    },
  ],
  webServer: {
    command: `pnpm exec next dev --hostname 127.0.0.1 --port ${playwrightPort}`,
    url: baseURL,
    reuseExistingServer: !process.env.CI,
    timeout: 60_000,
  },
});
