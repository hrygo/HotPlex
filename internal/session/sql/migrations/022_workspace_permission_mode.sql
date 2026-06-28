-- +goose Up
-- Issue #789: Workspace permission mode (read-only|workspace|auto-edit|bypass).
-- Nullable: NULL → bridge normalizes to the global default (bypass). Adding nullable
-- (not NOT NULL DEFAULT) avoids SQLite/PG ADD COLUMN rewrite differences and keeps
-- existing rows untouched (zero-damage backward-compatible upgrade).
ALTER TABLE workspaces ADD COLUMN permission_mode TEXT;

-- +goose Down
-- SQLite < 3.35 cannot DROP COLUMN; the column is inert once unread, so Down is a
-- no-op here. The PG companion drops it. On SQLite 3.35+ a manual DROP COLUMN works.
