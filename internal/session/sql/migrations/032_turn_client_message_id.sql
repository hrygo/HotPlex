-- +goose Up

ALTER TABLE turns ADD COLUMN client_message_id TEXT;

-- +goose Down
ALTER TABLE turns DROP COLUMN client_message_id;
