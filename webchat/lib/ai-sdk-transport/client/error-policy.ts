import { ErrorCode } from "./constants";

// These codes describe expected lifecycle, authentication, validation, or
// capacity outcomes that the UI can explain and recover from. They remain
// observable as warnings without triggering Next.js's console-error overlay.
const EXPECTED_CLIENT_ERROR_CODES = new Set<string>([
    ErrorCode.InvalidMessage,
    ErrorCode.SessionNotFound,
    ErrorCode.SessionExpired,
    ErrorCode.SessionTerminated,
    ErrorCode.SessionInvalidated,
    ErrorCode.SessionBusy,
    ErrorCode.SessionAlreadyConnected,
    ErrorCode.Unauthorized,
    ErrorCode.AuthRequired,
    ErrorCode.VersionMismatch,
    ErrorCode.ConfigInvalid,
    ErrorCode.RateLimited,
    ErrorCode.GatewayOverload,
    ErrorCode.ExecutionTimeout,
    ErrorCode.ReconnectRequired,
    ErrorCode.ResumeRetry,
    ErrorCode.NotSupported,
    ErrorCode.OperatorAbandoned,
]);

export function isExpectedClientError(code: string | undefined): boolean {
    return code !== undefined && EXPECTED_CLIENT_ERROR_CODES.has(code);
}
