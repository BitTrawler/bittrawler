# 0001 — Storage: SQLite, one file per month

Status: accepted.

## Decision

The store is a directory of SQLite files, one per calendar month of `created_at`,
written by one process. Long-term retention unlinks the oldest file. The driver
is `modernc.org/sqlite` (pure Go, FTS5 enabled).

## Why

Measured on the same machine against the same synthetic corpus. SQLite figures
are from 5,000,000 rows (four monthly files); PostgreSQL per-row figures are
from 500,000 rows with the same columns, where per-row cost is stable. SQLite
uses page size 4096 with WAL and `synchronous=NORMAL`. Reproduce the SQLite side
with `go test -bench=. ./internal/store`.

| | SQLite, monthly files | PostgreSQL, partitioned |
|---|---|---|
| bytes per row | 235 | 475 |
| text search, newest first | 0.7 ms | 6.8 ms |
| episode by content reference | 0.66 ms | 0.17 ms |
| facet counts | 5.4 ms | 4.3 ms |
| prune one month | unlink, 60 µs | DROP PARTITION, 0 s |
| dedupe a rediscovered torrent | 6 µs | 12 index probes |

Month files cover disjoint time ranges, so a query ordered `created_at DESC`
stops once the newest file has enough rows. Most searches touch one file.

Retention is the load-bearing part. A single SQLite file cannot prune cheaply:
deleting 1.5M rows and vacuuming took 45.7 s plus 20.5 s and rewrote the file.
Unlinking a month file writes nothing.

## Rejected

- **PostgreSQL with monthly partitions.** DROP PARTITION is as cheap as
  unlinking, but the same data is twice the size, writes more (about 1.1 KB of
  WAL per insert plus checkpoint writes), and needs a server.
- **One SQLite file.** Retention rewrites the database, which is the disk wear
  this fork exists to avoid.
- **Supporting both engines.** Doubles the storage layer and the test matrix.

## Consequences

- Counts, facets and relevance ranking need every month file; date-ordered
  queries stop at the newest. Shard-aware pagination is in the store.
- bm25 scores from separate indexes are not comparable, so relevance ranking
  across shards is an approximation.
- Each shard carries its own schema version and is migrated on startup.
- No transactions across shards: writes touch the current month, imports touch
  one month at a time.
- Backup is copying the directory. There is no dump tool.
- One writer process; readers run concurrently in WAL mode on local disks.
