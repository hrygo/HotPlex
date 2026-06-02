package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;

/**
 * ToolResultData for tool_result events.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class ToolResultData {
    private String id;
    private Object output;
    private String error;
    private String status;
    private FileDiff diff;

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

    public String getStatus() {
        return status;
    }

    public void setStatus(String status) {
        this.status = status;
    }

    public FileDiff getDiff() {
        return diff;
    }

    public void setDiff(FileDiff diff) {
        this.diff = diff;
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class FileDiff {
        private String path;
        @JsonProperty("old_text")
        private String oldText;
        @JsonProperty("new_text")
        private String newText;

        public FileDiff() {}

        public String getPath() {
            return path;
        }

        public void setPath(String path) {
            this.path = path;
        }

        public String getOldText() {
            return oldText;
        }

        public void setOldText(String oldText) {
            this.oldText = oldText;
        }

        public String getNewText() {
            return newText;
        }

        public void setNewText(String newText) {
            this.newText = newText;
        }
    }
}