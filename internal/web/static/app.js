// announce writes to the polite status region so a screen reader hears the
// result of an action that does not move focus.
function announce(message) {
  const region = document.getElementById('announcer');
  if (!region) {
    return;
  }

  region.textContent = '';
  window.setTimeout(() => {
    region.textContent = message;
  }, 50);
}

// Copy buttons, click-to-select values and the live feed. The interface works
// without JavaScript; this adds copying and refreshes the recent torrents while
// the page is open.
document.addEventListener('click', async (event) => {
  const button = event.target.closest('[data-copy]');
  if (!button) {
    return;
  }
  event.preventDefault();

  const copied = await copyText(button);

  const label = button.textContent;
  button.textContent = copied ? 'Copied' : 'Selected';
  button.classList.add('is-copied');
  announce(copied ? 'Copied to clipboard.' : 'Copy failed. The value is selected, so you can copy it yourself.');
  window.setTimeout(() => {
    button.textContent = label;
    button.classList.remove('is-copied');
  }, 1400);
});

// A search result expands in place to show the magnet and the facts the list
// leaves out. The detail block is already in the page, hidden, so this only
// shows it; the name still links to the torrent's own page.
document.addEventListener('click', (event) => {
  const row = event.target.closest('.result');
  if (!row || event.target.closest('a')) {
    return;
  }
  toggleDetails(row);
});

function toggleDetails(row) {
  const detail = row.nextElementSibling;
  if (!detail || !detail.classList.contains('result-detail')) {
    return;
  }

  const open = detail.hidden;
  detail.hidden = !open;
  row.classList.toggle('is-open', open);

  const toggle = row.querySelector('.name-toggle');
  if (toggle) {
    toggle.setAttribute('aria-expanded', String(open));
  }
}

// Clicking a value selects all of it, so a manual copy is one click and one
// keyboard shortcut. A long key is otherwise awkward to select by dragging.
document.addEventListener('click', (event) => {
  const block = event.target.closest('.code-block');
  if (block) {
    selectText(block);
  }
});

// copyText writes the button's value to the clipboard. The Clipboard API needs
// a secure context, so on a plain-HTTP page it selects the visible value and
// copies the selection, which is what Safari accepts.
async function copyText(button) {
  const value = button.dataset.copy ?? '';

  if (window.isSecureContext && navigator.clipboard) {
    try {
      await navigator.clipboard.writeText(value);

      return true;
    } catch {
      // Fall through: the permission may have been refused.
    }
  }

  const block = findValue(button);
  if (block) {
    selectText(block);
  }

  try {
    if (document.execCommand('copy')) {
      return true;
    }
  } catch {
    // Fall through to the control method.
  }

  try {
    const area = document.createElement('textarea');
    area.value = value;
    area.setAttribute('readonly', '');
    area.style.position = 'fixed';
    area.style.top = '0';
    area.style.left = '-1000px';
    document.body.appendChild(area);
    area.select();
    const copied = document.execCommand('copy');
    area.remove();

    return copied;
  } catch {
    return false;
  }
}

// findValue is the code block a copy button belongs to.
function findValue(button) {
  const row = button.closest('.copy-row, .key-row');

  return row ? row.querySelector('.code-block') : null;
}

// selectValue selects the value for a manual copy when copying fails.
function selectValue(button) {
  const block = findValue(button);
  if (block) {
    selectText(block);
  }
}

// selectText selects everything inside node.
function selectText(node) {
  const range = document.createRange();
  range.selectNodeContents(node);
  const selection = window.getSelection();
  selection.removeAllRanges();
  selection.addRange(range);
}

const numberFormat = new Intl.NumberFormat('en-GB');
const byteUnits = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];

function formatBytes(bytes) {
  let value = Number(bytes) || 0;
  let unit = 0;

  while (value >= 1024 && unit < byteUnits.length - 1) {
    value /= 1024;
    unit += 1;
  }

  if (unit === 0) {
    return `${Math.round(value)} B`;
  }

  return `${value.toFixed(value < 10 ? 1 : 0)} ${byteUnits[unit]}`;
}

function formatRate(rate) {
  const value = Number(rate) || 0;

  return value < 10 ? value.toFixed(1) : String(Math.round(value));
}

function formatAge(createdAt) {
  const seconds = Math.max(0, Date.now() / 1000 - createdAt);

  if (seconds < 60) {
    return 'now';
  }

  if (seconds < 3600) {
    return `${Math.floor(seconds / 60)}m`;
  }

  if (seconds < 86400) {
    return `${Math.floor(seconds / 3600)}h`;
  }

  return `${Math.floor(seconds / 86400)}d`;
}

function setText(selector, text) {
  for (const element of document.querySelectorAll(selector)) {
    element.textContent = text;
  }
}

function rowFor(torrent) {
  const row = document.createElement('tr');

  const name = document.createElement('td');
  name.className = 'name';

  const link = document.createElement('a');
  link.href = '/torrent/' + torrent.hash;
  link.textContent = torrent.name;
  name.append(link);

  const category = document.createElement('td');
  category.className = 'col-category';
  const label = document.createElement('span');
  label.className = 'cat';
  label.textContent = torrent.category;
  category.append(label);

  const size = document.createElement('td');
  size.className = 'num';
  size.textContent = formatBytes(torrent.size);

  const seeds = document.createElement('td');
  seeds.className = 'num';
  seeds.textContent = String(torrent.seeders);

  const files = document.createElement('td');
  files.className = 'num col-files';
  files.textContent = torrent.files >= 0 ? numberFormat.format(torrent.files) : '—';

  const age = document.createElement('td');
  age.className = 'num col-age';
  age.textContent = formatAge(torrent.created_at);

  row.append(name, category, size, files, seeds, age);

  return row;
}

function updateCounters(payload) {
  const counters = payload.counters || {};
  const ktable = payload.ktable || {};

  for (const element of document.querySelectorAll('[data-counter]')) {
    const key = element.dataset.counter;
    let value = counters[key];

    if (key === 'ktable_nodes') {
      value = ktable.nodes;
    } else if (key === 'ktable_hashes') {
      value = ktable.hashes;
    }

    if (typeof value === 'number') {
      element.textContent = numberFormat.format(value);
    }
  }
}

function updateRuntime(runtime) {
  if (!runtime) {
    return;
  }

  const values = {
    heap: formatBytes(runtime.heap_bytes),
    rss: formatBytes(runtime.rss_bytes),
    goroutines: numberFormat.format(runtime.goroutines),
    cpu: `${Number(runtime.cpu_percent).toFixed(1)}%`,
  };

  for (const element of document.querySelectorAll('[data-runtime]')) {
    const value = values[element.dataset.runtime];
    if (value !== undefined) {
      element.textContent = value;
    }
  }
}

function updateSpark(history, summary) {
  const spark = document.getElementById('spark');
  const svg = spark ? spark.querySelector('.spark-bars') : null;
  if (!svg || !Array.isArray(history)) {
    return;
  }

  const barWidth = 100 / Math.max(history.length, 1);

  svg.replaceChildren(...history.map((height, index) => {
    const rect = document.createElementNS('http://www.w3.org/2000/svg', 'rect');
    rect.setAttribute('x', String(round2(index * barWidth)));
    rect.setAttribute('y', String(round2(100 - height)));
    rect.setAttribute('width', String(round2(Math.max(barWidth - 0.6, 0.4))));
    rect.setAttribute('height', String(round2(height)));

    return rect;
  }));

  if (summary) {
    spark.setAttribute('aria-label', summary);
  }
}

// round2 keeps the SVG attributes short.
function round2(value) {
  return Math.round(value * 100) / 100;
}

// updateTables rebuilds the storage and classification tables from the feed.
// The server recounts them once a minute, so they change less often than the
// counters; the signature check avoids replacing the DOM on every poll.
function updateTables(payload) {
  updateShards(payload.shards);
  updateCategories(payload.categories);
}

function updateShards(rows) {
  const body = document.getElementById('shards');
  if (!body || !Array.isArray(rows)) {
    return;
  }

  const signature = JSON.stringify(rows);
  if (body.dataset.signature === signature) {
    return;
  }
  body.dataset.signature = signature;

  if (rows.length === 0) {
    body.replaceChildren(emptyRow(4, 'No shards yet.'));

    return;
  }

  body.replaceChildren(...rows.map((shard) => {
    const row = document.createElement('tr');
    row.append(
      textCell(shard.month),
      textCell(shard.rows, 'num'),
      textCell(shard.database, 'num'),
      textCell(shard.wal, 'num'),
    );

    return row;
  }));
}

// Category families collapse by default and stay open across a feed refresh, so
// a long classification list still fits on the page.
const expandedCategories = new Set();
let categoryRows = [];

function updateCategories(rows) {
  const body = document.getElementById('categories');
  if (!body || !Array.isArray(rows)) {
    return;
  }

  const signature = JSON.stringify(rows);
  if (body.dataset.signature === signature) {
    return;
  }
  body.dataset.signature = signature;

  categoryRows = rows;
  renderCategories();
}

function renderCategories() {
  const body = document.getElementById('categories');
  if (!body) {
    return;
  }

  if (categoryRows.length === 0) {
    body.replaceChildren(emptyRow(3, 'Nothing classified yet.'));

    return;
  }

  const rows = [];

  for (const family of categoryRows) {
    const children = family.children || [];
    const open = expandedCategories.has(family.name);

    const row = document.createElement('tr');
    row.className = 'family-row';

    const name = document.createElement('td');

    if (children.length === 0) {
      // Keep the chevron's space: a family with nothing beneath it should not
      // sit further left than the ones that open.
      name.append(staticFamily(family.name));
    } else {
      const toggle = document.createElement('button');
      toggle.type = 'button';
      toggle.className = 'family-toggle';
      toggle.dataset.family = family.name;
      toggle.setAttribute('aria-expanded', String(open));
      toggle.setAttribute('aria-label', `${open ? 'Hide' : 'Show'} ${family.name} subcategories`);

      const chevron = document.createElement('span');
      chevron.className = 'chevron';
      chevron.setAttribute('aria-hidden', 'true');

      toggle.append(chevron, categoryLabel(family.name));
      name.append(toggle);
    }

    row.append(name, textCell(family.rows, 'num'), textCell(family.share, 'num'));
    rows.push(row);

    for (const child of children) {
      const sub = document.createElement('tr');
      sub.className = 'subcategory-row';
      sub.hidden = !open;
      const subName = document.createElement('td');
      subName.textContent = child.name;
      sub.append(subName, textCell(child.rows, 'num'), textCell(child.share, 'num'));
      rows.push(sub);
    }
  }

  body.replaceChildren(...rows);
}

function staticFamily(text) {
  const wrapper = document.createElement('span');
  wrapper.className = 'family-static';

  const chevron = document.createElement('span');
  chevron.className = 'chevron is-hidden';
  chevron.setAttribute('aria-hidden', 'true');

  wrapper.append(chevron, categoryLabel(text));

  return wrapper;
}

function categoryLabel(text) {
  const label = document.createElement('span');
  label.className = 'cat';
  label.textContent = text;

  return label;
}

function toggleCategory(name) {
  const open = !expandedCategories.has(name);

  if (open) {
    expandedCategories.add(name);
  } else {
    expandedCategories.delete(name);
  }

  const body = document.getElementById('categories');
  if (!body) {
    return;
  }

  for (const toggle of body.querySelectorAll('.family-toggle')) {
    if (toggle.dataset.family !== name) {
      continue;
    }

    toggle.setAttribute('aria-expanded', String(open));
    toggle.setAttribute('aria-label', `${open ? 'Hide' : 'Show'} ${name} subcategories`);

    let row = toggle.closest('tr').nextElementSibling;
    while (row && row.classList.contains('subcategory-row')) {
      row.hidden = !open;
      row = row.nextElementSibling;
    }
  }
}

function textCell(text, className = '') {
  const cell = document.createElement('td');
  if (className) {
    cell.className = className;
  }
  cell.textContent = text;

  return cell;
}

function emptyRow(colspan, text) {
  const row = document.createElement('tr');
  const cell = textCell(text, 'empty');
  cell.colSpan = colspan;
  row.append(cell);

  return row;
}

function updateCrawlState(running) {
  for (const state of document.querySelectorAll('[data-crawl-state]')) {
    const dot = state.querySelector('.dot');
    if (dot) {
      dot.classList.toggle('is-live', running);
    }

    const label = state.querySelector('.crawl-label');
    if (label) {
      label.textContent = running ? 'Crawling' : 'Paused';
    }
  }

  const form = document.querySelector('[data-crawl-form]');
  if (!form) {
    return;
  }

  const action = form.querySelector('[data-crawl-action]');
  if (action) {
    action.value = running ? 'pause' : 'resume';
  }

  const button = form.querySelector('[data-crawl-button]');
  if (button) {
    button.textContent = running ? 'Pause Crawling' : 'Resume Crawling';
  }
}

async function refreshFeed() {
  const feed = document.getElementById('feed');
  if (!feed) {
    return;
  }

  try {
    const response = await fetch('/api/feed?limit=20', {
      cache: 'no-store',
      headers: { Accept: 'application/json' },
    });
    if (!response.ok) {
      return;
    }

    const payload = await response.json();

    setText('[data-stat="stored"]', numberFormat.format(payload.stats.stored));
    setText('[data-stat="rate"]', formatRate(payload.stats.rate_per_minute));
    setText('[data-stat="database"]', formatBytes(payload.stats.bytes));
    setText('[data-stat="bytes-per-torrent"]', formatBytes(payload.stats.bytes_per_row));
    setText('[data-stat="nodes"]', numberFormat.format(payload.ktable.nodes));
    setText('[data-stat="crawled"]', numberFormat.format(payload.stats.persisted));
    updateCrawlState(Boolean(payload.crawling));
    updateCounters(payload);

    const warmup = document.getElementById('warmup');
    if (warmup && payload.counters && payload.counters.nodes_discovered > 0) {
      warmup.remove();
    }
    updateRuntime(payload.runtime);
    updateSpark(payload.history, payload.history_summary);
    updateTables(payload);

    const torrents = payload.torrents || [];

    if (torrents.length === 0) {
      const row = document.createElement('tr');
      const cell = document.createElement('td');
      cell.colSpan = 6;
      cell.className = 'empty';
      cell.textContent = 'No torrents yet.';
      row.append(cell);
      feed.replaceChildren(row);

      return;
    }

    feed.replaceChildren(...torrents.map(rowFor));
  } catch {
    // A dropped poll is not worth reporting; the next one may succeed.
  }
}

// A reclassification pass runs in the background; keep the page current until
// it finishes.
if (document.querySelector('[data-reclassify-running]')) {
  window.setTimeout(() => window.location.reload(), 3000);
}

for (const form of document.querySelectorAll('form[data-confirm]')) {
  form.addEventListener('submit', (event) => {
    if (!window.confirm(form.dataset.confirm)) {
      event.preventDefault();
    }
  });
}

const categoriesBody = document.getElementById('categories');
if (categoriesBody) {
  categoriesBody.addEventListener('click', (event) => {
    const toggle = event.target.closest('.family-toggle');
    if (toggle) {
      toggleCategory(toggle.dataset.family);
    }
  });
}

if (document.getElementById('feed')) {
  refreshFeed();

  let feedTimer = window.setInterval(refreshFeed, 3000);

  const feedToggle = document.querySelector('[data-feed-toggle]');
  if (feedToggle) {
    feedToggle.addEventListener('click', () => {
      const paused = feedToggle.getAttribute('aria-pressed') === 'true';

      if (paused) {
        feedTimer = window.setInterval(refreshFeed, 3000);
      } else {
        window.clearInterval(feedTimer);
      }

      feedToggle.setAttribute('aria-pressed', String(!paused));
      feedToggle.textContent = paused ? 'Pause Updates' : 'Resume Updates';
      announce(paused ? 'Updates resumed.' : 'Updates paused.');
    });
  }

  // A page restored from the back/forward cache shows the figures it was left
  // with. Ask for the current ones straight away rather than waiting for the
  // next tick.
  window.addEventListener('pageshow', (event) => {
    if (event.persisted) {
      refreshFeed();
    }
  });
}
