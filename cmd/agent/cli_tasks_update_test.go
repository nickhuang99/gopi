package main

// The `gopi task update` CLI face: its flag whitelist, the --result /
// --payload parse arms, and the end-to-end revision through the REAL binary.
//
// This file was `cli_tasks_verify_test.go` until the `gopi task verify` verb was
// retired (2026-10-30, task 01a18799, slice 2/4): the parse/dispatch/exit-code
// arms went with the verb, the migrate + update arms stayed (they were never
// about verification — the pre-P2.5 CHECK-constraint rebuild is
// `ensureTasksSchema`'s, and --result is the OPERATOR's evidence writeback), and
// the name followed the content.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/taskverbs"
	"github.com/nick/gopi/internal/tools"
)

// TestTaskFlagMemberUpdateResult — bugfix-task-p1 regression: the verify
// loop's writeback command is `gopi task update <id> --status X --result
// "evidence: ..."` — --result must be a member of the update whitelist
// (before the fix the briefing's --summary hit the unknown-flag wall and
// the evidence could never be recorded).
func TestTaskFlagMemberUpdateResult(t *testing.T) {
	for _, f := range []string{"db", "status", "result", "payload"} {
		if !taskFlagMember("update", f) {
			t.Errorf("update --%s must be a member", f)
		}
	}
	for _, f := range []string{"summary", "timeout", "tags"} {
		if taskFlagMember("update", f) {
			t.Errorf("update --%s must NOT be a member", f)
		}
	}
}

// TestTaskFlagMemberUpdatePayload — epic 01a0fea1 item [2/5]: `--payload` is
// the REVISION face of `task update`, so it must be admitted there. It was
// deliberately in the "must NOT be a member" list above until this epic —
// the list is edited in the same change as the behaviour it pinned, and the
// sibling assertions (summary / tags / timeout still refused) stay, because
// the edit opens ONE field, not content-immutability as a class.
//
// The reverse direction is the other half of the same contract and is
// asserted here too: the flag is `update`'s, and the verbs that must NOT
// grow it — show (a read), verify (a verdict), list — keep refusing it, so
// the admission table stays a per-verb whitelist rather than becoming a
// global one.
func TestTaskFlagMemberUpdatePayload(t *testing.T) {
	if !taskFlagMember(taskverbs.Update, taskPayloadFlagName) {
		t.Fatalf("update --%s must be a member — the revision face is unreachable without it", taskPayloadFlagName)
	}
	for _, verb := range []string{
		taskverbs.Show, taskverbs.List, taskverbs.Archive,
		taskverbs.Clone, taskverbs.Search, taskverbs.DedupScan,
	} {
		if taskFlagMember(verb, taskPayloadFlagName) {
			t.Errorf("%s must NOT admit --%s (the whitelist is per-verb)", verb, taskPayloadFlagName)
		}
	}
}

// TestParseTasksUpdateResult — parse-level: --result is accepted on update,
// --summary stays rejected (M4.5 immutable content), and a --result without
// --status is refused.
func TestParseTasksUpdateResult(t *testing.T) {
	opts, err := parseTasksArgs("update", []string{"t-1", "--status", "verified", "--result", "evidence: go test ./... 12/12 PASS"})
	if err != nil {
		t.Fatalf("--result must parse: %v", err)
	}
	if opts.status != "verified" || opts.result != "evidence: go test ./... 12/12 PASS" {
		t.Fatalf("opts = %+v", opts)
	}
	if _, err := parseTasksArgs("update", []string{"t-1", "--result", "evidence only"}); err == nil ||
		!strings.Contains(err.Error(), "nothing to update") {
		t.Fatalf("--result without --status must be refused, got %v", err)
	}
	if _, err := parseTasksArgs("update", []string{"t-1", "--summary", "x", "--status", "verified"}); err == nil ||
		!strings.Contains(err.Error(), "unknown flag --summary") {
		t.Fatalf("--summary must stay rejected, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// --payload on `task update` (epic 01a0fea1 item [2/5]): the REVISION face of
// the ledger's briefing. Three things must agree for it to be a face at all —
// the admission table, the parser that reads the value, and the help page that
// advertises it — and the fourth, the engine, is exercised end to end through
// the REAL BINARY below (Rule 31: the surface entered the way an operator
// enters it, with argv and a ledger, not through the parse function alone).
// ---------------------------------------------------------------------------

// TestParseTasksUpdatePayload pins the parse arms: the value is read, the
// '@path' file reference is expanded byte-exactly, and a status-less
// invocation is a PURE EDIT (statusOmitted) rather than the old "nothing to
// update" refusal. It also pins what the flag must NOT do — a no-flag update
// is still refused, and `--payload` alone must not be read as a status move.
func TestParseTasksUpdatePayload(t *testing.T) {
	// The value lands on the options struct...
	opts, err := parseTasksArgs(taskverbs.Update, []string{"t-1", "--payload", "the new briefing"})
	if err != nil {
		t.Fatalf("--payload alone must parse: %v", err)
	}
	if !opts.payloadSet || opts.payload != "the new briefing" {
		t.Fatalf("opts.payloadSet=%v opts.payload=%q", opts.payloadSet, opts.payload)
	}
	if !opts.statusOmitted {
		t.Errorf("a status-less --payload update is a PURE EDIT — statusOmitted must be set")
	}
	if strings.TrimSpace(opts.status) != "" {
		t.Errorf("no status may be invented for a pure edit, got %q", opts.status)
	}

	// ...and it rides alongside --status + --reason (the full spelling).
	opts, err = parseTasksArgs(taskverbs.Update,
		[]string{"t-1", "--payload", "revised", "--status", "in_progress", "--reason", "briefing was stale"})
	if err != nil {
		t.Fatalf("--payload with --status/--reason must parse: %v", err)
	}
	if opts.status != "in_progress" || opts.reason != "briefing was stale" || opts.payload != "revised" {
		t.Fatalf("opts = %+v", opts)
	}
	if opts.statusOmitted {
		t.Errorf("a --status was given — statusOmitted must stay false")
	}

	// '@path' is the SAME file reference submit uses (one expansion function,
	// config.ExpandFileRef through the package alias): the file's bytes land
	// verbatim, including the trailing newline a briefing can depend on.
	path := filepath.Join(t.TempDir(), "briefing.txt")
	const body = "line one\nline \"two\" — with 中文 and $VARS\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	opts, err = parseTasksArgs(taskverbs.Update, []string{"t-1", "--payload", "@" + path})
	if err != nil {
		t.Fatalf("--payload @FILE must parse: %v", err)
	}
	if opts.payload != body {
		t.Errorf("the @file expansion must be byte-exact\nwant: %q\ngot:  %q", body, opts.payload)
	}

	// Presence, not value: `--payload ""` IS an edit request (the engine
	// refuses the empty value by name — a different outcome from "no edit was
	// asked for", and collapsing the two would make the refusal unreachable).
	opts, err = parseTasksArgs(taskverbs.Update, []string{"t-1", "--payload", ""})
	if err != nil {
		t.Fatalf("--payload with an empty value must still PARSE (the engine owns that refusal): %v", err)
	}
	if !opts.payloadSet {
		t.Errorf("--payload with an empty value must still be recorded as PRESENT")
	}

	// The nothing-asked-for refusal now lists every arm, and is still raised
	// when no arm is used.
	if _, err := parseTasksArgs(taskverbs.Update, []string{"t-1"}); err == nil ||
		!strings.Contains(err.Error(), "nothing to update") {
		t.Fatalf("a bare update must be refused, got %v", err)
	} else if !strings.Contains(err.Error(), "--payload") {
		t.Errorf("the refusal must name the arms, including --payload: %v", err)
	}

	// And the flag belongs to THIS verb: a read verb refuses it by name.
	if _, err := parseTasksArgs(taskverbs.Show, []string{"t-1", "--payload", "x"}); err == nil ||
		!strings.Contains(err.Error(), "unknown flag --"+taskPayloadFlagName) {
		t.Fatalf("show must refuse --%s by name, got %v", taskPayloadFlagName, err)
	}
}

// TestTaskVerbUpdatePayloadRevisesThroughTheRealBinary is the end-to-end half
// (Rule 31): the operator's own argv against a real ledger, through the built
// binary — revise the briefing, then READ IT BACK with `task show`, and
// confirm the row is still dispatchable afterwards (its payload non-empty, its
// status untouched by the edit).
func TestTaskVerbUpdatePayloadRevisesThroughTheRealBinary(t *testing.T) {
	sb := g1NewSandbox(t)
	g1Seed(t, sb, tools.TaskStatusPending, "")

	const revised = "REVISED briefing: the first draft named the wrong binary"
	out := sb.g1RunOK(t, "task", taskverbs.Update, g1FixtureID,
		"--payload", revised, "--reason", "original briefing was stale", "--db", sb.db)
	if !strings.Contains(out, "Task payload updated: "+g1FixtureID) {
		t.Errorf("the revision must report what it did:\n%s", out)
	}

	// The read-back: this is the assertion that the edit reached the LEDGER
	// and not merely the printer.
	shown := sb.g1RunOK(t, "task", taskverbs.Show, g1FixtureID, "--db", sb.db)
	if !strings.Contains(shown, revised) {
		t.Errorf("`task show` must render the revised briefing:\n%s", shown)
	}
	if !strings.Contains(shown, "status:         "+tools.TaskStatusPending) {
		t.Errorf("a pure --payload edit must not move the status:\n%s", shown)
	}

	// A refusal is a refusal BY NAME, with a non-zero exit and a ledger that
	// did not move: the blank-payload arm.
	before := shown
	bad, code := sb.g1Run(t, "task", taskverbs.Update, g1FixtureID,
		"--payload", "", "--reason", "trying to blank it", "--db", sb.db)
	if code == 0 {
		t.Fatalf("a blank payload must be refused:\n%s", bad)
	}
	if !strings.Contains(bad, "refusing to blank the payload column") {
		t.Errorf("the refusal must name the reason:\n%s", bad)
	}
	// ...and the missing-reason arm (a revision with no WHY).
	bad, code = sb.g1Run(t, "task", taskverbs.Update, g1FixtureID,
		"--payload", "another briefing", "--db", sb.db)
	if code == 0 {
		t.Fatalf("a revision with no --reason must be refused:\n%s", bad)
	}
	if !strings.Contains(bad, "must carry --reason") {
		t.Errorf("the refusal must name the missing reason:\n%s", bad)
	}
	after := sb.g1RunOK(t, "task", taskverbs.Show, g1FixtureID, "--db", sb.db)
	if after != before {
		t.Errorf("a refused revision must leave the row byte-identical:\nbefore:\n%s\nafter:\n%s", before, after)
	}

	// The rule text the help prints must describe the behaviour, and the OLD
	// claim (payload immutable) must be gone from this verb's paragraph — an
	// advert that survives its behaviour is the drift Rule 15 exists to stop.
	block := updateSynopsisBlock(t)
	if !strings.Contains(block, "--payload TEXT|@FILE") {
		t.Errorf("the help must advertise --payload on update:\n%s", block)
	}
	if strings.Contains(block, "(summary/payload) is immutable") {
		t.Errorf("the help still claims the payload is immutable:\n%s", block)
	}
	if !strings.Contains(block, "payload_revised") {
		t.Errorf("the help must tell the operator the revision is an audit record:\n%s", block)
	}
}

// updateSynopsisBlock returns the update verb's own block from the family help
// (its synopsis lines plus the description paragraph) — the text an operator
// reads for this verb, and the only place its contract is published.
func updateSynopsisBlock(t *testing.T) string {
	t.Helper()
	head := "  gopi task " + taskverbs.Update + " "
	lines := strings.Split(tasksUsage, "\n")
	for i, ln := range lines {
		if !strings.HasPrefix(ln, head) {
			continue
		}
		var b strings.Builder
		for _, l := range lines[i:] {
			if strings.TrimSpace(l) == "" {
				break
			}
			b.WriteString(l)
			b.WriteString("\n")
		}
		return b.String()
	}
	t.Fatalf("the family help carries no %q block — the synopsis scan is reading blind", head)
	return ""
}
