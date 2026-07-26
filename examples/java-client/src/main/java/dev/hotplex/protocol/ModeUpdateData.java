package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;

/**
 * ModeUpdateData carries an agent execution mode switch notification.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class ModeUpdateData {
    @JsonProperty("current_mode_id")
    private String currentModeId;

    public ModeUpdateData() {}

    public ModeUpdateData(String currentModeId) {
        this.currentModeId = currentModeId;
    }

    public String getCurrentModeId() {
        return currentModeId;
    }

    public void setCurrentModeId(String currentModeId) {
        this.currentModeId = currentModeId;
    }
}
