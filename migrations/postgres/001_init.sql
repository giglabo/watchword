CREATE TABLE IF NOT EXISTS entries (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    word       VARCHAR(500) NOT NULL,
    payload    TEXT NOT NULL,
    status     VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ,

    CONSTRAINT uq_word_status UNIQUE (word, status)
);

CREATE INDEX IF NOT EXISTS idx_entries_word ON entries (word);
CREATE INDEX IF NOT EXISTS idx_entries_status ON entries (status);
CREATE INDEX IF NOT EXISTS idx_entries_word_like ON entries (word varchar_pattern_ops);
CREATE INDEX IF NOT EXISTS idx_entries_expires_at ON entries (expires_at) WHERE status = 'active';
