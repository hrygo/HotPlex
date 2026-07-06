package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// hashableFields returns the UserActivity fields included in self_hash computation.
// The fields are: all except ID, PrevHash, SelfHash.
type hashableFields struct {
	Ts           int64  `json:"ts"`
	UserID       string `json:"user_id"`
	UserIDType   string `json:"user_id_type"`
	Platform     string `json:"platform"`
	SessionID    string `json:"session_id"`
	Action       string `json:"action"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	Outcome      string `json:"outcome"`
	DetailJSON   string `json:"detail_json"`
	EventRef     string `json:"event_ref"`
	IP           string `json:"ip"`
	UserAgent    string `json:"user_agent"`
}

// ComputeSelfHash returns sha256(prevHash || canonical(ua fields except prev/self_hash)).
// For the genesis row, prevHash is "".
func ComputeSelfHash(prevHash string, ua *UserActivity) (string, error) {
	if ua == nil {
		return "", fmt.Errorf("audit: nil UserActivity")
	}
	payload, err := json.Marshal(hashableFields{
		Ts:           ua.Ts,
		UserID:       ua.UserID,
		UserIDType:   ua.UserIDType,
		Platform:     ua.Platform,
		SessionID:    ua.SessionID,
		Action:       ua.Action,
		ResourceType: ua.ResourceType,
		ResourceID:   ua.ResourceID,
		Outcome:      ua.Outcome,
		DetailJSON:   ua.DetailJSON,
		EventRef:     ua.EventRef,
		IP:           ua.IP,
		UserAgent:    ua.UserAgent,
	})
	if err != nil {
		return "", fmt.Errorf("audit: hash marshal: %w", err)
	}
	h := sha256.New()
	h.Write([]byte(prevHash))
	h.Write(payload)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// VerifyChain validates a sequence of UserActivity rows in id order.
// Returns the first broken row's id (0 if no break) and a reason.
// The genesis row's prev_hash must be "".
// Each row's self_hash must equal ComputeSelfHash(previous self_hash, row).
// checkpointHash, if non-empty, overrides the genesis requirement (used after GC rebase).
func VerifyChain(rows []UserActivity, checkpointHash string) (brokenID int64, reason string) {
	expectedPrev := checkpointHash
	for i := range rows {
		row := rows[i]
		if row.PrevHash != expectedPrev {
			return row.ID, "prev_hash_mismatch"
		}
		computed, err := ComputeSelfHash(expectedPrev, &row)
		if err != nil {
			return row.ID, "compute_error:" + err.Error()
		}
		if computed != row.SelfHash {
			return row.ID, "self_hash_mismatch"
		}
		expectedPrev = row.SelfHash
	}
	return 0, ""
}
