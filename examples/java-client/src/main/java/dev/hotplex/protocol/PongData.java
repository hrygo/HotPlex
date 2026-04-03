package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;

/**
 * PongData for pong events.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class PongData {
    private Long timestamp;

    public PongData() {}

    public PongData(Long timestamp) {
        this.timestamp = timestamp;
    }

    public Long getTimestamp() {
        return timestamp;
    }

    public void setTimestamp(Long timestamp) {
        this.timestamp = timestamp;
    }
}