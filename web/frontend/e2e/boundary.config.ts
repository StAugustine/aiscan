import { defineConfig } from '@playwright/test'
import { fileURLToPath } from 'node:url'

const port = process.env.CYBER_BOUNDARY_PORT || '38479'
const baseURL = process.env.BASE_URL || `http://127.0.0.1:${port}`
const guiURL = process.env.CYBER_BOUNDARY_GUI_URL || `http://127.0.0.1:${process.env.CYBER_BOUNDARY_BACKEND_PORT || '38480'}`
const artifacts = fileURLToPath(new URL('../../../.runlogs/rc7-boundary/', import.meta.url))
export default defineConfig({
  testDir: '.', testMatch: 'boundary*.spec.ts',
  timeout: 35_000, expect: { timeout: 8_000 }, workers: 1, retries: 0,
  reporter: [['list'], ['html', { open: 'never', outputFolder: `${artifacts}report` }]],
  outputDir: `${artifacts}results`,
  webServer: process.env.BASE_URL ? undefined : {
    cwd: '..', command: 'node e2e/boundary-server.mjs', url: baseURL,
    timeout: 240_000, reuseExistingServer: false, stdout: 'pipe', stderr: 'pipe',
  },
  use: { baseURL, headless: true, viewport: { width: 1280, height: 900 }, screenshot: 'only-on-failure', trace: 'retain-on-failure' },
  projects: [
    { name: 'webui', testMatch: 'boundary.spec.ts', use: { baseURL: guiURL } },
    { name: 'browser-client', testMatch: ['boundary-transport.spec.ts', 'boundary-history.spec.ts', 'boundary-projection.spec.ts', 'boundary-guardrail.spec.ts'] },
  ],
})
