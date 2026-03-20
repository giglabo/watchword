CREATE TABLE IF NOT EXISTS download_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    entry_id TEXT NOT NULL,
    word TEXT NOT NULL,
    filename TEXT NOT NULL,
    requested_at TEXT NOT NULL,
    client_ip TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_download_history_entry_id ON download_history(entry_id);
CREATE INDEX IF NOT EXISTS idx_download_history_requested_at ON download_history(requested_at);
