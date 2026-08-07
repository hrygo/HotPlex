package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;

/**
 * SkillEntry describes a single skill with name, description, source and
 * optional invokability status.
 *
 * <p>The {@code status} field mirrors the gateway's {@code SkillStatus}
 * wire values: {@code callable}, {@code discoverable} or {@code unavailable}.
 * It is optional ({@code omitempty} on the wire) so older envelopes without
 * it decode with {@code status == null}.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class SkillEntry {
    private String name;
    private String description;
    private String source;
    private String status;

    public SkillEntry() {}

    public SkillEntry(String name, String description, String source) {
        this(name, description, source, null);
    }

    public SkillEntry(String name, String description, String source, String status) {
        this.name = name;
        this.description = description;
        this.source = source;
        this.status = status;
    }

    public String getName() {
        return name;
    }

    public void setName(String name) {
        this.name = name;
    }

    public String getDescription() {
        return description;
    }

    public void setDescription(String description) {
        this.description = description;
    }

    public String getSource() {
        return source;
    }

    public void setSource(String source) {
        this.source = source;
    }

    public String getStatus() {
        return status;
    }

    public void setStatus(String status) {
        this.status = status;
    }
}
