import { test, expect, type Page } from "@playwright/test";
import {
    emitGatewayEvent,
    installMockGateway,
} from "./fixtures/mock-gateway";

const COMPOSER_PLACEHOLDER = "输入消息，或输入 '/' 使用命令...";
const WORKER_TYPES = [
    "claude_code",
    "codex_cli",
    "opencode_server",
    "acp",
] as const;

async function waitForChatReady(page: Page) {
    await page.getByPlaceholder(COMPOSER_PLACEHOLDER).waitFor({
        state: "visible",
        timeout: 20_000,
    });
}

async function openChat(page: Page, workerType: string) {
    await installMockGateway(page, workerType);
    await page.goto("/");
    await waitForChatReady(page);
    await expect
        .poll(async () => {
            const events = await page.evaluate(
                () =>
                    (window as Window & { __aepEvents?: Array<{ event: { type: string } }> })
                        .__aepEvents ?? [],
            );
            return events.some((event) => event.event.type === "init");
        })
        .toBe(true);
}

async function emitPermissionRequest(page: Page, id: string) {
    await emitGatewayEvent(page, "permission_request", {
        id,
        tool_name: "Read",
        description: "Read a controlled test fixture",
        args: [JSON.stringify({ tool: "Read", path: "/tmp/hotplex-e2e/sample.txt" })],
    });
}

async function emitQuestionRequest(page: Page, id: string) {
    await emitGatewayEvent(page, "question_request", {
        id,
        questions: [
            {
                id: "choice",
                header: "Choice",
                question: "Choose a test value",
                options: [{ label: "One" }, { label: "Two" }],
            },
        ],
    });
}

async function emitElicitationRequest(page: Page, id: string) {
    await emitGatewayEvent(page, "elicitation_request", {
        id,
        mcp_server_name: "",
        message: "Provide a test value",
    });
}

for (const workerType of WORKER_TYPES) {
test(`[${workerType} Worker] done expires unfinished interactions, preserves success, and allows the next round`, async ({
    page,
}) => {
    await openChat(page, workerType);

    const resolvedPermissionID = "permission-resolved";
    await emitPermissionRequest(page, resolvedPermissionID);
    await expect(
        page.getByText("工具执行授权", { exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name: "允许" }).click();
    await expect(
        page.getByText("正在提交授权...", { exact: true }),
    ).toBeVisible();
    await emitGatewayEvent(page, "permission_response", {
        id: resolvedPermissionID,
        allowed: true,
    });
    await expect(
        page.getByText("已允许执行", { exact: true }),
    ).toBeVisible();

    const permissionID = "permission-pending";
    const questionID = "question-pending";
    const elicitationID = "elicitation-pending";
    await emitPermissionRequest(page, permissionID);
    await emitQuestionRequest(page, questionID);
    await emitElicitationRequest(page, elicitationID);
    await expect(
        page.getByText("输入请求", { exact: true }),
    ).toBeVisible();
    await expect(
        page.getByText("交互输入请求", { exact: true }),
    ).toBeVisible();

    // Keep one interaction in submitting while the others remain pending.
    await page.getByRole("button", { name: "允许" }).click();
    await expect(
        page.getByText("正在提交授权...", { exact: true }),
    ).toHaveCount(1);

    await emitGatewayEvent(page, "done", {
        success: true,
        reason: "completed",
    }, "done-terminal-interactions");

    // Completed tool calls collapse to compact tabs; expand the interaction
    // cards so their terminal statuses remain observable.
    await page.getByText("ASK_PERMISSION", { exact: true }).nth(0).click();
    await page.getByText("ASK_PERMISSION", { exact: true }).nth(0).click();
    await page.getByText("QUESTION_REQUEST", { exact: true }).click();

    await expect(
        page.getByText("授权已过期", { exact: true }),
    ).toHaveCount(1);
    await expect(
        page.getByText("问题已过期", { exact: true }),
    ).toHaveCount(1);
    await expect(
        page.getByText("请求已过期", { exact: true }),
    ).toHaveCount(1);
    await expect(
        page.getByText("已允许执行", { exact: true }),
    ).toHaveCount(1);

    // Late worker acknowledgements must not turn terminal cards back into
    // resolved/rejected cards after the interaction map has been cleared.
    await emitGatewayEvent(page, "permission_response", {
        id: permissionID,
        allowed: true,
    });
    await emitGatewayEvent(page, "question_response", {
        id: questionID,
        answers: { choice: "One" },
    });
    await emitGatewayEvent(page, "elicitation_response", {
        id: elicitationID,
        action: "accept",
    });
    await expect(
        page.getByText("授权已过期", { exact: true }),
    ).toHaveCount(1);
    await expect(
        page.getByText("问题已过期", { exact: true }),
    ).toHaveCount(1);
    await expect(
        page.getByText("请求已过期", { exact: true }),
    ).toHaveCount(1);
    await expect(
        page.getByText("已允许执行", { exact: true }),
    ).toHaveCount(1);

    const nextPermissionID = "permission-next-round";
    await emitPermissionRequest(page, nextPermissionID);
    await expect(
        page.getByRole("button", { name: "允许" }),
    ).toBeVisible();
});

test(`[${workerType} Worker] SESSION_TERMINATED expires pending interactions and ignores late responses`, async ({
    page,
}) => {
    await openChat(page, workerType);

    const questionID = "question-session-terminated";
    await emitQuestionRequest(page, questionID);
    await expect(page.getByText("输入请求", { exact: true })).toBeVisible();

    await emitGatewayEvent(page, "error", {
        code: "SESSION_TERMINATED",
        message: "session terminated by test",
    });

    await expect(
        page.getByText("问题已过期", { exact: true }),
    ).toHaveCount(1);
    await emitGatewayEvent(page, "question_response", {
        id: questionID,
        answers: { choice: "One" },
    });
    await expect(
        page.getByText("问题已过期", { exact: true }),
    ).toHaveCount(1);
    await expect(page.getByText("已回答", { exact: true })).toHaveCount(0);
});

for (const terminalState of ["terminated", "deleted"] as const) {
    test(`[${workerType} Worker] state=${terminalState} expires elicitation interactions`, async ({
        page,
    }) => {
        await openChat(page, workerType);

        const elicitationID = `elicitation-${terminalState}`;
        await emitElicitationRequest(page, elicitationID);
        await expect(
            page.getByText("交互输入请求", { exact: true }),
        ).toBeVisible();

        await emitGatewayEvent(page, "state", { state: terminalState });
        await expect(
            page.getByText("请求已过期", { exact: true }),
        ).toHaveCount(1);

        await emitGatewayEvent(page, "elicitation_response", {
            id: elicitationID,
            action: "accept",
        });
        await expect(
            page.getByText("请求已过期", { exact: true }),
        ).toHaveCount(1);
        await expect(
            page.getByText("已接受请求", { exact: true }),
        ).toHaveCount(0);
    });
}
}
