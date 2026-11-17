import { createHash } from 'node:crypto';
import { test, expect } from '@playwright/test';

const hash = (index: number) => createHash('sha1').update(`bittrawler-links-${index}`).digest('hex');

// These snapshots pin the accessibility tree of the structures the axe rules
// cannot express: a result as a list item with a heading, and the chart's name.

test('a search result is a list item with a heading and labelled values', async ({ page }) => {
  await page.goto('/search?q=1080p');
  await expect(
    page.locator('li.result-item').filter({ hasText: 'Shogun.S01E03' }),
  ).toMatchAriaSnapshot(`
    - listitem:
      - heading /Shogun\\.S01E03[\\d,.]+[hmsp]+\\.DSNP\\.WEB-DL\\.DDP5\\.1\\.H\\.\\d+-NTb/ [level=2]:
        - button "Show details"
        - link /Shogun\\.S01E03[\\d,.]+[hmsp]+\\.DSNP\\.WEB-DL\\.DDP5\\.1\\.H\\.\\d+-NTb/:
          - /url: /torrent/e282c9b990d8cbe571e1b0f8516710f42e7eb2f5
      - text: "/Category: TV\\\\/HD Size: 3\\\\.8 GiB Seeds: \\\\d+ Peers: \\\\d+ Age: [\\\\d,.]+[bkmBKM]+/"
  `);
});

test('the crawler card names its activity chart', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('#spark')).toMatchAriaSnapshot(`
    - img /No torrents ingested in the last \\d+ minutes\\./
  `);
});

test('a torrent page exposes its download card', async ({ page }) => {
  await page.goto(`/torrent/${hash(0)}`);
  await expect(page.locator('.grid.two > section.card').first()).toMatchAriaSnapshot(`
    - heading "Download" [level=2]
    - text: Magnet
    - code: /magnet:\\?xt=urn:btih:b401fbe89f39b8a80d0bdcc2887afb01743777d6&dn=Dune\\.Part\\.Two[\\d,.]+[hmsp]+\\.WEB-DL\\.DDP5\\.1\\.Atmos\\.HDR\\.HEVC-CMRG&xl=\\d+/
    - button "Copy"
    - text: Info Hash
    - code: b401fbe89f39b8a80d0bdcc2887afb01743777d6
    - button "Copy"
  `);
});
