#!/bin/bash
cat << 'INNER_EOF' > process_agg_fix.go
package chatapps

import (
"context"
"log/slog"
"strings"
"sync"
"time"

"github.com/hrygo/hotplex/chatapps/base"
)
INNER_EOF
head -n 610 chatapps/processor_aggregator.go >> process_agg_fix.go
cat << 'INNER_EOF' >> process_agg_fix.go
	for _, msg := range messages {
		if msg.RichContent == nil {
			continue
		}

		// Merge attachments
		merged.Attachments = append(merged.Attachments, msg.RichContent.Attachments...)

INNER_EOF
tail -n +613 chatapps/processor_aggregator.go >> process_agg_fix.go
mv process_agg_fix.go chatapps/processor_aggregator.go
go build ./...
