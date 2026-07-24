package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonProperty;

/**
 * internal_reset event payload: worker in-place reset notification.
 */
public class InternalResetData {
    @JsonProperty("generation")
    private long generation;

    public InternalResetData() {}

    public long getGeneration() {
        return generation;
    }

    public void setGeneration(long generation) {
        this.generation = generation;
    }
}
