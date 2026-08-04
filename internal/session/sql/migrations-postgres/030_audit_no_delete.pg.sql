-- +goose Up
-- Root-cause fix for audit-chain breakage (broken_id=1253 era): rows were
-- DELETEable with no trace, orphaning the hash chain (manual/scripted
-- deletes never wrote a checkpoint anchor). DELETE is now only permitted
-- for rows covered by an existing checkpoint anchor — the GC prune path
-- (gc.go Tick) and the checkpoint-anchored DeleteBefore write the anchor
-- in the SAME transaction as the delete (an application-level guarantee;
-- a SQL trigger cannot observe transaction identity). The anchor check
-- uses the invariant that every surviving row has id >= the latest
-- checkpoint's next_id (each GC prune deletes the whole prefix up to
-- next_id-1), so any live row fails the NOT EXISTS test and is rejected;
-- GC's own pruned rows pass it.
-- See SQLite companion for the trigger rationale.

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION "fn_ua_no_delete"() RETURNS TRIGGER AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM "audit_chain_checkpoints" WHERE "next_id" > OLD."id") THEN
        RAISE EXCEPTION 'audit: rows are immutable except via checkpoint-anchored GC';
    END IF;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER "trg_ua_no_delete"
BEFORE DELETE ON "user_activity"
FOR EACH ROW EXECUTE FUNCTION "fn_ua_no_delete"();

-- +goose Down
DROP TRIGGER IF EXISTS "trg_ua_no_delete" ON "user_activity";
DROP FUNCTION IF EXISTS "fn_ua_no_delete"();
