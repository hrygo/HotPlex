package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;

/**
 * ContextCategory represents a named token usage bucket in ContextUsageData.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class ContextCategory {
    private String name;
    private int tokens;

    public ContextCategory() {}

    public ContextCategory(String name, int tokens) {
        this.name = name;
        this.tokens = tokens;
    }

    public String getName() {
        return name;
    }

    public void setName(String name) {
        this.name = name;
    }

    public int getTokens() {
        return tokens;
    }

    public void setTokens(int tokens) {
        this.tokens = tokens;
    }
}
