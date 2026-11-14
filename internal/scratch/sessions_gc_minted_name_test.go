package scratch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The MINTED-SUFFIX arm of the OURS predicate, driven through the REAL
// PlanSessions / SweepSessions pair.
//
// THE MEASURED DEFECT this pins (host host-a, 2026-10-30): the OURS test was
// a bare canonical-uuid test, so the two suffix shapes THIS SYSTEM mints — a
// reopen successor (`<uuid>-r1`, `-r2`) and a clean-room clone (`<uuid>-c1`) —
// read as foreign names and could never be collected at any age:
//
//  4. 01a17a8a-1037-7ae9-a175-1e7e8162fcea-c1   826.0 MiB   0s   keep
//     not a session container name (a session directory is named by a canonical uuid)
//
// 47 directories / 4.0 GiB were frozen that way. The fix widens the NAME test
// and NOTHING else, so this test asserts BOTH directions in one plan: the
// suffixed containers become candidates (nothing else about them changed), while
// the ledger row, the freshness window, the live-process arm and the refusal of
// foreign names are all unmoved.
func TestSessionsGCMintedSuffixContainers(t *testing.T) {
	root := sessionsRoot(t)

	reissue := sessionContainer(t, root, sessGCOldUUID+SessionContainerSuffixSeparator+SessionContainerReissueMarker+"1")
	clone := sessionContainer(t, root, sessGCOldUUID2+SessionContainerSuffixSeparator+SessionContainerCloneMarker+"1")
	ledgerHeld := sessionContainer(t, root, sessGCFreshID)
	foreign := filepath.Join(root, sessGCNotOurs)
	if err := os.MkdirAll(foreign, 0o700); err != nil {
		t.Fatalf("create %s: %v", sessGCNotOurs, err)
	}

	// The near misses: each is a spelling this system does NOT mint, so each
	// must still be refused BY NAME even though it carries a suffix-shaped tail.
	nearMisses := []string{
		sessGCOldUUID + "-r",                    // no ordinal
		sessGCOldUUID + "-r01",                  // a leading zero: not the ordinal spelling
		sessGCOldUUID + "-r1x",                  // trailing junk
		sessGCOldUUID + "-z1",                   // a marker nobody mints
		sessGCOldUUID + "-r1-r2",                // two suffixes: the minters write ONE
		"0f8e2a101111400080000000" + "0000aa01", // a bare 32-hex stem, no dashes
	}
	for _, name := range nearMisses {
		if err := os.MkdirAll(filepath.Join(root, name), 0o700); err != nil {
			t.Fatalf("create near miss %s: %v", name, err)
		}
	}

	withLedger(t, map[string]string{sessGCFreshID: "task " + sessGCFreshID + " is completed"}, nil)

	// AGE every fixture LAST: creating an entry inside a directory updates its
	// own mtime, so aging before the writes would be undone by them.
	for _, dir := range append([]string{reissue, clone, ledgerHeld, foreign}, nearMissPaths(root, nearMisses)...) {
		sessionAged(t, dir)
	}

	plans, err := PlanSessions(root, SessionsGCStaleAge)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	by := sessionPlanByName(plans)

	// ① the two minted-suffix containers are CANDIDATES: the name test no
	// longer refuses them, and nothing else about them changed.
	for _, name := range []string{
		sessGCOldUUID + "-r1",
		sessGCOldUUID2 + "-c1",
	} {
		p, ok := by[name]
		if !ok {
			t.Fatalf("%s is missing from the plan", name)
		}
		if !p.Collectable() {
			t.Fatalf("%s is an aged, unheld, unclaimed container this system minted yet was kept: %s", name, p.Reason)
		}
	}

	// ② a LEDGER ROW still wins: the canonical container with a row is kept,
	// with the ledger reason — recognising the suffix did not widen ownership.
	if p, ok := by[sessGCFreshID]; !ok {
		t.Fatalf("%s is missing from the plan", sessGCFreshID)
	} else if p.Collectable() {
		t.Fatalf("%s is named by a ledger row yet was called collectable", sessGCFreshID)
	} else if !strings.Contains(p.Reason, SessionsKeepLedgerRow) {
		t.Fatalf("the keep reason does not name the ledger row: %q", p.Reason)
	}

	// ③ foreign names and the near misses are still refused BY NAME.
	for _, name := range append([]string{sessGCNotOurs}, nearMisses...) {
		p, ok := by[name]
		if !ok {
			t.Fatalf("%s is missing from the plan", name)
		}
		if p.Collectable() {
			t.Fatalf("%s is not a name this system mints yet was called collectable", name)
		}
		if !strings.Contains(p.Reason, SessionsKeepNotOurs) {
			t.Fatalf("%s was kept for the wrong reason: %q", name, p.Reason)
		}
	}

	// ④ the DESTRUCTIVE arm agrees with the report: exactly the two minted
	// candidates go, and every kept directory is still on disk.
	removed, _, err := SweepSessions(root, SessionsGCStaleAge)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != 2 {
		t.Fatalf("the sweep removed %d entries, want exactly the 2 minted-suffix containers", removed)
	}
	for _, gone := range []string{reissue, clone} {
		if _, serr := os.Stat(gone); serr == nil {
			t.Fatalf("%s was collectable but the sweep left it", gone)
		}
	}
	for _, kept := range append([]string{ledgerHeld, foreign}, nearMissPaths(root, nearMisses)...) {
		if _, serr := os.Stat(kept); serr != nil {
			t.Fatalf("the sweep deleted a directory it had kept: %s: %v", kept, serr)
		}
	}
}

func nearMissPaths(root string, names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, filepath.Join(root, n))
	}
	return out
}

// TestMintedSessionContainerNameShapes pins the NAME predicate itself, so a
// future widening (a third marker, a deeper ordinal) fails HERE by name rather
// than silently in the sweep.
func TestMintedSessionContainerNameShapes(t *testing.T) {
	const id = "01a17a8a-1037-7ae9-a175-1e7e8162fcea"
	cases := []struct {
		name string
		want bool
		why  string
	}{
		{id, true, "a first-run container"},
		{id + "-r1", true, "a reopen successor container"},
		{id + "-r2", true, "a second reopen successor"},
		{id + "-c1", true, "a clean-room clone container"},
		{id + "-c12", true, "a two-digit clone ordinal the mint can produce"},
		{"{" + id + "}", true, "the brace spelling IsUUIDName already accepts"},
		{id + "-r", false, "no ordinal"},
		{id + "-r0", false, "the mint starts at 1"},
		{id + "-r01", false, "a leading zero is not the ordinal spelling"},
		{id + "-r1x", false, "trailing junk"},
		{id + "-x1", false, "a marker nobody mints"},
		{id + "-r1-c1", false, "the minters write ONE suffix"},
		{id + "_r1", false, "the separator is a dash"},
		{id + "-r-1", false, "a negative ordinal"},
		{strings.ReplaceAll(id, "-", ""), false, "the dash-less branch/worktree stem"},
		{"two-rounds-evidence", false, "a hand-made evidence directory"},
		{"t-acc-live", false, "a legacy task container"},
		{"01a15103-8eaa-7836-be3d-3b9659bfd835-r1 ", false, "trailing whitespace"},
	}
	for _, tc := range cases {
		if got := IsMintedSessionContainerName(tc.name); got != tc.want {
			t.Errorf("IsMintedSessionContainerName(%q) = %v, want %v (%s)", tc.name, got, tc.want, tc.why)
		}
	}
}
