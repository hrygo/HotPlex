package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;

import java.util.List;

/**
 * MCPStatusData carries MCP server connection status from a worker.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class MCPStatusData {
    private List<MCPServerInfo> servers;

    public MCPStatusData() {}

    public MCPStatusData(List<MCPServerInfo> servers) {
        this.servers = servers;
    }

    public List<MCPServerInfo> getServers() {
        return servers;
    }

    public void setServers(List<MCPServerInfo> servers) {
        this.servers = servers;
    }
}
