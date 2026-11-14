package main

// cli_version_drift_test.go — `gopi version --drift`, driven as the REAL verb
// through the built binary (never the function): the report is what an operator
// reads, and a unit test of printDeployDrift would not prove the flag is
// reachable from the command line.
//
// THREE properties, and the third is why the flag lives on THIS verb:
//
//	① the flag is REACHABLE and prints the report's labels;
//	② it does not leak into the prompt path, exactly like `gopi version` itself
//	   (the same assertion the sibling test makes — a flag that started an agent
//	   session would be worse than the staleness it reports);
//	③ it creates NO session artifact and NO ledger row: the whole point of the
//	   report is that it is read-only (Rule 35 — the thing that rebuilds after a
//	   landing already ticks; this only lets an operator SEE the distance).

import (
	"os"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/tools"
)

// TestVersionDriftFlagPrintsTheReport drives `version --drift` and asserts the
// report's own labels are on stdout — the surface an operator reads.
func TestVersionDriftFlagPrintsTheReport(t *testing.T) {
	out, _ := runGopiVersionSandboxed(t, CliVerbVersion, VersionDriftFlag)
	for _, want := range []string{
		versionDriftRepoKey,
		versionDriftArtifactKey,
		versionDriftRevisionKey,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("`%s %s` must print %q; output:\n%s", CliVerbVersion, VersionDriftFlag, want, out)
		}
	}
	// The ref labels are the measurement's substance: a report that named no ref
	// would be a report about nothing.
	if !strings.Contains(out, "refs/heads/") {
		t.Errorf("the report must name the refs it measured; output:\n%s", out)
	}
}

// TestVersionDriftFlagLeavesNoArtifact is property ③ — the read-only contract,
// and the reason this is a report rather than a checker: no session JSONL, no
// ledger row, no profile writes (Rule 35: nothing new persists because of it).
func TestVersionDriftFlagLeavesNoArtifact(t *testing.T) {
	_, home := runGopiVersionSandboxed(t, CliVerbVersion, VersionDriftFlag)
	assertNoSessionArtifact(t, VersionDriftFlag, home)
}

// TestVersionDriftFlagDoesNotStartAnAgent is property ② — the same guard the
// version verb itself carries: the flag must short-circuit BEFORE config
// resolution, so a stale-artifact report can never cost an agent session.
func TestVersionDriftFlagDoesNotStartAnAgent(t *testing.T) {
	out, _ := runGopiVersionSandboxed(t, CliVerbVersion, VersionDriftFlag)
	if strings.Contains(out, "unknown flag") {
		t.Fatalf("--drift is not reachable on the version verb: %q", out)
	}
	if strings.Contains(out, "[gopi] session:") {
		t.Fatalf("--drift leaked into the prompt path: %q", out)
	}
}

// TestVersionDriftHelpMentionsTheFlag pins the help text: a flag an operator
// cannot discover from `-h` is a flag that does not exist for them.
func TestVersionDriftHelpMentionsTheFlag(t *testing.T) {
	out, _ := runGopiVersionSandboxed(t, CliVerbVersion, HelpFlagLong)
	if !strings.Contains(out, VersionDriftFlag) {
		t.Fatalf("`%s %s` must document %s; output:\n%s", CliVerbVersion, HelpFlagLong, VersionDriftFlag, out)
	}
}

// TestVersionDriftFlagIsNamedOnce pins the flag to its single spelling
// (Rules 1/13/14): the usage text, the dispatch arm and this test must read ONE
// constant, or a rename silently un-documents the flag.
func TestVersionDriftFlagIsNamedOnce(t *testing.T) {
	if !strings.HasPrefix(VersionDriftFlag, "--") {
		t.Errorf("VersionDriftFlag = %q; a long flag is spelled with two dashes", VersionDriftFlag)
	}
	body, err := os.ReadFile("cli_version.go")
	if err != nil {
		t.Fatal(err)
	}
	// ONE occurrence is the definition itself; any second one is a re-typed
	// literal at a use site, which is the drift this assertion exists to catch.
	want := `"` + VersionDriftFlag + `"`
	if n := strings.Count(string(body), want); n != 1 {
		t.Errorf("the flag's literal appears %d time(s) in cli_version.go, want exactly 1 (its definition); "+
			"every other site must read VersionDriftFlag", n)
	}
}

// TestDeployDriftRefLine_StatesEveryVerdict drives the PRINT function directly,
// because the verdict words are the deliverable: a report that says an artifact
// 284 commits ahead of a ref is "0 behind" is the shipped false-clean, and a
// test that only asserted the tool layer left the print site free to reproduce
// it (measured: a mutation restoring the old "<N> behind" shape passed
// ./internal/tools/ untouched).
func TestDeployDriftRefLine_StatesEveryVerdict(t *testing.T) {
	const tip = "0123456789abcdef0123456789abcdef01234567"
	cases := []struct {
		name string
		ref  tools.DeployDriftRef
		want string
		deny string
	}{
		{
			name: "follows",
			ref:  tools.DeployDriftRef{Ref: "refs/heads/master", Tip: tip, Follows: true},
			want: versionDriftFollowsValue,
		},
		{
			name: "behind says the exact number",
			ref:  tools.DeployDriftRef{Ref: "refs/heads/master", Tip: tip, Behind: 67},
			want: "67 " + versionDriftBehindKey,
		},
		{
			name: "ahead is NOT a zero behind",
			ref:  tools.DeployDriftRef{Ref: "refs/remotes/gitPushcheck/master", Tip: tip, Ahead: 284},
			want: "284 " + versionDriftAheadKey,
			deny: "0 " + versionDriftBehindKey,
		},
		{
			name: "diverged names both distances",
			ref:  tools.DeployDriftRef{Ref: "refs/heads/master", Tip: tip, Ahead: 3, Behind: 5},
			want: versionDriftDivergedValue,
			deny: "0 " + versionDriftBehindKey,
		},
		{
			name: "unmeasured states its reason",
			ref:  tools.DeployDriftRef{Ref: "refs/heads/master", Reason: "this ground cannot resolve the ref"},
			want: versionDriftUnmeasuredValue,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := deployDriftRefLine(tc.ref)
			if !strings.Contains(got, tc.want) {
				t.Errorf("line %q must state %q", got, tc.want)
			}
			if tc.deny != "" && strings.Contains(got, tc.deny) {
				t.Errorf("line %q must NOT say %q — that is the shipped false-clean", got, tc.deny)
			}
		})
	}
}

// --- The SUMMARY sentence must be incapable of contradicting the lines above it.
//
// THE DEFECT (measured 2026-10-29, host host-a, deployed artifact d50f746d7):
// the report printed, in one block,
//
//	refs/heads/master:  follows the ref — the artifact was built from it
//	refs/remotes/origin/master:  1 ahead commits … not stale behind it
//	refs/remotes/gitPushcheck/master:  426 ahead commits … not stale behind it
//	STALE: this machine runs an artifact that does not follow the ref(s) above.
//
// The last line denied the three above it, and `tools.DeployDriftVerdictAhead`'s
// own doc says an AHEAD artifact "is not stale". The summary is now derived from
// `tools.DeployDriftRef.CountsAsStale` — the SAME predicate the per-ref line
// renders — so the two cannot disagree again. These legs drive the print layer,
// not the tool layer: the previous row's red proof measured that a mutation at
// this layer SURVIVES a tool-layer suite (devdoc
// deploy-drift-measures-the-running-artifact.md §4).

// driftRefFor builds one measured entry with the ref spelling the report prints.
func driftRefFor(ref string, follows bool, ahead, behind int, scratch bool) tools.DeployDriftRef {
	return tools.DeployDriftRef{Ref: ref, Tip: "0123456789abcdef0123456789abcdef01234567",
		Follows: follows, Ahead: ahead, Behind: behind, Scratch: scratch}
}

// TestDeployDriftSummary_AheadNeverPrintsStale is the epic's acceptance leg (a)
// at the PRINT layer: artifact == master == origin, an older scratch mirror
// answers AHEAD ⇒ no `STALE:` anywhere in the summary.
func TestDeployDriftSummary_AheadNeverPrintsStale(t *testing.T) {
	d := tools.DeployDrift{Refs: []tools.DeployDriftRef{
		driftRefFor("refs/heads/master", true, 0, 0, false),
		driftRefFor("refs/remotes/origin/master", true, 0, 0, false),
		driftRefFor("refs/remotes/gitPushcheck/master", false, 426, 0, true),
	}}
	got := deployDriftSummary(d)
	if strings.Contains(got, versionDriftStalePrefix) {
		t.Fatalf("the summary must NOT print %q when no ref carries a commit the artifact lacks; got:\n%s",
			versionDriftStalePrefix, got)
	}
	if !strings.Contains(got, versionDriftCleanLine) {
		t.Errorf("a clean verdict must be STATED, not left as silence (Rule 15); got:\n%s", got)
	}
	// And the per-ref line for that scratch entry must agree with the summary.
	line := deployDriftRefLine(d.Refs[2])
	if strings.Contains(line, versionDriftStalePrefix) {
		t.Errorf("the per-ref line must not carry the summary sentence; got: %s", line)
	}
	if !strings.Contains(line, versionDriftScratchNotVoted) {
		t.Errorf("a scratch ref's line must STATE that it does not vote; got: %s", line)
	}
}

// TestDeployDriftSummary_BehindMasterStillPrintsStale is the epic's acceptance
// leg (b) at the PRINT layer — the reverse guarantee: `5 behind master` must
// still produce `STALE:`, and it must NAME the ref that voted so the remedy is
// aimed at the right ref.
func TestDeployDriftSummary_BehindMasterStillPrintsStale(t *testing.T) {
	const ref = "refs/heads/master"
	d := tools.DeployDrift{Refs: []tools.DeployDriftRef{
		driftRefFor(ref, false, 0, 5, false),
		driftRefFor("refs/remotes/origin/master", false, 0, 5, false),
		driftRefFor("refs/remotes/gitPushcheck/master", false, 0, 5, true),
	}}
	got := deployDriftSummary(d)
	if !strings.Contains(got, versionDriftStalePrefix) {
		t.Fatalf("an artifact 5 commits behind %s MUST be reported %q; got:\n%s", ref, versionDriftStalePrefix, got)
	}
	if !strings.Contains(got, ref) {
		t.Errorf("the STALE sentence must NAME the ref that carries the missing commits; got:\n%s", got)
	}
	if strings.Contains(got, "refs/remotes/gitPushcheck/master") {
		t.Errorf("the STALE sentence must not name a scratch ref as a reason; got:\n%s", got)
	}
}

// TestDeployDriftSummary_ScratchRefAloneDoesNotMakeStale pins the SECOND half of
// the fix as its own statement: a scratch mirror that is genuinely BEHIND the
// artifact's revision would otherwise have voted (Behind > 0), and it must not.
func TestDeployDriftSummary_ScratchRefAloneDoesNotMakeStale(t *testing.T) {
	d := tools.DeployDrift{Refs: []tools.DeployDriftRef{
		driftRefFor("refs/heads/master", true, 0, 0, false),
		driftRefFor("refs/remotes/gitPushcheck/master", false, 0, 405, true),
	}}
	if got := deployDriftSummary(d); strings.Contains(got, versionDriftStalePrefix) {
		t.Fatalf("a scratch ref alone must never make the artifact stale; got:\n%s", got)
	}
}
