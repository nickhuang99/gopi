package tools

import "strings"

// memoryDBPathOr resolves a possibly-empty --db override to the default path.
//
// This lives HERE, and not beside the code that first needed it, because it is
// not a part of any one mechanism — it is the ordinary "the caller may have
// passed --db, otherwise use the default" resolution, and three consumers
// outside the memory-asset family call it (memory_sync_row.go,
// memory_sync_row_chain.go, task_transfer.go). It used to sit in
// memory_sync_assets.go:1057, which is the very file the cross-node asset
// channel is deleted from; a removal there would have taken this function with
// it and broken task_transfer.go — a file no deletion batch names, compiled by
// people who never heard of that channel. Rule 43: move the general tool OUT
// first, in the same change that deletes the mechanism.
func memoryDBPathOr(dbPath string) string {
	if strings.TrimSpace(dbPath) == "" {
		return memoryDBPath()
	}
	return dbPath
}
