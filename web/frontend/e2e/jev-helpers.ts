import type { Page } from '@playwright/test'

// Record-focused cases explicitly select the alternate renderer. Production
// always opens the flowchart, regardless of old local-storage preferences.
export async function showExecutionLanes(page: Page) {
  for (const workflow of await page.getByTestId('agent-workflow').all()) {
    const toggle = workflow.getByRole('button', { name: '切换为泳道图', exact: true })
    if (await toggle.count()) {
      await toggle.click()
      await workflow.locator('.workflow-follow').click()
    }
  }
}
