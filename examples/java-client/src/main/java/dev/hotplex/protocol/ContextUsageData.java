package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.List;

/**
 * ContextUsageData carries context window usage breakdown from a worker.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class ContextUsageData {
    @JsonProperty("total_tokens")
    private int totalTokens;
    @JsonProperty("max_tokens")
    private int maxTokens;
    private int percentage;
    private String model;
    private List<ContextCategory> categories;
    @JsonProperty("memory_files")
    private Integer memoryFiles;
    @JsonProperty("mcp_tools")
    private Integer mcpTools;
    private Integer agents;
    private ContextSkillInfo skills;

    public ContextUsageData() {}

    public int getTotalTokens() {
        return totalTokens;
    }

    public void setTotalTokens(int totalTokens) {
        this.totalTokens = totalTokens;
    }

    public int getMaxTokens() {
        return maxTokens;
    }

    public void setMaxTokens(int maxTokens) {
        this.maxTokens = maxTokens;
    }

    public int getPercentage() {
        return percentage;
    }

    public void setPercentage(int percentage) {
        this.percentage = percentage;
    }

    public String getModel() {
        return model;
    }

    public void setModel(String model) {
        this.model = model;
    }

    public List<ContextCategory> getCategories() {
        return categories;
    }

    public void setCategories(List<ContextCategory> categories) {
        this.categories = categories;
    }

    public Integer getMemoryFiles() {
        return memoryFiles;
    }

    public void setMemoryFiles(Integer memoryFiles) {
        this.memoryFiles = memoryFiles;
    }

    public Integer getMcpTools() {
        return mcpTools;
    }

    public void setMcpTools(Integer mcpTools) {
        this.mcpTools = mcpTools;
    }

    public Integer getAgents() {
        return agents;
    }

    public void setAgents(Integer agents) {
        this.agents = agents;
    }

    public ContextSkillInfo getSkills() {
        return skills;
    }

    public void setSkills(ContextSkillInfo skills) {
        this.skills = skills;
    }
}
