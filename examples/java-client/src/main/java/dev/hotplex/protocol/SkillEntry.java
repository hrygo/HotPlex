package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;

/**
 * SkillEntry describes a single skill with name, description and source.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class SkillEntry {
    private String name;
    private String description;
    private String source;

    public SkillEntry() {}

    public SkillEntry(String name, String description, String source) {
        this.name = name;
        this.description = description;
        this.source = source;
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
}
