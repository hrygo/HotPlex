import { afterEach, describe, expect, it, vi } from "vitest";

import { BrowserHotPlexClient } from "./browser-client";
import { AEP_VERSION, EventKind, WorkerType } from "./constants";
import type { Envelope } from "./types";

afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
});

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

    it("routes durable input acknowledgements", () => {
        const client = new BrowserHotPlexClient({
            url: "ws://127.0.0.1:8888/ws",
            workerType: WorkerType.CodexCLI,
        });
        const listener = vi.fn();
        client.on("inputAck", listener);
        const data = {
            client_message_id: "evt-client-1",
            execution_id: "exec-1",
            status: "delivered" as const,
        };
        const env = envelope(EventKind.InputAck, data);

        route(client, env);

        expect(listener).toHaveBeenCalledOnce();
        expect(listener).toHaveBeenCalledWith(data, env);
    });
});

describe("BrowserHotPlexClient input retry identity", () => {
    it("uses a new envelope ID after a definitive SESSION_BUSY rejection", async () => {
        vi.useFakeTimers();
        vi.stubGlobal("WebSocket", { OPEN: 1 });
        const client = new BrowserHotPlexClient({
            url: "ws://127.0.0.1:8888/ws",
            workerType: WorkerType.CodexCLI,
        });
        const internal = client as unknown as {
            _sessionId: string;
            ws: { readyState: number };
            _send(value: Envelope): void;
        };
        internal._sessionId = "session-1";
        internal.ws = { readyState: 1 };
        const send = vi.spyOn(internal, "_send").mockImplementation(() => undefined);

        const pending = client.sendInputAsync("hello");
        const first = send.mock.calls[0][0];
        route(client, envelope(EventKind.Error, { code: "SESSION_BUSY", message: "busy" }));
        await vi.advanceTimersByTimeAsync(1_000);

        expect(send).toHaveBeenCalledTimes(2);
        expect(send.mock.calls[1][0].id).not.toBe(first.id);

        route(client, envelope(EventKind.Done, { success: true }));
        await expect(pending).resolves.toBeUndefined();
    });
});
