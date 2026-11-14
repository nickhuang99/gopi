package main

// Regression for the PARSER half of `gopi task update --force` (epic
// 01a17dcd). The flag is the CLI face of the operator's delivery-proof escape;
// the write path's own behaviour is pinned in internal/tools
// (task_delivery_force_test.go). What can only be tested HERE is that the
// command line the operator types is read the way it was meant:
//
//   - `--force` is ADMITTED by `task update` (and only by it — the flag table
//     is per-verb, so a sibling verb must refuse it by name);
//   - it is VALUELESS, so `--force` does not EAT the token after it (a bare
//     switch that swallowed `--status`'s value would corrupt the command line
//     it was typed on);
//   - it is NOT an arm of its own: `--force` with no status/repo/payload still
//     asks for nothing, exactly as before the flag existed.

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/cliverbs"
	"github.com/nickhuang99/gopi/internal/taskverbs"
)

// TestTaskUpdateForceFlagIsAdmittedAndValueless is the four-site check the
// flag's own constant documents (Rule 1b/13): admission table, parser, help
// page and the dispatch arm must agree on ONE spelling, and cliverbs is the
// spelling all four reference.
func TestTaskUpdateForceFlagIsAdmittedAndValueless(t *testing.T) {
	if taskForceFlagName != cliverbs.TaskUpdateForceWord {
		t.Fatalf("cmd/agent's flag name %q must alias cliverbs.TaskUpdateForceWord %q — two spellings of one flag "+
			"is the drift Rule 13/14 bans", taskForceFlagName, cliverbs.TaskUpdateForceWord)
	}
	if !taskFlagMember(taskverbs.Update, taskForceFlagName) {
		t.Fatalf("`task update --%s` is not in the admission table — the flag the help page advertises is unreachable",
			taskForceFlagName)
	}

	// VALUELESS: the token after `--force` must stay a FLAG, not become its
	// value. Parsing `--force --status completed` must therefore yield
	// force=true AND status=completed.
	opts, err := parseTasksArgs(taskverbs.Update, []string{"t-1", "--force", "--status", "completed"})
	if err != nil {
		t.Fatalf("parse `task update t-1 --force --status completed`: %v", err)
	}
	if !opts.force {
		t.Fatalf("--%s was parsed but NOT registered as set — a bare switch that eats the next token is exactly "+
			"the failure this registration prevents", taskForceFlagName)
	}
	if opts.status != "completed" {
		t.Fatalf("status = %q after `--force --status completed`, want \"completed\" — the switch consumed the "+
			"next flag's value", opts.status)
	}

	// NOT AN ARM: on its own the flag asks for nothing (the nothing-to-update
	// refusal is unchanged), so a caller cannot accidentally omit the real
	// instruction and still get a write.
	if _, err := parseTasksArgs(taskverbs.Update, []string{"t-1", "--force"}); err == nil ||
		!strings.Contains(err.Error(), "nothing to update") {
		t.Fatalf("`task update t-1 --force` alone must still be refused as nothing-to-update, got: %v", err)
	}
}

// TestTaskUpdateForceFlagIsPerVerb pins that the escape reaches ONE verb: the
// table is a whitelist, so a sibling verb refuses the spelling by name rather
// than silently accepting an escape it does not implement.
func TestTaskUpdateForceFlagIsPerVerb(t *testing.T) {
	for _, verb := range []string{taskverbs.Show, taskverbs.List, taskverbs.Submit, taskverbs.Archive} {
		if taskFlagMember(verb, taskForceFlagName) {
			t.Errorf("%s must NOT admit --%s (the escape belongs to `task update` alone)", verb, taskForceFlagName)
		}
	}
	if _, err := parseTasksArgs(taskverbs.Show, []string{"t-1", "--" + taskForceFlagName}); err == nil ||
		!strings.Contains(err.Error(), "unknown flag --"+taskForceFlagName) {
		t.Fatalf("`task show --%s` must be refused BY NAME, got: %v", taskForceFlagName, err)
	}
}

// TestLedgerOwnsFlagIsDerived pins the exemption the anti-readd gate now
// consults (cli_tasks_advert_gate_test.go's documentableServiceFlags): the
// family usage may document ONE flag spelling that a ledger verb really takes.
//
// The point is that it is READ from the admission table, never hand-listed: the
// flag that is exempt today (`--force`, because `task update` admits it) and the
// flags that are not (`--daemon`, `--every`, `--wait` — no ledger verb takes
// them) are told apart by the SAME function that decides what `task <verb>`
// accepts, so the day `task update` stops accepting `--force` the exemption
// disappears by itself and the gate starts enforcing again. A hand-kept list
// here would be the second home for one fact that Rule 13/14 bans.
func TestLedgerOwnsFlagIsDerived(t *testing.T) {
	if !ledgerOwnsFlag(cliverbs.TaskUpdateForceFlag) {
		t.Fatalf("ledgerOwnsFlag(%s) = false, but `task update` admits it — the anti-readd gate would "+
			"red-flag the ledger's OWN flag", cliverbs.TaskUpdateForceFlag)
	}
	for _, serviceOnly := range []string{"--daemon", "--every", "--wait"} {
		if ledgerOwnsFlag(serviceOnly) {
			t.Errorf("ledgerOwnsFlag(%s) = true, but NO ledger verb admits it — the exemption has leaked "+
				"past the admission table it is supposed to read", serviceOnly)
		}
	}
}
