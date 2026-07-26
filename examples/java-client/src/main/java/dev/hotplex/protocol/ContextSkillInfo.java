package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;

import java.util.List;

/**
 * ContextSkillInfo carries skill-related context usage breakdown.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class ContextSkillInfo {
    private int total;
    private int included;
    private int tokens;
    private List<String> names;

    public ContextSkillInfo() {}

    public ContextSkillInfo(int total, int included, int tokens, List<String> names) {
        this.total = total;
        this.included = included;
        this.tokens = tokens;
        this.names = names;
    }

    public int getTotal() {
        return total;
    }

    public void setTotal(int total) {
        this.total = total;
    }

    public int getIncluded() {
        return included;
    }

    public void setIncluded(int included) {
        this.included = included;
    }

    public int getTokens() {
        return tokens;
    }

    public void setTokens(int tokens) {
        this.tokens = tokens;
    }

    public List<String> getNames() {
        return names;
    }

    public void setNames(List<String> names) {
        this.names = names;
    }
}
