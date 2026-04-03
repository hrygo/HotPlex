package dev.hotplex.protocol;

/**
 * Error codes for AEP v1 protocol.
 */
public enum ErrorCode {
    WorkerStartFailed("WORKER_START_FAILED"),
    WorkerCrash("WORKER_CRASH"),
    WorkerTimeout("WORKER_TIMEOUT"),
    WorkerOOM("WORKER_OOM"),
    WorkerSIGKILL("PROCESS_SIGKILL"),
    InvalidMessage("INVALID_MESSAGE"),
    SessionNotFound("SESSION_NOT_FOUND"),
    SessionExpired("SESSION_EXPIRED"),
    SessionTerminated("SESSION_TERMINATED"),
    SessionInvalidated("SESSION_INVALIDATED"),
    SessionBusy("SESSION_BUSY"),
    Unauthorized("UNAUTHORIZED"),
    AuthRequired("AUTH_REQUIRED"),
    InternalError("INTERNAL_ERROR"),
    ProtocolViolation("PROTOCOL_VIOLATION"),
    VersionMismatch("VERSION_MISMATCH"),
    ConfigInvalid("CONFIG_INVALID"),
    RateLimited("RATE_LIMITED"),
    GatewayOverload("GATEWAY_OVERLOAD"),
    ExecutionTimeout("EXECUTION_TIMEOUT"),
    ReconnectRequired("RECONNECT_REQUIRED"),
    WorkerOutputLimit("WORKER_OUTPUT_LIMIT");

    private final String value;

    ErrorCode(String value) {
        this.value = value;
    }

    public String getValue() {
        return value;
    }

    public static ErrorCode fromValue(String value) {
        for (ErrorCode code : values()) {
            if (code.value.equals(value)) {
                return code;
            }
        }
        throw new IllegalArgumentException("Unknown error code: " + value);
    }
}