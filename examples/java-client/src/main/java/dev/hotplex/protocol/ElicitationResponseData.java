package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;

import java.util.Map;

/**
 * ElicitationResponseData is the payload for ElicitationResponse events.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class ElicitationResponseData {
    private String id;
    private String action;
    private Map<String, Object> content;

    public ElicitationResponseData() {}

    public ElicitationResponseData(String id, String action, Map<String, Object> content) {
        this.id = id;
        this.action = action;
        this.content = content;
    }

    public String getId() {
        return id;
    }

    public void setId(String id) {
        this.id = id;
    }

    public String getAction() {
        return action;
    }

    public void setAction(String action) {
        this.action = action;
    }

    public Map<String, Object> getContent() {
        return content;
    }

    public void setContent(Map<String, Object> content) {
        this.content = content;
    }
}
