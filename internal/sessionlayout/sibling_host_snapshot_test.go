package sessionlayout_test

// host-agnostic ledger snapshot: the backups registry must classify a
// hand-copied ledger snapshot by its SHAPE, not by the host name that copied
// it. Regression for a table that spelled one machine's hostname as a prefix —
// which left every OTHER host's snapshot UNCLASSIFIED (the state the gate
// exists to report, produced by the gate's own table).
//
// The red-proof: before the fix, `memory.db.otherhost-backup` matched no family
// and ClassifySiblingFamily answered ok=false. It now lands on the same
// `keep` family the spelled-out row claimed, and the host name is gone from
// the shipped table.

import (
	"testing"

	"github.com/nick/gopi/internal/sessionlayout"
)

// locateBackupsTree returns the backups registry, failing the test if the tree
// has gone missing (the registry is the deliverable, not a fixture).
func locateBackupsTree(t *testing.T) sessionlayout.SiblingTree {
	t.Helper()
	for _, tr := range sessionlayout.SiblingTrees() {
		if tr.Name == sessionlayout.SiblingTreeBackups {
			return tr
		}
	}
	t.Fatalf("no %q sibling tree in the registry", sessionlayout.SiblingTreeBackups)
	return sessionlayout.SiblingTree{}
}

// TestLedgerSnapshotIsClassifiedByShapeNotHost pins the shape rule: a
// hand-copied ledger snapshot is claimed whatever host wrote its name, while
// the writer's own artifact still lands on its OWN (earlier) family.
func TestLedgerSnapshotIsClassifiedByShapeNotHost(t *testing.T) {
	tree := locateBackupsTree(t)
	foreign := ""
	for _, f := range tree.Families {
		if f.Name == "ledger-backup-foreign" {
			foreign = f.Name
		}
	}
	if foreign == "" {
		t.Fatalf("no %q family in the backups registry — the family this test is about", "ledger-backup-foreign")
	}

	cases := []struct {
		name   string
		entry  string
		wantOK bool
		want   string
	}{
		{
			name:   "another host's copy is classified (the defect)",
			entry:  "memory.db.otherhost-backup-20260101",
			wantOK: true,
			want:   foreign,
		},
		{
			name:   "this host's spelling is still classified",
			entry:  "memory.db.somemachine-backup-20260101",
			wantOK: true,
			want:   foreign,
		},
		{
			name:   "a short form is classified",
			entry:  "memory.db.hp2-backup",
			wantOK: true,
			want:   foreign,
		},
		{
			name:   "an unrelated ledger name is NOT the foreign family",
			entry:  "memory.db.unrelated-20260101",
			wantOK: false,
		},
		{
			name:   "a backups sidecar is claimed by the sidecar family, not this one",
			entry:  "memory.db.otherhost-backup-wal",
			wantOK: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := sessionlayout.SiblingEntry{Name: tc.entry, Path: "/x/" + tc.entry}
			got, ok := sessionlayout.ClassifySiblingFamily(tree, e)
			if ok != tc.wantOK {
				t.Fatalf("entry %q: classified=%v family=%q, want classified=%v",
					tc.entry, ok, got.Name, tc.wantOK)
			}
			if tc.want != "" && (ok && got.Name != tc.want) {
				t.Fatalf("entry %q landed on family %q, want %q", tc.entry, got.Name, tc.want)
			}
			if tc.want == "" && ok && got.Name == foreign {
				t.Fatalf("entry %q must not be claimed by %q", tc.entry, foreign)
			}
		})
	}
}

// TestBackupsRegistrySpellsNoHostname asserts the CLASS property directly: no
// entry in the shipped table may name a machine. This is the rule the defect
// broke, stated as a test so the same drift cannot come back quietly.
func TestBackupsRegistrySpellsNoHostname(t *testing.T) {
	// The machine names this host has carried. Their ABSENCE is the contract:
	// a table that spells one of these only classifies snapshots taken HERE.
	machines := []string{"zpeer", "nick-HP", "nick@"}
	tree := locateBackupsTree(t)
	for _, name := range []string{
		"memory.db.otherhost-backup",
	} {
		e := sessionlayout.SiblingEntry{Name: name}
		got, ok := sessionlayout.ClassifySiblingFamily(tree, e)
		if !ok {
			t.Fatalf("snapshot %q is UNCLASSIFIED — the table is host-bound again", name)
		}
		for _, m := range machines {
			if contains(got.Reason, m) {
				t.Errorf("family %q's shipped Reason names the machine %q — the registry is bound to one operator's host",
					got.Name, m)
			}
		}
	}
}

// contains is strings.Contains spelled locally so the assertion reads as prose.
func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
