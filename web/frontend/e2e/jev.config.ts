import { defineConfig } from '@playwright/test'
import { fileURLToPath } from 'node:url'

// These tests mount production components in a source fixture. The embedded
// Go server intentionally serves only the production build, not e2e/*.tsx.
const port = process.env.CYBER_JEV_UI_PORT || '38184'
const baseURL = process.env.JEV_UI_BASE_URL || `http://127.0.0.1:${port}`

export default defineConfig({
  testDir: '.',
  testMatch: ['jev-motion.spec.ts', 'workflow.spec.ts', 'jev-ui.spec.ts', 'jev-decision-ui.spec.ts', 'jev-presentation.spec.ts',
    'jev-replay.spec.ts', 'jev-live-replay.spec.ts', 'jev-robustness.spec.ts', 'jev-v2.spec.ts'],
  timeout: 60_000,
  expect: { timeout: 15_000 },
  workers: 1,
  retries: 0,
  outputDir: '../test-results/jev',
  reporter: [['list'], ['json', { outputFile: '../test-results/jev-results.json' }],
    ['html', { outputFolder: '../playwright-report/jev', open: 'never' }]],
  webServer: process.env.JEV_UI_BASE_URL ? undefined : {
    command: `node ./node_modules/vite/bin/vite.js --host 127.0.0.1 --port ${port} --strictPort`,
    cwd: fileURLToPath(new URL('..', import.meta.url)),
    url: `${baseURL}/e2e/fixtures/jev.html`,
    timeout: 30_000,
    reuseExistingServer: false,
  },
  use: { baseURL, headless: true, viewport: { width: 1280, height: 720 },
    actionTimeout: 10_000, screenshot: 'only-on-failure', trace: 'retain-on-failure' },
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }],
})
