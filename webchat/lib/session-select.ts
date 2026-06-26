// pickDefaultSession —— 切换 workspace(或刷新页面)时默认进入哪个 session 的选择策略。
//
// 背景:修复 "切换 workspace 未能直接进入默认 session"。
// 旧逻辑(useSessions.refreshSessions)先读 localStorage 的 savedId 恢复"上次选的":
//   1) 多 session 时会进入陈旧的旧会话,而非最近活跃会话;
//   2) savedId 对应 session 被物理删除(GC 7 天后)时,变量未清会阻断 anchor auto-create,
//      导致落入空状态。
// 新策略:显式 initialSessionId(命中)> 最近活跃(updated_at 最大)> null(由调用方 auto-create)。
// savedId 不再参与选择 —— SessionPanel 高亮用的是 activeSession prop,不读 localStorage。

/** pickDefaultSession 只依赖这两个字段,任何含它们的 session 类型均可(如 SessionInfo)。 */
export interface PickableSession {
    id: string;
    updated_at: string;
}

/**
 * 选择默认 session。
 * @param sessions        非 deleted 的候选 session 列表(调用方已过滤)。
 * @param initialSessionId 显式指定的 session(如 URL 恢复);未命中则忽略。
 * @returns 选中的 session,或 null(表示无可用 session,调用方应 auto-create anchor)。
 */
export function pickDefaultSession<T extends PickableSession>(
    sessions: readonly T[],
    initialSessionId?: string | null,
): T | null {
    if (sessions.length === 0) return null;

    // 1. 显式指定且命中 —— 最高优先级(如 URL 直链某 session)。
    if (initialSessionId) {
        const found = sessions.find((s) => s.id === initialSessionId);
        if (found) return found;
    }

    // 2. 最近活跃(by updated_at desc)。reduce 无初值,单元素直接返回;
    //    相等时保留首个最大值,行为稳定可预测。
    return sessions.reduce((a, b) =>
        new Date(a.updated_at) > new Date(b.updated_at) ? a : b,
    );
}
