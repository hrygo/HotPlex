import { test, expect, type Page } from "@playwright/test";

const COMPOSER_PLACEHOLDER = "输入消息，或输入 '/' 使用命令...";

type SentEnvelope = {
    id: string;
    event: { type: string; data: Record<string, unknown> };
};

async function installMockGateway(page: Page) {
    await page.addInitScript(() => {
        type Envelope = {
            version: string;
            id: string;
            seq: number;
            session_id: string;
            timestamp: number;
            event: { type: string; data: Record<string, unknown> };
        };
        type TestWindow = typeof window & {
            __aepEvents: Envelope[];
            __mockAEP: {
                emit(
                    type: string,
                    data: Record<string, unknown>,
                    id?: string,
                ): void;
                disconnect(): void;
                pauseNextConnect(): void;
                setNextInitState(state: "idle" | "running"): void;
                setNextInputOutcome(
                    outcome: "delivered" | "unknown" | "failed",
                ): void;
            };
        };

        const testWindow = window as TestWindow;
        const NativeWebSocket = window.WebSocket;
        testWindow.__aepEvents = [];
        let serverSequence = 0;
        let activeSocket: MockWebSocket | null = null;
        let nextInitState: "idle" | "running" = "idle";
        let nextInputOutcome: "delivered" | "unknown" | "failed" = "delivered";
        let pauseNextSocketOpen = false;

        class MockWebSocket extends EventTarget {
            static readonly CONNECTING = 0;
            static readonly OPEN = 1;
            static readonly CLOSING = 2;
            static readonly CLOSED = 3;

            readonly url: string;
            readyState = MockWebSocket.CONNECTING;
            onopen: ((event: Event) => void) | null = null;
            onmessage: ((event: MessageEvent) => void) | null = null;
            onerror: ((event: Event) => void) | null = null;
            onclose: ((event: CloseEvent) => void) | null = null;

            constructor(url: string | URL) {
                super();
                this.url = String(url);
                activeSocket = this;
                queueMicrotask(() => {
                    if (pauseNextSocketOpen) {
                        pauseNextSocketOpen = false;
                        return;
                    }
                    this.readyState = MockWebSocket.OPEN;
                    const event = new Event("open");
                    this.dispatchEvent(event);
                    this.onopen?.(event);
                });
            }

            send(payload: string) {
                const envelope = JSON.parse(payload) as Envelope;
                testWindow.__aepEvents.push(envelope);
                if (envelope.event.type === "init") {
                    this.emit("init_ack", { state: nextInitState });
                    return;
                }
                if (envelope.event.type === "input") {
                    const outcome = nextInputOutcome;
                    nextInputOutcome = "delivered";
                    queueMicrotask(() => {
                        this.emit("input.ack", {
                            client_message_id: envelope.id,
                            execution_id: `execution-${envelope.id}`,
                            status: "accepted",
                        });
                        this.emit("input.ack", {
                            client_message_id: envelope.id,
                            execution_id: `execution-${envelope.id}`,
                            status: outcome,
                            ...(outcome === "delivered"
                                ? {}
                                : {
                                      error_code: `TEST_${outcome.toUpperCase()}`,
                                  }),
                        });
                    });
                    return;
                }
                if (envelope.event.type === "ping") {
                    this.emit("pong", {});
                }
            }

            close(code = 1000, reason = "mock closed") {
                if (this.readyState === MockWebSocket.CLOSED) return;
                this.readyState = MockWebSocket.CLOSED;
                if (activeSocket === this) activeSocket = null;
                const event = new CloseEvent("close", { code, reason });
                this.dispatchEvent(event);
                this.onclose?.(event);
            }

            emit(type: string, data: Record<string, unknown>, id?: string) {
                serverSequence += 1;
                const envelope: Envelope = {
                    version: "aep/v1",
                    id: id ?? `server-${serverSequence}`,
                    seq: serverSequence,
                    session_id: "session-e2e",
                    timestamp: Date.now(),
                    event: { type, data },
                };
                const event = new MessageEvent("message", {
                    data: JSON.stringify(envelope),
                });
                this.dispatchEvent(event);
                this.onmessage?.(event);
            }
        }

        testWindow.__mockAEP = {
            emit(type, data, id) {
                activeSocket?.emit(type, data, id);
            },
            disconnect() {
                activeSocket?.close(1012, "mock reconnect");
            },
            pauseNextConnect() {
                pauseNextSocketOpen = true;
            },
            setNextInitState(state) {
                nextInitState = state;
            },
            setNextInputOutcome(outcome) {
                nextInputOutcome = outcome;
            },
        };
        const RoutedWebSocket = new Proxy(NativeWebSocket, {
            construct(Target, args) {
                const [rawUrl] = args as [string | URL];
                const url = new URL(String(rawUrl), window.location.href);
                if (url.pathname !== "/ws") {
                    return Reflect.construct(Target, args);
                }
                return new MockWebSocket(rawUrl);
            },
        });
        Object.defineProperty(window, "WebSocket", {
            configurable: true,
            writable: true,
            value: RoutedWebSocket,
        });
    });

    await page.route("**/api/**", async (route) => {
        const request = route.request();
        const url = new URL(request.url());
        const json = (body: unknown, status = 200) =>
            route.fulfill({
                status,
                contentType: "application/json",
                body: JSON.stringify(body),
            });

        if (url.pathname === "/api/auth/me") {
            await json({
                id: "user-e2e",
                username: "e2e",
                display_name: "E2E User",
                role: "admin",
                status: "active",
                created_at: 1,
                updated_at: 1,
            });
            return;
        }
        if (url.pathname === "/api/workspaces" && request.method() === "GET") {
            await json({
                workspaces: [
                    {
                        id: "workspace-e2e",
                        name: "E2E Workspace",
                        work_dir: "/tmp/e2e",
                        owner_user_id: "user-e2e",
                        worker_preference: "codex_cli",
                        permission_mode: "workspace",
                        agent_config_overrides: "{}",
                        status: "active",
                        created_at: 1,
                        updated_at: 1,
                    },
                ],
                limit: 100,
                offset: 0,
            });
            return;
        }
        if (url.pathname === "/api/sessions" && request.method() === "GET") {
            await json({
                sessions: [
                    {
                        id: "session-e2e",
                        user_id: "user-e2e",
                        worker_type: "codex_cli",
                        state: "idle",
                        title: "Queue E2E",
                        work_dir: "/tmp/e2e",
                        created_at: new Date(1).toISOString(),
                        updated_at: new Date(2).toISOString(),
                    },
                ],
                limit: 20,
                offset: 0,
            });
            return;
        }
        if (url.pathname === "/api/sessions/session-e2e/history") {
            await json({ records: [], has_more: false });
            return;
        }
        if (url.pathname === "/api/workers") {
            await json([{ type: "codex_cli", installed: true }]);
            return;
        }
        if (request.method() === "DELETE") {
            await route.fulfill({ status: 204 });
            return;
        }
        await json({});
    });
}

async function waitForChatReady(page: Page) {
    await page.getByPlaceholder(COMPOSER_PLACEHOLDER).waitFor({
        state: "visible",
        timeout: 20_000,
    });
}

function composerInput(page: Page) {
    return page.getByPlaceholder(COMPOSER_PLACEHOLDER);
}

function sendButton(page: Page) {
    return page.getByRole("button", { name: "发送消息" });
}

async function sentInputs(page: Page): Promise<SentEnvelope[]> {
    return page.evaluate(() =>
        (
            (window as typeof window & { __aepEvents: SentEnvelope[] })
                .__aepEvents ?? []
        ).filter((event) => event.event.type === "input"),
    );
}

async function sentEvents(page: Page): Promise<SentEnvelope[]> {
    return page.evaluate(
        () =>
            (window as typeof window & { __aepEvents: SentEnvelope[] })
                .__aepEvents ?? [],
    );
}

async function emitDone(page: Page, id: string, reason = "completed") {
    await page.evaluate(
        ({ eventId, doneReason }) => {
            (
                window as typeof window & {
                    __mockAEP: {
                        emit(
                            type: string,
                            data: Record<string, unknown>,
                            id?: string,
                        ): void;
                    };
                }
            ).__mockAEP.emit(
                "done",
                { success: true, reason: doneReason },
                eventId,
            );
        },
        { eventId: id, doneReason: reason },
    );
}

async function emitGatewayEvent(
    page: Page,
    type: string,
    data: Record<string, unknown>,
    id?: string,
) {
    await page.evaluate(
        ({ eventType, eventData, eventId }) => {
            (
                window as typeof window & {
                    __mockAEP: {
                        emit(
                            type: string,
                            data: Record<string, unknown>,
                            id?: string,
                        ): void;
                    };
                }
            ).__mockAEP.emit(eventType, eventData, eventId);
        },
        { eventType: type, eventData: data, eventId: id },
    );
}

async function setNextInputOutcome(
    page: Page,
    outcome: "delivered" | "unknown" | "failed",
) {
    await page.evaluate((nextOutcome) => {
        (
            window as typeof window & {
                __mockAEP: {
                    setNextInputOutcome(value: typeof nextOutcome): void;
                };
            }
        ).__mockAEP.setNextInputOutcome(nextOutcome);
    }, outcome);
}

async function disconnectGateway(
    page: Page,
    reconnectState: "idle" | "running",
    pauseReconnect = false,
) {
    await page.evaluate(
        ({ state, pause }) => {
            const mock = (
                window as typeof window & {
                    __mockAEP: {
                        setNextInitState(value: typeof state): void;
                        pauseNextConnect(): void;
                        disconnect(): void;
                    };
                }
            ).__mockAEP;
            mock.setNextInitState(state);
            if (pause) mock.pauseNextConnect();
            mock.disconnect();
        },
        { state: reconnectState, pause: pauseReconnect },
    );
}

test.describe("Chat Page", () => {
    test.beforeEach(async ({ page }) => {
        await installMockGateway(page);
        await page.goto("/");
        await waitForChatReady(page);
    });

    test("renders an authenticated chat with a usable composer", async ({
        page,
    }) => {
        await expect(
            page.getByRole("heading", { name: "HotPlex" }),
        ).toBeVisible();
        await expect(composerInput(page)).toBeEditable();
        await expect(sendButton(page)).toBeDisabled();

        await composerInput(page).fill("Hello");
        await expect(sendButton(page)).toBeEnabled();
        await composerInput(page).press("Shift+Enter");
        await composerInput(page).pressSequentially("world");
        await expect(composerInput(page)).toHaveValue(/Hello\nworld/);
    });

    test("queues follow-ups and drains them as one merged input per terminal turn", async ({
        page,
    }) => {
        const input = composerInput(page);
        await input.fill("first");
        await input.press("Enter");
        await expect.poll(async () => (await sentInputs(page)).length).toBe(1);

        await input.fill("second");
        await input.press("Enter");
        await input.fill("third");
        await page.getByRole("button", { name: "将消息加入后续队列" }).click();

        const panel = page.getByRole("region", { name: "后续消息队列" });
        await expect(panel).toContainText("second");
        await expect(panel).toContainText("third");
        expect(await sentInputs(page)).toHaveLength(1);

        await emitDone(page, "done-first");
        await expect.poll(async () => (await sentInputs(page)).length).toBe(2);
        const merged = (await sentInputs(page))[1]?.event.data.content;
        expect(merged).toContain("second");
        expect(merged).toContain("third");
        await expect(panel).toHaveCount(0);

        // A duplicate terminal envelope must not dispatch anything further.
        await emitDone(page, "done-first");
        await expect.poll(async () => (await sentInputs(page)).length, {
            timeout: 1_000,
        }).toBe(2);
    });

    test("drains queued follow-ups as one merged input after an idle reconnect", async ({
        page,
    }) => {
        const input = composerInput(page);
        await input.fill("first");
        await input.press("Enter");
        await expect.poll(async () => (await sentInputs(page)).length).toBe(1);

        await input.fill("delivered before disconnect");
        await input.press("Enter");
        await input.fill("must drain after idle reconnect");
        await input.press("Enter");

        await disconnectGateway(page, "idle");

        await expect
            .poll(async () => (await sentInputs(page)).length, {
                timeout: 10_000,
            })
            .toBe(2);
        const drained = (await sentInputs(page))[1]?.event.data.content ?? "";
        expect(drained).toContain("delivered before disconnect");
        expect(drained).toContain("must drain after idle reconnect");
    });

    test("drops an unknown delivery outcome and keeps the queue draining", async ({
        page,
    }) => {
        const input = composerInput(page);
        await input.fill("first");
        await input.press("Enter");
        await input.fill("ambiguous follow-up");
        await input.press("Enter");

        await setNextInputOutcome(page, "unknown");
        await emitDone(page, "done-before-unknown");
        await expect.poll(async () => (await sentInputs(page)).length).toBe(2);

        // The current runtime does not preserve unknown-dispatched items for
        // manual retry: the queue empties and the outcome surfaces in the
        // transcript instead of a failed queue entry.
        await expect(
            page.getByRole("region", { name: "后续消息队列" }),
        ).toHaveCount(0);
        await expect.poll(async () => (await sentInputs(page)).length, {
            timeout: 1_000,
        }).toBe(2);

        // The stale turn is still considered active, so new inputs queue
        // instead of dispatching immediately.
        await input.fill("must stay behind the unknown turn");
        await input.press("Enter");
        const panel = page.getByRole("region", { name: "后续消息队列" });
        await expect(panel).toContainText("must stay behind the unknown turn");
        expect(await sentInputs(page)).toHaveLength(2);

        // The next terminal envelope ends the stale turn and drains the queue.
        await emitDone(page, "done-after-unknown");
        await expect.poll(async () => (await sentInputs(page)).length).toBe(3);
        expect((await sentInputs(page))[2]?.event.data.content).toContain(
            "must stay behind the unknown turn",
        );
    });

    test("deletes a failed send-now item from the queue", async ({ page }) => {
        const input = composerInput(page);
        await input.fill("first");
        await input.press("Enter");
        await input.fill("urgent to delete");
        await input.press("Enter");

        await disconnectGateway(page, "running", true);
        const urgent = page
            .getByRole("listitem")
            .filter({ hasText: "urgent to delete" });
        await urgent.getByRole("button", { name: /展开队列第/ }).click();
        await urgent
            .getByRole("button", { name: /停止当前轮次并立即发送队列第/ })
            .click();
        await expect(urgent).toContainText("需要处理");

        await urgent.getByRole("button", { name: /删除队列第/ }).click();

        await expect(
            page.getByRole("region", { name: "后续消息队列" }),
        ).toHaveCount(0);
        expect(await sentInputs(page)).toHaveLength(1);
    });

    test("converges unknown delivery when the original turn finishes late", async ({
        page,
    }) => {
        const input = composerInput(page);
        await input.fill("first");
        await input.press("Enter");
        await input.fill("late terminal follow-up");
        await input.press("Enter");

        await setNextInputOutcome(page, "unknown");
        await emitDone(page, "done-before-late-terminal");
        await expect.poll(async () => (await sentInputs(page)).length).toBe(2);
        // The current runtime drops unknown-dispatched items, so the queue is
        // empty; convergence is verified on the transcript below.
        const panel = page.getByRole("region", { name: "后续消息队列" });
        await expect(panel).toHaveCount(0);

        await emitGatewayEvent(
            page,
            "message.start",
            { id: "late-assistant", role: "assistant", content_type: "text" },
            "late-message-start",
        );
        await emitGatewayEvent(page, "message.delta", {
            message_id: "late-assistant",
            content: "late response content",
        });
        await emitGatewayEvent(
            page,
            "done",
            {
                success: true,
                reason: "late-terminal-for-unknown",
                stats: {
                    _session: {
                        model_name: "late-convergence-model",
                        turn_count: 2,
                        tool_call_count: 0,
                        duration: "1s",
                        duration_seconds: 1,
                        total_input_tok: 10,
                        total_output_tok: 5,
                        context_fill: 10,
                        context_window: 100,
                        context_pct: 10,
                        total_cost_usd: 0,
                        turn_duration_ms: 1000,
                        turn_input_tok: 6,
                        turn_output_tok: 5,
                        turn_cost_usd: 0,
                        tool_names: null,
                        work_dir: "/tmp/hotplex-e2e",
                    },
                },
            },
            "late-terminal-for-unknown",
        );
        await expect(panel).toHaveCount(0);
        await expect(
            page.getByText("late terminal follow-up", { exact: true }),
        ).toHaveCount(1);
        const lateResponse = page.getByText("late response content", {
            exact: true,
        });
        await expect(lateResponse).toBeVisible();
        const lateResponseBody = lateResponse.locator(
            "xpath=ancestor::div[contains(concat(' ', normalize-space(@class), ' '), ' msg-assistant-body ')][1]",
        );
        await expect(lateResponseBody).toContainText("late-convergence-model");
        await expect(lateResponseBody.locator(".streaming-cursor")).toHaveCount(
            0,
        );
        await expect.poll(async () => (await sentInputs(page)).length, {
            timeout: 1_000,
        }).toBe(2);
        await expect(
            page.getByRole("button", { name: /重试队列第/ }),
        ).toHaveCount(0);
    });

    test("converges unknown delivery when its delivered ACK arrives late", async ({
        page,
    }) => {
        const input = composerInput(page);
        await input.fill("first");
        await input.press("Enter");
        await input.fill("late acknowledgement follow-up");
        await input.press("Enter");

        await setNextInputOutcome(page, "unknown");
        await emitDone(page, "done-before-late-ack");
        await expect.poll(async () => (await sentInputs(page)).length).toBe(2);
        const originalDispatch = (await sentInputs(page))[1];
        // The current runtime drops unknown-dispatched items, so the queue is
        // already empty; the late delivered ACK must remain a no-op.
        const panel = page.getByRole("region", { name: "后续消息队列" });
        await expect(panel).toHaveCount(0);

        await emitGatewayEvent(page, "input.ack", {
            client_message_id: originalDispatch.id,
            execution_id: `execution-${originalDispatch.id}`,
            status: "delivered",
        });
        await expect(panel).toHaveCount(0);
        expect(await sentInputs(page)).toHaveLength(2);
    });

    test("reports send-now failure instead of silently degrading while reconnecting", async ({
        page,
    }) => {
        const input = composerInput(page);
        await input.fill("first");
        await input.press("Enter");
        await input.fill("urgent while reconnecting");
        await input.press("Enter");

        await disconnectGateway(page, "running", true);
        const urgent = page
            .getByRole("listitem")
            .filter({ hasText: "urgent while reconnecting" });
        await urgent.getByRole("button", { name: /展开队列第/ }).click();
        await urgent
            .getByRole("button", {
                name: /停止当前轮次并立即发送队列第/,
            })
            .click();

        await expect(urgent).toContainText("需要处理");
        await expect(urgent).toContainText("连接中断");
        expect(
            (await sentEvents(page)).filter(
                (event) => event.event.type === "control",
            ),
        ).toHaveLength(0);
    });

    test("edits and deletes queued prompts before their only dispatch", async ({
        page,
    }) => {
        const input = composerInput(page);
        await input.fill("first");
        await input.press("Enter");
        await input.fill("draft prompt");
        await input.press("Enter");
        await input.fill("remove me");
        await input.press("Enter");

        const draftItem = page
            .getByRole("listitem")
            .filter({ hasText: "draft prompt" });
        await draftItem.getByRole("button", { name: /展开队列第/ }).click();
        await draftItem.getByRole("button", { name: /编辑队列第/ }).click();
        const editInput = draftItem.getByRole("textbox", {
            name: /编辑队列第/,
        });
        await editInput.fill("final prompt");
        await page.getByRole("button", { name: "保存" }).click();

        const removedItem = page
            .getByRole("listitem")
            .filter({ hasText: "remove me" });
        await removedItem.getByRole("button", { name: /删除队列第/ }).click();
        await expect(page.getByText("remove me")).toHaveCount(0);

        await emitDone(page, "done-edit");
        await expect.poll(async () => (await sentInputs(page)).length).toBe(2);
        const inputs = await sentInputs(page);
        expect(inputs[1]?.event.data.content).toBe("final prompt");
        expect(
            inputs.some((event) => event.event.data.content === "draft prompt"),
        ).toBe(false);
        expect(
            inputs.some((event) => event.event.data.content === "remove me"),
        ).toBe(false);
    });

    test("send now stops the current turn and promotes the selected item", async ({
        page,
    }) => {
        const input = composerInput(page);
        await input.fill("first");
        await input.press("Enter");
        await input.fill("normal follow-up");
        await input.press("Enter");
        await input.fill("urgent follow-up");
        await input.press("Enter");

        const urgent = page
            .getByRole("listitem")
            .filter({ hasText: "urgent follow-up" });
        await urgent.getByRole("button", { name: /展开队列第/ }).click();
        await urgent
            .getByRole("button", {
                name: /停止当前轮次并立即发送队列第/,
            })
            .click();

        await expect
            .poll(
                async () =>
                    (await sentEvents(page)).filter(
                        (event) => event.event.type === "control",
                    ).length,
            )
            .toBe(1);
        expect(await sentInputs(page)).toHaveLength(1);

        await emitDone(page, "done-stopped", "stopped_by_user");
        await expect.poll(async () => (await sentInputs(page)).length).toBe(2);
        const drained = (await sentInputs(page))[1]?.event.data.content ?? "";
        expect(drained).toContain("urgent follow-up");
        expect(drained).toContain("normal follow-up");
    });

    test("keeps the draft when the 20-item queue is full", async ({ page }) => {
        const input = composerInput(page);
        await input.fill("first");
        await input.press("Enter");

        for (let index = 1; index <= 20; index += 1) {
            await input.fill(`queued-${index}`);
            await input.press("Enter");
        }
        await expect(page.getByRole("listitem")).toHaveCount(20);

        await emitGatewayEvent(
            page,
            "message",
            {
                role: "assistant",
                content: "final answer before terminal done",
            },
            "message-before-done",
        );
        await input.fill("overflow draft");
        await input.press("Enter");
        await expect(input).toHaveValue("overflow draft");
        await expect(
            page.getByText("队列最多容纳 20 条消息", { exact: false }),
        ).toBeVisible();
        await expect(page.getByRole("listitem")).toHaveCount(20);
    });

    test("collapses and restores the desktop session sidebar", async ({
        page,
    }) => {
        const collapse = page.getByRole("button", { name: "折叠侧边栏" });
        await collapse.click();
        const expand = page.getByRole("button", { name: "展开侧边栏" });
        await expect(expand).toBeVisible();
        await expand.click();
        await expect(page.getByText("Queue E2E")).toBeVisible();
    });
});
