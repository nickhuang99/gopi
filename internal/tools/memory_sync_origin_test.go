package tools

// Cross-host provenance (epic 自动记忆 item 5/5). What these tests pin, in the
// order the epic's acceptance criteria state them:
//
//	RED  — before this change the transport carried five keys and the three
//	       provenance columns added by item 2/5 never travelled: a row
//	       imported from a peer read back as "nothing recorded", which is
//	       byte-identical to a row nobody captured. The first test asserts the
//	       pointer SURVIVES a real export→import round trip; the second one
//	       goes through a real PATCH FILE, so it exercises the JSON contract
//	       rather than the in-memory value.
//
//	GREEN— the receiver must be TOLD, in a sentence it can act on, that the
//	       detail belongs to another node (Rule 15: no silent dangling). That
//	       sentence is originLocalOnlyNote / originLocalOnlyRender, and the
//	       tests below pin all three verdicts (local / remote / unknown-host)
//	       plus the exact string appearing in a REAL memory search render.
//
//	COMPAT— a DB whose schema predates item 2/5 (and a patch from a sender
//	       that predates it) must keep working: the export projects absent
//	       columns as absent values instead of failing, and an old patch's
//	       missing `origin` key imports as the zero pointer. The legacy fixture
//	       is the SAME one memory_origin_test.go builds by dropping the three
//	       columns off a real schema (Rule 32: never hand-roll a schema).
//
// Rule 32: every DB here comes from syncTestDB() / originTestDB() (the
// scaffold), never a hand-resolved path. Rule 31: the search assertions run the
// REAL MemorySearch entry point, not a helper.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// RED → GREEN: the pointer survives the transport.
// ---------------------------------------------------------------------------

// TestSyncOriginTravelsWithTheRow is the RED test for this item. Before the
// fix, `Origin` did not exist on SyncRow, so the export dropped all three
// columns and the import could not restore them: the peer's row read as
// "nothing recorded" even though the sender had recorded a full pointer.
func TestSyncOriginTravelsWithTheRow(t *testing.T) {
	_ = syncTestDB(t)

	want := MemoryOrigin{SessionID: "sess-abc123", Round: 7, Host: "peer1"}
	if _, err := MemoryStoreWithOrigin("origin topic", "origin travels question", "origin travels answer", "", nil, want); err != nil {
		t.Fatalf("store with origin: %v", err)
	}

	patch, _, err := SyncExport(SyncExportOptions{})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(patch.Rows) != 1 {
		t.Fatalf("want 1 exported row, got %d", len(patch.Rows))
	}
	// ① THE PROJECTION carried it.
	if patch.Rows[0].Origin == nil {
		t.Fatal("RED: the exported row has no origin — the three columns did not reach the wire (projection gap)")
	}
	if got := patch.Rows[0].Origin; got.SessionID != want.SessionID || got.Round != want.Round || got.Host != want.Host {
		t.Fatalf("projected origin = %+v, want %+v", got, want)
	}
}

// TestSyncOriginSurvivesThePatchFile drives the SAME fact through the real
// file format: WriteSyncPatch → SyncImport on a SECOND ledger. This is the leg
// that actually travels between hosts, and it is the one where a missing JSON
// tag (a field that serializes but never parses back) would be invisible to the
// in-memory test above.
func TestSyncOriginSurvivesThePatchFile(t *testing.T) {
	_ = syncTestDB(t)

	want := MemoryOrigin{SessionID: "sess-9f0e", Round: 12, Host: "peer1"}
	if _, err := MemoryStoreWithOrigin("wire topic", "wire question for the peer", "wire answer body", "", []string{"kw1", "kw2", "kw3"}, want); err != nil {
		t.Fatalf("store with origin: %v", err)
	}
	patch, _, err := SyncExport(SyncExportOptions{})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	// The JSON contract: the pointer rides as ONE optional object, and the five
	// original row keys are untouched (that shape is the compatibility channel
	// memory_sync_origin.go documents).
	raw, err := json.Marshal(patch.Rows[0])
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	if _, ok := keys[syncOriginWireKey]; !ok {
		t.Fatalf("RED: the row's JSON carries no %q key: %s", syncOriginWireKey, raw)
	}

	dir := t.TempDir()
	patchPath := filepath.Join(dir, "patch.json")
	if err := WriteSyncPatch(patch, patchPath); err != nil {
		t.Fatalf("write patch: %v", err)
	}

	// A SECOND, EMPTY ledger is the peer. Its environment is switched only
	// after the export above was read off the sender's.
	peerDir := t.TempDir()
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(peerDir, "memory.db"))
	t.Setenv("GOPI_HOME", peerDir)

	stats, err := SyncImport(patchPath)
	if err != nil {
		t.Fatalf("import on the peer: %v", err)
	}
	if stats.RowsImported != 1 {
		t.Fatalf("peer imported %d rows, want 1 (%+v)", stats.RowsImported, stats)
	}

	db := openRawDB(t, memoryDBPath())
	defer db.Close()
	var session, host string
	var round int
	if err := db.QueryRowContext(t.Context(),
		"SELECT "+originSessionCol+", "+originRoundCol+", "+originHostCol+
			" FROM distilled_qa WHERE question = ?", "wire question for the peer").
		Scan(&session, &round, &host); err != nil {
		t.Fatalf("read the imported row back: %v", err)
	}
	if session != want.SessionID || round != want.Round || host != want.Host {
		t.Fatalf("RED: the imported row's pointer = (session=%q round=%d host=%q), want (%q %d %q) — provenance was dropped at the transport",
			session, round, host, want.SessionID, want.Round, want.Host)
	}
}

// ---------------------------------------------------------------------------
// The explicit unreachable state (deliverable (b)).
// ---------------------------------------------------------------------------

// TestOriginLocalOnlyNote pins all three verdicts of the classifier, including
// the two that must NOT produce a note: the same host (the pointer resolves
// here) and no pointer at all (nothing to say).
func TestOriginLocalOnlyNote(t *testing.T) {
	remote := &SyncRowOrigin{SessionID: "sess-remote", Round: 3, Host: "peer1"}
	local := &SyncRowOrigin{SessionID: "sess-local", Round: 3, Host: "hp"}
	noHost := &SyncRowOrigin{SessionID: "sess-nohost", Round: 3}

	cases := []struct {
		name     string
		origin   *SyncRowOrigin
		thisHost string
		wantNote bool
		wantSubs []string
	}{
		{
			name: "remote host is unreachable and is NAMED", origin: remote, thisHost: "hp",
			wantNote: true,
			wantSubs: []string{originLocalOnlyPrefix, `"peer1"`, remote.SessionID},
		},
		{
			name: "same host resolves here — no note", origin: local, thisHost: "hp",
		},
		{
			name: "host comparison is case-insensitive", origin: local, thisHost: "HP",
		},
		{
			name:   "a pointer with no host is reported as unknown, not as an address",
			origin: noHost, thisHost: "hp", wantNote: true,
			wantSubs: []string{originLocalOnlyPrefix, originLocalOnlyUnknownHost, noHost.SessionID},
		},
		{
			name: "no pointer at all — no note", origin: nil, thisHost: "hp",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			note := originLocalOnlyNote(tc.origin, tc.thisHost)
			if tc.wantNote && note == "" {
				t.Fatal("expected the explicit unreachable sentence, got nothing (Rule 15: silent dangling)")
			}
			if !tc.wantNote && note != "" {
				t.Fatalf("expected no note, got %q", note)
			}
			for _, sub := range tc.wantSubs {
				if !strings.Contains(note, sub) {
					t.Errorf("note %q is missing %q", note, sub)
				}
			}
		})
	}
}

// TestOriginLocalOnlyRenderCarriesAMarker: the rendered form must be findable
// without parsing English prose — a reader (and this test) locates it by the
// marker constant.
func TestOriginLocalOnlyRenderCarriesAMarker(t *testing.T) {
	o := &SyncRowOrigin{SessionID: "sess-render", Round: 1, Host: "peer1"}
	got := originLocalOnlyRender(o, "hp")
	if !strings.Contains(got, originRenderLocalOnlyMarker) {
		t.Fatalf("render missing the %q marker: %q", originRenderLocalOnlyMarker, got)
	}
	if !strings.Contains(got, "peer1") || !strings.Contains(got, "sess-render") {
		t.Fatalf("render must name the host and the pointer: %q", got)
	}
	if seg := originLocalOnlyRender(o, "peer1"); seg != "" {
		t.Fatalf("the owning node must render no note, got %q", seg)
	}
}

// ---------------------------------------------------------------------------
// The state appears on the REAL render surface.
// ---------------------------------------------------------------------------

// TestImportedOriginRendersLocalOnlyOnThePeer is the end-to-end assertion of
// the epic's acceptance criterion: a row captured on ANOTHER node, arriving by
// patch, is rendered by this node's REAL search with the explicit
// "detail only on its own host <host>" paragraph. The host stamped on the row
// is deliberately NOT this node's name, so the test does not depend on which
// machine it runs on.
func TestImportedOriginRendersLocalOnlyOnThePeer(t *testing.T) {
	_ = syncTestDB(t)

	foreignHost := "some-other-node"
	if strings.EqualFold(OriginHostName(), foreignHost) {
		t.Skip("this node IS named the synthetic foreign host; pick another name")
	}
	if _, err := MemoryStoreWithOrigin("peer topic", "peer captured question",
		"peer captured answer body", "", nil,
		MemoryOrigin{SessionID: "sess-peer-1", Round: 4, Host: foreignHost}); err != nil {
		t.Fatalf("store the foreign row: %v", err)
	}

	out, err := MemorySearch("peer captured question", 5, "")
	if err != nil {
		t.Fatalf("MemorySearch: %v", err)
	}
	if !strings.Contains(out, originRenderLocalOnlyMarker) {
		t.Fatalf("the render does not carry the %q marker — the unreachable state is silent:\n%s",
			originRenderLocalOnlyMarker, out)
	}
	if !strings.Contains(out, foreignHost) {
		t.Fatalf("the render does not NAME the host holding the detail:\n%s", out)
	}
	if !strings.Contains(out, originLocalOnlyPrefix) {
		t.Fatalf("the render does not carry the explicit unreachable sentence:\n%s", out)
	}
	if !strings.Contains(out, "peer captured answer body") {
		t.Fatalf("the self-contained BODY must still be searchable in full (scheme C):\n%s", out)
	}
}

// TestLocalOriginRendersNoLocalOnlyNote is the other half: an ordinary local
// row must NOT be made to look foreign, i.e. the paragraph is a fact about the
// pointer's host, never an unconditional addition to every hit.
func TestLocalOriginRendersNoLocalOnlyNote(t *testing.T) {
	_ = syncTestDB(t)

	if _, err := MemoryStoreWithOrigin("local topic", "local captured question",
		"local captured answer body", "", nil,
		MemoryOrigin{SessionID: "sess-local-1", Round: 5, Host: OriginHostName()}); err != nil {
		t.Fatalf("store the local row: %v", err)
	}
	out, err := MemorySearch("local captured question", 5, "")
	if err != nil {
		t.Fatalf("MemorySearch: %v", err)
	}
	if strings.Contains(out, originRenderLocalOnlyMarker) {
		t.Fatalf("a row captured on THIS node must not be labelled unreachable:\n%s", out)
	}
	if !strings.Contains(out, originRenderPrefix) {
		t.Fatalf("the ordinary origin= segment must still render:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// Compatibility channel: an old schema and an old patch.
// ---------------------------------------------------------------------------

// TestExportSucceedsOnAPreOriginSchema: the projection is built from a column
// probe, so a ledger whose distilled_qa predates item 2/5 exports its rows
// instead of dying on `no such column` — the old receiver's failure mode this
// channel exists to avoid. The rows still travel; only the pointer is absent,
// which is exactly what the absent columns mean.
func TestExportSucceedsOnAPreOriginSchema(t *testing.T) {
	path := originTestDB(t)
	originLegacyDB(t, path)
	t.Setenv(memoryDBEnv, path)

	// The legacy fixture keeps the rows it was seeded with — read one back so
	// the assertion is about a row that really exists.
	db := openRawDB(t, path)
	defer db.Close()
	var q string
	if err := db.QueryRowContext(t.Context(), "SELECT question FROM distilled_qa LIMIT 1").Scan(&q); err != nil {
		t.Fatalf("read a legacy row: %v", err)
	}

	patch, _, err := SyncExport(SyncExportOptions{})
	if err != nil {
		t.Fatalf("export from a pre-origin schema must succeed (compatibility channel): %v", err)
	}
	if len(patch.Rows) == 0 {
		t.Fatal("the legacy DB's rows did not export")
	}
	for _, r := range patch.Rows {
		if r.Origin != nil {
			t.Fatalf("a DB with no provenance columns produced a pointer: %+v", r.Origin)
		}
	}
}

// TestImportOfAPreOriginPatchStoresNoPointer: a patch from an older sender
// carries no `origin` key at all. The import must store the row with the zero
// pointer — absent stays absent — rather than failing or inventing one.
func TestImportOfAPreOriginPatchStoresNoPointer(t *testing.T) {
	_ = syncTestDB(t)

	// Hand-written to the OLD format: the five original keys, no origin. This
	// is the shape memory_sync_origin.go's compatibility argument claims an
	// older sender produces — so it is asserted against the real parser.
	oldPatch := `{"version":1,"node":"old-node","exported_at":1,"rows":[
		{"topic":"old topic","question":"old sender question","answer":"old sender answer"}]}`
	dir := t.TempDir()
	patchPath := filepath.Join(dir, "old.json")
	if err := os.WriteFile(patchPath, []byte(oldPatch), 0o644); err != nil {
		t.Fatal(err)
	}

	peerDir := t.TempDir()
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(peerDir, "memory.db"))
	t.Setenv("GOPI_HOME", peerDir)

	stats, err := SyncImport(patchPath)
	if err != nil {
		t.Fatalf("import an old-format patch: %v", err)
	}
	if stats.RowsImported != 1 {
		t.Fatalf("imported %d rows, want 1 (%+v)", stats.RowsImported, stats)
	}
	db := openRawDB(t, memoryDBPath())
	defer db.Close()
	var session, host string
	var round int
	if err := db.QueryRowContext(t.Context(),
		"SELECT "+originSessionCol+", "+originRoundCol+", "+originHostCol+
			" FROM distilled_qa WHERE question = ?", "old sender question").
		Scan(&session, &round, &host); err != nil {
		t.Fatalf("read the imported row back: %v", err)
	}
	if session != "" || host != "" || round != 0 {
		t.Fatalf("an old patch must import as 'nothing recorded', got session=%q round=%d host=%q", session, round, host)
	}
}

// TestImportRefusesAHalfPointerFromAPeer: a foreign patch claiming a round with
// no session is a claim no reader can follow, so it is refused (Rule 15). This
// pins that the new transport does NOT bypass that seam, and — the part that
// matters operationally — that the refusal is a PER-ROW verdict:
//
//	the bad row is REFUSED and COUNTED, its neighbours in the same patch still
//	land, and no row is stored for the malformed pointer.
//
// The alternative (letting memoryStoreRow's error abort the whole patch) was
// rejected on purpose: one misbuilt sender's row would kill the entire sync
// link between two nodes until a human fixed the far side, which is exactly the
// "one row takes the whole channel down" failure this channel exists to avoid.
// The refusal is still loud — it names the row and the reason in stats.Errors.
func TestImportRefusesAHalfPointerFromAPeer(t *testing.T) {
	_ = syncTestDB(t)

	// ONE bad row surrounded by two GOOD ones: the neighbours are what prove
	// the refusal is per-row rather than a whole-patch abort.
	halfPatch := `{"version":1,"node":"peer","exported_at":1,"rows":[
		{"topic":"good1","question":"good neighbour one","answer":"body one"},
		{"topic":"half","question":"half pointer question","answer":"half pointer answer",
		 "origin":{"round_index":9,"source_host":"peer1"}},
		{"topic":"good2","question":"good neighbour two","answer":"body two"}]}`
	dir := t.TempDir()
	patchPath := filepath.Join(dir, "half.json")
	if err := os.WriteFile(patchPath, []byte(halfPatch), 0o644); err != nil {
		t.Fatal(err)
	}

	peerDir := t.TempDir()
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(peerDir, "memory.db"))
	t.Setenv("GOPI_HOME", peerDir)

	stats, err := SyncImport(patchPath)
	if err != nil {
		t.Fatalf("one malformed pointer must not abort the whole patch: %v", err)
	}
	if stats.RowsRefused != 1 {
		t.Fatalf("RowsRefused = %d, want 1 (a round with no session is unreadable): %+v", stats.RowsRefused, stats)
	}
	if stats.RowsImported != 2 {
		t.Fatalf("RowsImported = %d, want 2 — the bad row's neighbours must still land: %+v", stats.RowsImported, stats)
	}
	// The refusal is ATTRIBUTED (Rule 15: silence is the defect).
	if len(stats.Errors) != 1 || !strings.Contains(stats.Errors[0], originSessionCol) {
		t.Fatalf("the refusal must name the reason (%s): %v", originSessionCol, stats.Errors)
	}
	db := openRawDB(t, memoryDBPath())
	defer db.Close()
	var n int
	if err := db.QueryRowContext(t.Context(),
		"SELECT count(*) FROM distilled_qa WHERE question = ?", "half pointer question").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("a half pointer was stored — the refusal must be a provable no-op")
	}
	var stored int
	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM distilled_qa").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != 2 {
		t.Fatalf("stored %d rows, want exactly the 2 good ones", stored)
	}
}

// TestOriginWireRoundTripsThroughJSON pins the conversion pair directly,
// including the two shapes that must stay absent: nil (an older sender) and the
// zero value (a row that recorded nothing).
func TestOriginWireRoundTripsThroughJSON(t *testing.T) {
	// Absent shapes both convert to the zero store pointer.
	for _, o := range []*SyncRowOrigin{nil, {}} {
		if got := o.memoryOrigin(); !got.None() || got.Host != "" || got.Round != 0 {
			t.Fatalf("absent wire origin %+v became %+v, want the zero MemoryOrigin", o, got)
		}
	}
	full := &SyncRowOrigin{SessionID: "s", Round: 2, Host: "h"}
	if got := full.memoryOrigin(); got.SessionID != "s" || got.Round != 2 || got.Host != "h" {
		t.Fatalf("round trip lost a field: %+v", got)
	}
	// A store value that recorded nothing emits no wire pointer at all.
	if got := originOfWire(MemoryOrigin{}); got != nil {
		t.Fatalf("a row that recorded nothing emitted %+v, want nil", got)
	}
}
