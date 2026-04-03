package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.List;
import java.util.Map;

/**
 * InitData for client -> gateway init message.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class InitData {

    private String version;
    @JsonProperty("worker_type")
    private String workerType;
    @JsonProperty("session_id")
    private String sessionId;
    private InitAuth auth;
    private InitConfig config;
    @JsonProperty("client_caps")
    private ClientCaps clientCaps;

    public InitData() {}

    public String getVersion() {
        return version;
    }

    public void setVersion(String version) {
        this.version = version;
    }

    public String getWorkerType() {
        return workerType;
    }

    public void setWorkerType(String workerType) {
        this.workerType = workerType;
    }

    public String getSessionId() {
        return sessionId;
    }

    public void setSessionId(String sessionId) {
        this.sessionId = sessionId;
    }

    public InitAuth getAuth() {
        return auth;
    }

    public void setAuth(InitAuth auth) {
        this.auth = auth;
    }

    public InitConfig getConfig() {
        return config;
    }

    public void setConfig(InitConfig config) {
        this.config = config;
    }

    public ClientCaps getClientCaps() {
        return clientCaps;
    }

    public void setClientCaps(ClientCaps clientCaps) {
        this.clientCaps = clientCaps;
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