package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.List;

/**
 * QuestionRequestData is the payload for QuestionRequest events.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class QuestionRequestData {
    private String id;
    @JsonProperty("tool_name")
    private String toolName;
    private List<Question> questions;

    public QuestionRequestData() {}

    public QuestionRequestData(String id, String toolName, List<Question> questions) {
        this.id = id;
        this.toolName = toolName;
        this.questions = questions;
    }

    public String getId() {
        return id;
    }

    public void setId(String id) {
        this.id = id;
    }

    public String getToolName() {
        return toolName;
    }

    public void setToolName(String toolName) {
        this.toolName = toolName;
    }

    public List<Question> getQuestions() {
        return questions;
    }

    public void setQuestions(List<Question> questions) {
        this.questions = questions;
    }
}
