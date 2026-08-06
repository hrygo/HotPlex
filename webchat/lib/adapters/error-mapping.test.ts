import { describe, expect, it } from "vitest";

import { mapErrorToMessage } from "./error-mapping";

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
