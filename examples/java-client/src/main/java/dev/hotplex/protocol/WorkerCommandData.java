package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;

import java.util.Map;

/**
 * WorkerCommandData is the payload for worker stdio command events.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class WorkerCommandData {
    private String command;
    private String args;
    private Map<String, Object> extra;

    public WorkerCommandData() {}

    public WorkerCommandData(String command, String args, Map<String, Object> extra) {
        this.command = command;
        this.args = args;
        this.extra = extra;
    }

    public String getCommand() {
        return command;
    }

    public void setCommand(String command) {
        this.command = command;
    }

    public String getArgs() {
        return args;
    }

    public void setArgs(String args) {
        this.args = args;
    }

    public Map<String, Object> getExtra() {
        return extra;
    }

    public void setExtra(Map<String, Object> extra) {
        this.extra = extra;
    }
}
