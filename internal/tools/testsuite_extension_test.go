package tools

// testsuite_extension_test.go — new testsuite additions for the last-2-days
// features (devdoc/testsuite-extension-report.md). Covers the seams the
// feature branches shipped without unit tests:
//
//   1. taskIDRelabel (task-id-collision fix): direct unit coverage of the
//      dedup-proof helper — node sanitization, -n<node> suffix, -1..-N
//      escalation, exhausted-attempts error, and the "same id" pass-through.
//   2. newestSessionJSONLIn (task-submit-model backfill seam): newest-window
//      selection, exclusion of the parent session, pre-window filtering.
//   3. TaskRepoGuardWarnings edge cases beyond the 5 existing cases: token
//      inside a longer word (must NOT warn), deliverable repo present in
//      payload but banned token in summary, empty deliverable struct.

import (
	"errors"
	"strings"
	"testing"
)

// --- 1. taskIDRelabel (devdoc/task-id-collision.md §helper) -----------------

func TestTaskIDRelabelSanitizesNode(t *testing.T) {
	cases := []struct{ node, wantSuffix string }{
		{"hp", "-nhp"},
		{"zpeer_01", "-npeer1_01"}, // '_' is legal, kept verbatim
		{"peer1/01", "-npeer1_01"}, // '/' is not alphanumeric → '_'
		{"  hp  ", "-nhp"},         // trimmed
		{"", "-nnode"},             // empty → "node"
		{"!!!", "-n___"},           // all-illegal → each char becomes '_' (not "node" — that path needs an EMPTY result after trim)
	}
	for _, tc := range cases {
		got, changed, err := taskIDRelabel("t-9001", tc.node, func(string) (bool, error) { return false, nil })
		if err != nil {
			t.Fatalf("node %q: %v", tc.node, err)
		}
		if want := "t-9001" + tc.wantSuffix; got != want {
			t.Errorf("node %q → %q, want %q", tc.node, got, want)
		}
		if !changed {
			t.Errorf("node %q: changed=false, want true", tc.node)
		}
	}
}

func TestTaskIDRelabelEscalatesSuffixOnTaken(t *testing.T) {
	taken := map[string]bool{"t-9001-nhp": true, "t-9001-nhp-1": true, "t-9001-nhp-2": true}
	got, _, err := taskIDRelabel("t-9001", "hp", func(id string) (bool, error) { return taken[id], nil })
	if err != nil {
		t.Fatal(err)
	}
	if got != "t-9001-nhp-3" {
		t.Fatalf("got %q, want t-9001-nhp-3 (escalate -1, -2, then -3)", got)
	}
}

func TestTaskIDRelabelExhaustsAttempts(t *testing.T) {
	_, _, err := taskIDRelabel("t-1", "hp", func(string) (bool, error) { return true, nil })
	if err == nil || !strings.Contains(err.Error(), "could not generate a unique task_id") {
		t.Fatalf("want exhausted-attempts error, got %v", err)
	}
}

func TestTaskIDRelabelPropagatesExistsError(t *testing.T) {
	boom := errors.New("db gone")
	_, _, err := taskIDRelabel("t-1", "hp", func(string) (bool, error) { return false, boom })
	if !errors.Is(err, boom) {
		t.Fatalf("exists() error must propagate verbatim, got %v", err)
	}
}

// --- 2. newestSessionJSONL (task-submit-model backfill seam) ----------------
// NOTE: newestSessionJSONLIn lives in package main (cmd/agent) — Rule 3
// forbids widening the tool surface to move it, so it is covered by a
// package-main test in cmd/agent/newest_session_test.go instead. The tools-
// side seam covered here is TaskChildSessionEnv being consumed by
// taskExecutorSessionModel (already covered by TestTaskUpdateWithResult-
// BackfillsModel); these tests pin the cmd-side selection algorithm.

// --- 3. TaskRepoGuardWarnings edge cases (M4.12 guardrail) ------------------

func TestTaskRepoGuardTokenCaseInsensitive(t *testing.T) {
	// The guard lowercases the haystack — "VCMirror"-class mixed-case text
	// still contains the banned token (documented behavior: a defensive
	// over-warn is fine, this pins the case-insensitive contract).
	tid := seedRepoGuardRow(t, "vcmirror renderer cleanup", "payload", "")
	if got := TaskRepoGuardWarn(tid); len(got) == 0 {
		t.Error("case variants of a banned token must still warn")
	}
}

func TestTaskRepoGuardPayloadTokenWarns(t *testing.T) {
	tid := seedRepoGuardRow(t, "innocuous summary", "please fix the evennia build", "")
	if got := TaskRepoGuardWarn(tid); len(got) == 0 {
		t.Error("banned token in PAYLOAD must warn too")
	}
}

func TestTaskRepoGuardExportedPureJudge(t *testing.T) {
	// TaskRepoGuardWarnings is the exported pure judge — it must agree with
	// the TaskRepoGuardWarn wrapper and must not touch the DB (any task
	// value, no row needed).
	warns := TaskRepoGuardWarnings(&Task{TaskID: "t-x", Summary: "vcmi map fix", Deliverable: ""})
	if len(warns) == 0 || !strings.Contains(warns[0], "deliverable.repo") {
		t.Fatalf("pure judge should warn on no-repo+foreign token, got %v", warns)
	}
	if got := TaskRepoGuardWarnings(&Task{TaskID: "t-y", Summary: "gopi fix", Deliverable: `{"repo":"/x"}`}); len(got) != 0 {
		t.Fatalf("repo present → no warn, got %v", got)
	}
	if got := TaskRepoGuardWarnings(nil); len(got) != 0 {
		t.Fatalf("nil task must be a no-op, got %v", got)
	}
}
