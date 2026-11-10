# 0002 — Search and TV matching

Status: accepted.

## Decision

Text search is FTS5 over the release name with `unicode61`, plus synthetic
tokens in three extra indexed columns: `seasons` (`s5 s6`), `episodes`
(packed `season*10000+episode`, e.g. `e50001`) and `kind` (`pack` or `ep`).

The final word of a query is a prefix match, so `s51` matches `s51e01`. Quoted
text is a phrase; a leading `-` excludes a term.

Season search matches the season token: packs and every episode of that season.
Episode search matches the packed episode token: season packs are excluded.
Daily episodes match on `air_date`. TVDB season 0 means specials.

Category is one stored Newznab category. A requested parent category matches its
1000-wide range; a leaf matches exactly. A TV release with no resolution token
is filed as HD (5040), so Sonarr's default 5030/5040 categories find it.

## Why

`unicode61` splits `Midnight.Run.S06E03` into `midnight run s06e03`. PostgreSQL's
text search parser treats the dotted name as a hostname and emits a single token,
which forces a separately normalised copy of the name into the table. That column
is not needed here.

Most of this fixes behaviour bitmagnet gets wrong:

- exact-lexeme matching means `s51` never matches `s51e01`;
- the season predicate upstream deliberately excludes episodes
  (bitmagnet #478), so a season search returns packs only;
- category filtering maps the Arr defaults onto a nullable resolution column,
  which hides releases with no resolution token.

## Rejected

- **Text-only season matching.** Works only when the release name contains the
  token, and Sonarr sends season and episode as parameters.
- **Including packs in episode searches.** Sonarr's match specification rejects
  a pack for an episode search, and one pack would answer every episode query of
  that season.
- **Storing normalised search text.** Unnecessary with FTS5's tokeniser, and it
  costs about 45 bytes per row.

## Open

- Packs-first ordering for season searches is not implemented; results are
  ordered by date so date-ordered pagination and early stopping keep working.
- Relevance ranking across shards is approximate (see 0001).
