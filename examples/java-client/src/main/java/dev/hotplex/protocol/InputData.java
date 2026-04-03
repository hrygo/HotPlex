package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;

import java.util.Map;

/**
 * InputData for input events.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class InputData {
    private String content;
    private Map<String, Object> metadata;

    public InputData() {}

    public InputData(String content, Map<String, Object> metadata) {
        this.content = content;
        this.metadata = metadata;
    }

    public String getContent() {
        return content;
    }

    public void setContent(String content) {
        this.content = content;
    }

    public Map<String, Object> getMetadata() {
        return metadata;
    }

    public void setMetadata(Map<String, Object> metadata) {
        this.metadata = metadata;
    }
}