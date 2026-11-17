import { createHash } from 'node:crypto';
import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

const TAGS = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa', 'best-practice'];

// The screenshot harness numbers the seed rows, and their hashes are the SHA-1
// of a fixed string.
const hash = (index: number) => createHash('sha1').update(`bittrawler-links-${index}`).digest('hex');

type PageState = {
  name: string;
  path: string;
  title: RegExp;
  expand?: boolean;
};

const states: PageState[] = [
  { name: 'overview', path: '/', title: /Overview/ },
  { name: 'search', path: '/search?q=1080p', title: /Search/ },
  { name: 'search-expanded', path: '/search?q=1080p', title: /Search/, expand: true },
  { name: 'search-all', path: '/search', title: /Search/ },
  { name: 'search-none', path: '/search?q=zzzznotfound', title: /Search/ },
  { name: 'torrent-film', path: `/torrent/${hash(0)}`, title: /Dune/ },
  { name: 'torrent-tv', path: `/torrent/${hash(2)}`, title: /Breaking/ },
  { name: 'settings', path: '/settings', title: /Settings/ },
  { name: 'settings-advanced', path: '/settings?advanced=1', title: /Settings/ },
  { name: 'not-found', path: '/torrent/9999999999999999999999999999999999999999', title: /Torrent Not Found/ },
];

test.describe('accessibility, light and dark', () => {
  for (const scheme of ['light', 'dark'] as const) {
    test.describe(scheme, () => {
      test.use({ colorScheme: scheme });

      for (const state of states) {
        test(`${state.name} has no axe violations`, async ({ page }) => {
          await page.goto(state.path);
          await expect(page).toHaveTitle(state.title);
          // The stylesheet transitions colours over 120-140ms, so axe must not
          // read them mid-transition.
          await page.waitForTimeout(300);

          if (state.expand) {
            await page.locator('.name-toggle').first().click();
            await expect(page.locator('.result-detail').first()).toBeVisible();
          }

          const results = await new AxeBuilder({ page }).withTags(TAGS).analyze();

          const summary = results.violations.map(
            (violation) =>
              `${violation.id} (${violation.impact}): ${violation.nodes
                .map((node) => node.target.join(' '))
                .join(', ')}`,
          );

          expect(summary, JSON.stringify(results.violations, null, 2)).toEqual([]);
        });
      }
    });
  }
});

test.describe('accessibility on a phone', () => {
  test.use({ colorScheme: 'light', viewport: { width: 375, height: 812 } });

  for (const state of [states[0], states[1]]) {
    test(`${state.name} has no axe violations at 375 pixels`, async ({ page }) => {
      await page.goto(state.path);
      await expect(page).toHaveTitle(state.title);
      await page.waitForTimeout(300);

      const results = await new AxeBuilder({ page }).withTags(TAGS).analyze();
      expect(
        results.violations.map((violation) => violation.id),
        JSON.stringify(results.violations, null, 2),
      ).toEqual([]);
    });
  }
});
