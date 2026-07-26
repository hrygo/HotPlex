package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;

import java.util.List;

/**
 * PlanData carries a plan/todo update (ACP AgentPlanUpdate).
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class PlanData {
    private List<PlanItem> items;

    public PlanData() {}

    public PlanData(List<PlanItem> items) {
        this.items = items;
    }

    public List<PlanItem> getItems() {
        return items;
    }

    public void setItems(List<PlanItem> items) {
        this.items = items;
    }
}
