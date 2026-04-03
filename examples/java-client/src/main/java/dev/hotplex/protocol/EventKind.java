package dev.hotplex.protocol;

/**
 * Event kinds for AEP v1 protocol.
 */
public enum EventKind {
    Error("error"),
    State("state"),
    Input("input"),
    Done("done"),
    Message("message"),
    MessageStart("message.start"),
    MessageDelta("message.delta"),
    MessageEnd("message.end"),
    ToolCall("tool_call"),
    ToolResult("tool_result"),
    Reasoning("reasoning"),
    Step("step"),
    Raw("raw"),
    PermissionRequest("permission_request"),
    PermissionResponse("permission_response"),
    Ping("ping"),
    Pong("pong"),
    Control("control"),
    InitAck("init_ack"),
    Init("init");

    private final String value;

    EventKind(String value) {
        this.value = value;
    }

    public String getValue() {
        return value;
    }

    public static EventKind fromValue(String value) {
        for (EventKind kind : values()) {
            if (kind.value.equals(value)) {
                return kind;
            }
        }
        throw new IllegalArgumentException("Unknown event kind: " + value);
    }
}