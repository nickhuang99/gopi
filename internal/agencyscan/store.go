package agencyscan

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nick/gopi/internal/config"
	_ "modernc.org/sqlite" // DB driver: the ledger + knowledge base live in one sqlite file
)

// This file is the package's I/O boundary — the ONLY place that touches the
// filesystem, the environment or the task queue. Everything in agencyscan.go
// is pure logic over values, so the mining rules can be tested without a DB.
//
// Coupling note (Rule 3 / import-cycle safety): agencyscan is a LEAF package
// and reaches the ledger through internal/tools, which is where the ledger's
// SQL, the schema and TaskSubmit already live. No new store is created and no
// new schema is introduced — a proposal is an ordinary 'pending' row in the
// existing tasks table.
const (
	// memoryDBEnv is the ledger/knowledge path override (mirror of
	// internal/tools' memoryDBEnv — this leaf package must not import the
	// constant from a package that also owns the write path).
	memoryDBEnv = "GOPI_MEMORY_DB"

	// dbBusyTimeoutMSec mirrors internal/tools.dbBusyTimeoutMSec: a
	// concurrent writer (a running agent) must not fail this reader.
	dbBusyTimeoutMSec = 5000

	// ledgerReadOnlyMode disables writes on this connection: the miner is a
	// READER (the one write it performs goes through TaskSubmit). A miner
	// bug can therefore never corrupt the ledger.
	queryOnlyPragma = "PRAGMA query_only = ON"
)

// scanStderr is the diagnostic sink (Rule 15b: a refused proposal is
// RECORDED, never swallowed).
var scanStderr io.Writer = os.Stderr

// memoryDBPath resolves the ledger path (GOPI_MEMORY_DB > ~/.gopi/memory.db).
// Mirrors internal/tools.memoryDBPath so a sandboxed pass (a test HOME or
// GOPI_MEMORY_DB) reads exactly the DB the rest of gopi writes.
func memoryDBPath() string {
	if v := strings.TrimSpace(os.Getenv(memoryDBEnv)); v != "" {
		return v
	}
	gd, err := config.GopiDir()
	if err != nil {
		return ""
	}
	return filepath.Join(gd, "memory.db")
}

// openLedgerReadOnlyContext opens the ledger with writes disabled.
func openLedgerReadOnlyContext(ctx context.Context, path string) (*sql.DB, error) {
	if path == "" {
		return nil, fmt.Errorf("no ledger path (GOPI_MEMORY_DB unset and GopiDir unresolvable)")
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("ledger %s: %w", path, err)
	}
	db, err := sql.Open("sqlite", path+"?_txlock=immediate")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout = "+strconv.Itoa(dbBusyTimeoutMSec)); err != nil {
		db.Close() //stylegate:ignore opened handle closed after its last caller finished
		return nil, err
	}
	if _, err := db.ExecContext(ctx, queryOnlyPragma); err != nil {
		db.Close() //stylegate:ignore opened handle closed after its last caller finished
		return nil, err
	}
	return db, nil
}

// submitProposal enqueues ONE improvement proposal through internal/tools'
// TaskSubmit — the pass's ONLY write and its only output channel (no new
// store, no new tool, no daemon). The row is an ordinary pending task: the
// operator sees it in `gopi task list` and decides.
func submitProposal(summary, payload string, c candidate) error {
	// TagQuiet is the load-bearing one: it fences the row out of every
	// automatic claimer (the queue already honours it), so the pass PROPOSES
	// to an operator instead of dispatching work to itself. Without it this
	// mechanism would be an auto-dispatch machine — the failure mode the epic
	// names explicitly (see devdoc/agency-proposals.md).
	tags := strings.Join([]string{TagImprovement, TagAgencyProposal, c.Kind, TagQuiet}, ",")
	_, err := SubmitFn(SubmitRequest{
		Summary: summary,
		Payload: payload,
		Tags:    tags,
		Source:  AutoTaggerSource,
	})
	return err
}

// SubmitRequest is the narrow view of tools.TaskSubmitOptions this package
// uses (kept local so the mining logic never depends on the ledger package's
// full option set — and so internal/agencyscan stays a LEAF).
type SubmitRequest struct {
	Summary string
	Payload string
	Tags    string
	Source  string
}

// SubmitFn is the submit seam — wired by internal/tools at init (the reverse
// of the usual import direction, so agencyscan stays importable from
// internal/tools without a cycle). An unwired seam makes a proposal a
// RECORDED no-op instead of a panic (Rule 15: never silent, never a crash).
var SubmitFn = func(req SubmitRequest) (string, error) {
	return "", fmt.Errorf("agencyscan: submit seam not wired (no proposal enqueued for %q)", req.Summary)
}

// SetSubmitFunc installs the ledger-backed submit seam. Called once from
// internal/tools' init; tests may swap it to observe proposals without
// touching a real queue.
func SetSubmitFunc(fn func(SubmitRequest) (string, error)) {
	if fn == nil {
		return
	}
	SubmitFn = fn
}
