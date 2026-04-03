package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.Map;

/**
 * DoneData for done events.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class DoneData {
    private Boolean success;
    private Map<String, Object> stats;
    @JsonProperty("dropped")
    private Boolean dropped;

    public DoneData() {}

    public DoneData(Boolean success, Map<String, Object> stats, Boolean dropped) {
        this.success = success;
        this.stats = stats;
        this.dropped = dropped;
    }

    public Boolean getSuccess() {
        return success;
    }

    public void setSuccess(Boolean success) {
        this.success = success;
    }

    public Map<String, Object> getStats() {
        return stats;
    }

    public void setStats(Map<String, Object> stats) {
        this.stats = stats;
    }

    public Boolean getDropped() {
        return dropped;
    }

    public void setDropped(Boolean dropped) {
        this.dropped = dropped;
    }
}