-- +goose Up
ALTER TABLE sessions ADD COLUMN bot_name TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE sessions DROP COLUMN bot_name;
