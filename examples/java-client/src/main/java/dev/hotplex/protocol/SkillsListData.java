package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;

import java.util.List;

/**
 * SkillsListData carries discovered skills to the client.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class SkillsListData {
    private List<SkillEntry> skills;
    private int total;
    private String filter;

    public SkillsListData() {}

    public SkillsListData(List<SkillEntry> skills, int total, String filter) {
        this.skills = skills;
        this.total = total;
        this.filter = filter;
    }

    public List<SkillEntry> getSkills() {
        return skills;
    }

    public void setSkills(List<SkillEntry> skills) {
        this.skills = skills;
    }

    public int getTotal() {
        return total;
    }

    public void setTotal(int total) {
        this.total = total;
    }

    public String getFilter() {
        return filter;
    }

    public void setFilter(String filter) {
        this.filter = filter;
    }
}
