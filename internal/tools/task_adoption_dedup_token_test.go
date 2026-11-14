package tools

// Regression guard for the ⑤ same-branch-once dedup (epic: the adoption
// guard must match the merge-conflict tag as a WHOLE TOKEN, never as a
// substring). Before the fix the guard used
// `tags LIKE '%merge-conflict%'`, so a row tagged `merge-conflictz` made the
// guard report a live conflict task for a branch that had none and ③b
// silently skipped dispatching the conflict task the branch needed.
//
// The reader under test is now resolveConflictRowIdentity (task_merge.go),
// which unions a TAG-TOKEN test over the tray of merge-conflict rows with the
// branch's summary identity (F6, devdoc/merge-resolve-effort-audit.md) — the
// table below pins the token half, which is what the reader's payload channel
// inherits; the summary half is pinned by the F6 regression test
// (task_merge_resolve_identity_test.go).
//
// The table pins THREE readings against one fixture each:
//   - the SQL gate's payload channel (resolveConflictRowIdentity) — the
//     reader under test, asked through resolveConflictRowIdentityLive;
//   - the Go token reader the package already trusts for merger-owned rows
//     (resolveRowIsMergerOwned) — the semantics the SQL must not drift from;
//   - the reader/writer payload alignment (taskMergePayloadBranchPrefix vs
//     the line taskMergePayloadBranchLine renders).
//
// SQLite LIKE is case-INSENSITIVE while Go token comparison is not, which is
// why the gate uses GLOB; the "different case" row pins that choice.

import (
	"bytes"
	"context"
	"fmt"
	"github.com/nickhuang99/gopi/internal/heavytest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// adoptionDedupBranchLine renders a briefing line exactly as the writers do
// (adoptionDispatchConflict / taskMergeConflictPayload) — the alignment
// source for the payload assertions.
func adoptionDedupBranchLine(branch, tip string) string {
	return fmt.Sprintf(taskMergePayloadBranchLine, branch, tip)
}

// adoptionDedupSeedRow inserts one live (in_progress) ledger row — the ⑤
// candidate set is non-terminal rows, so the fixture must be live for the
// tag question to be what the gate answers.
func adoptionDedupSeedRow(t *testing.T, id, tags, payload string) {
	t.Helper()
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	now := float64(time.Now().Unix())
	if _, err := db.Exec(
		`INSERT INTO tasks (task_id, summary, status, tags, payload, source, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, "dedup fixture "+id, taskStatusInProgress, tags, payload, "auto", now, now); err != nil {
		t.Fatalf("seed row %s: %v", id, err)
	}
}

func TestAdoptionConflictDedupMatchesTagTokenNotSubstring(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	const branch = "feat/dedup-token"

	tests := []struct {
		name string
		tags string
		// wantLive is the SQL gate's verdict for a row whose payload DOES
		// name the branch — so the verdict is the tag question alone.
		wantLive bool
		// wantGoMergerOwned is resolveRowIsMergerOwned's verdict on the same
		// tags (the package's existing token reader: merge-conflict AND
		// auto-spawn, both exact tokens).
		wantGoMergerOwned bool
	}{
		{
			name:              "exact token alone",
			tags:              taskTagMergeConflict,
			wantLive:          true,
			wantGoMergerOwned: false,
		},
		{
			name:              "exact token last in a comma list",
			tags:              "auto-dispatch," + taskTagMergeConflict,
			wantLive:          true,
			wantGoMergerOwned: false,
		},
		{
			name:              "exact token first in a comma list",
			tags:              taskTagMergeConflict + ",auto-dispatch",
			wantLive:          true,
			wantGoMergerOwned: false,
		},
		{
			name:              "merger-owned pair",
			tags:              taskTagMergeConflict + ",auto-spawn",
			wantLive:          true,
			wantGoMergerOwned: true,
		},
		{
			name:              "mid-token suffix must not match",
			tags:              taskTagMergeConflict + "z",
			wantLive:          false,
			wantGoMergerOwned: false,
		},
		{
			name:              "mid-token prefix must not match",
			tags:              "x" + taskTagMergeConflict,
			wantLive:          false,
			wantGoMergerOwned: false,
		},
		{
			name:              "mid-token embedded in a list must not match",
			tags:              "auto-dispatch," + taskTagMergeConflict + "zz",
			wantLive:          false,
			wantGoMergerOwned: false,
		},
		{
			name:              "mid-token next to a real token",
			tags:              taskTagMergeConflict + "z," + taskTagMergeConflict,
			wantLive:          true,
			wantGoMergerOwned: false,
		},
		{
			name:              "different case must not match (GLOB, not LIKE)",
			tags:              "Merge-Conflict",
			wantLive:          false,
			wantGoMergerOwned: false,
		},
		{
			name:              "unrelated tag",
			tags:              "docs",
			wantLive:          false,
			wantGoMergerOwned: false,
		},
		{
			name:              "empty tags",
			tags:              "",
			wantLive:          false,
			wantGoMergerOwned: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
			adoptionDedupSeedRow(t, "t-dedup-1", tc.tags, adoptionDedupBranchLine(branch, "abc1234"))

			got, err := resolveConflictRowIdentityLive(context.Background(), branch)
			if err != nil {
				t.Fatalf("dedup gate: %v", err)
			}
			if got != tc.wantLive {
				t.Errorf("tags %q: adoptionConflictTaskLive = %v, want %v", tc.tags, got, tc.wantLive)
			}
			if gotGo := resolveRowIsMergerOwned(tc.tags); gotGo != tc.wantGoMergerOwned {
				t.Errorf("tags %q: resolveRowIsMergerOwned = %v, want %v (Go/SQL token agreement)",
					tc.tags, gotGo, tc.wantGoMergerOwned)
			}
			// Invariant keeping the two readers from drifting apart in the
			// direction that matters: every merger-owned row is also a live
			// conflict row for the adoption gate.
			if tc.wantGoMergerOwned && !tc.wantLive {
				t.Errorf("tags %q: merger-owned rows must satisfy the adoption gate", tc.tags)
			}
		})
	}
}

// TestAdoptionConflictDedupIgnoresOtherBranches pins the payload half of the
// guard: a real merge-conflict token for ANOTHER branch is not this branch's
// live conflict row, while a payload rendered by the shared writer IS.
func TestAdoptionConflictDedupIgnoresOtherBranches(t *testing.T) {
	const branch = "feat/dedup-payload"

	tests := []struct {
		name    string
		payload string
		want    bool
	}{
		{
			name:    "writer-rendered line for this branch",
			payload: "Conflict resolution: " + adoptionDedupBranchLine(branch, "abc1234") + " does not merge cleanly into master in repo /repo.\n",
			want:    true,
		},
		{
			name:    "payload names another branch",
			payload: "Conflict resolution: " + adoptionDedupBranchLine("feat/other", "abc1234") + " does not merge cleanly into master in repo /repo.\n",
			want:    false,
		},
		{
			name:    "payload has the branch name without the tip marker",
			payload: "Conflict resolution: branch " + branch + " does not merge cleanly into master.\n",
			want:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
			adoptionDedupSeedRow(t, "t-dedup-payload-1", taskTagMergeConflict, tc.payload)

			got, err := resolveConflictRowIdentityLive(context.Background(), branch)
			if err != nil {
				t.Fatalf("dedup gate: %v", err)
			}
			if got != tc.want {
				t.Errorf("payload %q: adoptionConflictTaskLive = %v, want %v", tc.payload, got, tc.want)
			}
		})
	}
}

// TestAdoptionConflictDedupIgnoresTerminalRows pins the status half: a
// token-tagged row that already reached a terminal end must not block the
// dispatch (only NON-TERMINAL rows are "live").
func TestAdoptionConflictDedupIgnoresTerminalRows(t *testing.T) {
	const branch = "feat/dedup-status"
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	adoptionDedupSeedRow(t, "t-dedup-terminal-1", taskTagMergeConflict,
		"Conflict resolution: "+adoptionDedupBranchLine(branch, "abc1234")+" does not merge cleanly.\n")

	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE tasks SET status = ? WHERE task_id = ?`, taskStatusCompleted, "t-dedup-terminal-1"); err != nil {
		t.Fatalf("terminal flip: %v", err)
	}

	got, err := resolveConflictRowIdentityLive(context.Background(), branch)
	if err != nil {
		t.Fatalf("dedup gate: %v", err)
	}
	if got {
		t.Error("a completed row must not count as a live conflict task (⑤ is non-terminal only)")
	}

	// Sanity: the same row IS live while non-terminal — the flip above is
	// what changed the verdict, not the fixture.
	if _, err := db.Exec(`UPDATE tasks SET status = ? WHERE task_id = ?`, taskStatusPending, "t-dedup-terminal-1"); err != nil {
		t.Fatalf("live flip: %v", err)
	}
	got, err = resolveConflictRowIdentityLive(context.Background(), branch)
	if err != nil {
		t.Fatalf("dedup gate (live): %v", err)
	}
	if !got {
		t.Error("a pending row with the real token and this branch's payload must be live")
	}
}

// TestTaskTagTokenSQLTestIsTokenScoped exercises the shared fragment the gate
// is built from — the same expression, asked directly, so a future rewrite of
// the gate's SQL cannot hide a broken fragment.
func TestTaskTagTokenSQLTestIsTokenScoped(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()

	tests := []struct {
		tags string
		want bool
	}{
		{taskTagMergeConflict, true},
		{"a," + taskTagMergeConflict + ",b", true},
		{taskTagMergeConflict + "z", false},
		{"x" + taskTagMergeConflict, false},
		{"", false},
		{"   ", false},
	}
	for _, tc := range tests {
		var got int
		q := `SELECT COUNT(*) FROM (SELECT ? AS tags) WHERE ` + taskTagTokenSQLTest(taskTagMergeConflict)
		if err := db.QueryRow(q, tc.tags).Scan(&got); err != nil {
			t.Fatalf("tags %q: %v", tc.tags, err)
		}
		if (got > 0) != tc.want {
			t.Errorf("tags %q: fragment verdict = %v, want %v", tc.tags, got > 0, tc.want)
		}
	}

	// The fragment must be a boolean SQL expression (the callers append an
	// alias / use it in WHERE); a syntax break fails here.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) AS n FROM (SELECT '' AS tags) WHERE ` + taskTagTokenSQLTest(taskTagMergeConflict)).Scan(&n); err != nil {
		t.Fatalf("fragment is not a usable SQL expression: %v", err)
	}
}

// stripGoComments removes line comments and blank lines from Go source so the
// Rule 13 scan below judges CODE, not prose. A comment that DOCUMENTS the
// banned idiom (tasks.go's "`tags LIKE '%bug%'` counted rows whose only bug
// was mid-token") is the evidence that the fix happened — flagging it forced
// either a worse comment or a gate that was quietly wrong. Block comments are
// handled by the same line sweep (/^/* … */ lines carry no live code here).
func stripGoComments(src []byte) []byte {
	var out bytes.Buffer
	inBlock := false
	for _, line := range bytes.Split(src, []byte("\n")) {
		trimmed := bytes.TrimSpace(line)
		switch {
		case inBlock:
			if bytes.Contains(trimmed, []byte("*/")) {
				inBlock = false
			}
			continue
		case bytes.HasPrefix(trimmed, []byte("/*")):
			if !bytes.Contains(trimmed, []byte("*/")) {
				inBlock = true
			}
			continue
		case bytes.HasPrefix(trimmed, []byte("//")):
			continue
		}
		out.Write(line)
		out.WriteByte('\n')
	}
	return out.Bytes()
}

// TestStripGoCommentsKeepsTheScanHonest is the red-proof for the scan above:
// the stripper must drop comment text (the false positive that broke the gate
// on master) WITHOUT dropping live code — a real re-inlined `tags LIKE '`
// statement must still be caught.
func TestStripGoCommentsKeepsTheScanHonest(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want bool // true = the needle must survive the strip
	}{
		{"live code is kept", "q := `WHERE tags LIKE '%x%'`\n", true},
		{"line comment is dropped", "\t// was `tags LIKE '%bug%'` before\n", false},
		{"block comment is dropped", "/* tags LIKE '%x%' */\n", false},
		{"code after a comment line", "// explain\ntags LIKE '%x%'\n", true},
	}
	for _, tc := range cases {
		got := bytes.Contains(stripGoComments([]byte(tc.src)), []byte("tags LIKE '%"))
		if got != tc.want {
			t.Errorf("%s: needle survived=%v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestMergeConflictTagHasOneLiteralAndOneTokenMatcher is the Rule 13 gate
// over the package: the tag VALUE lives only at its single const definition
// (taskTagMergeConflict, task_adoption.go) and the TAG-MATCH idiom only in the
// shared fragment (taskTagTokenSQLTest, same file) — every other production
// file must reference them. A future re-inline therefore fails here instead of
// silently reintroducing the substring/mid-token false positive.
func TestMergeConflictTagHasOneLiteralAndOneTokenMatcher(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	// The ONE allowed holder: the file defining the tag value const and the
	// shared SQL fragment.
	const holder = "task_adoption.go"
	// Shapes that must not come back anywhere in production code:
	banned := []struct {
		needle string
		why    string
	}{
		{`"merge-conflict"`, `reference taskTagMergeConflict (defined in ` + holder + `)`},
		{`tags LIKE '%`, `match the tag at TOKEN level via taskTagTokenSQLTest (defined in ` + holder + `)`},
		{`payload LIKE '%`, `derive the payload needle from taskMergePayloadBranchPrefix instead of an inline prefix literal`},
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		if e.Name() == holder {
			continue
		}
		b, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatalf("ReadFile %s: %v", e.Name(), err)
		}
		code := stripGoComments(b)
		for _, bd := range banned {
			if bytes.Contains(code, []byte(bd.needle)) {
				t.Errorf("%s carries %q — %s", e.Name(), bd.needle, bd.why)
			}
		}
	}
}
