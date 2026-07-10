import { describe, expect, it, vi } from "vitest";

import { BrowserHotPlexClient } from "./browser-client";
import { AEP_VERSION, EventKind, WorkerType } from "./constants";
import type { Envelope } from "./types";

function route(client: BrowserHotPlexClient, env: Envelope): void {
    (client as unknown as { _routeEvent(value: Envelope): void })._routeEvent(env);
}

function envelope(type: string, data: unknown): Envelope {
    return {
        version: AEP_VERSION,
        id: "ack-envelope",
        seq: 1,
        session_id: "session-1",
        timestamp: Date.now(),
        event: { type, data },
    };
}

describe("BrowserHotPlexClient interaction acknowledgements", () => {
    it.each([
        [EventKind.PermissionResponse, "permissionResponse", { id: "permission-1", allowed: true }],
        [EventKind.QuestionResponse, "questionResponse", { id: "question-1", answers: { q: "a" } }],
        [EventKind.ElicitationResponse, "elicitationResponse", { id: "elicitation-1", action: "accept" }],
    ] as const)("routes %s as %s", (kind, eventName, data) => {
        const client = new BrowserHotPlexClient({
            url: "ws://127.0.0.1:8888/ws",
            workerType: WorkerType.CodexCLI,
        });
        const listener = vi.fn();
        client.on(eventName, listener);

        const env = envelope(kind, data);
        route(client, env);

        expect(listener).toHaveBeenCalledOnce();
        expect(listener).toHaveBeenCalledWith(data, env);
    });
});
