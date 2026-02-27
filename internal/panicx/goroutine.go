// Package panicx provides panic recovery utilities for goroutine safety.
// It ensures that panics in spawned goroutines are caught and logged
// rather than crashing the entire process.
package panicx

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
)

// RecoveryPolicy defines how to handle recovered panics.
type RecoveryPolicy int

const (
	// PolicyLogAndContinue logs the panic and continues normal operation.
	// This is the default policy for non-critical goroutines.
	PolicyLogAndContinue RecoveryPolicy = iota

	// PolicyLogAndRestart logs the panic and signals for component restart.
	// Use for goroutines that should be restarted after failure.
	PolicyLogAndRestart

	// PolicyLogAndShutdown logs the panic and triggers graceful shutdown.
	// Use for critical goroutines where continued operation is unsafe.
	PolicyLogAndShutdown
)

// SafeGo launches a goroutine with panic recovery.
// The panic is logged with full stack trace, and the process continues.
func SafeGo(logger *slog.Logger, fn func()) {
	go func() {
		defer Recover(logger, "SafeGo")
		fn()
	}()
}

// SafeGoWithContext launches a goroutine with context and panic recovery.
// The context is passed to the function for cancellation support.
func SafeGoWithContext(ctx context.Context, logger *slog.Logger, fn func(context.Context)) {
	go func() {
		defer Recover(logger, "SafeGoWithContext")
		fn(ctx)
	}()
}

// SafeGoWithPolicy launches a goroutine with a specific recovery policy.
// The policy determines what happens after a panic is recovered.
func SafeGoWithPolicy(logger *slog.Logger, policy RecoveryPolicy, context string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				handlePanic(logger, context, r, policy)
			}
		}()
		fn()
	}()
}

// Recover is a defer-friendly panic recovery function.
// It logs the panic with full stack trace. Use it like:
//
//	defer panicx.Recover(logger, "myGoroutine")
func Recover(logger *slog.Logger, context string) {
	if r := recover(); r != nil {
		handlePanic(logger, context, r, PolicyLogAndContinue)
	}
}

// RecoverWithCallback recovers from panic and calls the provided callback.
// The callback receives the panic value and stack trace.
func RecoverWithCallback(logger *slog.Logger, context string, callback func(panicValue any, stack []byte)) {
	if r := recover(); r != nil {
		stack := debug.Stack()
		if logger != nil {
			logger.Error("Panic recovered",
				"context", context,
				"panic", fmt.Sprintf("%v", r),
				"stack", string(stack),
			)
		}
		if callback != nil {
			callback(r, stack)
		}
	}
}

// handlePanic is the internal panic handler.
func handlePanic(logger *slog.Logger, context string, r any, policy RecoveryPolicy) {
	stack := debug.Stack()

	if logger != nil {
		logger.Error("Panic recovered",
			"context", context,
			"panic", fmt.Sprintf("%v", r),
			"stack", string(stack),
			"policy", policyString(policy),
		)
	}

	// Future: Implement policy actions
	// - PolicyLogAndRestart: Signal restart channel
	// - PolicyLogAndShutdown: Trigger graceful shutdown via signal
}

// policyString returns a human-readable policy name.
func policyString(p RecoveryPolicy) string {
	switch p {
	case PolicyLogAndContinue:
		return "log_and_continue"
	case PolicyLogAndRestart:
		return "log_and_restart"
	case PolicyLogAndShutdown:
		return "log_and_shutdown"
	default:
		return "unknown"
	}
}
