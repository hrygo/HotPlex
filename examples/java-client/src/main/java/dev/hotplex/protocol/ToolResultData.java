package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;

/**
 * ToolResultData for tool_result events.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class ToolResultData {
    private String id;
    private Object output;
    private String error;

    public ToolResultData() {}

    public ToolResultData(String id, Object output, String error) {
        this.id = id;
        this.output = output;
        this.error = error;
    }

    public String getId() {
        return id;
    }

    public void setId(String id) {
        this.id = id;
    }

    public Object getOutput() {
        return output;
    }

    public void setOutput(Object output) {
        this.output = output;
    }

    public String getError() {
        return error;
    }

    public void setError(String error) {
        this.error = error;
    }
}