package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;

/**
 * PlanItem represents a single item in a plan.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class PlanItem {
    private String content;
    private String priority;
    private String status;

    public PlanItem() {}

    public PlanItem(String content, String priority, String status) {
        this.content = content;
        this.priority = priority;
        this.status = status;
    }

    public String getContent() {
        return content;
    }

    public void setContent(String content) {
        this.content = content;
    }

    public String getPriority() {
        return priority;
    }

    public void setPriority(String priority) {
        this.priority = priority;
    }

    public String getStatus() {
        return status;
    }

    public void setStatus(String status) {
        this.status = status;
    }
}
