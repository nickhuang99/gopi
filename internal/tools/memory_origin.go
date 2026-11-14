// Provenance of a distilled memory row (epic 自动记忆, item 2/5).
//
// THE DECISION THIS FILE IMPLEMENTS. The epic's item 1/5 (task
// 01a11df4-abca…, devdoc/auto-memory-candidate.md) ruled for scheme C: a
// captured `[IMPORTANT]` note becomes a SELF-CONTAINED memory row (its own body,
// capped at autoMemoryBodyMaxBytes, thinking excluded) rather than a pointer
// into a session log. C is what makes cross-host sync correct without inventing
// a "detail lives on <host>, unreachable" state — the body and the row are born
// and die together.
//
// A self-contained body does NOT mean provenance is dropped: the row still
// records WHERE it came from, so a reader can walk back to the session that
// produced it. That is what this file adds, and it is exactly the difference
// between ADDRESSABLE and merely STORED (the same Rule 16 argument this package
// already made once for `source_type`):
//
//	session_id   the session the note was captured in
//	round_index  the session's own 1-based round number
//	source_host  the node the row was written on
//
// The three are ONE pointer, so they are read and written through ONE type,
// MemoryOrigin. A pointer that is present but incomplete is REFUSED at the
// write seam (Rule 15: no silent half-provenance) — a row claiming a round with
// no session can never be resolved by a reader.
//
// Rule 1: every column name, default and prefix in this file is a named
// constant, referenced by the schema DDL, the heal, the INSERT, the search
// projection/scan and the renderer alike.
package tools

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// ---------------------------------------------------------------------------
// Constants (Rule 1 / Rule 13: one spelling, referenced everywhere).
// ---------------------------------------------------------------------------

const (
	// The three provenance columns on distilled_qa. They are ADDITIVE: an old
	// or foreign schema simply lacks them and reads back as absent.
	originSessionCol = "session_id"
	originRoundCol   = "round_index"
	originHostCol    = "source_host"

	// originTextDDL is the declared DDL of the two TEXT provenance columns
	// (TEXT DEFAULT '' — the house pattern for this table's additive columns:
	// an ALTER-added column cannot be NOT NULL without a default, and '' reads
	// as "not recorded").
	originTextDDL = "TEXT DEFAULT ''"

	// originRoundDDL is round_index's declared DDL — INTEGER, not TEXT. The
	// round is a NUMBER (sessionevent.RoundIndexBase plus the session's own
	// 0-based iteration), so it is stored and ordered as one; storing it as
	// text would sort "10" before "9".
	originRoundDDL = "INTEGER DEFAULT 0"

	// duplicateColumnErrText is SQLite's answer when a column already exists —
	// the PRAGMA probe's TOCTOU loser reads it and treats it as success. One
	// spelling, referenced by every idempotent ALTER in this package.
	duplicateColumnErrText = "duplicate column name"

	// originSyncNodeEnv is the node-identity override BOTH this column and the
	// sync layer resolve through (internal/config's catalog owns the value;
	// this constant is the package-local spelling of the same variable, so the
	// two never drift).
	originSyncNodeEnv = "GOPI_SYNC_NODE"

	// originHostAbsent is the value stamped when the hostname cannot be read at
	// all. It is a VALUE the reader can recognize (Rule 15), never an empty
	// column: '' reads exactly like a row whose host was never recorded, and
	// "this node cannot name itself" is a different fact.
	originHostAbsent = "unknown-host"

	// originRenderSeparator separates the fields inside one rendered origin
	// segment (`origin=sess=<id> round=<n> host=<h>`).
	originRenderSeparator = " "

	// originFieldSeparator separates each rendered key from its value.
	originFieldSeparator = "="

	// originRenderPrefix opens the optional origin segment of a search hit's
	// meta line. The segment is OMITTED entirely for a row that recorded
	// nothing (an import, a CLI store, a pre-feature row), so the pre-existing
	// render contract stays byte-identical for every such row.
	originRenderPrefix = "origin="

	// originRenderSessionKey / originRenderRoundKey / originRenderHostKey name
	// the three rendered field keys.
	originRenderSessionKey = "sess"
	originRenderRoundKey   = "round"
	originRenderHostKey    = "host"
)

// originColumns is the ordered column set this feature owns — the ONE list the
// heal, the INSERT and the search projection read (Rule 14: a column added in
// one place and forgotten in another is the defect this prevents).
var originColumns = []string{originSessionCol, originRoundCol, originHostCol}

// originColumnDDL returns the (name, DDL) pairs in originColumns order, derived
// from the constants above so the heal and the tests read one definition.
func originColumnDDL() []struct{ name, ddl string } {
	ddl := map[string]string{
		originSessionCol: originTextDDL,
		originRoundCol:   originRoundDDL,
		originHostCol:    originTextDDL,
	}
	out := make([]struct{ name, ddl string }, 0, len(originColumns))
	for _, c := range originColumns {
		out = append(out, struct{ name, ddl string }{name: c, ddl: ddl[c]})
	}
	return out
}

// MemoryOrigin is the provenance pointer a distilled row carries back to the
// session round it was captured from. Its zero value means "nothing recorded" —
// the state of every row written before this feature and of every row whose
// caller knows no session (a CLI store, a sync import).
type MemoryOrigin struct {
	SessionID string `json:"session_id,omitempty"`
	// Round is the session's own 1-based round number (sessionevent.
	// RoundNumberOf applied to the loop's 0-based iteration).
	// sessionevent.RoundIndexUnknown means "no round recorded".
	Round int `json:"round_index,omitempty"`
	// Host is the node the row was written on. Empty means "not recorded" and
	// is resolved at write time by HostOrResolve.
	Host string `json:"source_host,omitempty"`
}

// originPointerOf returns nil for a hit that recorded nothing (so the JSON
// field is omitted entirely and a pre-feature row's contract is unchanged) and
// the value itself otherwise. A session with no round IS a recorded pointer:
// the round is simply absent, and a reader is told so rather than handed a
// fabricated round.
func originPointerOf(o MemoryOrigin) *MemoryOrigin {
	if strings.TrimSpace(o.SessionID) == "" && strings.TrimSpace(o.Host) == "" &&
		o.Round == sessionevent.RoundIndexUnknown {
		return nil
	}
	return &o
}

// None reports whether no provenance at all was recorded.
func (o MemoryOrigin) None() bool {
	return strings.TrimSpace(o.SessionID) == "" && o.Round == sessionevent.RoundIndexUnknown
}

// Complete reports whether the pointer may be STORED. Two shapes are valid:
//
//   - absent altogether (nothing is claimed — a CLI store, an import, a row
//     written before this feature); and
//   - a session, with or without a recorded round. A session whose round was
//     never stamped is an honest row (`round_index` stays RoundIndexUnknown,
//     which every reader already treats as "not recorded"), not a broken one.
//
// A ROUND WITH NO SESSION is refused: the round is a cursor INTO a session, so
// without one it can never be resolved into anything — storing it would be a
// claim no reader can follow (Rule 15).
func (o MemoryOrigin) Complete() bool {
	session := strings.TrimSpace(o.SessionID)
	if session == "" && o.Round == sessionevent.RoundIndexUnknown {
		return true // absent: nothing was claimed
	}
	return session != ""
}

// Refusal names what is missing from a pointer the store will not take (Rule
// 15: the refusal says WHICH half is wrong, so the caller fixes the right one).
func (o MemoryOrigin) Refusal() string {
	return fmt.Sprintf("memory_store: a round (%d) was given with no session_id — a round is a cursor into a session and cannot be resolved without it (pass the session_id, or %d for 'no round recorded')",
		o.Round, sessionevent.RoundIndexUnknown)
}

// HostOrResolve returns the host to stamp on a row: the value the caller gave,
// else this node's ordinary identity (the sync layer's own rule — the
// GOSYNC node override, else the hostname).
func (o MemoryOrigin) HostOrResolve() string {
	if h := strings.TrimSpace(o.Host); h != "" {
		return h
	}
	return OriginHostName()
}

// OriginHostName resolves THIS node's name for the source_host column: the
// node-identity override first (the same override the sync layer honours, so a
// simulated node tags rows with its simulated name and the column agrees with
// the wire), then the hostname. A hostname that cannot be read is reported as
// the recognizable originHostAbsent value, never as an empty column.
func OriginHostName() string {
	if n := strings.TrimSpace(os.Getenv(originSyncNodeEnv)); n != "" {
		return n
	}
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		return originHostAbsent
	}
	return host
}

// ---------------------------------------------------------------------------
// Schema heal (additive, idempotent, concurrency-safe).
// ---------------------------------------------------------------------------

// ensureOriginSchema adds the three provenance columns when missing — the same
// PRAGMA-probe + ALTER pattern as ensureProvenanceSchema / ensureFeedbackSchema
// (zero migration scripts; an old or foreign schema simply lacks the columns and
// reads back as absent). NEVER drops, NEVER rewrites: the pointer is additive
// metadata about a row that already exists.
//
// The probe-then-ALTER window is NOT safe under concurrent first use — two gopi
// processes opening the shared ~/.gopi/memory.db both see a column missing, both
// run the ALTER, and the loser gets "duplicate column name" (measured: the #1145
// incident, TestMemorySearchNonBlocking -race). The loser's error is swallowed
// and ONLY that one — the column exists either way, which is all a heal needs —
// while every other error is returned.
func ensureOriginSchema(ctx context.Context, db *sql.DB) error {
	cols, err := tableColumns(ctx, db, "distilled_qa")
	if err != nil {
		return fmt.Errorf("origin schema: probe distilled_qa: %w", err)
	}
	for _, c := range originColumnDDL() {
		if cols[c.name] {
			continue
		}
		if _, err := db.ExecContext(ctx,
			"ALTER TABLE distilled_qa ADD COLUMN "+c.name+" "+c.ddl); err != nil {
			if !strings.Contains(err.Error(), duplicateColumnErrText) {
				return fmt.Errorf("origin schema: add %s: %w", c.name, err)
			}
		}
	}
	return nil
}

// originHasCols reports whether distilled_qa carries ALL the provenance
// columns (memory_origin.go). A probe error reads as "absent" — the heal that
// follows will add them or report a real failure itself.
func originHasCols(ctx context.Context, db *sql.DB) bool {
	cols, err := tableColumns(ctx, db, "distilled_qa")
	if err != nil {
		return false
	}
	for _, c := range originColumns {
		if !cols[c] {
			return false
		}
	}
	return true
}

// originSelect renders the search projection's optional provenance columns, in
// originColumns order, for the schemas that have them. A legacy/foreign schema
// renders nothing and the scan adapts (the same shape as searchExtraSelect).
func originSelect(cols map[string]bool) string {
	take := []string{}
	for _, c := range originColumns {
		if cols[c] {
			take = append(take, "d."+c)
		}
	}
	if len(take) == 0 {
		return ""
	}
	return ", " + strings.Join(take, ", ")
}

// originRenderSegment renders one hit's provenance into the meta line, or ""
// when the row recorded nothing. A row that carries a session but no host (an
// imported row) renders the fields it has — the segment never invents a value
// it does not have.
func originRenderSegment(o MemoryOrigin) string {
	if o.None() && strings.TrimSpace(o.Host) == "" {
		return ""
	}
	fields := []string{}
	if s := strings.TrimSpace(o.SessionID); s != "" {
		fields = append(fields, originRenderSessionKey+originFieldSeparator+s)
	}
	if sessionevent.RoundIndexRecorded(o.Round) {
		fields = append(fields, fmt.Sprintf("%s%s%d", originRenderRoundKey, originFieldSeparator, o.Round))
	}
	if h := strings.TrimSpace(o.Host); h != "" {
		fields = append(fields, originRenderHostKey+originFieldSeparator+h)
	}
	if len(fields) == 0 {
		return ""
	}
	return originRenderPrefix + strings.Join(fields, originRenderSeparator)
}
