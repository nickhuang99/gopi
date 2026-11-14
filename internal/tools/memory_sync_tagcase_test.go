package tools

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

// Tag tokens the sync-gate case-policy parity test drives the ledger with.
// Named constants (Rule 1b): these ARE persisted token values the gates
// compare, and the test's whole point is that the SQL side and the Go side
// read the same spelling identically.
const (
	syncTagCaseTknBug   = "bug"
	syncTagCaseTknUI    = "ui"
	syncTagCaseTknUpper = "BUG"
	// syncTagCaseTknHostMixed is the real mixed-case token the live ledger
	// carries (a hostname-derived from-<node> tag — ~/.gopi/memory.db,
	// 135/1043 rows). Case policy has to be right for THIS shape, not just
	// for a synthetic "BUG".
	syncTagCaseTknHostMixed = "from-host-b"
	// syncTagCasePeer is a peer whose node name carries uppercase — the
	// from-<peer> gate's mirror direction (a wrong-case channel tag under
	// the old case-folding LIKE escaped silently).
	syncTagCasePeer      = "peerbox-X"
	syncTagCasePeerWrong = "PEERBOX-X"
	// syncTagCaseBystanderPeer is a peer name NO fixture row is tagged
	// with: passing it makes the anti-reflow gate a provable no-op, which
	// is what isolates the partition face from the reflow face.
	syncTagCaseBystanderPeer = "peer-none"
	// syncTagCaseRowUnix is the fixed created_at/updated_at anchor so every
	// fixture row is inside any export window (no time-dependent gates).
	syncTagCaseRowUnix = 1788000000.0
)

// syncTagCaseRow is one raw fixture row: `tags` is written EXACTLY as
// persisted, never re-normalized — the gates must see the mixed-case
// spellings the live ledger already holds.
type syncTagCaseRow struct {
	taskID string
	tags   string
}

// syncTagCaseRows is the fixture. Every row is PENDING (live) so it is
// subject to the anti-reflow gate; the shapes cover exact / comma-bounded /
// suffix / mid-token / uppercase / empty / channel-tagged rows.
func syncTagCaseRows() []syncTagCaseRow {
	return []syncTagCaseRow{
		{"t-bug-exact", syncTagCaseTknBug},
		{"t-bug-bounded", syncTagCaseTknUI + "," + syncTagCaseTknBug + ",docs"},
		{"t-bug-upper", syncTagCaseTknUpper},
		{"t-bug-suffix", syncTagCaseTknBug + "033"},
		{"t-bug-prefix-token", syncTagCaseTknBug + "flow"},
		{"t-bug-midtoken", "debug-" + syncTagCaseTknUI},
		{"t-ui-only", syncTagCaseTknUI},
		{"t-empty-tags", ""},
		{"t-from-peer", SyncTaskTagPrefix + syncTagCasePeer},
		{"t-from-peer-upper", strings.ToUpper(SyncTaskTagPrefix + syncTagCasePeer)},
		{"t-from-host-mixed", syncTagCaseTknHostMixed},
		{"t-from-host-lower", strings.ToLower(syncTagCaseTknHostMixed)},
		{"t-from-host-mixed-bug", syncTagCaseTknHostMixed + "," + syncTagCaseTknBug},
	}
}

// syncTagCaseSeed creates the ledger (via openTasksDB — the production
// first-open path, so the schema is the real one) and writes the fixture
// rows raw.
func syncTagCaseSeed(t *testing.T, dbPath string, rows []syncTagCaseRow) {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("open fixture ledger %s: %v", dbPath, err)
	}
	defer db.Close() //stylegate:ignore test-scope handle closed at fixture end; the temp dir owns the file
	for _, r := range rows {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO tasks (task_id, summary, status, tags, source, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			r.taskID, "case-policy fixture "+r.taskID, taskStatusPending, r.tags,
			"human", syncTagCaseRowUnix, syncTagCaseRowUnix); err != nil {
			t.Fatalf("seed fixture row %s (tags=%q): %v", r.taskID, r.tags, err)
		}
	}
}

// syncTagCaseGoIDs is the GO-side truth for a fixture: the ids whose tags
// pass the stated predicate. This is the reader the SQL gate must agree
// with — hasTag (exact token compare) over splitSources (comma split,
// per-token TrimSpace), the same pair the sync path uses at
// memory_sync_delta.go's conflict/worker-tag readers.
func syncTagCaseGoIDs(rows []syncTagCaseRow, pred func(tags string) bool) []string {
	var out []string
	for _, r := range rows {
		if pred(r.tags) {
			out = append(out, r.taskID)
		}
	}
	sort.Strings(out)
	return out
}

// syncTagCaseScanIDs runs a task_id query and returns the sorted ids.
func syncTagCaseScanIDs(t *testing.T, dbPath, query string, args ...any) []string {
	t.Helper()
	db, err := openTasksDB(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open %s: %v", dbPath, err)
	}
	defer db.Close() //stylegate:ignore test-scope handle
	rows, err := db.QueryContext(context.Background(), query, args...)
	if err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	defer rows.Close() //stylegate:ignore test-scope handle
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan id: %v", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate ids: %v", err)
	}
	sort.Strings(out)
	return out
}

// syncTagCaseShippedPartition returns what the SHIPPED v1 export ships for a
// tag partition (Peer = bystander → the anti-reflow gate cannot contribute).
func syncTagCaseShippedPartition(t *testing.T, dbPath, token string) []string {
	t.Helper()
	patch, _, err := SyncTaskExport(SyncTaskExportOptions{
		DBPath: dbPath, Peer: syncTagCaseBystanderPeer, Tags: token,
	})
	if err != nil {
		t.Fatalf("SyncTaskExport(tags=%q): %v", token, err)
	}
	return syncTagCasePatchIDs(patch)
}

// syncTagCaseShippedPartitionDelta is the same face on the v2 channel — the
// SECOND splice of the shared fragment must behave identically.
func syncTagCaseShippedPartitionDelta(t *testing.T, dbPath, token string) []string {
	t.Helper()
	patch, _, err := SyncTaskExportDelta(SyncTaskDeltaOptions{
		DBPath: dbPath, Peer: syncTagCaseBystanderPeer, Tags: token,
	})
	if err != nil {
		t.Fatalf("SyncTaskExportDelta(tags=%q): %v", token, err)
	}
	return syncTagCasePatchIDs(patch)
}

// syncTagCaseShippedToPeer returns what the shipped v1 export RE-DELIVERS to
// a peer (no tag partition → only the anti-reflow gate can exclude rows).
func syncTagCaseShippedToPeer(t *testing.T, dbPath, peer string) []string {
	t.Helper()
	patch, _, err := SyncTaskExport(SyncTaskExportOptions{DBPath: dbPath, Peer: peer})
	if err != nil {
		t.Fatalf("SyncTaskExport(peer=%q): %v", peer, err)
	}
	return syncTagCasePatchIDs(patch)
}

// syncTagCaseShippedToPeerDelta is the v2 splice of the anti-reflow gate.
func syncTagCaseShippedToPeerDelta(t *testing.T, dbPath, peer string) []string {
	t.Helper()
	patch, _, err := SyncTaskExportDelta(SyncTaskDeltaOptions{DBPath: dbPath, Peer: peer})
	if err != nil {
		t.Fatalf("SyncTaskExportDelta(peer=%q): %v", peer, err)
	}
	return syncTagCasePatchIDs(patch)
}

func syncTagCasePatchIDs(patch *SyncTaskPatch) []string {
	out := make([]string, 0, len(patch.Rows))
	for _, r := range patch.Rows {
		out = append(out, r.TaskID)
	}
	sort.Strings(out)
	return out
}

// syncTagCaseQueryOldLIKE is the PRE-FIX query text, verbatim from the
// merged tree (memory_sync_task.go:310 / :333, memory_sync_delta.go:156 /
// :180): the comma-bounded LIKE whose ASCII case-folding the Go readers do
// not share. It is used ONLY by the red-proof, to show the divergence the
// fix removes — the shipped code no longer contains it.
const syncTagCaseQueryOldLIKE = "(',' || COALESCE(tags,'') || ',') LIKE ?"

// TestSyncTagTokenMatch_SQLGoParity is the acceptance test for the decided
// case policy (devdoc/sync-tag-case-policy.md): for every fixture row the
// SHIPPED SQL gate and the GO token rule must return the SAME population —
// including the mixed-case rows ("BUG", "FROM-PEERBOX-X", from-host-b)
// that the old case-folding LIKE split apart from the readers.
func TestSyncTagTokenMatch_SQLGoParity(t *testing.T) {
	dbPath := taskSyncTestDB(t)
	fixture := syncTagCaseRows()
	syncTagCaseSeed(t, dbPath, fixture)

	// Premise guard: the bystander peer must be mentioned by NO fixture
	// row, or the partition face would silently also be filtered by the
	// anti-reflow gate and the comparison would be meaningless.
	for _, r := range fixture {
		if hasTag(r.tags, SyncTaskTagPrefix+syncTagCaseBystanderPeer) {
			t.Fatalf("fixture row %s is channel-tagged for the bystander peer — partition face would not be isolated", r.taskID)
		}
	}

	// ── Face A: the tag partition gate (Tags=...), both channels ──────────
	// Go truth = pure exact-token membership (hasTag/splitSources).
	partitions := []string{
		syncTagCaseTknBug, syncTagCaseTknUI, syncTagCaseTknUpper,
		syncTagCaseTknBug + "033", "debug-" + syncTagCaseTknUI,
		syncTagCaseTknBug + "flow", syncTagCaseTknHostMixed,
		strings.ToLower(syncTagCaseTknHostMixed), syncTagCasePeer,
	}
	for _, token := range partitions {
		want := syncTagCaseGoIDs(fixture, func(tags string) bool { return hasTag(tags, token) })
		for _, face := range []struct {
			name string
			got  []string
		}{
			{"v1 export (memory_sync_task.go)", syncTagCaseShippedPartition(t, dbPath, token)},
			{"v2 delta (memory_sync_delta.go)", syncTagCaseShippedPartitionDelta(t, dbPath, token)},
		} {
			if diff := syncTagCaseDiff(want, face.got); diff != "" {
				t.Errorf("tag %q: %s diverges from the Go token rule — %s", token, face.name, diff)
			}
		}
		// The uppercase token is the row the OLD gate silently added: pin
		// that its Go set is exactly the uppercase row (so the parity above
		// is not vacuous on this token).
		if token == syncTagCaseTknUpper && len(want) != 1 {
			t.Errorf("tag %q: Go rule must match exactly the uppercase row, got %v", token, want)
		}
	}

	// ── Face B: the anti-reflow gate (Peer=...), both channels ────────────
	// Go truth = "the row does NOT carry the channel token from-<peer>",
	// i.e. exactly hasTag(tags, from-<peer>) — the same discriminator the
	// reader/writer path uses (syncDeltaMergeTags strips from-* by this
	// exact compare; an importer writes it with this exact spelling).
	peers := []string{
		syncTagCasePeer, syncTagCasePeerWrong,
		"host-b", "hostb", syncTagCaseBystanderPeer,
	}
	for _, peer := range peers {
		channelToken := SyncTaskTagPrefix + peer
		want := syncTagCaseGoIDs(fixture, func(tags string) bool { return !hasTag(tags, channelToken) })
		for _, face := range []struct {
			name string
			got  []string
		}{
			{"v1 export (memory_sync_task.go)", syncTagCaseShippedToPeer(t, dbPath, peer)},
			{"v2 delta (memory_sync_delta.go)", syncTagCaseShippedToPeerDelta(t, dbPath, peer)},
		} {
			if diff := syncTagCaseDiff(want, face.got); diff != "" {
				t.Errorf("peer %q: %s diverges from the Go channel rule — %s", peer, face.name, diff)
			}
		}
	}

	// Both DIRECTIONS the epic asked for, stated as positive assertions
	// (not just "no divergence"):
	//  ① over-inclusion: a "BUG"-tagged row must NOT ride the "bug"
	//     partition (the old LIKE shipped it, invisibly to every reader).
	if got := syncTagCaseShippedPartition(t, dbPath, syncTagCaseTknBug); syncTagCaseContains(got, "t-bug-upper") {
		t.Errorf("uppercase row rode the %q partition: %v", syncTagCaseTknBug, got)
	}
	//  ② silent drop: a from-<PEER>-tagged row (wrong case) must STILL be
	//     re-delivered — the old LIKE excluded it, which is the documented
	//     27-row silent-loss class (a row whose channel tag nobody's reader
	//     recognizes must not be swallowed by the gate either).
	if got := syncTagCaseShippedToPeer(t, dbPath, syncTagCasePeer); !syncTagCaseContains(got, "t-from-peer-upper") {
		t.Errorf("wrong-case channel row was dropped instead of shipped: %v", got)
	}
	// …and the exact-case channel row IS excluded (the gate still works).
	if got := syncTagCaseShippedToPeer(t, dbPath, syncTagCasePeer); syncTagCaseContains(got, "t-from-peer") {
		t.Errorf("exact-case channel row reflowed to its own peer: %v", got)
	}
}

// TestSyncTagTokenMatch_OldLIKEDiverges is the RED-PROOF: it re-runs the
// PRE-FIX query text against the same fixture and asserts it DIVERGES from
// the Go token rule — on the very rows the fix targets. If this ever stops
// failing, the parity test above has become vacuous (a case-insensitive
// fixture rebuild would do it) and must be re-derived.
func TestSyncTagTokenMatch_OldLIKEDiverges(t *testing.T) {
	dbPath := taskSyncTestDB(t)
	fixture := syncTagCaseRows()
	syncTagCaseSeed(t, dbPath, fixture)

	// ① Partition direction: the old LIKE folds case, so asking for the
	// UPPERCASE token also returns every lowercase 'bug' row — rows the Go
	// rule rejects. The fix's GLOB does not.
	oldUpper := syncTagCaseScanIDs(t, dbPath,
		`SELECT task_id FROM tasks WHERE `+syncTagCaseQueryOldLIKE+` ORDER BY task_id`,
		"%,%"+syncTagCaseTknUpper+",%")
	goUpper := syncTagCaseGoIDs(fixture, func(tags string) bool { return hasTag(tags, syncTagCaseTknUpper) })
	newUpper := syncTagCaseShippedPartition(t, dbPath, syncTagCaseTknUpper)
	t.Logf("RED-PROOF ① token %q: old LIKE=%d rows %v | Go rule=%d rows %v | fixed GLOB=%d rows %v",
		syncTagCaseTknUpper, len(oldUpper), oldUpper, len(goUpper), goUpper, len(newUpper), newUpper)
	if syncTagCaseDiff(oldUpper, goUpper) == "" {
		t.Fatalf("red-proof ① is vacuous: the old LIKE agreed with the Go rule on %q", syncTagCaseTknUpper)
	}
	if syncTagCaseDiff(newUpper, goUpper) != "" {
		t.Errorf("fixed GLOB still diverges on %q — %s", syncTagCaseTknUpper, syncTagCaseDiff(newUpper, goUpper))
	}
	if !syncTagCaseContains(oldUpper, "t-bug-exact") {
		t.Errorf("red-proof ① expected the old LIKE to fold %q onto the lowercase row, got %v", syncTagCaseTknUpper, oldUpper)
	}

	// ② Anti-reflow direction (the mirror, and the dangerous one): the old
	// LIKE excluded the wrong-case channel row from the peer's push —
	// silently dropping a row the Go readers do not recognize as
	// channel-born. The fix ships it; the Go rule agrees.
	oldShip := syncTagCaseScanIDs(t, dbPath,
		`SELECT task_id FROM tasks WHERE NOT `+syncTagCaseQueryOldLIKE+` ORDER BY task_id`,
		"%,%"+SyncTaskTagPrefix+syncTagCasePeer+",%")
	goShip := syncTagCaseGoIDs(fixture, func(tags string) bool {
		return !hasTag(tags, SyncTaskTagPrefix+syncTagCasePeer)
	})
	newShip := syncTagCaseShippedToPeer(t, dbPath, syncTagCasePeer)
	// Rows the old gate SWALLOWED: in the Go rule's set, absent from the
	// old LIKE's set.
	droppedByOld := syncTagCaseMissing(goShip, oldShip)
	t.Logf("RED-PROOF ② peer %q: old LIKE ships %d | Go rule ships %d | fixed GLOB ships %d (swallowed-by-old: %v)",
		syncTagCasePeer, len(oldShip), len(goShip), len(newShip), droppedByOld)
	if syncTagCaseDiff(oldShip, goShip) == "" {
		t.Fatalf("red-proof ② is vacuous: the old LIKE agreed with the Go channel rule for peer %q", syncTagCasePeer)
	}
	if !syncTagCaseContains(droppedByOld, "t-from-peer-upper") {
		t.Errorf("red-proof ② expected the old LIKE to swallow the wrong-case channel row; swallowed=%v", droppedByOld)
	}
	if syncTagCaseDiff(newShip, goShip) != "" {
		t.Errorf("fixed GLOB still diverges for peer %q — %s", syncTagCasePeer, syncTagCaseDiff(newShip, goShip))
	}
}

// TestSyncTagTokenFragmentShape pins the ONE shared fragment's mechanics:
// the callers' real query strings carry the comma-anchored GLOB, and the
// pattern argument the fix passes is the star-wrapped token — so a future
// edit that reverts to LIKE (or drops an anchor) fails here, not in
// production.
func TestSyncTagTokenFragmentShape(t *testing.T) {
	got := syncTagTokenMatchSQL(syncTagColumn)
	if !strings.Contains(got, " GLOB ?") {
		t.Errorf("shared fragment %q must be a GLOB (LIKE folds ASCII case — the defect)", got)
	}
	if strings.Contains(got, "LIKE") {
		t.Errorf("shared fragment %q reintroduced LIKE", got)
	}
	if want := "(',' || COALESCE(" + syncTagColumn + ",'') || ',') GLOB ?"; got != want {
		t.Errorf("shared fragment = %q, want %q", got, want)
	}
	if got, want := syncTagTokenPattern(syncTagCaseTknBug), "*,"+syncTagCaseTknBug+",*"; got != want {
		t.Errorf("token pattern = %q, want %q", got, want)
	}
	// The anchor is the ONLY reason 'bug' cannot hit 'debug-ui'/'bug033' —
	// assert the pattern the callers pass really carries both commas.
	pat := syncTagTokenPattern(syncTagCaseTknBug)
	if !strings.HasPrefix(pat, "*,") || !strings.HasSuffix(pat, ",*") {
		t.Errorf("token pattern %q lost a comma anchor (substring matching would return)", pat)
	}
}

// TestSyncTagTokenMatch_LiveLedgerCopyParity measures the divergence on the
// real ledger, READ-ONLY: syncTagCaseCopyProbe opens it with mode=ro +
// query_only, so the measurement cannot write to the live ledger (the epic
// forbids that) — no WAL checkpoint, no temp spill. The synthetic parity test
// above is what actually exercises the shipped SQL on a COPY; this test only
// reports what the live data says. Absent live DB → skip, and say so.
func TestSyncTagTokenMatch_LiveLedgerCopyParity(t *testing.T) {
	live := memoryDBPath()
	if _, err := os.Stat(live); err != nil {
		t.Skipf("live ledger %s absent (%v) — the synthetic parity test still covers the policy", live, err)
	}
	rows, err := syncTagCaseCopyProbe(live)
	if err != nil {
		t.Fatalf("live ledger copy %s unmeasurable: %v", live, err)
	}
	t.Logf("live-ledger copy %s: %d rows, LIKE-vs-GLOB divergence for %q = %d row(s) %v",
		live, rows.total, syncTagCaseTknBug, len(rows.reflowDiff), rows.reflowDiff)
	if len(rows.reflowDiff) != 0 {
		t.Errorf("%d row(s) in the live ledger are read differently by LIKE and GLOB — the fix changes their treatment: %v",
			len(rows.reflowDiff), rows.reflowDiff)
	}
}

type syncTagCaseLiveProbe struct {
	total      int
	reflowDiff []string
}

// syncTagCaseCopyProbe reads a ledger read-only (busy_timeout so a
// concurrent writer holding the WAL lock does not fail the measurement;
// query_only so the probe can never write) and reports what the old LIKE
// and the shipped GLOB disagree about (the measurement the devdoc records).
// mode=ro + query_only is what keeps the caller's live-ledger measurement
// non-mutating; the devdoc's per-token table used a cp'd copy for the
// multi-token sweep (this probe answers one token at a time).
func syncTagCaseCopyProbe(path string) (syncTagCaseLiveProbe, error) {
	var out syncTagCaseLiveProbe
	db, err := sql.Open("sqlite", LedgerReadOnlyDSN(path))
	if err != nil {
		return out, fmt.Errorf("open read-only: %w", err)
	}
	defer db.Close() //stylegate:ignore read-only probe handle
	if err := db.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&out.total); err != nil {
		return out, fmt.Errorf("count rows: %w", err)
	}
	query := `SELECT tags FROM tasks WHERE ` + syncTagCaseQueryOldLIKE + ` EXCEPT SELECT tags FROM tasks WHERE ` + syncTagTokenMatchSQL(syncTagColumn)
	rows, err := db.Query(query, "%,"+syncTagCaseTknBug+",%", syncTagTokenPattern(syncTagCaseTknBug))
	if err != nil {
		return out, fmt.Errorf("divergence probe: %w", err)
	}
	defer rows.Close() //stylegate:ignore read-only probe cursor
	for rows.Next() {
		var tags string
		if err := rows.Scan(&tags); err != nil {
			return out, fmt.Errorf("scan divergence row: %w", err)
		}
		out.reflowDiff = append(out.reflowDiff, tags)
	}
	return out, rows.Err()
}

// syncTagCaseDiff renders the symmetric difference between two id sets, or
// "" when they are equal (the only comparison the parity test accepts).
func syncTagCaseDiff(want, got []string) string {
	missing, extra := syncTagCaseMissing(want, got), syncTagCaseMissing(got, want)
	if len(missing) == 0 && len(extra) == 0 {
		return ""
	}
	return fmt.Sprintf("want %v, got %v (missing %v, unexpected %v)", want, got, missing, extra)
}

// syncTagCaseMissing returns the entries of a absent from b (both sorted
// id lists).
func syncTagCaseMissing(a, b []string) []string {
	inB := make(map[string]bool, len(b))
	for _, s := range b {
		inB[s] = true
	}
	var out []string
	for _, s := range a {
		if !inB[s] {
			out = append(out, s)
		}
	}
	return out
}

func syncTagCaseContains(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
