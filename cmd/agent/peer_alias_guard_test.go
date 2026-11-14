package main

// peer_alias_guard_test.go — the cmd/agent half of the option-injection fix
// (internal/tools/peer_alias.go): every ssh face reached from the CLI must
// refuse an option-shaped peer alias BEFORE it becomes an argv element of
// ssh. The legs covered here:
//
//	· --peer on harvest / diff / sync (parse-time refusal — the operator
//	  learns on argv, not mid-run),
//	· --report-to / --repo on `task run` (the remote writeback path),
//	· compute_nodes entries (the dispatch node name + report_to + repo),
//	· dispatchSSH's host (the shared ssh hop).

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/tools"
)

// TestPeerFlagsRefuseOptionShapedAlias — the CLI parse faces. An alias like
// "-oProxyCommand=sh" is ONE argv element and ssh parses a leading "-" as an
// OPTION, so each of these must be rejected at parse time.
func TestPeerFlagsRefuseOptionShapedAlias(t *testing.T) {
	const bad = "-oProxyCommand=/tmp/payload"
	//
	// WHAT CHANGED, AND WHY THE ASSERTION IS "REFUSED" RATHER THAN "SAYS
	// invalid peer" (2026-11-05/06). Every verb this arm used to parse —
	// `harvest`, `diff`, `sync` — belonged to the cross-node sync family that
	// commit 5677cc6e DELETED, so no `--peer` flag survives on `gopi task` for
	// the alias validator to reach: the refusal now comes from the DELETION
	// sentence (`unknown subcommand` / `unknown flag`), one layer earlier. That
	// is still a refusal, and it is still the property this guard exists for —
	// an option-shaped alias must never reach a process that would hand it to
	// ssh — so the assertion here is REFUSED, with the surviving face's
	// wording checked where the flag actually lives
	// (TestWorkerRunFlagsRefuseOptionShapedAlias: `--report-to` / `--repo` on
	// the runner, and `gopi notify --peer`, whose refusal was OBSERVED at this
	// tree's own binary: `./bin/gopi notify --peer '-oProxyCommand=x' --session
	// s --message m` answers `notify --peer: invalid peer alias
	// "-oProxyCommand=x": a leading "-" makes ssh parse it as an OPTION (e.g.
	// -oProxyCommand=… runs a local command), not a host`).
	//
	// A GREEN HERE MUST NOT BE EXPLAINED BY A BROKEN PARSE, so each case also
	// pins what the refusal IS: the deleted spellings must be refused as
	// RETIRED VOCABULARY, and the live spelling must parse cleanly.
	//
	// The expected fragment per verb is MEASURED, not guessed — each of these
	// was read off the parse itself, which is why `harvest` (a REMOVED verb,
	// refused by name) and `sync`/`diff` (deleted FLAGS, refused as `--peer`
	// unknown) differ. Pinning the fragment is what stops this arm passing on a
	// parse that is broken for some unrelated reason.
	cases := []struct {
		verb string
		want string
	}{
		{"harvest", `unknown subcommand "harvest"`},
		{"diff", "unknown flag --peer"},
		{"sync", "unknown flag --peer"},
	}
	for _, c := range cases {
		_, err := parseTasksArgs(c.verb, []string{"--peer", bad})
		if err == nil {
			t.Errorf("%s --peer %s must be refused (option-shaped alias, on a deleted verb)", c.verb, bad)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s refusal must name the deletion (%q), got: %v", c.verb, c.want, err)
		}
	}
	// The validator ITSELF still refuses the option-shaped alias — the property,
	// independent of which surface happens to reach it (Rule 14: one validator).
	if _, err := tools.ValidateSSHPeerAlias(bad); err == nil {
		t.Errorf("tools.ValidateSSHPeerAlias(%q) accepted an option-shaped alias", bad)
	} else if !strings.Contains(err.Error(), "invalid peer") {
		t.Errorf("the validator's refusal must say invalid peer, got: %v", err)
	}
	if _, err := tools.ValidateSSHPeerAlias("hp"); err != nil {
		t.Errorf("ValidateSSHPeerAlias(\"hp\") must accept a well-formed alias, got: %v", err)
	}

	// Well-formed values still parse on a SURVIVING verb (the guard is not a
	// blanket refusal of the family, and the parse path itself is not the thing
	// failing above).
	if _, err := parseTasksArgs("list", []string{"--status", tools.TaskStatusPending}); err != nil {
		t.Errorf("list --status must parse, got: %v", err)
	}
}

// TestWorkerRunFlagsRefuseOptionShapedAlias — the SAME option-injection guard
// at the face that INHERITED the flags (epic worker-run-entry: `gopi worker
// <task-id> --report-to <alias> [--repo <target>]` is the retired verb's
// flag set). The refusal is asserted through scanWorkerArgs — the path the
// worker dispatcher itself uses — so a future change cannot drop the guard
// while the CLI still looks right.
func TestWorkerRunFlagsRefuseOptionShapedAlias(t *testing.T) {
	const bad = "-oProxyCommand=/tmp/payload"
	for _, argv := range [][]string{
		{"t-1", "--report-to", bad},
		{"t-1", "--repo", bad + ":/home/user/work/GoPi"},
		{"t-1", "--report-to=" + bad},
	} {
		wa, unknown := scanWorkerArgs(argv)
		if unknown != "" {
			t.Fatalf("scanWorkerArgs(%v) refused the word %q, want the flag-level refusal", argv, unknown)
		}
		if wa.refuse == "" {
			t.Errorf("scanWorkerArgs(%v) accepted an option-shaped ssh peer alias", argv)
		} else if !strings.Contains(wa.refuse, "invalid peer") {
			t.Errorf("scanWorkerArgs(%v) refusal must say invalid peer, got: %s", argv, wa.refuse)
		}
	}
	// Well-formed values still scan (the guard is not a blanket refusal).
	for _, argv := range [][]string{
		{"t-1", "--report-to", "zpeer", "--repo", "zpeer:/home/user/work/GoPi"},
		{"t-1"},
	} {
		wa, unknown := scanWorkerArgs(argv)
		if unknown != "" || wa.refuse != "" {
			t.Errorf("scanWorkerArgs(%v) = unknown %q / refuse %q, want accepted", argv, unknown, wa.refuse)
		}
	}
}
