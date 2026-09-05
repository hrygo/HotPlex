package codexcli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker"
)

func TestServerCommanderSetModelReturnsErrNotImplemented(t *testing.T) {
	t.Parallel()

	commander := &ServerCommander{}
	_, err := commander.SendControlRequest(context.Background(), "set_model", nil)
	require.ErrorIs(t, err, worker.ErrNotImplemented)
}
