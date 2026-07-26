package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;

/**
 * input.ack event payload: durable input acceptance/delivery acknowledgement.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class InputAckData {
    @JsonProperty("client_message_id")
    private String clientMessageId;

    @JsonProperty("execution_id")
    private String executionId;

    @JsonProperty("status")
    private String status; // accepted / delivered / unknown / failed

    @JsonProperty("duplicate")
    private Boolean duplicate;

    @JsonProperty("error_code")
    private String errorCode;

    public InputAckData() {}

    public String getClientMessageId() {
        return clientMessageId;
    }

    public void setClientMessageId(String clientMessageId) {
        this.clientMessageId = clientMessageId;
    }

    public String getExecutionId() {
        return executionId;
    }

    public void setExecutionId(String executionId) {
        this.executionId = executionId;
    }

    public String getStatus() {
        return status;
    }

    public void setStatus(String status) {
        this.status = status;
    }

    public Boolean getDuplicate() {
        return duplicate;
    }

    public void setDuplicate(Boolean duplicate) {
        this.duplicate = duplicate;
    }

    public String getErrorCode() {
        return errorCode;
    }

    public void setErrorCode(String errorCode) {
        this.errorCode = errorCode;
    }
}
