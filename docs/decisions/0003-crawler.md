# 0003 — Crawling and intake

Status: accepted.

## Decision

One process crawls, classifies and writes. The pipeline is: nodes arrive, are
maintained in the routing table, are asked for `sample_infohashes`, and the
hashes they return are triaged; an unseen hash is looked up with `get_peers`,
its metadata is requested from those peers and checked against the banning
rules, its seeders are scraped, and the resulting row is written in a batch.

- Triage is a membership test, not a state machine. A hash is checked against
  the blocklist and then against the store; if the store holds it, it is
  dropped. There is no rescrape, no in-place seeder update, and no partially
  indexed row: a row is complete when it is written.
- Seeders and leechers are the values from one BEP 33 scrape at discovery and
  are never refreshed.
- A bloom filter of hashes already seen by this process sits in front of the
  store check, so a hash discovered by several nodes is triaged once.
- Hashes are triaged in batches of 500 or after 5 s; rows are written in
  batches of 100 or after 5 s, and the last batch is flushed on shutdown.
- A metadata fetch is bounded twice: at most four peers, and 45 s for the whole
  fetch including the scrape.
- The blocklist is a separate SQLite file in the data directory, not a table in
  a monthly shard: it is unpartitioned, small, and must outlive retention.
- Crawl workers are goroutines with bounded channels. Backpressure is the
  queue; there is no queue table and no separate queue process.

## Why

The write budget decides most of this. A random update to an indexed row costs
about 8 KB of WAL because SQLite writes whole pages, so refreshing seeders pays
for data that is a snapshot anyway. Dedupe is the only lookup on the hot path,
and one query per shard per batch is cheap where one query per hash would not
be. Fetching metadata once per torrent rather than once per discovery is what
makes the crawl affordable. Bounding peers and time stops one popular torrent
from parking a worker for minutes, which is what a per-peer retry loop with no
overall budget does.

## Rejected

- Rescraping known torrents to refresh seeders; see 0001, and bitmagnet pays for
  it on every rediscovery.
- A persistent queue table. The store is the record of what has been crawled,
  and in-flight work is not worth the write cost of journaling it.
- A bloom filter behind the blocklist. The table is small enough to query
  directly, and deciding not to index something deserves an exact answer.
- Persisting the seen-hash bloom filter. It is a per-process optimisation only.

## Open

- Crawl rate has not been measured against the size of the routing table. The
  sample interval policy — honour the interval a node asks for unless it keeps
  producing new hashes — is a guess until measured.
- Blocking a hash that is already indexed does not delete the row. A purge
  belongs to the admin surface.
- The blocklist has no expiry and no size cap.
- The classifier is not wired in yet, so rows are written with zero
  classification fields; the metadata is in memory when they are written and is
  not kept for a later pass.
