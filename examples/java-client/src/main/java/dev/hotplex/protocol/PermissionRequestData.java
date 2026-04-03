package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.List;

/**
 * PermissionRequestData for permission_request events.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class PermissionRequestData {
    private String id;
    @JsonProperty("tool_name")
    private String toolName;
    private String description;
    private List<String> args;

    public PermissionRequestData() {}

    public PermissionRequestData(String id, String toolName, String description, List<String> args) {
        this.id = id;
        this.toolName = toolName;
        this.description = description;
        this.args = args;
    }

    public String getId() {
        return id;
    }

    public void setId(String id) {
        this.id = id;
    }

    public String getToolName() {
        return toolName;
    }

    public void setToolName(String toolName) {
        this.toolName = toolName;
    }

    public String getDescription() {
        return description;
    }

    public void setDescription(String description) {
        this.description = description;
    }

    public List<String> getArgs() {
        return args;
    }

    public void setArgs(List<String> args) {
        this.args = args;
    }
}