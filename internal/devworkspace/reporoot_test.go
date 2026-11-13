package devworkspace

import (
	"os/exec"
	"strings"
	"testing"
)

// repoRootDir is this package's repo root, resolved once (the tests here shell out
// to generators by repo-relative path, so a wrong root is a silent skip).
//
// WHY THIS FILE EXISTS. The helper used to live in `census_redrive_test.go`, one
// of the three files the census-citation retirement deleted on 2026-11-05.
// `man_gate_test.go` was its OTHER reader — a reader outside the retired
// mechanism — so under Rule 43 it is a general test tool rather than a mechanism
// part: moved here in the same commit that deletes the rest, which is what keeps
// no intermediate state of this branch red.
func repoRootDir(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("resolving the repo root failed: %v", err)
	}
	return strings.TrimSpace(string(out))
}
