package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;

import java.util.Map;

/**
 * QuestionResponseData is the payload for QuestionResponse events.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class QuestionResponseData {
    private String id;
    private Map<String, String> answers;

    public QuestionResponseData() {}

    public QuestionResponseData(String id, Map<String, String> answers) {
        this.id = id;
        this.answers = answers;
    }

    public String getId() {
        return id;
    }

    public void setId(String id) {
        this.id = id;
    }

    public Map<String, String> getAnswers() {
        return answers;
    }

    public void setAnswers(Map<String, String> answers) {
        this.answers = answers;
    }
}
