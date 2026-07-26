package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.Map;

/**
 * ElicitationRequestData is the payload for ElicitationRequest events.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class ElicitationRequestData {
    private String id;
    @JsonProperty("mcp_server_name")
    private String mcpServerName;
    private String message;
    private String mode;
    private String url;
    @JsonProperty("elicitation_id")
    private String elicitationId;
    @JsonProperty("requested_schema")
    private Map<String, Object> requestedSchema;

    public ElicitationRequestData() {}

    public String getId() {
        return id;
    }

    public void setId(String id) {
        this.id = id;
    }

    public String getMcpServerName() {
        return mcpServerName;
    }

    public void setMcpServerName(String mcpServerName) {
        this.mcpServerName = mcpServerName;
    }

    public String getMessage() {
        return message;
    }

    public void setMessage(String message) {
        this.message = message;
    }

    public String getMode() {
        return mode;
    }

    public void setMode(String mode) {
        this.mode = mode;
    }

    public String getUrl() {
        return url;
    }

    public void setUrl(String url) {
        this.url = url;
    }

    public String getElicitationId() {
        return elicitationId;
    }

    public void setElicitationId(String elicitationId) {
        this.elicitationId = elicitationId;
    }

    public Map<String, Object> getRequestedSchema() {
        return requestedSchema;
    }

    public void setRequestedSchema(Map<String, Object> requestedSchema) {
        this.requestedSchema = requestedSchema;
    }
}
