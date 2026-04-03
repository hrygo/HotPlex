package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;

import java.util.Map;

/**
 * ToolCallData for tool_call events.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class ToolCallData {
    private String id;
    private String name;
    private Map<String, Object> input;

    public ToolCallData() {}

    public ToolCallData(String id, String name, Map<String, Object> input) {
        this.id = id;
        this.name = name;
        this.input = input;
    }

    public String getId() {
        return id;
    }

    public void setId(String id) {
        this.id = id;
    }

    public String getName() {
        return name;
    }

    public void setName(String name) {
        this.name = name;
    }

    public Map<String, Object> getInput() {
        return input;
    }

    public void setInput(Map<String, Object> input) {
        this.input = input;
    }
}