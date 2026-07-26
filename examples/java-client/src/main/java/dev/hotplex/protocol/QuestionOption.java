package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;

/**
 * QuestionOption represents a single selectable option in a question.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class QuestionOption {
    private String label;
    private String description;
    private String preview;

    public QuestionOption() {}

    public QuestionOption(String label, String description, String preview) {
        this.label = label;
        this.description = description;
        this.preview = preview;
    }

    public String getLabel() {
        return label;
    }

    public void setLabel(String label) {
        this.label = label;
    }

    public String getDescription() {
        return description;
    }

    public void setDescription(String description) {
        this.description = description;
    }

    public String getPreview() {
        return preview;
    }

    public void setPreview(String preview) {
        this.preview = preview;
    }
}
