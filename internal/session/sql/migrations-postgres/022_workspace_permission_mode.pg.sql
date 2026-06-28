-- +goose Up
-- Issue #789: Workspace permission mode (see SQLite companion for rationale).
ALTER TABLE workspaces ADD COLUMN permission_mode TEXT;

-- +goose Down
ALTER TABLE workspaces DROP COLUMN permission_mode;
