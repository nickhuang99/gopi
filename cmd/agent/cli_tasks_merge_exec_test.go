package main

// Tests for the merge-backlog executor's CLI routing (epic
// tasks-merge-executor-live): the `gopi task merge-execute <task-id>`
// subcommand's argument parsing and flag membership. The executor ENGINE tests
// (real git fixture) live in internal/tools/task_merge_executor_test.go.
//
// The serve-pass throttle tests (TestMergeExecIntervalFromEnv /
// TestServeMergeExecSweepThrottle) were REMOVED with the dead pipeline they
// covered: `serveMergeExecSweep` had no production caller — the merge-backlog
// row pipeline was retired from the merger pass in d11af9a6 (2026-09-04) and
// replaced by the config-driven per-repo sweep (MergerRepoSweep), and F4 of
// devdoc/merge-resolve-effort-audit.md recorded the dead server-side in-queue
// merge executor that outlived it by 14 days. The CLI face above stays: it is
// the manual seam an operator uses to execute one merge-backlog row.

import (
	"strings"
	"testing"
)

func TestParseTasksArgsMergeExecute(t *testing.T) {
	// Happy path: exactly one positional task id + optional --db.
	opts, err := parseTasksArgs("merge-execute", []string{"t-123"})
	if err != nil {
		t.Fatalf("merge-execute t-123: %v", err)
	}
	if opts.taskID != "t-123" {
		t.Fatalf("taskID = %q, want t-123", opts.taskID)
	}
	opts, err = parseTasksArgs("merge-execute", []string{"--db=/tmp/x.db", "t-9"})
	if err != nil {
		t.Fatalf("merge-execute --db: %v", err)
	}
	if opts.taskID != "t-9" || opts.db != "/tmp/x.db" {
		t.Fatalf("got taskID=%q db=%q", opts.taskID, opts.db)
	}
	// Zero or two+ positionals → refused; cross-verb flags → refused.
	if _, err := parseTasksArgs("merge-execute", nil); err == nil ||
		!strings.Contains(err.Error(), "exactly one task id") {
		t.Fatalf("missing task id must be refused, got %v", err)
	}
	if _, err := parseTasksArgs("merge-execute", []string{"a", "b"}); err == nil {
		t.Fatalf("two positionals must be refused")
	}
	if _, err := parseTasksArgs("merge-execute", []string{"t-1", "--repo", "/x"}); err == nil ||
		!strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("cross-verb flag must be refused, got %v", err)
	}
}

func TestTaskFlagMemberMergeExecute(t *testing.T) {
	for _, f := range []string{"db"} {
		if !taskFlagMember("merge-execute", f) {
			t.Errorf("merge-execute --%s must be a member", f)
		}
	}
	for _, f := range []string{"repo", "submit", "payload", "tags"} {
		if taskFlagMember("merge-execute", f) {
			t.Errorf("merge-execute --%s must NOT be a member", f)
		}
	}
}
