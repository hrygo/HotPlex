package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;

/**
 * StateData for state events.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class StateData {
    private SessionState state;
    private String message;

    public StateData() {}

    public StateData(SessionState state, String message) {
        this.state = state;
        this.message = message;
    }

    public SessionState getState() {
        return state;
    }

    public void setState(SessionState state) {
        this.state = state;
    }

    public String getMessage() {
        return message;
    }

    public void setMessage(String message) {
        this.message = message;
    }
}