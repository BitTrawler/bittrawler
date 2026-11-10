CREATE TABLE torrents (
    id           INTEGER PRIMARY KEY,
    info_hash    BLOB    NOT NULL,
    name         TEXT    NOT NULL,
    size         INTEGER NOT NULL,
    created_at   INTEGER NOT NULL,
    category     INTEGER NOT NULL,
    content_id   INTEGER,
    seasons      TEXT,
    episodes     TEXT,
    kind         TEXT,
    air_date     TEXT,
    files_count  INTEGER,
    primary_file TEXT,
    seeders      INTEGER NOT NULL,
    leechers     INTEGER NOT NULL
);

-- newest-first ordering and date-bounded queries
CREATE INDEX torrents_created_at_idx ON torrents (created_at);

-- category-filtered feeds
CREATE INDEX torrents_category_created_at_idx ON torrents (category, created_at);

-- identifier searches once a content reference is known
CREATE INDEX torrents_content_id_idx ON torrents (content_id) WHERE content_id IS NOT NULL;

-- dedupe: has this infohash been seen while it is still in the window
CREATE INDEX torrents_info_hash_idx ON torrents (info_hash);

-- daily episodes, matched by air date
CREATE INDEX torrents_air_date_idx ON torrents (air_date) WHERE air_date IS NOT NULL;

-- The search index is external content: it holds tokens only, the values live
-- in torrents. The name is tokenised by unicode61, which splits scene names on
-- punctuation into words, and seasons/episodes/kind are synthetic token columns.
CREATE VIRTUAL TABLE torrents_fts USING fts5(
    name,
    seasons,
    episodes,
    kind,
    content = 'torrents',
    content_rowid = 'id',
    tokenize = 'unicode61 remove_diacritics 2'
);

-- +goose StatementBegin
CREATE TRIGGER torrents_ai AFTER INSERT ON torrents BEGIN
    INSERT INTO torrents_fts (rowid, name, seasons, episodes, kind)
    VALUES (new.id, new.name, new.seasons, new.episodes, new.kind);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER torrents_ad AFTER DELETE ON torrents BEGIN
    INSERT INTO torrents_fts (torrents_fts, rowid, name, seasons, episodes, kind)
    VALUES ('delete', old.id, old.name, old.seasons, old.episodes, old.kind);
END;
-- +goose StatementEnd
