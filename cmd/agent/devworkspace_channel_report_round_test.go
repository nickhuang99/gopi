package main

// devworkspace_channel_report_round_test.go — the CLOSING ROUND's execution
// context (epic 01a1837e, measured 2026-10-29).
//
// WHAT WAS BROKEN. runAgentRound started the round with whatever environment
// this process happened to carry, and the round therefore re-entered every
// path a fresh `gopi "<prompt>"` takes: no wrapper row id meant a BARE PROMPT
// (internal/agent/loop.go: plainPrompt = taskChildRowID == ""), and a bare
// prompt means (a) the start hook auto-registers a ledger row of its own —
// summary = the briefing's first sentence — and (b) adoptAnnouncedCloneGround
// opens a SECOND clone workspace under that new row's id. That round's own
// close-out then ran the delivery loop, which spawned another closing round.
// Self-sustaining: 39 of the 44 in_progress rows on this ledger, and 70 `[auto]`
// delivery-class rows whose summaries matched the briefing template exactly.
//
// These tests pin the assembled environment, name by name, and they assert the
// NEGATIVE beside the positive: the same reader faces that decide "bare prompt"
// and "auto-register" are driven here, so a future change that stops wiring the
// pair turns this file red rather than silently restoring the loop.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/taskreport"
	"github.com/nickhuang99/gopi/internal/tools"
)

// reportRoundTestRow is the parent row the round reports on. A fixed spelling
// (not a minted uuid) so the assembled path can be compared verbatim.
const reportRoundTestRow = "01a1837e-b32c-7f01-b5f7-ea0c82f8bcea"

// reportRoundEnvValues returns every VALUE the assembled environment carries
// for name, in order. Duplicates are visible to the caller on purpose: a name
// that appears twice is order- and reader-dependent (the child-env contract),
// and the assembly must produce exactly one entry per name.
func reportRoundEnvValues(env []string, name string) []string {
	var out []string
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		if k == name {
			out = append(out, v)
		}
	}
	return out
}

// autoRegistersFromEnv answers, for an ALREADY-ASSEMBLED child environment,
// the question internal/agent's start hook asks of its OWN process: "would this
// run auto-register a ledger row?" It reads the env directly rather than the
// process (the assembly is what is under test, not this process) and judges the
// value with the ONE exported vocabulary — agent.AutoTaskRegisterOffValue, the
// same function autoTaskRegisterEnabledWith honors. Absent means ON, because the
// switch is default-on; only an explicit OFF value turns it off.
func autoRegistersFromEnv(env []string) bool {
	values := reportRoundEnvValues(env, tools.AutoRegisterOffEnv)
	for _, v := range values {
		if agent.AutoTaskRegisterOffValue(v) {
			return false
		}
	}
	return true
}

// TestClosingRoundEnvRegistersNoRowAndKeepsTheReportOnTheParentRow is the
// acceptance test: the OFF pair is really in the assembled child environment,
// and the report destination is the parent row's own session directory.
func TestClosingRoundEnvRegistersNoRowAndKeepsTheReportOnTheParentRow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)

	env, err := cloneRoundChildEnv(os.Environ(), reportRoundTestRow)
	if err != nil {
		t.Fatalf("cloneRoundChildEnv: %v", err)
	}

	// ① THE FIX ITSELF: the OFF pair is present, spelled as the ONE constant
	// internal/agent owns (never re-typed as two halves here).
	off := reportRoundEnvValues(env, envNameOf(agent.AutoTaskRegisterOff))
	if len(off) != 1 {
		t.Fatalf("the round's environment carries %d entries for %s, want exactly 1: the OFF pair %q",
			len(off), envNameOf(agent.AutoTaskRegisterOff), agent.AutoTaskRegisterOff)
	}
	if !agent.AutoTaskRegisterOffValue(off[0]) {
		t.Fatalf("%s=%q does not read as OFF — internal/agent.AutoTaskRegisterOffValue refused it", envNameOf(agent.AutoTaskRegisterOff), off[0])
	}

	// ② The same reader internal/agent uses to decide "auto-register a row on
	// start" must answer OFF for THIS environment. Driving the real predicate
	// is what keeps this test from agreeing with a pair nothing consumes.
	if autoRegistersFromEnv(env) {
		t.Fatalf("a round assembled from this environment would still auto-register a ledger row (the reproduction loop)")
	}

	// ③ The round is NOT a bare prompt: the wrapper row id is the parent's, so
	// internal/agent's plainPrompt is false and adoptAnnouncedCloneGround is
	// skipped (the channel owns the ground and the delivery).
	wrapper := reportRoundEnvValues(env, tools.TaskWrapperEnv)
	if len(wrapper) != 1 || wrapper[0] != reportRoundTestRow {
		t.Fatalf("%s = %q (want exactly one entry, the parent row %q) — a missing or foreign id makes the round a bare prompt that mints its own workspace",
			tools.TaskWrapperEnv, wrapper, reportRoundTestRow)
	}

	// ④ The report goes to the PARENT row's session directory, not to a
	// directory minted for this round.
	wantPath, err := tools.TaskReportPath(reportRoundTestRow)
	if err != nil {
		t.Fatalf("tools.TaskReportPath(%s): %v", reportRoundTestRow, err)
	}
	gotPath := reportRoundEnvValues(env, taskreport.PathEnv)
	if len(gotPath) != 1 || gotPath[0] != wantPath {
		t.Fatalf("%s = %q, want exactly one entry equal to %q", taskreport.PathEnv, gotPath, wantPath)
	}
	if owner := reportRoundEnvValues(env, taskreport.OwnEnv); len(owner) != 1 || owner[0] != reportRoundTestRow {
		t.Fatalf("%s = %q, want exactly one entry equal to the parent row %q", taskreport.OwnEnv, owner, reportRoundTestRow)
	}
	// The destination's PARENT must exist — an exported path whose container is
	// absent is a `cat >` that cannot open (the 2026-10-28 failure).
	if dir := filepath.Dir(gotPath[0]); !strings.Contains(dir, reportRoundTestRow) {
		t.Fatalf("the report destination %q is not inside the parent row's session directory (%q)", gotPath[0], dir)
	}

	// ⑤ An inherited report contract is REPLACED, not duplicated: the round's
	// own destination is the one writer, whatever this process carried.
	inherited := append([]string{
		taskreport.PathEnv + "=/somewhere/else/task-report.md",
		taskreport.OwnEnv + "=" + "01a10000-0000-0000-0000-000000000000",
	}, os.Environ()...)
	env2, err := cloneRoundChildEnv(inherited, reportRoundTestRow)
	if err != nil {
		t.Fatalf("cloneRoundChildEnv(inherited): %v", err)
	}
	if got := reportRoundEnvValues(env2, taskreport.PathEnv); len(got) != 1 || got[0] != wantPath {
		t.Fatalf("an inherited %s survived beside the round's own: %q", taskreport.PathEnv, got)
	}
}

// TestClosingRoundEnvFailsLoudlyWithoutAParentRow pins Rule 15 on the same
// seam: no parent row id means no destination, and the seam REFUSES rather
// than starting a round whose report belongs to nobody.
func TestClosingRoundEnvFailsLoudlyWithoutAParentRow(t *testing.T) {
	env, err := cloneRoundChildEnv(os.Environ(), "  ")
	if err == nil {
		t.Fatalf("an empty parent row id was accepted and produced an environment (%d entries) — the round would report to nobody", len(env))
	}
	if env != nil {
		t.Fatalf("a refused assembly still returned an environment (%d entries)", len(env))
	}
}

// TestSpawnCloneChannelAgentPassesTheAssembledEnv is the test the epic was
// MISSING, and its absence is the reason the fix landed and did nothing.
//
// The three tests above assert the ASSEMBLY (cloneRoundChildEnv) and they were
// green from the first commit — while production still started the round with
// this process's own environment, because the spawn body assigned cmd.Env twice
// and the second assignment (the ground pair, built from os.Environ) discarded
// the first. An assembly test cannot see that: it stops one frame short of the
// seam that decides what the child actually is.
//
// So this test drives the SEAM (cloneRoundCmd, the builder
// spawnCloneChannelAgent runs) and asserts on the environment the child would
// receive — the assembled pairs SURVIVE, the ground pair is present exactly
// once, and no inherited copy of any of them is duplicated. Removing the OFF
// pair from the code path that reaches the command, or putting the
// os.Environ() assignment back, turns this red.
func TestSpawnCloneChannelAgentPassesTheAssembledEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)

	// An INHERITED environment that already carries every name under test, so a
	// seam that falls back to os.Environ() cannot pass by accident: the child
	// it builds must carry the ASSEMBLED values, and exactly one entry each.
	inherited := append([]string{
		tools.TaskGroundDirEnv + "=/stale/ground",
		tools.TaskWrapperEnv + "=" + "01a10000-0000-0000-0000-000000000000",
		envNameOf(agent.AutoTaskRegisterOff) + "=",
		taskreport.PathEnv + "=/stale/report.md",
		taskreport.OwnEnv + "=" + "01a10000-0000-0000-0000-000000000000",
	}, os.Environ()...)

	env, err := cloneRoundChildEnv(inherited, reportRoundTestRow)
	if err != nil {
		t.Fatalf("cloneRoundChildEnv: %v", err)
	}

	const roundDir = "/workspace/the-round-stands-here"
	cmd := cloneRoundCmd(roundDir, "the briefing", env)

	// ① The OFF pair SURVIVED the seam — the whole fix, at the frame that
	// decides it. (This is the assertion that would have caught the defect: the
	// pre-fix body returned an environment with zero entries for this name.)
	off := reportRoundEnvValues(cmd.Env, envNameOf(agent.AutoTaskRegisterOff))
	if len(off) != 1 || !agent.AutoTaskRegisterOffValue(off[0]) {
		t.Fatalf("the command the spawn seam builds carries %d entries for %s (%q), want exactly 1 OFF pair %q — the round would auto-register a ledger row of its own",
			len(off), envNameOf(agent.AutoTaskRegisterOff), off, agent.AutoTaskRegisterOff)
	}
	if autoRegistersFromEnv(cmd.Env) {
		t.Fatalf("the child this seam starts would still auto-register a ledger row (the reproduction loop)")
	}

	// ② The wrapper identity is the PARENT row, exactly once (not the stale
	// inherited id, and not a duplicate pair the reader could take either way).
	if got := reportRoundEnvValues(cmd.Env, tools.TaskWrapperEnv); len(got) != 1 || got[0] != reportRoundTestRow {
		t.Fatalf("%s on the child = %q, want exactly one entry equal to the parent row %q",
			tools.TaskWrapperEnv, got, reportRoundTestRow)
	}

	// ③ The report destination survived too, and it is still the parent row's.
	wantPath, err := tools.TaskReportPath(reportRoundTestRow)
	if err != nil {
		t.Fatalf("tools.TaskReportPath: %v", err)
	}
	if got := reportRoundEnvValues(cmd.Env, taskreport.PathEnv); len(got) != 1 || got[0] != wantPath {
		t.Fatalf("%s on the child = %q, want exactly one entry equal to %q", taskreport.PathEnv, got, wantPath)
	}

	// ④ The ground pair is present ONCE, with the dir the round stands in — the
	// value the pre-fix code wrote, which must now REPLACE the stale inherited
	// one instead of being the only thing left in the field.
	if got := reportRoundEnvValues(cmd.Env, tools.TaskGroundDirEnv); len(got) != 1 || got[0] != roundDir {
		t.Fatalf("%s on the child = %q, want exactly one entry equal to %q", tools.TaskGroundDirEnv, got, roundDir)
	}

	// ⑤ The command itself is the round: the workspace is its working
	// directory and the briefing is its stdin (the two facts a bare
	// environment check would miss).
	if cmd.Dir != roundDir {
		t.Fatalf("the round's working directory = %q, want %q", cmd.Dir, roundDir)
	}
	if cmd.Stdin == nil {
		t.Fatalf("the round's stdin is nil — the briefing never reaches it")
	}

	// ⑥ A seam handed NO environment still starts a working child rather than
	// an empty one (the defensive branch: no production caller does this).
	if cmd2 := cloneRoundCmd(roundDir, "b", nil); len(cmd2.Env) == 0 {
		t.Fatalf("cloneRoundCmd(nil env) produced an EMPTY child environment")
	}
}
