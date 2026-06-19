-- +goose Up
-- HasAdmin (GET /api/auth/bootstrap-status) runs
-- SELECT 1 FROM users WHERE role='admin' LIMIT 1 on an unauthenticated public
-- endpoint polled by the login page. Index role to avoid a full scan under
-- scripted traffic (spec §8.6 bootstrap discovery). Pairs with the endpoint's
-- Cache-Control header to dampen DB load.
CREATE INDEX idx_users_role ON users(role);

-- +goose Down
DROP INDEX IF EXISTS idx_users_role;
