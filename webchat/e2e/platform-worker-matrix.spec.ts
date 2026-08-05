import { test, expect, type Page } from "@playwright/test";
import {
    installMockGateway,
    sentEvents,
    sentInputs,
    emitGatewayEvent,
    emitDone,
} from "./fixtures/mock-gateway";

const COMPOSER_PLACEHOLDER = "输入消息，或输入 '/' 使用命令...";

/**
 * Frozen worker matrix contract (task 3 brief). The page-level core flow runs
 * once per combination; a separate guard test pins the exact ID set so a
 * truncated loop cannot silently go green.
 */
const WEBCHAT_WORKERS = [
    { id: "W-C", workerType: "claude_code" },
    { id: "W-O", workerType: "opencode_server" },
    { id: "W-X", workerType: "codex_cli" },
    { id: "W-A", workerType: "acp" },
] as const;

/** Controlled, non-sensitive interaction fixture path — never a local user path. */
const CONTROLLED_PATH = "/tmp/hotplex-e2e/sample.txt";

async function waitForChatReady(page: Page) {
    await page.getByPlaceholder(COMPOSER_PLACEHOLDER).waitFor({
        state: "visible",
        timeout: 20_000,
    });
}

function composerInput(page: Page) {
    return page.getByPlaceholder(COMPOSER_PLACEHOLDER);
}

async function controlStops(page: Page) {
    return (await sentEvents(page)).filter(
        (event) =>
            event.event.type === "control" &&
            event.event.data.action === "stop",
    );
}

test("W-C/W-O/W-X/W-A/webchat/matrix/exact-ids", async () => {
    // Loop-silent-pass guard: the parameterized rows below are only complete
    // if every ID in the frozen contract is present.
    expect(WEBCHAT_WORKERS.map((worker) => worker.id)).toEqual([
        "W-C",
        "W-O",
        "W-X",
        "W-A",
    ]);
});

for (const combo of WEBCHAT_WORKERS) {
    test(`${combo.id}/webchat/${combo.workerType}/C01+K01+C05 core flow`, async ({
        page,
    }) => {
        await installMockGateway(page, combo.workerType);
        // TEMP MUTATION: force the session list to codex_cli
        await page.route(/\/api\/sessions(\?|$)/, async (route) => {
            await route.fulfill({
                status: 200,
                contentType: "application/json",
                body: JSON.stringify({
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
                }),
            });
        });
        await page.goto("/");
        await waitForChatReady(page);

        // ── C01: the /ws init envelope carries the combo's worker_type ─────
        await expect
            .poll(async () => {
                const init = (await sentEvents(page)).find(
                    (event) => event.event.type === "init",
                );
                return init?.event.data.worker_type;
            })
            .toBe(combo.workerType);

        // C01: first input dispatches with the literal content and a non-empty
        // client id; the two-phase input.ack (accepted → delivered) leaves the
        // turn active with the composer in queue mode.
        const input = composerInput(page);
        await input.fill(`matrix-basic-${combo.id}`);
        await input.press("Enter");

        await expect.poll(async () => (await sentInputs(page)).length).toBe(1);
        const firstInput = (await sentInputs(page))[0];
        expect(firstInput?.event.data.content).toBe(`matrix-basic-${combo.id}`);
        expect(firstInput?.id).toBeTruthy();

        await expect(
            page.getByRole("button", { name: "将消息加入后续队列" }),
        ).toBeVisible();
        // The user bubble commits in the same state update as the pending
        // assistant placeholder. Waiting for it guarantees the placeholder is
        // committed before we emit worker events, so the delta can attach.
        await expect(
            page.getByText(`matrix-basic-${combo.id}`, { exact: true }),
        ).toHaveCount(1);

        // C01: message.start + two deltas + completed done → exactly one merged
        // result and a completed (non-streaming) turn.
        await emitGatewayEvent(page, "message.start", {
            id: `assistant-basic-${combo.id}`,
            role: "assistant",
            content_type: "text",
        });
        await emitGatewayEvent(page, "message.delta", {
            message_id: `assistant-basic-${combo.id}`,
            content: "matrix answer part one",
        });
        await emitGatewayEvent(page, "message.delta", {
            message_id: `assistant-basic-${combo.id}`,
            content: " and part two",
        });
        // The done handler captures the pending-assistant id before the
        // delta's state update runs; emitting done before the merged text is
        // committed would remove the message the delta just attached to.
        await expect(
            page.getByText("matrix answer part one and part two", {
                exact: true,
            }),
        ).toBeVisible();
        await emitDone(page, `done-basic-${combo.id}`, "completed");

        await expect(
            page.getByText("matrix answer part one and part two", {
                exact: true,
            }),
        ).toHaveCount(1);
        await expect(page.locator(".streaming-cursor")).toHaveCount(0);
        await expect(
            page.getByRole("button", { name: "发送消息" }),
        ).toBeVisible();

        // ── K01: permission_request renders the card; Allow emits exactly one
        //        permission_response with ID/allowed fidelity ────────────────
        const permissionId = `permission-${combo.id}`;
        await emitGatewayEvent(page, "permission_request", {
            id: permissionId,
            tool_name: "Read",
            description: `Read ${CONTROLLED_PATH}`,
            args: [JSON.stringify({ tool: "Read", path: CONTROLLED_PATH })],
        });

        await expect(
            page.getByText("工具执行授权", { exact: true }),
        ).toBeVisible();
        await expect(
            page.getByText(CONTROLLED_PATH, { exact: true }),
        ).toHaveCount(1);
        await expect(
            page.getByText("Read", { exact: true }).first(),
        ).toBeVisible();

        await page.getByRole("button", { name: "允许" }).click();

        await expect
            .poll(async () =>
                (await sentEvents(page)).filter(
                    (event) => event.event.type === "permission_response",
                ).length,
            )
            .toBe(1);
        const permissionResponses = (await sentEvents(page)).filter(
            (event) => event.event.type === "permission_response",
        );
        expect(permissionResponses[0]?.event.data.id).toBe(permissionId);
        expect(permissionResponses[0]?.event.data.allowed).toBe(true);

        // Gateway echo resolves the card to its approved terminal state.
        await emitGatewayEvent(page, "permission_response", {
            id: permissionId,
            allowed: true,
        });
        await expect(
            page.getByText("已允许执行", { exact: true }),
        ).toBeVisible();

        // ── Second long turn: delta streams, user stops via the existing
        //    stop UI; outbound control(action=stop) appears exactly once ─────
        await input.fill(`matrix-stop-${combo.id}`);
        await input.press("Enter");
        await expect.poll(async () => (await sentInputs(page)).length).toBe(2);
        expect((await sentInputs(page))[1]?.event.data.content).toBe(
            `matrix-stop-${combo.id}`,
        );
        await expect(
            page.getByText(`matrix-stop-${combo.id}`, { exact: true }),
        ).toHaveCount(1);

        await emitGatewayEvent(page, "message.delta", {
            message_id: `stop-msg-${combo.id}`,
            content: "long response that will be stopped",
        });
        await expect(
            page.getByText("long response that will be stopped", {
                exact: true,
            }),
        ).toBeVisible();

        await page.getByRole("button", { name: "停止生成" }).click();
        await expect.poll(async () => (await controlStops(page)).length).toBe(1);
        expect((await controlStops(page))[0]?.event.data.action).toBe("stop");

        // The stop UI flips to the disabled "正在停止…" state while waiting for
        // the terminal done.
        await expect(
            page.getByRole("button", { name: "正在停止…" }),
        ).toBeVisible();

        // ── A consecutive second stop must not emit a second control ────────
        // The button is disabled now; dispatch a bubbling click (the same
        // path a fast second user click would take). The stop coalescing
        // (adapter stoppingRef + transport pendingStop) must swallow it.
        await page.evaluate(() => {
            const button = document.querySelector(
                'button[aria-label="正在停止…"]',
            );
            if (button) {
                button.dispatchEvent(
                    new MouseEvent("click", {
                        bubbles: true,
                        cancelable: true,
                    }),
                );
            }
        });
        await expect
            .poll(async () => (await controlStops(page)).length, {
                timeout: 2_000,
            })
            .toBe(1);

        // ── done(reason="stopped_by_user") ends the pending stop; repeating
        //    the same done id must not produce a second terminal UI ──────────
        await emitDone(page, `done-stop-${combo.id}`, "stopped_by_user");

        await expect(
            page.getByRole("button", { name: "正在停止…" }),
        ).toHaveCount(0);
        await expect(
            page.getByRole("button", { name: "停止生成" }),
        ).toHaveCount(0);
        await expect(
            page.getByRole("button", { name: "发送消息" }),
        ).toBeVisible();
        await expect(page.locator(".streaming-cursor")).toHaveCount(0);
        await expect(
            page.getByText("long response that will be stopped", {
                exact: true,
            }),
        ).toHaveCount(1);

        const assistantBubbles = await page
            .locator(".msg-assistant-body")
            .count();
        await emitDone(page, `done-stop-${combo.id}`, "stopped_by_user");
        await expect(page.locator(".msg-assistant-body")).toHaveCount(
            assistantBubbles,
        );
        await expect
            .poll(async () => (await controlStops(page)).length, {
                timeout: 1_000,
            })
            .toBe(1);
        await expect.poll(async () => (await sentInputs(page)).length).toBe(2);

        // ── Same session: the next input dispatches and completes normally;
        //    the input list grows by exactly one ─────────────────────────────
        const userBubblesBefore = await page
            .locator(".msg-user-bubble")
            .count();
        await input.fill(`matrix-next-${combo.id}`);
        await input.press("Enter");
        await expect.poll(async () => (await sentInputs(page)).length).toBe(3);
        expect((await sentInputs(page))[2]?.event.data.content).toBe(
            `matrix-next-${combo.id}`,
        );
        await expect(
            page.getByText(`matrix-next-${combo.id}`, { exact: true }),
        ).toHaveCount(1);

        await emitGatewayEvent(page, "message.delta", {
            message_id: `next-msg-${combo.id}`,
            content: "next turn completed answer",
        });
        // Same ordering contract as the first turn: the terminal done must not
        // overtake the delta's state update, or it removes the pending message.
        await expect(
            page.getByText("next turn completed answer", { exact: true }),
        ).toBeVisible();
        await emitDone(page, `done-next-${combo.id}`, "completed");

        await expect(
            page.getByText("next turn completed answer", { exact: true }),
        ).toHaveCount(1);
        await expect(page.locator(".msg-user-bubble")).toHaveCount(
            userBubblesBefore + 1,
        );
        await expect(
            page.getByRole("button", { name: "发送消息" }),
        ).toBeVisible();
    });
}
