CREATE TABLE IF NOT EXISTS entries (
    id         TEXT PRIMARY KEY,
    word       TEXT NOT NULL,
    payload    TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'active',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    expires_at TEXT,

    CONSTRAINT uq_word_status UNIQUE (word, status)
);

CREATE INDEX IF NOT EXISTS idx_entries_word ON entries (word);
CREATE INDEX IF NOT EXISTS idx_entries_status ON entries (status);
CREATE INDEX IF NOT EXISTS idx_entries_expires_at ON entries (expires_at) WHERE status = 'active';
