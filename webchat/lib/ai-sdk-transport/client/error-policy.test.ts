import { describe, expect, it } from "vitest";

import { ErrorCode } from "./constants";
import { isExpectedClientError } from "./error-policy";

describe("isExpectedClientError", () => {
    it.each([
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
    ])("classifies %s as an expected client-visible outcome", (code) => {
        expect(isExpectedClientError(code)).toBe(true);
    });

    it.each([
        ErrorCode.WorkerStartFailed,
        ErrorCode.WorkerCrash,
        ErrorCode.WorkerTimeout,
        ErrorCode.WorkerOOM,
        ErrorCode.WorkerSIGKILL,
        ErrorCode.InternalError,
        ErrorCode.ProtocolViolation,
        ErrorCode.WorkerOutputLimit,
        ErrorCode.TurnTimeout,
    ])("keeps %s classified as a genuine runtime fault", (code) => {
        expect(isExpectedClientError(code)).toBe(false);
    });

    it("does not downgrade unknown error codes", () => {
        expect(isExpectedClientError("UNKNOWN_ERROR")).toBe(false);
        expect(isExpectedClientError(undefined)).toBe(false);
    });
});
