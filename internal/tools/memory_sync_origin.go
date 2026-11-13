// Package tools — memory_sync_origin.go: provenance ON THE WIRE, and the
// explicit state a reader gets when a pointer's detail is not reachable from
// this node (epic 自动记忆, item 5/5).
//
// THE DEFECT THIS FILE CLOSES. Item 1/5 (devdoc/auto-memory-candidate.md)
// ruled for scheme C — a captured `[IMPORTANT]` note is a SELF-CONTAINED body
// (4 KiB cap, thinking excluded), so the BODY always travels with the row and
// "the pointer dangles" is not a CONTENT failure. Item 2/5 then added a
// provenance pointer (session_id / round_index / source_host) to distilled_qa
// — the row became ADDRESSABLE — but the wire type SyncRow carried only
// topic/question/answer/keywords/stored_unix. Three columns on the row, no
// columns on the transport: the classic "row gained a column, transport did
// not" silent loss (memory #8459, measured once already on this repo's task
// ledger where a missing parent_task_id turned a serial chain into N parallel
// rows).
//
// WHY PROVENANCE MUST TRAVEL EVEN UNDER SCHEME C. C removes the CONTENT
// dependency, not the ADDRESSABILITY one: "where did this come from?" is a
// question about the ORIGINATING NODE, and a row that arrives without it can
// never answer. Dropping the three columns at the transport would make the
// receiver's row look like a row nobody captured — a state that is
// indistinguishable from "recorded nothing", which is exactly the silent
// degradation Rule 15 forbids.
//
// AND WHY THE RECEIVER MUST SAY SO EXPLICITLY. The receiver holds the body and
// the pointer, but NOT the pointed-at session log: `~/.gopi/sessions/<sid>/`
// lives on the SENDING host. So a reader on the receiving node must be TOLD
// that the detail is on another machine — never left to discover it as a
// missing file, and never shown a bare pointer that quietly resolves to
// nothing. That explicit sentence is originLocalOnlyRender (below), and the
// predicate that decides it is originLocallyReachable — neither needs a single
// syscall, because under scheme C no reader ever opens a session log to answer
// (Rule 35: this feature adds NO persistent dependency and reads no log).
//
// THE COMPATIBILITY CHANNEL (the 2026-09-21 lesson, memory #8459). A newer
// sender's patch must not kill an older receiver and vice versa:
//
//	field SHAPE  — the three provenance fields ride as one `*origin` JSON
//	               object (syncRowOrigin above), so the patch keeps its three
//	               original row keys byte-for-byte and an older receiver's
//	               encoding/json drops the unknown key harmlessly — it loses
//	               provenance, never the row.
//	COLUMN SHAPE — the export projects through syncOriginColumnExpr /
//	               syncOriginShellExpr, a COALESCE'd, column-PRESENCE-GATED
//	               tail: a DB whose distilled_qa predates item 2/5 (or is a
//	               foreign schema) exports with the three columns ABSENT and
//	               absent IS the same state the zero value means, so it needs
//	               no special case anywhere downstream.
package tools

import (
	"context"
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Constants (Rule 1 / Rule 13: one spelling, referenced everywhere).
// ---------------------------------------------------------------------------

const (
	// syncOriginWireKey is the JSON key of the row's provenance object. It is
	// the SyncRow field's own tag, spelled once here because the compatibility
	// argument above depends on the exact shape (three original keys + ONE new
	// optional key), and a second spelling is how that shape drifts.
	syncOriginWireKey = "origin"

	// syncOriginColumnAbsent is stamped by the COALESCE'd projection when the
	// column does not exist in this DB at all. It is a RECOGNIZABLE value
	// (Rule 15), distinct from '' — which means "the column exists and the row
	// recorded nothing". Both read as "no provenance" at the import seam, but
	// only '' is a claim about the ROW.
	syncOriginColumnAbsent = ""

	// syncOriginSelectSep separates the provenance tail from the four
	// pre-existing export projections. The tail is joined with comma+space so
	// the assembled SELECT stays readable in a log.
	syncOriginSelectSep = ", "
)

// ---------------------------------------------------------------------------
// On the wire — projection, scan and write (the three places, memory #8459).
// ---------------------------------------------------------------------------

// syncOriginColumnsPresent reports WHICH of the three provenance columns this
// DB's distilled_qa actually declares — the one probe the projection, the shell
// projection and the import all read (Rule 14). A column that is absent is not
// an error: it is an older or foreign schema, and absence reads back as
// "nothing recorded" everywhere downstream.
func syncOriginColumnsPresent(ctx context.Context, q QueryExecer) ([]string, error) {
	// The table name is spelled the way every other probe in this package
	// spells it (tableColumns(ctx, db, "distilled_qa")) — one shape at every
	// call site rather than a constant referenced once.
	cols, err := tableColumns(ctx, q, "distilled_qa")
	if err != nil {
		return nil, fmt.Errorf("sync origin: probe distilled_qa: %w", err)
	}
	out := make([]string, 0, len(originColumns))
	for _, c := range originColumns {
		if cols[c] {
			out = append(out, c)
		}
	}
	return out, nil
}

// syncOriginColumnExpr renders the export SELECT's provenance tail, COALESCE'd
// so a NULL never scans as a Go zero by accident and joined into the SAME
// comma-separated projection the four pre-existing columns use. With no columns
// present it returns the literal constant projection (see
// syncAbsentOriginSelectExpr) so the row's scan shape is CONSTANT regardless of
// schema — the compatibility channel: no caller branches on schema.
func syncOriginColumnExpr(present []string) string {
	if len(present) == 0 {
		return syncAbsentOriginSelectExpr
	}
	parts := make([]string, 0, len(originColumns))
	for _, c := range originColumns {
		parts = append(parts, "COALESCE("+c+originSelectDefault(present, c)+")")
	}
	return strings.Join(parts, syncOriginSelectSep)
}

// originSelectDefault supplies the COALESCE fallback for one column: 0 for the
// INTEGER round, ” for the two TEXT columns (the same pairing the column DDL
// declares in memory_origin.go).
func originSelectDefault(present []string, col string) string {
	for _, p := range present {
		if p == col {
			if col == originRoundCol {
				return ",0"
			}
			return ",''"
		}
	}
	// Absent column: the projection substitutes the recognizable absent value
	// in both type shapes so the scan shape never changes.
	if col == originRoundCol {
		return ",0"
	}
	return ",''"
}

// syncAbsentOriginSelectExpr is what the projection substitutes when this DB
// declares NONE of the three columns: three typed constants in originColumns
// order, so an old DB's export produces the same three scan targets as a new
// one's.
var syncAbsentOriginSelectExpr = strings.Join([]string{
	"'" + syncOriginColumnAbsent + "'",
	"0",
	"'" + syncOriginColumnAbsent + "'",
}, syncOriginSelectSep)
