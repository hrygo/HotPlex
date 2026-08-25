import { describe, expect, it } from "vitest";

import {
    mapErrorToNotice,
    isExpectedCommandRejection,
    mapErrorToMessage,
} from "./error-mapping";
import { ErrorCode } from "@/lib/ai-sdk-transport/client/constants";

describe("mapErrorToMessage", () => {
    it.each([
        // Known gateway error codes keep their friendly messages.
        [
            "TURN_TIMEOUT",
            undefined,
            "Session timeout: The agent took too long to respond (limit: 15m). You may want to break your request into smaller steps.",
        ],
        [
            "WORKER_CRASH",
            undefined,
            "The coding agent crashed unexpectedly. Please try again or reset the session.",
        ],
        [
            "WORKER_START_FAILED",
            undefined,
            "The coding agent could not start. Try again or choose a different worker.",
        ],
        [
            "WORKER_TIMEOUT",
            undefined,
            "The coding agent stopped responding. Try again or start a new session.",
        ],
        [
            "WORKER_OOM",
            undefined,
            "The coding agent ran out of memory. Try a smaller request or start a new session.",
        ],
        [
            "PROCESS_SIGKILL",
            undefined,
            "The coding agent was force-stopped. Try again or start a new session.",
        ],
        [
            "SESSION_NOT_FOUND",
            "no worker attached to session",
            "This session is no longer available. Start a new session and try again.",
        ],
        [
            "SESSION_EXPIRED",
            undefined,
            "This session has expired due to inactivity. Please start a new session.",
        ],
        [
            "SESSION_TERMINATED",
            undefined,
            "This session has ended. Start a new session to continue.",
        ],
        [
            "SESSION_INVALIDATED",
            undefined,
            "This session is no longer valid. Start a new session to continue.",
        ],
        [
            "SESSION_BUSY",
            undefined,
            "This session is still processing another request. Wait for it to finish, then try again.",
        ],
        [
            "SESSION_ALREADY_CONNECTED",
            undefined,
            "This session is open in another connection. Close the other connection or wait, then try again.",
        ],
        [
            "RATE_LIMITED",
            undefined,
            "Rate limit exceeded (429): The upstream AI provider is rate-limiting requests or quota is exhausted. Please try again later or check your API key/quota limits.",
        ],
        [
            "UNAUTHORIZED",
            undefined,
            "You are signed out or do not have access to this session. Sign in again or choose another session.",
        ],
        [
            "AUTH_REQUIRED",
            undefined,
            "Sign in to continue.",
        ],
        [
            "INTERNAL_ERROR",
            "database connection details",
            "HotPlex encountered an internal error. Try again; if it continues, contact your administrator.",
        ],
        [
            "PROTOCOL_VIOLATION",
            "unknown control action foo",
            "The client sent or received an invalid protocol message. Refresh the page and try again.",
        ],
        [
            "VERSION_MISMATCH",
            "version mismatch: expected 1, got 0",
            "This WebChat version is incompatible with the Gateway. Refresh the page or update HotPlex.",
        ],
        [
            "CONFIG_INVALID",
            "invalid path: /private/internal/path",
            "The requested configuration is invalid. Check the command or workspace settings and try again.",
        ],
        [
            "GATEWAY_OVERLOAD",
            undefined,
            "HotPlex is temporarily overloaded. Wait a moment and try again.",
        ],
        [
            "EXECUTION_TIMEOUT",
            undefined,
            "Delivery timed out and the result is unknown. Check the session before sending the request again.",
        ],
        [
            "RECONNECT_REQUIRED",
            undefined,
            "The Gateway requires a new connection. Wait for WebChat to reconnect or refresh the page.",
        ],
        [
            "WORKER_OUTPUT_LIMIT",
            undefined,
            "The agent produced too much output and was terminated. Try to narrow down your request.",
        ],
        [
            "RESUME_RETRY",
            "Recovering session...",
            "🔄 Recovering the session after an unexpected interruption...",
        ],
        [
            "NOT_SUPPORTED",
            "clear: worker: not implemented",
            "This worker does not support /clear. Use /reset or /new instead.",
        ],
        [
            "NOT_SUPPORTED",
            "rewind: claudecode: rewind: control: request failed: File rewinding is not enabled.",
            "File rewind is not enabled for this worker. Use /reset or /new instead.",
        ],
        [
            "INVALID_MESSAGE",
            "invalid permission mode: permission mode required",
            "Permission mode is required. Use /perm <mode> to choose one.",
        ],
        [
            "OPERATOR_ABANDONED",
            undefined,
            "This pending execution was abandoned by an administrator. Check the session before trying again.",
        ],
    ])(
        "maps known code %s to its friendly message",
        (code, message, expected) => {
            expect(mapErrorToMessage(code, message)).toBe(expected);
        },
    );

    it("renders session recovery as a neutral notice instead of an error", () => {
        expect(mapErrorToNotice("RESUME_RETRY", "Recovering session...")).toEqual({
            text: "🔄 Recovering the session after an unexpected interruption...",
            status: "complete",
        });
    });

    it("renders terminal failures as warning-prefixed error notices", () => {
        expect(mapErrorToNotice("SESSION_NOT_FOUND", "gone")).toEqual({
            text: "⚠️ This session is no longer available. Start a new session and try again.",
            status: "error",
        });
    });

    it("does not expose raw Gateway details for canonical error codes", () => {
        for (const code of Object.values(ErrorCode)) {
            const message = mapErrorToMessage(code, "RAW_GATEWAY_DETAIL");
            expect(message).not.toContain("RAW_GATEWAY_DETAIL");
            expect(message).not.toBe(`Error: ${code}`);
        }
    });

    describe("CODEX_ERROR", () => {
        it.each([
            // Codex CLI auth-failure patterns → actionable re-login hint.
            [
                "Your access token could not be refreshed. Please log out and sign in again.",
                "Your Codex login has expired. Please sign out and sign in again in the Codex CLI, then retry.",
            ],
            [
                "Authentication failed. Please log in again.",
                "Your Codex login has expired. Please sign out and sign in again in the Codex CLI, then retry.",
            ],
            [
                "Request failed with status 401 Unauthorized",
                "Your Codex login has expired. Please sign out and sign in again in the Codex CLI, then retry.",
            ],
            [
                "UNAUTHORIZED: invalid credentials",
                "Your Codex login has expired. Please sign out and sign in again in the Codex CLI, then retry.",
            ],
        ])(
            "maps auth-failure message %s to a re-login hint",
            (message, expected) => {
                expect(mapErrorToMessage("CODEX_ERROR", message)).toBe(
                    expected,
                );
            },
        );

        it("prefixes non-auth Codex errors with a source label", () => {
            expect(mapErrorToMessage("CODEX_ERROR", "something else broke")).toBe(
                "Codex error: something else broke",
            );
        });
    });

    describe("NOT_SUPPORTED", () => {
        it("maps an unsupported command to a generic actionable message", () => {
            expect(mapErrorToMessage("NOT_SUPPORTED", "compact: unavailable")).toBe(
                "This command is not supported by the current worker.",
            );
        });

        it("maps a legacy internal error for a disabled capability", () => {
            expect(
                mapErrorToMessage(
                    "INTERNAL_ERROR",
                    "rewind: claudecode: rewind: control: request failed: File rewinding is not enabled.",
                ),
            ).toBe(
                "File rewind is not enabled for this worker. Use /reset or /new instead.",
            );
        });
    });

    describe("INVALID_MESSAGE", () => {
        it("maps missing model arguments to an actionable command hint", () => {
            expect(mapErrorToMessage("INVALID_MESSAGE", "model name required")).toBe(
                "Model name is required. Use /model <model> instead.",
            );
        });

        it("maps ambiguous Skill input without exposing parser details", () => {
            expect(
                mapErrorToMessage("INVALID_MESSAGE", "ambiguous Skill invocation"),
            ).toBe("That Skill command is ambiguous. Choose a specific command and try again.");
        });

        it("does not expose malformed protocol details", () => {
            expect(
                mapErrorToMessage(
                    "INVALID_MESSAGE",
                    "malformed input data: parse failed at private field",
                ),
            ).toBe("The command could not be processed. Check its format and try again.");
        });
    });

    describe("isExpectedCommandRejection", () => {
        it.each([
            ["CONFIG_INVALID", "command rejected"],
            ["NOT_SUPPORTED", "command unavailable"],
            ["INVALID_MESSAGE", "invalid permission mode: permission mode required"],
            ["INVALID_MESSAGE", "model name required"],
            ["INVALID_MESSAGE", "ambiguous Skill invocation"],
            ["INTERNAL_ERROR", "rewind: file rewinding is not enabled"],
        ])("recognizes user-fixable command error %s", (code, message) => {
            expect(isExpectedCommandRejection(code, message)).toBe(true);
        });

        it("keeps malformed protocol errors as errors", () => {
            expect(isExpectedCommandRejection("INVALID_MESSAGE", "malformed input data")).toBe(false);
        });

        it("keeps unsupported protocol versions as errors", () => {
            expect(isExpectedCommandRejection("VERSION_MISMATCH", "unsupported version")).toBe(false);
        });
    });

    describe("default", () => {
        it.each([
            // 429-style messages are detected by text regardless of code.
            ["429 Too Many Requests", "Rate limit exceeded (429): The upstream AI provider is rate-limiting requests or quota is exhausted. Please try again later or check your API key/quota limits."],
            ["the API is rate limiting us", "Rate limit exceeded (429): The upstream AI provider is rate-limiting requests or quota is exhausted. Please try again later or check your API key/quota limits."],
        ])(
            "maps rate-limit text %s to the friendly 429 message",
            (message, expected) => {
                expect(mapErrorToMessage("SOMETHING_ELSE", message)).toBe(
                    expected,
                );
            },
        );

        it("passes through unknown messages verbatim", () => {
            expect(mapErrorToMessage(undefined, "boom")).toBe("boom");
        });

        it("falls back to the error code when no message", () => {
            expect(mapErrorToMessage("FOO", undefined)).toBe("Error: FOO");
        });

        it("falls back to a generic message when nothing is present", () => {
            expect(mapErrorToMessage(undefined, undefined)).toBe(
                "An unexpected error occurred.",
            );
        });
    });
});
