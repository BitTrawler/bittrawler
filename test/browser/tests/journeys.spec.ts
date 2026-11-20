import { createHash } from 'node:crypto';
import { test, expect } from '@playwright/test';

const hash = (index: number) => createHash('sha1').update(`bittrawler-links-${index}`).digest('hex');

const MAGNET_PREFIX = 'magnet:?xt=urn:btih:';

test('signing in and out', async ({ page }) => {
  // The storage state is signed in; start from a signed-out browser.
  await page.context().clearCookies();
  await page.goto('/login');

  await page.getByLabel('Username').fill('admin');
  await page.getByLabel('Password').fill('wrong-password');
  await page.getByRole('button', { name: 'Sign In' }).click();
  await expect(page.locator('.notice.error')).toContainText('Wrong username or password');

  await page.getByLabel('Password').fill('correct-horse-battery');
  await page.getByRole('button', { name: 'Sign In' }).click();
  await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible();

  await page.getByRole('button', { name: 'Sign Out' }).click();
  await expect(page).toHaveURL(/\/login$/);
});

test('searching, expanding a result and the keyboard disclosure', async ({ page }) => {
  await page.goto('/');
  await page.locator('.side-search input[name=q]').fill('1080p');
  await page.locator('.side-search input[name=q]').press('Enter');

  await expect(page).toHaveURL(/\/search\?q=1080p/);
  await expect(page.locator('li.result-item')).toHaveCount(8);

  // The toggle answers to the keyboard, and the detail row is hidden until then.
  const toggle = page.locator('.name-toggle').first();
  const detail = page.locator('.result-detail').first();
  await expect(detail).toBeHidden();
  await toggle.focus();
  await page.keyboard.press('Enter');
  await expect(detail).toBeVisible();
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
  await expect(detail.locator('.code-block')).toContainText(MAGNET_PREFIX);
});

test('copying a magnet link puts it on the clipboard and announces it', async ({ page }) => {
  await page.goto(`/torrent/${hash(0)}`);

  await page.getByRole('button', { name: 'Copy' }).first().click();

  await expect(page.locator('#announcer')).toContainText('Copied to clipboard');
  const clipboard = await page.evaluate(() => navigator.clipboard.readText());
  expect(clipboard.startsWith(MAGNET_PREFIX)).toBe(true);
});

test('the skip link is the first tab stop and moves focus to the content', async ({ page }) => {
  await page.goto('/');
  await page.keyboard.press('Tab');
  await expect(page.locator('.skip')).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(page.locator('#content')).toBeFocused();
});

test('each live feed pauses on its own', async ({ page }) => {
  await page.goto('/');

  const polls: string[] = [];
  page.on('request', (request) => {
    if (request.url().includes('/api/feed')) {
      polls.push(request.url());
    }
  });

  // The poll runs every three seconds, so one has to happen before pausing.
  await expect.poll(() => polls.length, { timeout: 8_000 }).toBeGreaterThan(0);

  // Pausing one list leaves the other live, so the poll carries on.
  await page.getByRole('button', { name: 'Pause updates to Recently Added' }).click();
  await expect(page.getByRole('button', { name: 'Resume updates to Recently Added' })).toBeVisible();

  const onePaused = polls.length;
  await expect.poll(() => polls.length, { timeout: 8_000 }).toBeGreaterThan(onePaused);

  // Pausing the second list stops the poll entirely.
  await page.getByRole('button', { name: 'Pause updates to Recently Rejected' }).click();
  const bothPaused = polls.length;
  await page.waitForTimeout(3_500);
  expect(polls.length).toBe(bothPaused);

  // Either list starts it again.
  await page.getByRole('button', { name: 'Resume updates to Recently Added' }).click();
  await expect.poll(() => polls.length, { timeout: 8_000 }).toBeGreaterThan(bothPaused);
  await page.getByRole('button', { name: 'Resume updates to Recently Rejected' }).click();
});

test('the pages make no requests off the machine', async ({ page }) => {
  const external: string[] = [];
  page.on('request', (request) => {
    const host = new URL(request.url()).hostname;
    if (host !== '127.0.0.1' && host !== 'localhost') {
      external.push(request.url());
    }
  });

  for (const path of ['/', '/search?q=1080p', `/torrent/${hash(0)}`, '/settings']) {
    await page.goto(path);
    await page.waitForTimeout(400);
  }

  expect(external).toEqual([]);
});

test('the catalogue link is on the torrent page', async ({ page }) => {
  await page.goto(`/torrent/${hash(0)}`);
  // Dune: Part Two is TMDB 693134 in the seed data.
  await expect(page.getByRole('link', { name: 'TMDB' })).toHaveAttribute(
    'href',
    'https://www.themoviedb.org/movie/693134',
  );
});

test('a missing torrent shows the not-found page', async ({ page }) => {
  await page.goto('/torrent/9999999999999999999999999999999999999999');
  await expect(page.getByRole('heading', { name: 'Torrent Not Found' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Back to search' })).toBeVisible();
});

test('the pages load without console errors or failed requests', async ({ page }) => {
  const problems: string[] = [];
  page.on('console', (message) => {
    if (message.type() === 'error') {
      problems.push(message.text());
    }
  });
  page.on('pageerror', (error) => problems.push(String(error)));
  page.on('requestfailed', (request) => problems.push(`request failed: ${request.url()}`));

  for (const path of ['/', '/search?q=1080p', `/torrent/${hash(0)}`, '/settings']) {
    await page.goto(path);
    await page.waitForTimeout(600);
  }

  expect(problems).toEqual([]);
});

// Destructive journeys run last: they remove rows from the throwaway index.
test('removing a torrent asks first, then drops it', async ({ page }) => {
  await page.goto(`/torrent/${hash(16)}`);
  await expect(page.getByRole('heading', { name: /Chernobyl/ })).toBeVisible();

  page.once('dialog', async (dialog) => {
    expect(dialog.message()).toContain('Remove this torrent from the index?');
    await dialog.accept();
  });

  await page.getByRole('button', { name: 'Remove', exact: true }).click();
  await expect(page).toHaveURL(/\?removed=removed/);
  await expect(page.locator('.notice')).toContainText('Torrent removed from the index.');

  await page.goto('/search?q=Chernobyl');
  await expect(page.locator('li.result-item')).toHaveCount(0);
});

test('blocking a torrent asks first, then drops and remembers it', async ({ page }) => {
  await page.goto(`/torrent/${hash(17)}`);
  await expect(page.getByRole('heading', { name: /Some.Misc.Archive/ })).toBeVisible();

  page.once('dialog', async (dialog) => {
    expect(dialog.message()).toContain('block its hash?');
    await dialog.accept();
  });

  await page.getByRole('button', { name: 'Block and Remove' }).click();
  await expect(page).toHaveURL(/\?removed=blocked/);
  await expect(page.locator('.notice')).toContainText('its hash blocked');

  await page.goto('/search?q=Some.Misc.Archive');
  await expect(page.locator('li.result-item')).toHaveCount(0);
});
