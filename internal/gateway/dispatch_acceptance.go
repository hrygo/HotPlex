package gateway

import "sync"

// runAcceptedDispatch releases an orchestration admission as soon as the
// provider has accepted a turn, while still returning the adapter's eventual
// completion result to the caller. The result channel is buffered so an
// adapter completing concurrently with acceptance never leaks its goroutine.
func runAcceptedDispatch(dispatch func(accepted func()) error, release func()) (bool, error) {
	acceptedCh := make(chan struct{})
	resultCh := make(chan error, 1)
	var acceptedOnce sync.Once
	accepted := func() {
		acceptedOnce.Do(func() { close(acceptedCh) })
	}

	go func() {
		resultCh <- dispatch(accepted)
	}()

	select {
	case <-acceptedCh:
		release()
		return true, <-resultCh
	case err := <-resultCh:
		// A fast success without an explicit callback is still a completed
		// delivery. An error means the request was not accepted. Either way,
		// this admission no longer protects useful work.
		wasAccepted := false
		select {
		case <-acceptedCh:
			wasAccepted = true
		default:
		}
		release()
		return wasAccepted, err
	}
}
