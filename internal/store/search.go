package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// Sentinel values for optional query fields.
const (
	AnySeason  = -1
	AnyEpisode = -1
)

// Sort selects the result order.
type Sort int

const (
	// SortNewest orders by created_at descending, which is what Torznab RSS
	// and paging need. Shards are time-ordered, so this can stop after the
	// newest shard has provided enough rows.
	SortNewest Sort = iota
	// SortRelevance orders by FTS5 bm25 rank, best first.
	SortRelevance
)

// Query describes a search. Zero values mean "no filter", except Season and
// Episode, which use AnySeason and AnyEpisode.
type Query struct {
	// Text is the user's search text. Bare words are ANDed and the final word
	// is treated as a prefix, so "s51" matches "s51e01". Quoted text is a
	// phrase and a leading - excludes a term.
	Text       string
	Categories []int // Newznab categories; a parent matches its children
	ContentID  int64 // 0 ignores
	Season     int
	Episode    int
	DailyDate  string // ISO date for daily episodes; "" ignores
	Sort       Sort
	Limit      int
	Offset     int
	Count      bool // also count the total number of matches
}

// NewQuery returns a query with the optional season and episode filters unset.
func NewQuery() Query {
	return Query{Season: AnySeason, Episode: AnyEpisode}
}

// Result is the outcome of a search.
type Result struct {
	Torrents []Torrent
	Total    int
}

const torrentColumns = `t.info_hash, t.name, t.size, t.created_at, t.category,
	t.content_id, t.seasons, t.episodes, t.air_date, t.files_count,
	t.primary_file, t.seeders, t.leechers`

// Search runs a query over the retained shards.
func (s *Store) Search(ctx context.Context, q Query) (Result, error) {
	if q.Limit <= 0 {
		q.Limit = 100
	}

	if q.Offset < 0 {
		q.Offset = 0
	}

	match := buildMatch(q)
	filter, filterArgs := q.filterSQL()

	// Text that produced no positive term (for example "-cam" on its own)
	// cannot be expressed to FTS5. Returning an empty page is honest; falling
	// through to no filter would return the whole index.
	if match == "" && strings.TrimSpace(q.Text) != "" {
		return Result{}, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	shards := s.shardsNewestFirstLocked()

	var res Result

	if q.Count {
		for _, sh := range shards {
			n, err := countShard(ctx, sh, match, filter, filterArgs)
			if err != nil {
				return res, err
			}

			res.Total += n
		}
	}

	need := q.Offset + q.Limit

	var collected []scoredTorrent

	for _, sh := range shards {
		rows, err := queryShard(ctx, sh, match, filter, filterArgs, need, q.Sort)
		if err != nil {
			return res, err
		}

		collected = append(collected, rows...)

		if q.Sort == SortNewest && len(collected) >= need {
			break
		}
	}

	if q.Sort == SortRelevance {
		sort.SliceStable(collected, func(i, j int) bool {
			return collected[i].rank < collected[j].rank
		})
	}

	if q.Offset < len(collected) {
		collected = collected[q.Offset:]
	} else {
		collected = nil
	}

	if len(collected) > q.Limit {
		collected = collected[:q.Limit]
	}

	res.Torrents = make([]Torrent, 0, len(collected))
	for _, st := range collected {
		res.Torrents = append(res.Torrents, st.Torrent)
	}

	return res, nil
}

type scoredTorrent struct {
	Torrent
	rank float64
}

func queryShard(ctx context.Context, sh *shard, match, filter string, filterArgs []any, limit int, sortBy Sort) ([]scoredTorrent, error) {
	args := make([]any, 0, len(filterArgs)+2)

	if match == "" {
		query := "SELECT " + torrentColumns + " FROM torrents t WHERE 1=1" + filter +
			" ORDER BY t.created_at DESC LIMIT ?"
		args = append(args, filterArgs...)
		args = append(args, limit)

		return scanTorrents(ctx, sh.db, query, args, false)
	}

	order := "t.created_at DESC"
	if sortBy == SortRelevance {
		order = "torrents_fts.rank"
	}

	query := "SELECT " + torrentColumns + ", torrents_fts.rank" +
		" FROM torrents_fts JOIN torrents t ON t.id = torrents_fts.rowid" +
		" WHERE torrents_fts MATCH ?" + filter +
		" ORDER BY " + order + " LIMIT ?"

	args = append(args, match)
	args = append(args, filterArgs...)
	args = append(args, limit)

	return scanTorrents(ctx, sh.db, query, args, true)
}

func scanTorrents(ctx context.Context, db *sql.DB, query string, args []any, withRank bool) ([]scoredTorrent, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	defer func() { _ = rows.Close() }()

	out := make([]scoredTorrent, 0, 16)

	for rows.Next() {
		var (
			hash        []byte
			name        string
			size        int64
			createdAt   int64
			category    int
			contentID   sql.NullInt64
			seasons     sql.NullString
			episodes    sql.NullString
			airDate     sql.NullString
			filesCount  sql.NullInt64
			primaryFile sql.NullString
			seeders     int
			leechers    int
			rank        float64
		)

		dest := []any{&hash, &name, &size, &createdAt, &category, &contentID,
			&seasons, &episodes, &airDate, &filesCount, &primaryFile,
			&seeders, &leechers}

		if withRank {
			dest = append(dest, &rank)
		}

		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}

		var t Torrent

		copy(t.InfoHash[:], hash)
		t.Name = name
		t.Size = size
		t.CreatedAt = timeFromUnix(createdAt)
		t.Category = category
		t.ContentID = contentID.Int64
		t.Seasons = parseSeasonTokens(seasons.String)
		t.Episodes = parseEpisodeTokens(episodes.String)
		t.AirDate = airDate.String
		t.FilesCount = -1
		if filesCount.Valid {
			t.FilesCount = int(filesCount.Int64)
		}

		t.PrimaryFile = primaryFile.String
		t.Seeders = seeders
		t.Leechers = leechers

		out = append(out, scoredTorrent{Torrent: t, rank: rank})
	}

	return out, rows.Err()
}

func countShard(ctx context.Context, sh *shard, match, filter string, filterArgs []any) (int, error) {
	var n int

	if match == "" {
		query := "SELECT count(*) FROM torrents t WHERE 1=1" + filter
		if err := sh.db.QueryRowContext(ctx, query, filterArgs...).Scan(&n); err != nil {
			return 0, fmt.Errorf("count: %w", err)
		}

		return n, nil
	}

	query := "SELECT count(*) FROM torrents_fts JOIN torrents t ON t.id = torrents_fts.rowid" +
		" WHERE torrents_fts MATCH ?" + filter
	args := append([]any{match}, filterArgs...)

	if err := sh.db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count: %w", err)
	}

	return n, nil
}

// filterSQL builds the SQL for the non-text filters.
func (q Query) filterSQL() (string, []any) {
	var (
		clauses []string
		args    []any
	)

	if len(q.Categories) > 0 {
		parts := make([]string, 0, len(q.Categories))

		for _, cat := range q.Categories {
			if cat%1000 == 0 {
				parts = append(parts, "(t.category BETWEEN ? AND ?)")
				args = append(args, cat, cat+999)

				continue
			}

			parts = append(parts, "t.category = ?")
			args = append(args, cat)
		}

		clauses = append(clauses, "("+strings.Join(parts, " OR ")+")")
	}

	if q.ContentID != 0 {
		clauses = append(clauses, "t.content_id = ?")
		args = append(args, q.ContentID)
	}

	if q.DailyDate != "" {
		clauses = append(clauses, "t.air_date = ?")
		args = append(args, q.DailyDate)
	}

	if len(clauses) == 0 {
		return "", nil
	}

	return " AND " + strings.Join(clauses, " AND "), args
}

// parsedTerm is one term of the user's text query.
type parsedTerm struct {
	phrase bool
	negate bool
	prefix bool
	text   string
}

// fts renders the term as an FTS5 phrase scoped to the name column.
func (t parsedTerm) fts() string {
	quoted := `"` + strings.ReplaceAll(t.text, `"`, `""`) + `"`
	if t.prefix && !t.phrase {
		return quoted + "*"
	}

	return quoted
}

// buildMatch compiles the query into an FTS5 expression. The last word is a
// prefix so that "s51" matches the token "s51e01", which is the behaviour that
// exact-lexeme matching in bitmagnet lacks.
func buildMatch(q Query) string {
	terms := parseTerms(q.Text)

	var (
		positive []string
		negative []string
	)

	for _, t := range terms {
		if t.negate {
			negative = append(negative, t.fts())
		} else {
			positive = append(positive, t.fts())
		}
	}

	var parts []string

	if len(positive) > 0 {
		parts = append(parts, "name:("+strings.Join(positive, " ")+")")
	}

	if q.Season != AnySeason && q.Season >= 0 {
		if q.Episode != AnyEpisode && q.Episode >= 0 {
			parts = append(parts, fmt.Sprintf("episodes:e%d", q.Season*10000+q.Episode))
		} else {
			parts = append(parts, fmt.Sprintf("seasons:s%d", q.Season))
		}
	}

	if len(parts) == 0 {
		return ""
	}

	expr := strings.Join(parts, " AND ")

	for _, n := range negative {
		expr += " NOT name:(" + n + ")"
	}

	return expr
}

// parseTerms splits query text into words and quoted phrases, honouring a
// leading - for exclusion. The last positive word becomes a prefix match.
func parseTerms(text string) []parsedTerm {
	var out []parsedTerm

	runes := []rune(text)
	lastPositive := -1

	for i := 0; i < len(runes); {
		for i < len(runes) && (runes[i] == ' ' || runes[i] == '\t' || runes[i] == '\n') {
			i++
		}

		if i >= len(runes) {
			break
		}

		negate := false
		if runes[i] == '-' && i+1 < len(runes) {
			negate = true
			i++
		}

		if i < len(runes) && runes[i] == '"' {
			var b strings.Builder

			i++

			for i < len(runes) && runes[i] != '"' {
				b.WriteRune(runes[i])
				i++
			}

			if i < len(runes) {
				i++
			}

			if s := strings.TrimSpace(b.String()); s != "" {
				out = append(out, parsedTerm{phrase: true, negate: negate, text: s})
			}

			continue
		}

		start := i
		for i < len(runes) && runes[i] != ' ' && runes[i] != '\t' && runes[i] != '\n' {
			i++
		}

		word := string(runes[start:i])
		prefix := false

		if strings.HasSuffix(word, "*") {
			prefix = true
			word = strings.TrimSuffix(word, "*")
		}

		if word == "" {
			continue
		}

		out = append(out, parsedTerm{negate: negate, prefix: prefix, text: word})

		if !negate {
			lastPositive = len(out) - 1
		}
	}

	if lastPositive >= 0 && !out[lastPositive].prefix {
		out[lastPositive].prefix = true
	}

	return out
}

func parseSeasonTokens(s string) []int {
	return parseTokens(s, 's')
}

func parseEpisodeTokens(s string) []int {
	return parseTokens(s, 'e')
}
