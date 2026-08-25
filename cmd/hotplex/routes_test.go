package main

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/admin"
)

func TestAdminRestartPrepareError_ClassifiesOnlyConflicts(t *testing.T) {
	t.Parallel()

	conflict := &restartLeaseConflictError{RequestID: "req_0123456789abcdef0123456789abcdef"}
	require.ErrorIs(t, adminRestartPrepareError(conflict), admin.ErrRestartConflict)

	internalErr := errors.New("instance discovery failed")
	require.Same(t, internalErr, adminRestartPrepareError(internalErr))
}
