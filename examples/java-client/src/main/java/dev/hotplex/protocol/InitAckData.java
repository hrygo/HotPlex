package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.List;

/**
 * InitAckData for gateway -> client init_ack message.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class InitAckData {

    @JsonProperty("session_id")
    private String sessionId;

    private SessionState state;

    @JsonProperty("server_caps")
    private ServerCaps serverCaps;

    private String error;

    private ErrorCode code;

    public InitAckData() {}

    public String getSessionId() {
        return sessionId;
    }

    public void setSessionId(String sessionId) {
        this.sessionId = sessionId;
    }

    public SessionState getState() {
        return state;
    }

    public void setState(SessionState state) {
        this.state = state;
    }

    public ServerCaps getServerCaps() {
        return serverCaps;
    }

    public void setServerCaps(ServerCaps serverCaps) {
        this.serverCaps = serverCaps;
    }

    public String getError() {
        return error;
    }

    public void setError(String error) {
        this.error = error;
    }

    public ErrorCode getCode() {
        return code;
    }

    public void setCode(ErrorCode code) {
        this.code = code;
    }

    /**
     * ServerCaps for server capabilities.
     */
    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class ServerCaps {
        @JsonProperty("protocol_version")
        private String protocolVersion;
        @JsonProperty("worker_type")
        private String workerType;
        @JsonProperty("supports_resume")
        private Boolean supportsResume;
        @JsonProperty("supports_delta")
        private Boolean supportsDelta;
        @JsonProperty("supports_tool_call")
        private Boolean supportsToolCall;
        @JsonProperty("supports_ping")
        private Boolean supportsPing;
        @JsonProperty("max_frame_size")
        private Long maxFrameSize;
        @JsonProperty("max_turns")
        private Integer maxTurns;
        private List<String> modalities;
        private List<String> tools;

        public ServerCaps() {}

        public String getProtocolVersion() {
            return protocolVersion;
        }

        public void setProtocolVersion(String protocolVersion) {
            this.protocolVersion = protocolVersion;
        }

        public String getWorkerType() {
            return workerType;
        }

        public void setWorkerType(String workerType) {
            this.workerType = workerType;
        }

        public Boolean getSupportsResume() {
            return supportsResume;
        }

        public void setSupportsResume(Boolean supportsResume) {
            this.supportsResume = supportsResume;
        }

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

        public Boolean getSupportsPing() {
            return supportsPing;
        }

        public void setSupportsPing(Boolean supportsPing) {
            this.supportsPing = supportsPing;
        }

        public Long getMaxFrameSize() {
            return maxFrameSize;
        }

        public void setMaxFrameSize(Long maxFrameSize) {
            this.maxFrameSize = maxFrameSize;
        }

        public Integer getMaxTurns() {
            return maxTurns;
        }

        public void setMaxTurns(Integer maxTurns) {
            this.maxTurns = maxTurns;
        }

        public List<String> getModalities() {
            return modalities;
        }

        public void setModalities(List<String> modalities) {
            this.modalities = modalities;
        }

        public List<String> getTools() {
            return tools;
        }

        public void setTools(List<String> tools) {
            this.tools = tools;
        }
    }
}