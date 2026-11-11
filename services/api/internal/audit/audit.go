// Package audit records who did what to which record. Entries never hold
// personal data such as names, emails or passwords: they name the actor by
// user id and the target by type and id, and carry the request id so an entry
// can be matched to the request logs.
package audit

import (
	"context"
	"encoding/json"

	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
)

// Entry is one audit record. ActorUserID may be empty for actions taken by
// the system or by an anonymous caller (a failed sign-in, for example).
type Entry struct {
	ActorUserID string
	Action      string
	TargetType  string
	TargetID    string
	Metadata    map[string]any
}

// Record writes an entry using q, which may be a transaction: an audited
// change and its audit entry commit together or not at all.
func Record(ctx context.Context, q db.Querier, e Entry) error {
	if e.Metadata == nil {
		e.Metadata = map[string]any{}
	}
	body, err := json.Marshal(e.Metadata)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `
		INSERT INTO audit_log (actor_user_id, action, target_type, target_id, metadata, request_id)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), $5::jsonb, NULLIF($6, ''))`,
		nullIfEmpty(e.ActorUserID), e.Action, e.TargetType, e.TargetID, string(body), httpx.RequestID(ctx))
	return err
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
