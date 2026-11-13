-- Reclassifying a row updates its name, season, episode and kind tokens, and
-- the search index is external content: without this trigger the old tokens
-- would keep matching after a row moved to a different category or season.
-- +goose StatementBegin
CREATE TRIGGER torrents_au AFTER UPDATE ON torrents BEGIN
    INSERT INTO torrents_fts (torrents_fts, rowid, name, seasons, episodes, kind)
    VALUES ('delete', old.id, old.name, old.seasons, old.episodes, old.kind);
    INSERT INTO torrents_fts (rowid, name, seasons, episodes, kind)
    VALUES (new.id, new.name, new.seasons, new.episodes, new.kind);
END;
-- +goose StatementEnd
