-- invitations.set_used_by: 将 CAS 消费时的占位 used_by（inv.CreatedBy）更新为真实接受者。
-- 条件 used_by = ? 确保只更新自己的占位，防并发误改（spec §8.6）。
UPDATE invitations SET used_by = ? WHERE id = ? AND used_by = ?
