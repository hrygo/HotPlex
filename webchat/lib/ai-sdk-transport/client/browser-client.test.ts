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
    it("reuses the pending envelope ID after an ambiguous reconnect", () => {
        vi.stubGlobal("WebSocket", { OPEN: 1 });
        const client = new BrowserHotPlexClient({
            url: "ws://127.0.0.1:8888/ws",
            workerType: WorkerType.CodexCLI,
        });
        const internal = client as unknown as {
            _sessionId: string;
            _reconnecting: boolean;
            ws: { readyState: number };
            _send(value: Envelope): void;
            _handleMessage(
                value: Envelope,
                resolve: (value: unknown) => void,
                reject: (err: Error) => void,
            ): void;
        };
        internal._sessionId = "session-1";
        internal.ws = { readyState: 1 };
        const send = vi.spyOn(internal, "_send").mockImplementation(() => undefined);

        client.sendInput("hello");
        const firstId = send.mock.calls[0][0].id;
        internal._reconnecting = true;

        internal._handleMessage(
            envelope(EventKind.InitAck, { state: "running" }),
            vi.fn(),
            vi.fn(),
        );

        expect(send).toHaveBeenCalledTimes(2);
        expect(send.mock.calls[1][0].id).toBe(firstId);
    });

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

    it("uses the rotated SESSION_BUSY ID once when reconnect wins the retry race", async () => {
        vi.useFakeTimers();
        vi.stubGlobal("WebSocket", { OPEN: 1 });
        const client = new BrowserHotPlexClient({
            url: "ws://127.0.0.1:8888/ws",
            workerType: WorkerType.CodexCLI,
        });
        const internal = client as unknown as {
            _sessionId: string;
            _reconnecting: boolean;
            ws: { readyState: number };
            _send(value: Envelope): void;
            _handleMessage(
                value: Envelope,
                resolve: (value: unknown) => void,
                reject: (err: Error) => void,
            ): void;
        };
        internal._sessionId = "session-1";
        internal.ws = { readyState: 1 };
        const send = vi.spyOn(internal, "_send").mockImplementation(() => undefined);

        const pending = client.sendInputAsync("hello");
        const rejectedId = send.mock.calls[0][0].id;
        route(client, envelope(EventKind.Error, { code: "SESSION_BUSY", message: "busy" }));
        internal._reconnecting = true;
        internal._handleMessage(
            envelope(EventKind.InitAck, { state: "running" }),
            vi.fn(),
            vi.fn(),
        );
        const retriedId = send.mock.calls[1][0].id;
        await vi.advanceTimersByTimeAsync(1_000);

        expect(retriedId).not.toBe(rejectedId);
        expect(send).toHaveBeenCalledTimes(2);

        route(client, envelope(EventKind.Done, { success: true }));
        await expect(pending).resolves.toBeUndefined();
    });

    it("does not resend after a terminal acknowledgement", () => {
        vi.stubGlobal("WebSocket", { OPEN: 1 });
        const client = new BrowserHotPlexClient({
            url: "ws://127.0.0.1:8888/ws",
            workerType: WorkerType.CodexCLI,
        });
        const internal = client as unknown as {
            _sessionId: string;
            _reconnecting: boolean;
            ws: { readyState: number };
            _send(value: Envelope): void;
            _handleMessage(
                value: Envelope,
                resolve: (value: unknown) => void,
                reject: (err: Error) => void,
            ): void;
        };
        internal._sessionId = "session-1";
        internal.ws = { readyState: 1 };
        const send = vi.spyOn(internal, "_send").mockImplementation(() => undefined);

        client.sendInput("hello");
        const clientMessageId = send.mock.calls[0][0].id;
        route(client, envelope(EventKind.InputAck, {
            client_message_id: clientMessageId,
            execution_id: "exec-1",
            status: "delivered",
            duplicate: true,
        }));
        internal._reconnecting = true;
        internal._handleMessage(
            envelope(EventKind.InitAck, { state: "running" }),
            vi.fn(),
            vi.fn(),
        );

        expect(send).toHaveBeenCalledOnce();
    });

    it("does not let sendInput orphan an async pending request", async () => {
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
        vi.spyOn(internal, "_send").mockImplementation(() => undefined);

        const pending = client.sendInputAsync("first");
        expect(() => client.sendInput("second")).toThrow("Input already pending");
        route(client, envelope(EventKind.Done, { success: true }));
        await expect(pending).resolves.toBeUndefined();
    });

    it("settles and clears a failed terminal acknowledgement", async () => {
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

        const pending = client.sendInputAsync("first");
        const clientMessageId = send.mock.calls[0][0].id;
        route(client, envelope(EventKind.InputAck, {
            client_message_id: clientMessageId,
            execution_id: "exec-1",
            status: "failed",
            error_code: "WORKER_UNAVAILABLE",
        }));

        await expect(pending).rejects.toThrow("WORKER_UNAVAILABLE");
        expect(() => client.sendInput("second")).not.toThrow();
    });

    it("keeps an unknown acknowledgement as a tombstone until late Done", async () => {
        vi.useFakeTimers();
        vi.stubGlobal("WebSocket", { OPEN: 1 });
        const client = new BrowserHotPlexClient({
            url: "ws://127.0.0.1:8888/ws",
            workerType: WorkerType.CodexCLI,
            heartbeat: { pingIntervalMs: 1_000_000 },
        });
        const internal = client as unknown as {
            _sessionId: string;
            ws: { readyState: number };
            _send(value: Envelope): void;
        };
        internal._sessionId = "session-1";
        internal.ws = { readyState: 1 };
        const send = vi.spyOn(internal, "_send").mockImplementation(() => undefined);

        const pending = client.sendInputAsync("first");
        const clientMessageId = send.mock.calls[0][0].id;
        route(client, envelope(EventKind.InputAck, {
            client_message_id: clientMessageId,
            execution_id: "exec-1",
            status: "unknown",
            error_code: "EXECUTION_TIMEOUT",
        }));

        await expect(pending).rejects.toThrow("EXECUTION_TIMEOUT");
        expect(() => client.sendInput("second")).toThrow("Input already pending");

        await vi.advanceTimersByTimeAsync(300_000);
        expect(() => client.sendInput("second")).toThrow("Input already pending");

        route(client, envelope(EventKind.Done, { success: true }));
        expect(() => client.sendInput("second")).not.toThrow();
    });

    it("settles a pending async input on explicit disconnect", async () => {
        vi.stubGlobal("WebSocket", { OPEN: 1, CONNECTING: 0 });
        const client = new BrowserHotPlexClient({
            url: "ws://127.0.0.1:8888/ws",
            workerType: WorkerType.CodexCLI,
        });
        const internal = client as unknown as {
            _sessionId: string;
            ws: { readyState: number; close: () => void };
            _send(value: Envelope): void;
        };
        internal._sessionId = "session-1";
        internal.ws = { readyState: 1, close: vi.fn() };
        vi.spyOn(internal, "_send").mockImplementation(() => undefined);

        const pending = client.sendInputAsync("hello");
        client.disconnect();

        await expect(pending).rejects.toThrow("Client disconnected");
    });

    it("settles a pending async input when reconnect attempts are exhausted", async () => {
        vi.stubGlobal("WebSocket", { OPEN: 1, CONNECTING: 0 });
        const client = new BrowserHotPlexClient({
            url: "ws://127.0.0.1:8888/ws",
            workerType: WorkerType.CodexCLI,
            reconnect: { enabled: true, maxAttempts: 1 },
        });
        const internal = client as unknown as {
            _sessionId: string;
            _connected: boolean;
            reconnectAttempt: number;
            pendingInput: unknown;
            ws: { readyState: number; close: () => void } | null;
            _send(value: Envelope): void;
            _handleClose(code: number, reason: string): void;
        };
        internal._sessionId = "session-1";
        internal._connected = true;
        internal.reconnectAttempt = 1;
        internal.ws = { readyState: 1, close: vi.fn() };
        vi.spyOn(internal, "_send").mockImplementation(() => undefined);

        const pending = client.sendInputAsync("hello");
        internal._handleClose(4001, "Reconnect failed");

        expect(internal.pendingInput).toBeNull();
        await expect(pending).rejects.toThrow("Reconnect failed");
    });
});
