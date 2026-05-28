-- name: SaveTelegramBot :exec
INSERT INTO telegram_bots (bot_username, token, instance_id, chat_id, last_heartbeat_at, last_used_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(bot_username) DO UPDATE SET
    token = excluded.token,
    instance_id = excluded.instance_id,
    chat_id = excluded.chat_id,
    last_heartbeat_at = excluded.last_heartbeat_at,
    last_used_at = excluded.last_used_at;

-- name: ListTelegramBots :many
SELECT * FROM telegram_bots
ORDER BY last_used_at DESC;

-- name: GetTelegramBotByUsername :one
SELECT * FROM telegram_bots
WHERE bot_username = ? LIMIT 1;

-- name: UpdateTelegramBotHeartbeat :exec
UPDATE telegram_bots
SET last_heartbeat_at = ?,
    instance_id = ?
WHERE bot_username = ?;

-- name: DeleteTelegramBot :exec
DELETE FROM telegram_bots
WHERE bot_username = ?;
