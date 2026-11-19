CREATE TABLE IF NOT EXISTS meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- IMAP UIDVALIDITY: tells clients that cached UIDs are stale when the
-- database is recreated and message ids restart.
INSERT OR IGNORE INTO meta (key, value) VALUES ('uid_validity', strftime('%s', 'now'));
