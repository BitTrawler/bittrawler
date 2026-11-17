// Lighthouse audits against the running app. Performance, accessibility and
// best practices, with a hard threshold on each. The accessibility category is
// a subset of the rules the Playwright axe pass runs; the value here is the
// performance budget and the third opinion.
import { createHash } from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';
import { chromium } from '@playwright/test';
import lighthouse from 'lighthouse';
import * as chromeLauncher from 'chrome-launcher';

const base = process.env.BASE_URL ?? 'http://127.0.0.1:18090';
const artifacts = path.resolve('artifacts');
const hash = createHash('sha1').update('bittrawler-links-0').digest('hex');

const state = JSON.parse(fs.readFileSync(path.join(artifacts, 'storage-state.json'), 'utf8'));
const cookie = state.cookies.map((entry) => `${entry.name}=${entry.value}`).join('; ');

const pages = [
  { name: 'overview', path: '/', auth: true },
  { name: 'search', path: '/search?q=1080p', auth: true },
  { name: 'torrent', path: `/torrent/${hash}`, auth: true },
  { name: 'settings', path: '/settings', auth: true },
  { name: 'login', path: '/login', auth: false },
];

const thresholds = {
  performance: 0.9,
  accessibility: 1,
  'best-practices': 0.9,
};

const chrome = await chromeLauncher.launch({
  chromePath: chromium.executablePath(),
  chromeFlags: ['--headless=new', '--no-sandbox', '--disable-gpu'],
});

const rows = [];
let failed = false;

try {
  for (const entry of pages) {
    const flags = {
      port: chrome.port,
      output: 'json',
      logLevel: 'error',
      onlyCategories: ['performance', 'accessibility', 'best-practices'],
      formFactor: 'desktop',
      screenEmulation: { disabled: true },
      throttling: { rttMs: 40, throughputKbps: 10_240, cpuSlowdownMultiplier: 1 },
      ...(entry.auth ? { extraHeaders: { Cookie: cookie } } : {}),
    };

    const result = await lighthouse(base + entry.path, flags);
    const report = result.lhr;

    fs.writeFileSync(
      path.join(artifacts, `lighthouse-${entry.name}.json`),
      JSON.stringify(report, null, 2),
    );

    const row = { page: entry.name, path: entry.path, scores: {} };

    for (const [category, minimum] of Object.entries(thresholds)) {
      const score = report.categories[category].score;
      row.scores[category] = score;
      if (score === null || score < minimum) {
        failed = true;
        row[category] = 'below threshold';
      }
    }

    if (!report.finalDisplayedUrl.startsWith(base.replace(/\/$/, ''))) {
      failed = true;
      row.finalUrl = report.finalDisplayedUrl;
    }

    rows.push(row);
  }
} finally {
  await chrome.kill();
}

fs.writeFileSync(path.join(artifacts, 'lighthouse-summary.json'), JSON.stringify(rows, null, 2));

const head = ['page', 'performance', 'accessibility', 'best-practices'];
console.log(head.map((cell) => cell.padEnd(16)).join(''));

for (const row of rows) {
  const scores = ['performance', 'accessibility', 'best-practices'].map((category) => {
    const score = row.scores[category];
    return (score === null ? 'n/a' : String(Math.round(score * 100))).padEnd(16);
  });

  console.log([row.page.padEnd(16), ...scores].join(''));
}

if (failed) {
  console.error('lighthouse: at least one page is below its threshold');
  process.exit(1);
}
