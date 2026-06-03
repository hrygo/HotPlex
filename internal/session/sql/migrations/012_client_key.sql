-- +goose Up
ALTER TABLE sessions ADD COLUMN client_key TEXT NOT NULL DEFAULT '';

-- +goose Down
