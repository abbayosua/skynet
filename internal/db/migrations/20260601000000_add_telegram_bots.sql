-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS telegram_bots (
    bot_username TEXT PRIMARY KEY,
    token TEXT NOT NULL,
    instance_id TEXT NOT NULL DEFAULT '',
    chat_id INTEGER NOT NULL DEFAULT 0,
    last_heartbeat_at INTEGER NOT NULL DEFAULT 0,
    last_used_at INTEGER NOT NULL,  -- Unix timestamp
    created_at INTEGER NOT NULL     -- Unix timestamp
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS telegram_bots;
-- +goose StatementEnd
