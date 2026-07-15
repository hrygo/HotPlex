import { describe, expect, it } from "vitest";

import { selectRecoveryWorkspace } from "./workspace-recovery";

const workspaces = [{ id: "a" }, { id: "b" }, { id: "c" }];

describe("selectRecoveryWorkspace", () => {
    it("keeps an available preferred workspace", () => {
        expect(selectRecoveryWorkspace(workspaces, "b", new Set())).toEqual({ id: "b" });
    });

    it("skips every failed workspace", () => {
        expect(selectRecoveryWorkspace(workspaces, "a", new Set(["a", "b"]))).toEqual({ id: "c" });
    });

    it("returns null when the recovery circuit is exhausted", () => {
        expect(selectRecoveryWorkspace(workspaces, "a", new Set(["a", "b", "c"]))).toBeNull();
    });
});
