/**
 * Maps gateway error events to user-friendly chat messages.
 *
 * Extracted from the runtime adapter so error classification is testable in
 * isolation. Raw worker messages (e.g. Codex CLI errors) are transparently
 * forwarded by the gateway, so recognizable failure classes get mapped here
 * instead of being dumped into the chat verbatim.
 */

const RATE_LIMIT_MESSAGE =
    "Rate limit exceeded (429): The upstream AI provider is rate-limiting requests or quota is exhausted. Please try again later or check your API key/quota limits.";

// Patterns the Codex CLI emits when its ChatGPT OAuth session is no longer
// usable (access token expired and refresh rejected). Surfaced as an
// actionable re-login hint instead of the raw English error.
const CODEX_AUTH_PATTERNS = [
    "token could not be refreshed",
    "log out and sign in",
    "sign in again",
    "authentication failed",
    "unauthorized",
    "401",
];

const CAPABILITY_REJECTION_PATTERN =
    /(not implemented|not supported|unsupported|not enabled)/i;
const USER_COMMAND_VALIDATION_PATTERN =
    /(invalid permission mode|permission mode required|model name required|ambiguous skill invocation)/i;

/** Returns true when an error is an expected, user-fixable command rejection. */
export function isExpectedCommandRejection(
    code: string | undefined,
    message: string | undefined,
): boolean {
    const msg = message || "";
    if (code === "CONFIG_INVALID" || code === "NOT_SUPPORTED") return true;
    // Legacy gateways may have emitted capability failures as INTERNAL_ERROR;
    // keep protocol/version errors with similar wording as real errors.
    if (code === "INTERNAL_ERROR" && CAPABILITY_REJECTION_PATTERN.test(msg)) {
        return true;
    }
    return code === "INVALID_MESSAGE" && USER_COMMAND_VALIDATION_PATTERN.test(msg);
}

export function mapErrorToMessage(
    code: string | undefined,
    message: string | undefined,
): string {
    const msgLower = (message || "").toLowerCase();
    const isFileRewindDisabled = msgLower.includes(
        "file rewinding is not enabled",
    );

    if (isFileRewindDisabled) {
        return "File rewind is not enabled for this worker. Use /reset or /new instead.";
    }

    switch (code) {
        case "TURN_TIMEOUT":
            return "Session timeout: The agent took too long to respond (limit: 15m). You may want to break your request into smaller steps.";
        case "WORKER_CRASH":
            return "The coding agent crashed unexpectedly. Please try again or reset the session.";
        case "SESSION_EXPIRED":
            return "This session has expired due to inactivity. Please start a new session.";
        case "RATE_LIMITED":
            return "You've reached the rate limit. Please wait a moment before sending more messages.";
        case "UNAUTHORIZED":
            return "Authentication failed: 401 — Check your API key configuration or consult the documentation.";
        case "WORKER_OUTPUT_LIMIT":
            return "The agent produced too much output and was terminated. Try to narrow down your request.";
        case "RESUME_RETRY":
            return `🔄 ${message || "Recovering session after unexpected crash..."}`;
        case "INVALID_MESSAGE":
            if (
                msgLower.includes("invalid permission mode") ||
                msgLower.includes("permission mode required")
            ) {
                return "Permission mode is required. Use /perm <mode> to choose one.";
            }
            if (msgLower.includes("model name required")) {
                return "Model name is required. Use /model <model> instead.";
            }
            if (msgLower.includes("ambiguous skill invocation")) {
                return "That Skill command is ambiguous. Choose a specific command and try again.";
            }
            return message || "The command could not be processed. Check its format and try again.";
        case "NOT_SUPPORTED":
            if (
                msgLower.includes("clear:") &&
                msgLower.includes("not implemented")
            ) {
                return "This worker does not support /clear. Use /reset or /new instead.";
            }
            return "This command is not supported by the current worker.";
        case "CODEX_ERROR": {
            if (CODEX_AUTH_PATTERNS.some((p) => msgLower.includes(p))) {
                return "Your Codex login has expired. Please sign out and sign in again in the Codex CLI, then retry.";
            }
            return `Codex error: ${message || "unknown"}`;
        }
        default: {
            if (
                msgLower.includes("429") ||
                msgLower.includes("too many requests") ||
                msgLower.includes("rate limit")
            ) {
                return RATE_LIMIT_MESSAGE;
            }
            return (
                message ||
                (code ? `Error: ${code}` : "An unexpected error occurred.")
            );
        }
    }
}
