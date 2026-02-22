export class HotPlexError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'HotPlexError';
  }
}

export class ConnectionError extends HotPlexError {
  constructor(message: string) {
    super(message);
    this.name = 'ConnectionError';
  }
}

export class TimeoutError extends HotPlexError {
  constructor(message: string) {
    super(message);
    this.name = 'TimeoutError';
  }
}

export class ExecutionError extends HotPlexError {
  public readonly errorType?: string;

  constructor(message: string, errorType?: string) {
    super(message);
    this.name = 'ExecutionError';
    this.errorType = errorType;
  }
}

export class DangerBlockedError extends ExecutionError {
  public readonly operation?: string;
  public readonly reason?: string;

  constructor(message: string, operation?: string, reason?: string) {
    super(message, 'danger_blocked');
    this.name = 'DangerBlockedError';
    this.operation = operation;
    this.reason = reason;
  }
}

export class SessionError extends HotPlexError {
  public readonly sessionId?: string;

  constructor(message: string, sessionId?: string) {
    super(message);
    this.name = 'SessionError';
    this.sessionId = sessionId;
  }
}

export class SessionNotFoundError extends SessionError {
  constructor(sessionId?: string) {
    super(`Session not found: ${sessionId}`, sessionId);
    this.name = 'SessionNotFoundError';
  }
}

export class SessionDeadError extends SessionError {
  constructor(sessionId?: string) {
    super(`Session is dead: ${sessionId}`, sessionId);
    this.name = 'SessionDeadError';
  }
}
