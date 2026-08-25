/**
 * Maps gateway error events to user-friendly chat messages.
 *
 * Extracted from the runtime adapter so error classification is testable in
 * isolation. Raw worker messages (e.g. Codex CLI errors) are transparently
 * forwarded by the gateway, so recognizable failure classes get mapped here
 * instead of being dumped into the chat verbatim.
 */

import { ErrorCode } from "@/lib/ai-sdk-transport/client/constants";

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
    if (code === ErrorCode.ConfigInvalid || code === ErrorCode.NotSupported) {
        return true;
    }
    // Legacy gateways may have emitted capability failures as INTERNAL_ERROR;
    // keep protocol/version errors with similar wording as real errors.
    if (
        code === ErrorCode.InternalError &&
        CAPABILITY_REJECTION_PATTERN.test(msg)
    ) {
        return true;
    }
    return (
        code === ErrorCode.InvalidMessage &&
        USER_COMMAND_VALIDATION_PATTERN.test(msg)
    );
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
        case ErrorCode.WorkerStartFailed:
            return "The coding agent could not start. Try again or choose a different worker.";
        case ErrorCode.WorkerCrash:
            return "The coding agent crashed unexpectedly. Please try again or reset the session.";
        case ErrorCode.WorkerTimeout:
            return "The coding agent stopped responding. Try again or start a new session.";
        case ErrorCode.WorkerOOM:
            return "The coding agent ran out of memory. Try a smaller request or start a new session.";
        case ErrorCode.WorkerSIGKILL:
            return "The coding agent was force-stopped. Try again or start a new session.";
        case ErrorCode.TurnTimeout:
            return "Session timeout: The agent took too long to respond (limit: 15m). You may want to break your request into smaller steps.";
        case ErrorCode.SessionNotFound:
            return "This session is no longer available. Start a new session and try again.";
        case ErrorCode.SessionExpired:
            return "This session has expired due to inactivity. Please start a new session.";
        case ErrorCode.SessionTerminated:
            return "This session has ended. Start a new session to continue.";
        case ErrorCode.SessionInvalidated:
            return "This session is no longer valid. Start a new session to continue.";
        case ErrorCode.SessionBusy:
            return "This session is still processing another request. Wait for it to finish, then try again.";
        case ErrorCode.SessionAlreadyConnected:
            return "This session is open in another connection. Close the other connection or wait, then try again.";
        case ErrorCode.Unauthorized:
            return "You are signed out or do not have access to this session. Sign in again or choose another session.";
        case ErrorCode.AuthRequired:
            return "Sign in to continue.";
        case ErrorCode.InternalError:
            return "HotPlex encountered an internal error. Try again; if it continues, contact your administrator.";
        case ErrorCode.ProtocolViolation:
            return "The client sent or received an invalid protocol message. Refresh the page and try again.";
        case ErrorCode.VersionMismatch:
            return "This WebChat version is incompatible with the Gateway. Refresh the page or update HotPlex.";
        case ErrorCode.ConfigInvalid:
            return "The requested configuration is invalid. Check the command or workspace settings and try again.";
        case ErrorCode.RateLimited:
            return RATE_LIMIT_MESSAGE;
        case ErrorCode.GatewayOverload:
            return "HotPlex is temporarily overloaded. Wait a moment and try again.";
        case ErrorCode.ExecutionTimeout:
            return "Delivery timed out and the result is unknown. Check the session before sending the request again.";
        case ErrorCode.ReconnectRequired:
            return "The Gateway requires a new connection. Wait for WebChat to reconnect or refresh the page.";
        case ErrorCode.WorkerOutputLimit:
            return "The agent produced too much output and was terminated. Try to narrow down your request.";
        case ErrorCode.ResumeRetry:
            return "🔄 Recovering the session after an unexpected interruption...";
        case ErrorCode.InvalidMessage:
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
            return "The command could not be processed. Check its format and try again.";
        case ErrorCode.NotSupported:
            if (
                msgLower.includes("clear:") &&
                msgLower.includes("not implemented")
            ) {
                return "This worker does not support /clear. Use /reset or /new instead.";
            }
            return "This command is not supported by the current worker.";
        case ErrorCode.OperatorAbandoned:
            return "This pending execution was abandoned by an administrator. Check the session before trying again.";
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

export interface ErrorNotice {
    text: string;
    status: "complete" | "error";
}

export function mapErrorToNotice(
    code: string | undefined,
    message: string | undefined,
): ErrorNotice {
    const text = mapErrorToMessage(code, message);
    if (code === ErrorCode.ResumeRetry) {
        return { text, status: "complete" };
    }
    return { text: `⚠️ ${text}`, status: "error" };
}
