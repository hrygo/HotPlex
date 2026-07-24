package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;

/**
 * ToolUpdateData carries intermediate tool call status (ACP tool_call_update).
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class ToolUpdateData {
    private String id;
    private String status;
    private Object content;
    private ToolResultData.FileDiff diff;
    @JsonProperty("raw_output")
    private String rawOutput;

    public ToolUpdateData() {}

    public ToolUpdateData(String id, String status, Object content, ToolResultData.FileDiff diff, String rawOutput) {
        this.id = id;
        this.status = status;
        this.content = content;
        this.diff = diff;
        this.rawOutput = rawOutput;
    }

    public String getId() {
        return id;
    }

    public void setId(String id) {
        this.id = id;
    }

    public String getStatus() {
        return status;
    }

    public void setStatus(String status) {
        this.status = status;
    }

    public Object getContent() {
        return content;
    }

    public void setContent(Object content) {
        this.content = content;
    }

    public ToolResultData.FileDiff getDiff() {
        return diff;
    }

    public void setDiff(ToolResultData.FileDiff diff) {
        this.diff = diff;
    }

    public String getRawOutput() {
        return rawOutput;
    }

    public void setRawOutput(String rawOutput) {
        this.rawOutput = rawOutput;
    }
}
