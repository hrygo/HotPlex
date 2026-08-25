package gateway

import (
	"errors"
	"sync"
)

var errWorkerDispatchPanic = errors.New("worker dispatch panicked")

// runAcceptedDispatch finalizes provider acceptance before releasing an
// orchestration admission, while still returning the adapter's eventual
// completion result to the caller. This ordering prevents a concurrent stop
// from publishing its terminal before the accepted input's durable side
// effects. The result channel is buffered so an adapter completing concurrently
// with acceptance never leaks its goroutine.
func runAcceptedDispatch(dispatch func(accepted func()) error, onAccepted, release func()) (bool, error) {
	acceptedCh := make(chan struct{})
	resultCh := make(chan error, 1)
	var acceptedOnce sync.Once
	accepted := func() {
		acceptedOnce.Do(func() { close(acceptedCh) })
	}

	go func() {
		resultCh <- invokeAcceptedDispatch(dispatch, accepted)
	}()

	select {
	case <-acceptedCh:
		finalizeAcceptedDispatch(onAccepted, release)
		return true, <-resultCh
	case err := <-resultCh:
		// A fast success without an explicit callback is still a completed
		// delivery. An error without a callback means the request was not
		// accepted. Either way, this admission no longer protects useful work.
		wasAccepted := err == nil
		select {
		case <-acceptedCh:
			wasAccepted = true
		default:
		}
		if wasAccepted {
			finalizeAcceptedDispatch(onAccepted, release)
		} else {
			release()
		}
		return wasAccepted, err
	}
}

func finalizeAcceptedDispatch(onAccepted, release func()) {
	defer release()
	if onAccepted != nil {
		onAccepted()
	}
}

func invokeAcceptedDispatch(dispatch func(accepted func()) error, accepted func()) (err error) {
	defer func() {
		if recover() != nil {
			err = errWorkerDispatchPanic
		}
	}()
	return dispatch(accepted)
}
