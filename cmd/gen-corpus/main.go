// Command gen-corpus regenerates the golden AEP envelope corpus from the
// current Go type definitions. Run after changing pkg/events/events.go.
//
// Usage: go run ./cmd/gen-corpus
package main

import (
	"fmt"
	"os"

	"github.com/hrygo/hotplex/pkg/aep/schema"
)

func main() {
	dir := "pkg/aep/schema/corpus"
	n, err := schema.GenerateCorpus(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gen-corpus: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Generated %d corpus fixtures in %s\n", n, dir)
}
