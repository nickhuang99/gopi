package main

// cli_tasks_advert_audit_flags_test.go — regression guard for a defect the
// CLI-surface restructure (2026-09-15) surfaced while moving `audit-merges`
// into `gopi merger -h`:
//
//	gopi task audit-merges --repo PATH --submit   →  "unknown flag --repo"
//
// The verb's synopsis advertised --repo/--submit/--db and its implementation
// (runTaskAuditMerges) read opts.repo/opts.submitAudit — but taskFlagMember had
// no AuditMerges case and the parse arm assigned NEITHER field, so every
// invocation carrying a documented flag was refused (Rule 15: an advertised
// flag that is not admitted). The fix landed with the restructure; this test
// is the minimal repro that keeps it fixed (Rule 5 — every fix ships a
// test), and it asserts the two halves together so a future edit cannot
// re-open it: the FLAG TABLE admits them and the PARSE ARM actually reads them
// into the options the runner consumes.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/taskverbs"
)

// TestTaskFlagMemberAuditMerges: the documented flags of audit-merges are
// admitted by the whitelist, and a foreign verb's flag is not.
func TestTaskFlagMemberAuditMerges(t *testing.T) {
	for _, f := range []string{"db", "repo", "submit"} {
		if !taskFlagMember(taskverbs.AuditMerges, f) {
			t.Errorf("audit-merges --%s must be a member — its synopsis (now in `gopi merger -h`) documents it and runTaskAuditMerges consumes it", f)
		}
	}
	for _, f := range []string{"all", "kill", "peer", "timeout", "payload"} {
		if taskFlagMember(taskverbs.AuditMerges, f) {
			t.Errorf("audit-merges --%s must NOT be a member — it is another verb's flag", f)
		}
	}
}

// TestParseTasksArgsAuditMergesReadsItsFlags is the behavioural half: a
// documented invocation must PARSE, and the parsed values must reach the
// options struct (not be accepted-and-ignored).
func TestParseTasksArgsAuditMergesReadsItsFlags(t *testing.T) {
	opts, err := parseTasksArgs(taskverbs.AuditMerges, []string{"--repo", "/x", "--submit", "--db", "/y.db"})
	if err != nil {
		t.Fatalf("`gopi task audit-merges --repo /x --submit --db /y.db` must parse — it is the documented form: %v", err)
	}
	if opts.repo != "/x" {
		t.Errorf("opts.repo = %q, want /x — --repo must reach the runner (an accepted-and-ignored flag is Rule 15's silent failure)", opts.repo)
	}
	if !opts.submitAudit {
		t.Error("opts.submitAudit = false with --submit given — the enqueue face would silently stay report-only")
	}
	if opts.db != "/y.db" {
		t.Errorf("opts.db = %q, want /y.db", opts.db)
	}
	// Report-only default: no --submit means no enqueue.
	plain, err := parseTasksArgs(taskverbs.AuditMerges, nil)
	if err != nil {
		t.Fatalf("bare audit-merges must parse: %v", err)
	}
	if plain.submitAudit {
		t.Error("bare `gopi task audit-merges` must stay report-only (no --submit ⇒ no enqueue)")
	}
	// A positional is still refused.
	if _, err := parseTasksArgs(taskverbs.AuditMerges, []string{"extra"}); err == nil ||
		!strings.Contains(err.Error(), "unexpected positional") {
		t.Fatalf("a positional must be refused, got %v", err)
	}
}
