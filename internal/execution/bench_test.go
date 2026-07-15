package execution

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/pkg/events"
)

func BenchmarkAcceptDispatch_FullCycle(b *testing.B) {
	store, sessionStore := newTestSQLStore(&testing.T{})
	ctx := context.Background()

	now := time.Now()
	if err := sessionStore.Upsert(ctx, &session.SessionInfo{
		ID: "session-bench", UserID: "u1", WorkerType: "claude_code",
		State: events.StateRunning, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msgID := fmt.Sprintf("msg-bench-%d", i)
		rec, _, err := store.Accept(ctx, AcceptRequest{
			SessionID: "session-bench", ClientMessageID: msgID, PayloadHash: "h",
			OwnerInstanceID: "gw-bench", WorkerRunID: msgID,
		})
		if err != nil {
			b.Fatal(err)
		}
		if err := store.MarkRunning(ctx, rec.ExecutionID, "gw-bench", rec.WorkerRunID); err != nil {
			b.Fatal(err)
		}
		if err := store.SetDelivery(ctx, rec.ExecutionID, "gw-bench", StatusDelivered, ""); err != nil {
			b.Fatal(err)
		}
		if err := store.FinishRuntime(ctx, rec.ExecutionID, rec.WorkerRunID, RuntimeCompleted, ""); err != nil {
			b.Fatal(err)
		}
	}
}
