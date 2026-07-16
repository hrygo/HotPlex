package gateway

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

func TestSnapshotLastInput_UsesFrozenConnection(t *testing.T) {
	t.Parallel()

	conn := &fakeWorkerConn{
		ch:        make(chan *events.Envelope),
		lastInput: "resume this turn",
	}

	require.Equal(t, "resume this turn", snapshotLastInput(conn))
}

func TestSnapshotLastInput_SanitizesControlCommand(t *testing.T) {
	t.Parallel()

	conn := &fakeWorkerConn{
		ch:        make(chan *events.Envelope),
		lastInput: "$reset",
	}

	require.Empty(t, snapshotLastInput(conn))
}
