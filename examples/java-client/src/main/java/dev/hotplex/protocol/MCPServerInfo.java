package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;

/**
 * MCPServerInfo describes a single MCP server's connection state.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class MCPServerInfo {
    private String name;
    private String status;

    public MCPServerInfo() {}

    public MCPServerInfo(String name, String status) {
        this.name = name;
        this.status = status;
    }

    public String getName() {
        return name;
    }

    public void setName(String name) {
        this.name = name;
    }

    public String getStatus() {
        return status;
    }

    public void setStatus(String status) {
        this.status = status;
    }
}
