-- +goose Up
ALTER TABLE sessions ADD COLUMN permission_ceiling TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE sessions DROP COLUMN permission_ceiling;
