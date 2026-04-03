package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;

/**
 * MessageDeltaData for message.delta events.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class MessageDeltaData {
    @JsonProperty("message_id")
    private String messageId;
    private String content;

    public MessageDeltaData() {}

    public MessageDeltaData(String messageId, String content) {
        this.messageId = messageId;
        this.content = content;
    }

    public String getMessageId() {
        return messageId;
    }

    public void setMessageId(String messageId) {
        this.messageId = messageId;
    }

    public String getContent() {
        return content;
    }

    public void setContent(String content) {
        this.content = content;
    }
}