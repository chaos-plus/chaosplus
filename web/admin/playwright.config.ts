import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
	use: { baseURL: 'http://127.0.0.1:5173', trace: 'retain-on-failure', channel: 'chrome' },
  projects: [
    { name: 'desktop', use: { ...devices['Desktop Chrome'] } },
    { name: 'mobile', use: { ...devices['Pixel 7'] } },
  ],
  webServer: { command: 'npm run dev -- --host 127.0.0.1', url: 'http://127.0.0.1:5173', reuseExistingServer: true },
})
