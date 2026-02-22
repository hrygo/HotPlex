"""HotPlex SDK Error Types"""


class HotPlexError(Exception):
    """Base exception for all HotPlex errors."""

    pass


class ConnectionError(HotPlexError):
    """Raised when connection to HotPlex server fails."""

    pass


class TimeoutError(HotPlexError):
    """Raised when an operation times out."""

    pass


class ExecutionError(HotPlexError):
    """Raised when execution fails."""

    def __init__(self, message: str, error_type: str = None):
        super().__init__(message)
        self.error_type = error_type


class DangerBlockedError(ExecutionError):
    """Raised when a dangerous operation is blocked by the WAF."""

    def __init__(self, message: str, operation: str = None, reason: str = None):
        super().__init__(message, error_type="danger_blocked")
        self.operation = operation
        self.reason = reason


class SessionError(HotPlexError):
    """Raised when session-related errors occur."""

    def __init__(self, message: str, session_id: str = None):
        super().__init__(message)
        self.session_id = session_id


class SessionNotFoundError(SessionError):
    """Raised when a session is not found."""

    pass


class SessionDeadError(SessionError):
    """Raised when a session is dead."""

    pass
