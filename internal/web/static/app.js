// Copy buttons and the live feed. The interface works without JavaScript; this
// adds copying and refreshes the recent torrents while the page is open.
document.addEventListener('click', async (event) => {
  const button = event.target.closest('[data-copy]');
  if (!button) {
    return;
  }
  event.preventDefault();

  try {
    await navigator.clipboard.writeText(button.dataset.copy);
  } catch {
    return;
  }

  const label = button.textContent;
  button.textContent = 'Copied';
  button.classList.add('is-copied');
  window.setTimeout(() => {
    button.textContent = label;
    button.classList.remove('is-copied');
  }, 1400);
});

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

function updateSpark(history) {
  const spark = document.getElementById('spark');
  if (!spark || !Array.isArray(history)) {
    return;
  }

  spark.replaceChildren(...history.map((height) => {
    const bar = document.createElement('span');
    bar.className = 'bar';
    bar.style.height = `${height}%`;

    return bar;
  }));
}

function updateCrawlState(running) {
  const state = document.querySelector('[data-crawl-state]');
  if (!state) {
    return;
  }

  const dot = state.querySelector('.dot');
  if (dot) {
    dot.classList.toggle('is-live', running);
  }

  const label = state.querySelector('.crawl-label');
  if (label) {
    label.textContent = running ? 'Crawling' : 'Paused';
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
    updateSpark(payload.history);

    const torrents = payload.torrents || [];

    if (torrents.length === 0) {
      const row = document.createElement('tr');
      const cell = document.createElement('td');
      cell.colSpan = 5;
      cell.className = 'empty';
      cell.textContent = 'Nothing indexed yet. New torrents appear here as the crawler writes them.';
      row.append(cell);
      feed.replaceChildren(row);

      return;
    }

    feed.replaceChildren(...torrents.map(rowFor));
  } catch {
    // A dropped poll is not worth reporting; the next one may succeed.
  }
}

if (document.getElementById('feed')) {
  refreshFeed();
  window.setInterval(refreshFeed, 3000);
}
