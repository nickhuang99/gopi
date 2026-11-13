package tools

import (
	"context"
	"fmt"
)

// EnsureLedger creates the task ledger (memory.db) at path when it is absent
// and initializes its schema.
//
// It is the provisioning face of openStoreDB (memory.go): the same
// ensureKnowledgeBase bootstrap gopi's first store/search already performs, made
// callable ahead of time so a deployment can be brought to a run-ready state in
// one run — `gopi config setup` — without a shell script (PROJECT.md Rule 10.7, zero
// dependencies). It creates nothing when the file is already there (the schema
// batch is idempotent), so a re-run is visibly a no-op.
//
// Whether the FILE was new is the caller's question, answered by its own stat
// before the call — this function deliberately does not guess it back out of a
// path that may already have been converted to a DSN elsewhere.
//
// The path is the caller's decision, resolved through config.LedgerPath (the
// ONE reader of the GOPI_MEMORY_DB override, Rule 2) — never re-derived here.
func EnsureLedger(path string) error {
	ctx := context.Background()
	if _, err := openStoreDB(ctx, path); err != nil {
		return fmt.Errorf("ensure ledger %s: %w", path, err)
	}
	return nil
}
