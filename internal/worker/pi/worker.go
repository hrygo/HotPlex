package pi

import (
	"github.com/hotplex/hotplex-worker/internal/worker"
	"github.com/hotplex/hotplex-worker/internal/worker/noop"
)

func init() {
	worker.Register(worker.TypePimon, func() (worker.Worker, error) {
		// // TODO: Implement actual Pi-Mono worker.
		return noop.NewWorker(), nil
	})
}
