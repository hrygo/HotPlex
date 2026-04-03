package dev.hotplex.protocol;

/**
 * Session state values for AEP v1 protocol.
 */
public enum SessionState {
    Created("created"),
    Running("running"),
    Idle("idle"),
    Terminated("terminated"),
    Deleted("deleted");

    private final String value;

    SessionState(String value) {
        this.value = value;
    }

    public String getValue() {
        return value;
    }

    public boolean isTerminal() {
        return this == Deleted;
    }

    public boolean isActive() {
        return this == Created || this == Running || this == Idle;
    }

    public static SessionState fromValue(String value) {
        for (SessionState state : values()) {
            if (state.value.equals(value)) {
                return state;
            }
        }
        throw new IllegalArgumentException("Unknown session state: " + value);
    }
}