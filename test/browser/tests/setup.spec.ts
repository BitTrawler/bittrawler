import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

const TAGS = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa', 'best-practice'];

// The setup page runs before an administrator exists, so this project is the
// first thing that touches the server. It audits the page and then creates the
// account everything else signs in with.
test('the setup page is accessible and the account is created', async ({ page }) => {
  await page.goto('/setup');
  await expect(page.getByRole('heading', { name: 'Set Up BitTrawler' })).toBeVisible();

  const results = await new AxeBuilder({ page }).withTags(TAGS).analyze();
  expect(results.violations).toEqual([]);

  // The form has to go through the browser: the handler refuses cross-origin
  // posts, and an API request has no Origin header.
  await page.locator('input[name=username]').fill('admin');
  await page.locator('input[name=password]').fill('correct-horse-battery');
  await page.locator('input[name=password_confirm]').fill('correct-horse-battery');
  await page.getByRole('button', { name: 'Finish Setup' }).click();

  await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible();

  await page.context().storageState({ path: 'artifacts/storage-state.json' });
});
