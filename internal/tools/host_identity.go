package tools

// Two aliases the LEDGER's own surfaces read, declared here because the files
// they used to live in belong to the cross-node sync mechanism and are being
// retired (devdoc/sync-retire-deletion-order.md).
//
// Rule 43's failure this file repairs, measured: `grep -rn` over the tree while
// the sync mechanism is still present shows their consumers are single-node:
//
//	syncUnknownHost     tasks.go:8716 (a stamp's fail-safe host),
//	                    task_reclaim_self.go:302 (a const alias)
//	TakeOverFlagSpelling taskprogress.go:116 (the occupied-but-idle sentence
//	                    an operator reads)
//
// Neither is a sync concept. Deleting memory_sync_task.go / memory_sync_row.go
// without moving these first would make `go build ./...` fail on files the
// retirement plan does not delete — the "batch line drawn across an
// interdependent set" defect this reframe exists to fix.

const (
	// TakeOverFlagSpelling is `--takeover`: the pull leg's half of a move
	// (`gopi task receive --takeover`), named in the refusal as the other
	// legal shape. It is ALSO the flag the ledger's own "occupied but idle"
	// recovery sentence tells an operator to pass, which is why it cannot
	// retire with the sync CLI surface.
	TakeOverFlagSpelling = "takeover"

	// syncUnknownHost is the fail-safe host identity when os.Hostname
	// fails or returns blank: an identity that can never equal a real
	// hostname, so every stamp of ours is still classified as OURS
	// (LiveRowsOwnedByOthers etc. degrade to "cannot compare", never to a
	// bogus foreign count). Single definition (Rule 13).
	syncUnknownHost = "unknown-host"
)
