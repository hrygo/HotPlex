package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.List;

/**
 * Question represents a single question with options.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class Question {
    private String question;
    private String header;
    private List<QuestionOption> options;
    @JsonProperty("multi_select")
    private boolean multiSelect;

    public Question() {}

    public Question(String question, String header, List<QuestionOption> options, boolean multiSelect) {
        this.question = question;
        this.header = header;
        this.options = options;
        this.multiSelect = multiSelect;
    }

    public String getQuestion() {
        return question;
    }

    public void setQuestion(String question) {
        this.question = question;
    }

    public String getHeader() {
        return header;
    }

    public void setHeader(String header) {
        this.header = header;
    }

    public List<QuestionOption> getOptions() {
        return options;
    }

    public void setOptions(List<QuestionOption> options) {
        this.options = options;
    }

    public boolean isMultiSelect() {
        return multiSelect;
    }

    public void setMultiSelect(boolean multiSelect) {
        this.multiSelect = multiSelect;
    }
}
