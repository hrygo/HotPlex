export type WorkspaceCandidate = { id: string };

export function selectRecoveryWorkspace<T extends WorkspaceCandidate>(
    workspaces: readonly T[],
    preferredId: string | null | undefined,
    failedIds: ReadonlySet<string>,
): T | null {
    const available = workspaces.filter((ws) => !failedIds.has(ws.id));
    return available.find((ws) => ws.id === preferredId) ?? available[0] ?? null;
}
