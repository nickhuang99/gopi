package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/tasksource"
	"github.com/nick/gopi/internal/testutil"
)

// This file pins the `gopi task submit --source` gate as an END-TO-END
// contract of the REAL binary, because the defect it closes was invisible to
// every unit test: `--source human` from an agent session exited 0, printed
// `Task queued: … source=agent`, and stored the row as `agent` — the caller's
// assertion silently replaced by the process's derived role, with the real
// rule living only in a source comment while `-h` advertised human as a
// selectable value (Rule 15; measured 2026-09-23).
//
// THE CONTRACT, three legs (the first two are what the fix changes):
//
//	① `--source human` with an agent chain above this process → exit 1, stderr
//	   naming the gate and the GOPI_HUMAN_TERMINAL escape, and NO row written.
//	② `GOPI_HUMAN_TERMINAL=1 … --source human` → exit 0, the row reads human.
//	③ `--source agent|worker|merger|server` inside the same agent session →
//	   each round-trips into the ledger with its OWN value (the regression
//	   floor: the refusal must not have narrowed the four machine roles).
//
// The probe env is built by cmd/agent's own cliContractEnv (an isolated
// GOPI_HOME, every ambient GOPI_*/ANTHROPIC_*/DEEPSEEK_* var dropped — a dev
// shell would otherwise supply a source signature of its own), plus the agent
// signature the spawn guard uses (internal/config's constants, Rule 14).

// sourceGateProbeTimeout is the same budget the family's surface probe uses: a
// correctly routed ledger verb never reaches the agent loop, so a probe that
// parks is a routing defect, not a slow test.
const sourceGateProbeTimeout = 25 // seconds

// sourceGateRoleRowIDs / sendGateStatusRowIDs are the FIXTURE ROW IDS those
// driven-through-the-real-binary subtests submit. They are full uuids, not
// slugs, because the CLI submit face refuses an id no ground name can be
// derived from (TaskSubmitOptions.RefuseUngroundableID, epic 01a12b86):
// formatting a slug at the call site (`"t-gate-role-" + role`) makes the test's
// own seeding submit exit 1. The map is keyed by the ROLE the argv spells, so
// each subtest still owns a distinct row.
var sourceGateRoleRowIDs = map[string]string{
	tasksource.Agent:  "a0fa3366-41b2-5f1f-ace3-b70f42e5525d",
	tasksource.Worker: "2e076c9d-d402-5831-94f8-0fbd896d4976",
	tasksource.Merger: "e3ccf291-71fa-5585-9cca-9fe5db97bd5c",
	tasksource.Server: "cd4ce518-08b2-5b10-bc24-7435c8509bab",
}

// sourceGateRoleRowID resolves the row id for a source role, failing loudly
// (Rule 15) rather than forming a slug id the submit face would refuse.
func sourceGateRoleRowID(t *testing.T, role string) string {
	t.Helper()
	id, ok := sourceGateRoleRowIDs[role]
	if !ok {
		t.Fatalf("no fixture row id for source role %q", role)
	}
	return id
}

// sourceGateProbe is one driven-argv probe's world: an isolated profile, the
// ledger inside it, and the repo the row must resolve.
type sourceGateProbe struct {
	bin  string
	home string
	db   string
	repo string
}

func sourceGateNewProbe(t *testing.T) sourceGateProbe {
	t.Helper()
	p := sourceGateProbe{bin: gopiTestBinPath(t), home: t.TempDir()}
	// Rule 32: the WRITER scaffold owns the database path and pins the env
	// (GOPI_MEMORY_DB + the legacy spelling masked) — never a hand-rolled path.
	p.db = testutil.NewTestDBPath(t)
	// `gopi task submit` verifies --repo (and, omitted, the CWD) as a git work
	// tree BEFORE the row is written, so a probe whose cwd is not a repo never
	// reaches the source gate at all. The probe therefore names this throwaway
	// repo explicitly.
	p.repo = t.TempDir()
	for _, argv := range [][]string{{"init", "-q"}, {"config", "user.email", "gate@example.invalid"}, {"config", "user.name", "gate probe"}} {
		if out, err := exec.Command("git", append([]string{"-C", p.repo}, argv...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %s in %s: %v\n%s", strings.Join(argv, " "), p.repo, err, out)
		}
	}
	return p
}

// sourceGateAgentEnv is the environment a gopi AGENT SESSION's shell carries —
// exactly the signature internal/config documents (an executor session id, the
// hermes session id, and an anchor marker whose pid is an ancestor's, never
// this process's own). It is appended to cliContractEnv's isolated profile.
func sourceGateAgentEnv(home, db string) []string {
	return append(cliContractEnv(home),
		config.MemoryDBEnv+"="+db,
		config.AgentSessionEnv+"=6424c4c1-0adb-48e8-bf43-d6d0baae5e0e",
		config.HermesSessionIDEnv+"=20260923_120000_gate01",
		config.AgentSessionAncestorEnv+"="+strconv.Itoa(os.Getpid()),
	)
}

// sourceGateRun drives the real binary under env and returns its combined
// output and exit code. A timeout is this test's own failure: the verb is
// ledger-routed and must return on its own.
func sourceGateRun(t *testing.T, p sourceGateProbe, env []string, args ...string) (string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), sourceGateProbeTimeout*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, p.bin, args...)
	cmd.Env = env
	cmd.Dir = p.repo
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if ctx.Err() != nil {
			t.Fatalf("gopi %s: TIMEOUT after %ds — argv fell through to the agent loop, or parked on a child\n%s",
				strings.Join(args, " "), sourceGateProbeTimeout, out)
		}
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("gopi %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		code = ee.ExitCode()
	}
	return string(out), code
}

// TestTaskSubmitSourceHumanClaimIsRefused drives leg ① and the refusal's
// CONTENT: exit 1, the gate named, the escape hatch named, and — the strongest
// half — the ledger left with NO row at all (a refusal that still wrote a row
// would be a different, quieter defect).
func TestTaskSubmitSourceHumanClaimIsRefused(t *testing.T) {
	p := sourceGateNewProbe(t)
	env := sourceGateAgentEnv(p.home, p.db)
	const row = "c2f77eb8-1846-54ba-89e8-7932bf0a27cb"

	out, code := sourceGateRun(t, p, env, "task", "submit", "gate probe: human claim", "--source", tasksource.Human, "--task-id", row, "--db", p.db)
	if code != 1 {
		t.Fatalf("`task submit --source %s` from an agent session: exit=%d want 1 (REFUSED)\n%s"+
			"Pre-fix this argv exited 0 and stored source=%s — a silent substitution.",
			tasksource.Human, code, out, tasksource.Agent)
	}
	for _, want := range []string{
		tasksource.Human,          // the claim that was refused
		config.HumanTerminalEnv,   // the escape hatch, by name
		"inside an agent session", // WHY it was refused
		"refusing",                // a refusal, not a warning
		tasksource.Agent,          // the role this process actually is
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the refusal must name %q:\n%s", want, out)
		}
	}
	// No row: the ledger is queried afterwards, through the CLI itself — an
	// unknown task_id is the ledger saying nothing was written.
	shown, showCode := sourceGateRun(t, p, env, "task", "show", row, "--db", p.db)
	if showCode == 0 {
		t.Errorf("a REFUSED submit wrote a row anyway (`task show %s` exit 0):\n%s", row, shown)
	}
	if !strings.Contains(shown, row) {
		t.Errorf("the post-refusal query must answer about the row asked for:\n%s", shown)
	}

	// The pre-fix symptom, pinned as the contrast: without --task-id the old
	// binary printed `Task queued: … source=agent` on rc=0. Assert the fixed
	// run cannot produce either.
	if strings.Contains(out, "Task queued") {
		t.Errorf("a refused submit must not report a queued row:\n%s", out)
	}
}

// TestTaskSubmitSourceHumanWithOperatorEscapeSucceeds drives leg ②: the gate is
// a gate, not a ban. The operator's own GOPI_HUMAN_TERMINAL claim still yields
// a human row — otherwise the fix would have removed the only way a person
// files one through an agent-flavoured shell.
func TestTaskSubmitSourceHumanWithOperatorEscapeSucceeds(t *testing.T) {
	p := sourceGateNewProbe(t)
	env := append(sourceGateAgentEnv(p.home, p.db), config.HumanTerminalEnv+"=1")
	const row = "7c2bed36-b19b-59ae-b102-fe0b8a87d700"

	out := sourceGateRunOK(t, p, env, "task", "submit", "gate probe: operator claim", "--source", tasksource.Human, "--task-id", row, "--db", p.db)
	if !strings.Contains(out, "source="+tasksource.Human) {
		t.Errorf("`%s=1 … --source %s` must store %s:\n%s", config.HumanTerminalEnv, tasksource.Human, tasksource.Human, out)
	}
	shown := sourceGateRunOK(t, p, env, "task", "show", row, "--db", p.db)
	if !strings.Contains(shown, "source:") || !strings.Contains(shown, tasksource.Human) {
		t.Errorf("the stamped row must read %s:\n%s", tasksource.Human, shown)
	}
}

// TestTaskSubmitSourceMachineRolesRoundTrip is the regression floor: the four
// machine roles an agent session legitimately names each keep their OWN value
// in the ledger. A refusal that also swallowed these would be a worse defect
// than the one being fixed, and only an end-to-end run can see it (the values
// are decided by the process signature, not by the flag table).
func TestTaskSubmitSourceMachineRolesRoundTrip(t *testing.T) {
	p := sourceGateNewProbe(t)
	env := sourceGateAgentEnv(p.home, p.db)

	for _, role := range []string{tasksource.Agent, tasksource.Worker, tasksource.Merger, tasksource.Server} {
		t.Run(role, func(t *testing.T) {
			// The id must be a FULL uuid: the CLI submit face refuses a slug
			// (RefuseUngroundableID) because no ground name can be derived from
			// one. A per-role constant keeps each subtest's row its own, and the
			// uuid shape keeps the submit legal — the map is keyed by the role
			// value the argv spells.
			row := sourceGateRoleRowID(t, role)
			out, code := sourceGateRun(t, p, env, "task", "submit", "gate probe: "+role, "--source", role, "--task-id", row, "--db", p.db)
			if code != 0 {
				t.Fatalf("`task submit --source %s`: exit=%d want 0\n%s", role, code, out)
			}
			if !strings.Contains(out, "source="+role) {
				t.Errorf("--source %s must be stored as %s:\n%s", role, role, out)
			}
			shown := sourceGateRunOK(t, p, env, "task", "show", row, "--db", p.db)
			if !strings.Contains(shown, role) {
				t.Errorf("the stamped row must read %s:\n%s", role, shown)
			}
		})
	}
	// The omitted flag keeps its derivation (no refusal leaks into the
	// explicit-typing path): inside an agent session it derives agent.
	out := sourceGateRunOK(t, p, env, "task", "submit", "gate probe: derived", "--task-id", "7bcc9291-3499-5d0f-9dca-7fe1fad0e081", "--db", p.db)
	if !strings.Contains(out, "source="+tasksource.Agent) {
		t.Errorf("an OMITTED --source must still be DERIVED (agent here), never refused:\n%s", out)
	}
}

// sourceGateRunOK runs and requires exit 0.
func sourceGateRunOK(t *testing.T, p sourceGateProbe, env []string, args ...string) string {
	t.Helper()
	out, code := sourceGateRun(t, p, env, args...)
	if code != 0 {
		t.Fatalf("gopi %s: exit %d (want 0)\n%s", strings.Join(args, " "), code, out)
	}
	return out
}
