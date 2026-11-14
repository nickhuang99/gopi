package tools

// Provenance ON THE WIRE, and the explicit unreachable state — the half of
// memory_sync_origin.go that does NOT belong to the cross-node sync mechanism.
//
// WHY THIS FILE EXISTS (Rule 43: move the general tool out before deleting the
// mechanism around it). `memory_sync_origin.go` was created for the sync epic
// (自动记忆 item 5/5) and the retirement plan deletes it, but a measured build
// experiment (devdoc/sync-retire-deletion-order.md §b3) shows that SEVEN files
// no batch names — memory.go, tasks.go, task_session_stamp.go,
// memory_provenance_latent.go, task_ledger_read_schema_align.go,
// archive_row_reconcile.go, task_merge_backlog_terminal_guard.go — consume
// symbols this file now owns. They are single-node code:
//
//	MemoryHit.MemoryOriginWire (memory.go) renders a search hit's provenance;
//	originLocalOnlyRender (memory.go:5226) appends the "detail only on its own
//	host" sentence to that hit.
//
// So the wire type and the reachability sentence are NOT sync machinery; they
// are what the store's own renderer needs. Leaving them in a file the deletion
// plan removes would make the plan compile only by deleting its consumers too —
// which is the exact failure this reframe exists to stop.
//
// WHAT STAYED BEHIND (genuinely sync-only, and correct to delete with the
// mechanism): the export projection — syncOriginColumnsPresent,
// syncOriginColumnExpr, originSelectDefault, syncAbsentOriginSelectExpr — and
// the constants syncOriginWireKey / syncOriginColumnAbsent / syncOriginSelectSep.
// They build the sync patch's SELECT tail; no single-node reader touches them.
//
// THE WIRE CONTRACT IS UNCHANGED. SyncRowOrigin is a distinct type from
// MemoryOrigin for one reason: this is a WIRE contract (stable JSON keys,
// additive-only) while MemoryOrigin is the store's own value with its own
// completeness rules. Keeping them apart means the wire can gain a field
// without silently changing what the store accepts. (Rule 4: this is a MOVE,
// not a rewrite — every line below is the line that was in
// memory_sync_origin.go.)

import (
	"fmt"
	"strings"
)

const (
	// originLocalOnlyPrefix / originLocalOnlyUnknownHost are the two halves of
	// the explicit unreachable state's sentence. The FIRST half is the epic's
	// own words for it:「细节仅在本机 <host>，不可达」. The second half is what
	// the sender stamps when it could not name itself (originhostbsent) — a
	// reader must not be told "on host unknown-host" as if that were an
	// address, so the sentence says the address is unknown instead.
	originLocalOnlyPrefix      = "detail only on its own host"
	originLocalOnlyUnknownHost = "which did not name itself"

	// originLocalOnlyRenderPrefix opens the paragraph appended to the rendered
	// hit. It is a SEPARATE line from the origin= meta segment on purpose: the
	// meta segment is a compact key=value address, this one is a sentence, and
	// fusing them would put prose inside a field list every other renderer
	// parses.
	originLocalOnlyRenderPrefix = "\n"

	// originLocalOnlyKeys names the three pointer fields in the rendered
	// paragraph so a reader can quote exactly what could not be resolved.
	originLocalOnlyKeys = "session_id/round_index/source_host"
)

// SyncRowOrigin is the provenance pointer AS IT TRAVELS. It is a distinct type
// from MemoryOrigin for one reason: this is a WIRE contract (stable JSON keys,
// additive-only) while MemoryOrigin is the store's own value with its own
// completeness rules. Keeping them apart means the wire can gain a field
// without silently changing what the store accepts.
type SyncRowOrigin struct {
	SessionID string `json:"session_id,omitempty"`
	Round     int    `json:"round_index,omitempty"`
	Host      string `json:"source_host,omitempty"`
}

// none reports whether nothing at all travelled — the state of a patch row from
// an older sender, a row that recorded nothing, or a pre-2/5 column set.
func (o *SyncRowOrigin) none() bool {
	return o == nil || (strings.TrimSpace(o.SessionID) == "" &&
		strings.TrimSpace(o.Host) == "" && o.Round == 0)
}

// memoryOrigin converts the wire value into the store's own pointer. A nil
// pointer (nothing travelled) becomes the zero MemoryOrigin, which the store
// already reads as "nothing recorded" — no special case at the write seam.
func (o *SyncRowOrigin) memoryOrigin() MemoryOrigin {
	if o.none() {
		return MemoryOrigin{}
	}
	return MemoryOrigin{SessionID: o.SessionID, Round: o.Round, Host: o.Host}
}

// originOfWire builds the wire value from a store pointer.
func originOfWire(o MemoryOrigin) *SyncRowOrigin {
	if o.None() && strings.TrimSpace(o.Host) == "" {
		return nil
	}
	return &SyncRowOrigin{SessionID: o.SessionID, Round: o.Round, Host: o.Host}
}

// originLocallyReachable reports whether the session a pointer names could be
// read ON THIS NODE. The criterion is EXACTLY the node identity, never a
// filesystem probe:
//
//	same host  → the session may be here; the pointer is resolvable in
//	             principle and nothing extra is rendered (the origin= segment
//	             already carries the address).
//	other host → the detail is provably NOT reachable from here, and a reader
//	             is handed the explicit sentence instead of a dangling pointer.
//	no host    → a row whose host was never recorded. It cannot be claimed
//	             reachable (nothing says so) and it must not be claimed
//	             unreachable either (nothing says which machine), so this state
//	             is reported as UNKNOWN rather than as either verdict.
//
// A filesystem check would be an invented dependency (Rule 35) AND the wrong
// question: under scheme C no reader ever opens the log, so the only thing that
// can be known — and the only thing the epic asks for — is whether the detail
// belongs to another machine.
type originReachability int

const (
	// originReachLocal: the pointer's host IS this node — nothing to warn
	// about.
	originReachLocal originReachability = iota
	// originReachRemote: the pointer came from another node — the detail is
	// only there, and the reader is told so by name.
	originReachRemote
	// originReachUnknownHost: a pointer with no recorded host. Distinct from
	// remote: there is no address to name.
	originReachUnknownHost
)

// originLocallyReachable classifies one pointer against this node's own name.
// thisHost is passed in (rather than resolved inside) so the classifier is
// testable without an environment (Rule 11's "可测函数").
func originLocallyReachable(o *SyncRowOrigin, thisHost string) originReachability {
	host := ""
	if o != nil {
		host = strings.TrimSpace(o.Host)
	}
	if host == "" {
		return originReachUnknownHost
	}
	if strings.TrimSpace(thisHost) != "" && sameOriginHost(host, thisHost) {
		return originReachLocal
	}
	return originReachRemote
}

// sameOriginHost compares two node names for identity. Case-insensitive: a
// hostname is not case-significant, and the wire value is produced by
// `os.Hostname()` on one node and by GOPI_SYNC_NODE on a simulated one, so
// "peerbox" and "Peerbox" must not read as two machines.
func sameOriginHost(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// originLocalOnlyNote is the EXPLICIT unreachable state: the sentence a reader
// on a node that does not own the session gets, naming the host that does.
// Empty string means "no note is warranted" — the pointer resolves here, or
// there is no pointer at all.
//
// It is a FUNCTION OF THE POINTER AND THIS HOST ONLY. Nothing is read, nothing
// is opened: the sentence is a fact about which machine holds the detail.
func originLocalOnlyNote(o *SyncRowOrigin, thisHost string) string {
	if o.none() {
		return ""
	}
	if strings.TrimSpace(o.SessionID) == "" {
		// A host with no session id says where, not what — the round/session
		// cannot be resolved even on the owning node, so claiming "the detail
		// is there" would be a claim about a pointer that was never complete.
		// (The store refuses such a row at the write seam; a foreign patch that
		// carries one is reported as a pointer with no session.)
		return ""
	}
	switch originLocallyReachable(o, thisHost) {
	case originReachRemote:
		return fmt.Sprintf("%s %q (recorded by another node) — %s %s, unreachable from here",
			originLocalOnlyPrefix, strings.TrimSpace(o.Host), originLocalOnlyKeys, o.SessionID)
	case originReachUnknownHost:
		return fmt.Sprintf("%s (the row recorded no source_host, %s) — %s %s, unreachable from here",
			originLocalOnlyPrefix, originLocalOnlyUnknownHost, originLocalOnlyKeys, o.SessionID)
	default:
		return ""
	}
}

// originLocalOnlyRender renders the note as the paragraph appended to a search
// hit — the surface a reader actually sees. It is deliberately a separate
// paragraph: an itinerary of the meta line's key=value fields would swallow the
// prose that carries the warning.
func originLocalOnlyRender(o *SyncRowOrigin, thisHost string) string {
	note := originLocalOnlyNote(o, thisHost)
	if note == "" {
		return ""
	}
	return originLocalOnlyRenderPrefix + originRenderLocalOnlyMarker + originFieldSeparator + note
}

// originRenderLocalOnlyMarker is the meta-line marker that opens the paragraph,
// so a reader (or a test) can find the warning without parsing English.
const originRenderLocalOnlyMarker = "detail_local_only"
