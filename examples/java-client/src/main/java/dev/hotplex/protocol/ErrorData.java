package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;

/**
 * ErrorData for error events.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class ErrorData {
    private ErrorCode code;
    private String message;

    public ErrorData() {}

    public ErrorData(ErrorCode code, String message) {
        this.code = code;
        this.message = message;
    }

    public ErrorCode getCode() {
        return code;
    }

    public void setCode(ErrorCode code) {
        this.code = code;
    }

    public String getMessage() {
        return message;
    }

    public void setMessage(String message) {
        this.message = message;
    }
}