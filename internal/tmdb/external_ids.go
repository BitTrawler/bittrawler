package tmdb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// ExternalIDs are the identifiers that other catalogue sites use for a title.
// They are used to link a stored torrent to the page for the title it is.
type ExternalIDs struct {
	// TVDB is the series id at TheTVDB. It is zero for a film, which TheTVDB
	// catalogues separately, and for a series TMDB has no TVDB id for.
	TVDB int64
}

// externalIDsCacheSize bounds the identifier cache. It is much smaller than the
// search cache because only a torrent page asks, one title view at a time.
const externalIDsCacheSize = 4096

// externalIDsKey identifies one cached answer. The medium is part of the key
// because film and television identifiers are separate spaces.
type externalIDsKey struct {
	medium Medium
	id     int64
}

// externalIDsEntry is one cached answer, positive or negative.
type externalIDsEntry struct {
	ids     ExternalIDs
	expires time.Time
}

// externalIDsResult is the part of an external_ids reply this client reads.
type externalIDsResult struct {
	TVDB *int64 `json:"tvdb_id"`
}

// ExternalIDs asks TMDB which identifiers another site uses for a title.
//
// The second result reports whether the answer is worth showing. A film has no
// TVDB id, and neither has a series TMDB does not know one for, so both are
// ordinary answers rather than errors. An error means TMDB could not answer.
//
// The answer is cached for the same periods as a search, so the lookup costs
// one request per title per positive-TTL window however often a page is opened.
func (c *Client) ExternalIDs(ctx context.Context, medium Medium, id int64) (ExternalIDs, bool, error) {
	if id <= 0 || (medium != MediumMovie && medium != MediumTV) {
		return ExternalIDs{}, false, nil
	}

	key := externalIDsKey{medium: medium, id: id}

	if entry, ok := c.externalIDsCache.Get(key); ok && time.Now().Before(entry.expires) {
		c.cacheHits.Add(1)

		return entry.ids, entry.ids.TVDB > 0, nil
	}

	path := "/" + string(medium) + "/" + strconv.FormatInt(id, 10) + "/external_ids"

	body, err := c.get(ctx, path, url.Values{})
	if err != nil {
		return ExternalIDs{}, false, err
	}

	var result externalIDsResult
	if err := json.Unmarshal(body, &result); err != nil {
		return ExternalIDs{}, false, fmt.Errorf("tmdb: decode external ids: %w", err)
	}

	var ids ExternalIDs

	if medium == MediumTV && result.TVDB != nil && *result.TVDB > 0 {
		ids.TVDB = *result.TVDB
	}

	entry := externalIDsEntry{ids: ids, expires: time.Now().Add(c.positiveTTL)}
	if ids.TVDB == 0 {
		entry.expires = time.Now().Add(c.negativeTTL)
	}

	c.externalIDsCache.Add(key, entry)

	return ids, ids.TVDB > 0, nil
}
