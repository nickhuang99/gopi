// task_ground_name_uuid_test.go — the GROUND NAME IS DERIVED FROM THE ROW'S
// FULL UUID (epic worktree-naming-uuid, 2026-10-07).
//
// TWO PROPERTIES, and the second is the one that needed a measured fixture:
//
//	① A row whose id is NOT a full uuid cannot name a ground. The resolution
//	   REFUSES, by name, before anything is created (Rule 15 — 出声，不静默).
//	   The refusal class the epic measured is `gopi-masterchk` /
//	   `gopi-crelease`: names that carry a hand-written SLUG where a 32-hex
//	   stem belongs. A silent accept would create
//	   `<worktree root>/gopi-masterchk` for a row, and that directory is
//	   indistinguishable from a human's own control worktree.
//
//	② A SHORT-STEM name is never mistaken for a derived one — the prefix
//	   misattribution. The fixture is REAL: the orphan branch
//	   `task/01a113e467767eb9a4c33cb1df1cdfbb` was believed to be "a name built
//	   from the 8-character prefix of another row" until it was measured. It is
//	   not: it is the correct, full, dash-free uuid name of row
//	   01a113e4-6776-7eb9-a4c3-3cb1df1cdfbb, which merely SHARES its first 8
//	   hex characters with row 01a113e4-6730-7b5b-bc35-0278c2c20b8b (its
//	   PARENT). Two real rows, one 8-hex prefix, two different grounds.
//
// WHY ② NEEDS A GUARD AND NOT JUST A COMMENT. The property "full id, so two
// rows never share a ground" is what the stem's own doc comment claims; it is
// exactly the property a future "prettier name" change would break, and the
// breakage is silent — the second row simply FINDS the first row's ground and
// reuses it. The red-proof for ② is the shortened-stem mutation the epic's own
// history describes (`taskGroundIDStem` truncating to the dash-delimited group
// — see that function's comment); with it, this test fails on the collision.

package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/fullid"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/worktreeroots"
)

// The two REAL rows whose 8-character prefixes collide. Kept verbatim (not
// synthesised): the whole point of the fixture is that it is the incident.
const (
	groundPrefixCollisionParent = "01a113e4-6730-7b5b-bc35-0278c2c20b8b"
	groundPrefixCollisionChild  = "01a113e4-6776-7eb9-a4c3-3cb1df1cdfbb"
	// groundOrphanBranch is the ref the child row's run left behind — the
	// 7-commit branch the epic measured. Its stem must equal the child's.
	groundOrphanBranch = "task/01a113e467767eb9a4c33cb1df1cdfbb"
)

// TestGroundNameRefusesANonUUIDRowId is ACCEPTANCE ①: a repo-aware row whose id
// is not a full uuid gets a REFUSAL, and the refusal is loud enough to act on.
//
// The three readings are independent: the resolution errors, nothing is
// created on disk (a refusal that still leaks a directory is not a refusal),
// and the sentence names the id, the shape, and the door.
func TestGroundNameRefusesANonUUIDRowId(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", testGroundDBPath(t))
	root := t.TempDir()
	t.Setenv(worktreeRootEnvNameForTest, root)
	repo := groundTestRepo(t, "name-repo")

	// The measured class: a human's control-worktree slug. `masterchk` and
	// `crelease` are the two this host actually carries.
	for _, slug := range []string{"gopi-masterchk", "gopi-crelease"} {
		if _, err := TaskSubmit(TaskSubmitOptions{
			Summary: "name fixture " + slug, Payload: "work", Repo: repo, TaskID: slug,
		}); err != nil {
			t.Fatalf("TaskSubmit(%s): %v", slug, err)
		}
		got, err := TaskRunGroundFor(slug)
		if err == nil {
			t.Fatalf("row %q has no full uuid, so no ground name can be derived — "+
				"the resolution must REFUSE; it returned %+v instead", slug, got)
		}
		// The directory must NOT exist: the refusal happens before creation.
		if _, statErr := os.Stat(filepath.Join(root, slug)); statErr == nil {
			t.Fatalf("the refusal for %q still created %s — a refusal that leaves a "+
				"directory behind is indistinguishable from a success", slug, filepath.Join(root, slug))
		}
		// Rule 15: the sentence must be actionable — cause, consequence, door.
		// Printed unconditionally: the refusal text IS the deliverable of this
		// test, and a green run that never shows it cannot be reviewed.
		msg := err.Error()
		t.Logf("refusal for %q:\n%s", slug, msg)
		for _, want := range []string{
			slug,                // names the row
			"not a full uuid",   // names the cause
			"prefix/slug shape", // names the consequence
			TaskRepoRepairVerb,  // names the door
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("refusal for %q does not contain %q — an operator cannot act on it:\n%s",
					slug, want, msg)
			}
		}
	}
}

// TestGroundRoleSeparatesDerivedFromHumanControl is the classification half of
// ①, on the three families this host actually carries. It is what makes
// "legal but not ours" expressible: a human's control worktree is NOT the same
// verdict as an unowned residue, and nothing in gopi may attribute one to a row.
func TestGroundRoleSeparatesDerivedFromHumanControl(t *testing.T) {
	derived := TaskGroundDirPrefix + "01a115487cba717091f5430964f8e087"
	cases := []struct {
		name string
		want string
	}{
		{derived, TaskGroundRoleUUIDDerived},
		// The incident's stem: 32 hex, full row id, dash-free ⇒ DERIVED. It
		// only LOOKED short because the row id it belongs to shares 8 chars
		// with its parent's.
		{TaskGroundDirPrefix + "01a113e467767eb9a4c33cb1df1cdfbb", TaskGroundRoleUUIDDerived},
		// The measured control worktrees: legal, human, and NOT a row's ground.
		{TaskGroundDirPrefix + "crelease", TaskGroundRoleHumanControl},
		{TaskGroundDirPrefix + "masterchk", TaskGroundRoleHumanControl},
		// The two the stub census (§1.2.1 of the devdoc) found under the real
		// worktree root, kept there because their `.git` is not a linked-worktree
		// pointer at all. They are in the SAME class as `crelease` by shape, and
		// this test says so rather than leaving the collapse to be discovered.
		{TaskGroundDirPrefix + "fixproof", TaskGroundRoleHumanControl},
		{TaskGroundDirPrefix + "voicerung2", TaskGroundRoleHumanControl},
		// A PREFIX-shaped stem is NEVER derived — this is the assertion that
		// makes the misattribution impossible by construction.
		{TaskGroundDirPrefix + "01a113e4", TaskGroundRoleHumanControl},
		{TaskGroundDirPrefix + "01a11548", TaskGroundRoleHumanControl},
		// Not in the family at all.
		{"", TaskGroundRoleForeign},
		{TaskGroundDirPrefix, TaskGroundRoleForeign},
		{"merge-archives", TaskGroundRoleForeign},
	}
	for _, c := range cases {
		if got := TaskGroundRole(c.name); got != c.want {
			t.Errorf("TaskGroundRole(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestGroundStemIsUUIDDerivedRequiresTheFullLength pins the predicate directly:
// length IS the assertion, because an 8-char prefix and a 32-char stem are both
// lowercase hex and cannot be told apart by character class alone.
func TestGroundStemIsUUIDDerivedRequiresTheFullLength(t *testing.T) {
	full := "01a113e467767eb9a4c33cb1df1cdfbb"
	if !taskGroundStemIsUUIDDerived(full) {
		t.Fatalf("a 32-char dash-free uuid stem must be uuid-derived: %q", full)
	}
	for _, short := range []string{"01a113e4", "01a113e467", strings.Repeat("a", 31)} {
		if taskGroundStemIsUUIDDerived(short) {
			t.Errorf("a %d-character stem %q is a PREFIX, not a derived name", len(short), short)
		}
	}
	// Non-hex at the right length is not a derived name either.
	if taskGroundStemIsUUIDDerived(strings.Repeat("z", taskGroundUUIDStemLen)) {
		t.Error("a 32-char non-hex stem must not read as uuid-derived")
	}
}

// TestGroundPrefixCollisionRowsGetDistinctGrounds is ACCEPTANCE ②: the real
// two-row collision, resolved through the real surface.
//
// Both rows are repo-aware and both are FULL uuids, so both MUST resolve — and
// the two resolutions must land on DIFFERENT directories. A ground shared
// between two rows is the silent failure this pins: the second row would reuse
// the first's worktree and neither run would notice.
func TestGroundPrefixCollisionRowsGetDistinctGrounds(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", testGroundDBPath(t))
	root := t.TempDir()
	t.Setenv(worktreeRootEnvNameForTest, root)
	repo := groundTestRepo(t, "collision-repo")

	for _, id := range []string{groundPrefixCollisionParent, groundPrefixCollisionChild} {
		if _, err := TaskSubmit(TaskSubmitOptions{
			Summary: "collision fixture " + id, Payload: "work", Repo: repo, TaskID: id,
		}); err != nil {
			t.Fatalf("TaskSubmit(%s): %v", id, err)
		}
	}

	parent, err := TaskRunGroundFor(groundPrefixCollisionParent)
	if err != nil {
		t.Fatalf("the parent row is a full uuid — it must resolve: %v", err)
	}
	child, err := TaskRunGroundFor(groundPrefixCollisionChild)
	if err != nil {
		t.Fatalf("the child row is a full uuid — it must resolve: %v", err)
	}

	// ① the two grounds are different directories.
	if parent.Dir == child.Dir {
		t.Fatalf("rows sharing an 8-hex prefix resolved to the SAME ground %s — "+
			"the second run would silently reuse the first's worktree", parent.Dir)
	}
	// ② and different branches, for the same reason.
	if parent.Branch == child.Branch {
		t.Fatalf("the two rows resolved to the same branch %s", parent.Branch)
	}
	// ③ the stems differ, and they differ by MORE than the shared prefix —
	//    i.e. the name was never built from the prefix.
	pStem := strings.TrimPrefix(filepath.Base(parent.Dir), TaskGroundDirPrefix)
	cStem := strings.TrimPrefix(filepath.Base(child.Dir), TaskGroundDirPrefix)
	if pStem == cStem {
		t.Fatalf("both stems are %q", pStem)
	}
	if pStem[:8] != cStem[:8] {
		t.Fatalf("the fixture no longer collides (%q vs %q) — it is not the incident", pStem, cStem)
	}
	if len(pStem) != taskGroundUUIDStemLen || len(cStem) != taskGroundUUIDStemLen {
		t.Fatalf("stems must be %d hex characters (the row's uuid minus dashes), got %d and %d",
			taskGroundUUIDStemLen, len(pStem), len(cStem))
	}
	// ④ each stem IS its own row's id, dash-free — the derivation, verbatim.
	if pStem != strings.ReplaceAll(groundPrefixCollisionParent, "-", "") ||
		cStem != strings.ReplaceAll(groundPrefixCollisionChild, "-", "") {
		t.Fatalf("stems are not the rows' own ids: %q / %q", pStem, cStem)
	}
	// ⑤ the orphan branch's stem is the CHILD's — the measurement that
	//    overturned the prefix reading of that 7-commit branch.
	orphanStem := strings.TrimPrefix(groundOrphanBranch, TaskGroundBranchPrefix)
	if orphanStem != cStem {
		t.Fatalf("%s is not the child row's derived name (child stem %q)", groundOrphanBranch, cStem)
	}
	// ⑥ and it is NOT the parent's: the branch was never a prefix of anything.
	if orphanStem == pStem {
		t.Fatal("the orphan branch's stem equals the parent's")
	}
	// ⑦ the resolution itself never used a prefix: the gate accepts only a
	//    full uuid, so a PREFIX operand cannot reach a ground at all — which
	//    is why the two rows above could not have been conflated even if they
	//    had been addressed by their shared 8 characters.
	sharedPrefix := groundPrefixCollisionChild[:8]
	if !fullid.CanBePrefix(sharedPrefix) {
		t.Fatalf("fixture error: %q is not prefix-shaped", sharedPrefix)
	}
	if fullid.IsUUID(sharedPrefix) {
		t.Fatalf("fixture error: the 8-char chunk %q reads as a full uuid", sharedPrefix)
	}
}

// testGroundDBPath pins a fresh ledger for these tests (Rule 32: the scaffold
// owns the path, never a hand-rolled one in the test body).
func testGroundDBPath(t *testing.T) string {
	t.Helper()
	return testutil.NewTestDBPath(t)
}

// worktreeRootEnvNameForTest is the repo-declared worktree root's own
// precedence layer (worktreeroots.Resolve: env > settings > default) — the
// same override groundFixture uses, taken from its constant rather than
// re-typed (Rule 1b/13).
const worktreeRootEnvNameForTest = worktreeroots.WorktreeRootEnvName
