-- +goose Up
-- HasAdmin (GET /api/auth/bootstrap-status) runs
-- SELECT 1 FROM users WHERE role='admin' LIMIT 1 on an unauthenticated public
-- endpoint polled by the login page. A partial index matches the predicate
-- exactly and avoids the poor selectivity of a full B-tree on a 2-value
-- column (review P2-3). Pairs with the endpoint's Cache-Control header.
CREATE INDEX idx_users_role ON "users"(role) WHERE role = 'admin';

-- +goose Down
DROP INDEX IF EXISTS idx_users_role;
