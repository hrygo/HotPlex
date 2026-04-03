package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;

/**
 * PermissionResponseData for permission_response events.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class PermissionResponseData {
    private String id;
    private Boolean allowed;
    private String reason;

    public PermissionResponseData() {}

    public PermissionResponseData(String id, Boolean allowed, String reason) {
        this.id = id;
        this.allowed = allowed;
        this.reason = reason;
    }

    public String getId() {
        return id;
    }

    public void setId(String id) {
        this.id = id;
    }

    public Boolean getAllowed() {
        return allowed;
    }

    public void setAllowed(Boolean allowed) {
        this.allowed = allowed;
    }

    public String getReason() {
        return reason;
    }

    public void setReason(String reason) {
        this.reason = reason;
    }
}