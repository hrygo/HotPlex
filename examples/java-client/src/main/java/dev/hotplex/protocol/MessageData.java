package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.Map;

/**
 * MessageData for message events.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class MessageData {
    private String id;
    private String role;
    private String content;
    @JsonProperty("content_type")
    private String contentType;
    private Map<String, Object> metadata;

    public MessageData() {}

    public MessageData(String id, String role, String content, String contentType, Map<String, Object> metadata) {
        this.id = id;
        this.role = role;
        this.content = content;
        this.contentType = contentType;
        this.metadata = metadata;
    }

    public String getId() {
        return id;
    }

    public void setId(String id) {
        this.id = id;
    }

    public String getRole() {
        return role;
    }

    public void setRole(String role) {
        this.role = role;
    }

    public String getContent() {
        return content;
    }

    public void setContent(String content) {
        this.content = content;
    }

    public String getContentType() {
        return contentType;
    }

    public void setContentType(String contentType) {
        this.contentType = contentType;
    }

    public Map<String, Object> getMetadata() {
        return metadata;
    }

    public void setMetadata(Map<String, Object> metadata) {
        this.metadata = metadata;
    }
}