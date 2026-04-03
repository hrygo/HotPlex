package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.List;
import java.util.Map;

/**
 * ControlData for control events.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class ControlData {
    private String action;
    private String reason;
    @JsonProperty("delay_ms")
    private Integer delayMs;
    private Boolean recoverable;
    private Map<String, Object> suggestion;
    private Map<String, Object> details;

    public ControlData() {}

    public ControlData(String action, String reason, Integer delayMs, Boolean recoverable, 
                       Map<String, Object> suggestion, Map<String, Object> details) {
        this.action = action;
        this.reason = reason;
        this.delayMs = delayMs;
        this.recoverable = recoverable;
        this.suggestion = suggestion;
        this.details = details;
    }

    public String getAction() {
        return action;
    }

    public void setAction(String action) {
        this.action = action;
    }

    public String getReason() {
        return reason;
    }

    public void setReason(String reason) {
        this.reason = reason;
    }

    public Integer getDelayMs() {
        return delayMs;
    }

    public void setDelayMs(Integer delayMs) {
        this.delayMs = delayMs;
    }

    public Boolean getRecoverable() {
        return recoverable;
    }

    public void setRecoverable(Boolean recoverable) {
        this.recoverable = recoverable;
    }

    public Map<String, Object> getSuggestion() {
        return suggestion;
    }

    public void setSuggestion(Map<String, Object> suggestion) {
        this.suggestion = suggestion;
    }

    public Map<String, Object> getDetails() {
        return details;
    }

    public void setDetails(Map<String, Object> details) {
        this.details = details;
    }

    /**
     * InitAuth for init auth data.
     */
    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class InitAuth {
        private String token;

        public InitAuth() {}

        public InitAuth(String token) {
            this.token = token;
        }

        public String getToken() {
            return token;
        }

        public void setToken(String token) {
            this.token = token;
        }
    }

    /**
     * InitConfig for init config data.
     */
    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class InitConfig {
        private String model;
        @JsonProperty("system_prompt")
        private String systemPrompt;
        @JsonProperty("allowed_tools")
        private List<String> allowedTools;
        @JsonProperty("disallowed_tools")
        private List<String> disallowedTools;
        @JsonProperty("max_turns")
        private Integer maxTurns;
        @JsonProperty("work_dir")
        private String workDir;
        private Map<String, Object> metadata;

        public InitConfig() {}

        public String getModel() {
            return model;
        }

        public void setModel(String model) {
            this.model = model;
        }

        public String getSystemPrompt() {
            return systemPrompt;
        }

        public void setSystemPrompt(String systemPrompt) {
            this.systemPrompt = systemPrompt;
        }

        public List<String> getAllowedTools() {
            return allowedTools;
        }

        public void setAllowedTools(List<String> allowedTools) {
            this.allowedTools = allowedTools;
        }

        public List<String> getDisallowedTools() {
            return disallowedTools;
        }

        public void setDisallowedTools(List<String> disallowedTools) {
            this.disallowedTools = disallowedTools;
        }

        public Integer getMaxTurns() {
            return maxTurns;
        }

        public void setMaxTurns(Integer maxTurns) {
            this.maxTurns = maxTurns;
        }

        public String getWorkDir() {
            return workDir;
        }

        public void setWorkDir(String workDir) {
            this.workDir = workDir;
        }

        public Map<String, Object> getMetadata() {
            return metadata;
        }

        public void setMetadata(Map<String, Object> metadata) {
            this.metadata = metadata;
        }
    }

    /**
     * ClientCaps for client capabilities.
     */
    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class ClientCaps {
        @JsonProperty("supports_delta")
        private Boolean supportsDelta;
        @JsonProperty("supports_tool_call")
        private Boolean supportsToolCall;
        @JsonProperty("supported_kinds")
        private List<String> supportedKinds;

        public ClientCaps() {}

        public Boolean getSupportsDelta() {
            return supportsDelta;
        }

        public void setSupportsDelta(Boolean supportsDelta) {
            this.supportsDelta = supportsDelta;
        }

        public Boolean getSupportsToolCall() {
            return supportsToolCall;
        }

        public void setSupportsToolCall(Boolean supportsToolCall) {
            this.supportsToolCall = supportsToolCall;
        }

        public List<String> getSupportedKinds() {
            return supportedKinds;
        }

        public void setSupportedKinds(List<String> supportedKinds) {
            this.supportedKinds = supportedKinds;
        }
    }
}