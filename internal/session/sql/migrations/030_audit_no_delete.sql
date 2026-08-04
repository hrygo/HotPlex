-- +goose Up
-- Root-cause fix for audit-chain breakage (broken_id=1253 era): rows were
-- DELETEable with no trace, orphaning the hash chain (manual/scripted
-- deletes never wrote a checkpoint anchor). DELETE is now only permitted
-- for rows anchored by a checkpoint written in the same transaction —
-- i.e. the GC prune path (gc.go Tick) and the checkpoint-anchored
-- DeleteBefore. The anchor check uses the invariant that every surviving
-- row has id >= the latest checkpoint's next_id (each GC prune deletes
-- the whole prefix up to next_id-1), so any live row fails the
-- NOT EXISTS test and is rejected; GC's own pruned rows pass it.

-- +goose StatementBegin
CREATE TRIGGER trg_ua_no_delete
BEFORE DELETE ON user_activity
FOR EACH ROW
BEGIN
    SELECT RAISE(ABORT, 'audit: rows are immutable except via checkpoint-anchored GC')
    WHERE NOT EXISTS (
        SELECT 1 FROM audit_chain_checkpoints WHERE next_id > OLD.id
    );
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS trg_ua_no_delete;
