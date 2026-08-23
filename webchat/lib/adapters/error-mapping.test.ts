import { describe, expect, it } from "vitest";

import {
    isExpectedCommandRejection,
    mapErrorToMessage,
} from "./error-mapping";

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
            "SESSION_EXPIRED",
            undefined,
            "This session has expired due to inactivity. Please start a new session.",
        ],
        [
            "RATE_LIMITED",
            undefined,
            "You've reached the rate limit. Please wait a moment before sending more messages.",
        ],
        [
            "UNAUTHORIZED",
            undefined,
            "Authentication failed: 401 — Check your API key configuration or consult the documentation.",
        ],
        [
            "WORKER_OUTPUT_LIMIT",
            undefined,
            "The agent produced too much output and was terminated. Try to narrow down your request.",
        ],
        [
            "RESUME_RETRY",
            "Recovering session...",
            "🔄 Recovering session...",
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
    ])(
        "maps known code %s to its friendly message",
        (code, message, expected) => {
            expect(mapErrorToMessage(code, message)).toBe(expected);
        },
    );

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
