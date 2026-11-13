package main

// test_capture_output_test.go — the package-wide stdout+stderr capture helper.
//
// RELOCATED HERE (Rule 43) when the ledger-agreement precondition was retired:
// `captureAllOutput` lived in task_ledger_agreement_redproof_test.go, but its
// reference distribution was NOT confined to that mechanism — task_sync_pass_test.go
// (5 sites) and task_sync_switch_test.go (1 site) also call it. A symbol with
// callers outside the mechanism being deleted is a GENERAL TOOL, so it moves to
// its own file in the SAME commit that deletes the mechanism: splitting the move
// from the deletion would leave the intermediate commit uncompilable.

import (
	"io"
	"os"
	"testing"
)

// captureAllOutput runs fn with BOTH os.Stdout and os.Stderr redirected to
// pipes and returns what each printed, concatenated — a subject's verdict may
// land on either stream, and an assertion that watched only one would pass on
// an un-fixed tree by accident.
func captureAllOutput(t *testing.T, fn func()) string {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout, os.Stderr = outW, errW
	done := make(chan [2]string, 1)
	go func() {
		var a, b [2]string
		oa, _ := io.ReadAll(outR) //stylegate:ignore test capture: the reader drains a pipe until the writer closes; its error cannot lose the assertion below
		a[0], a[1] = string(oa), ""
		ob, _ := io.ReadAll(errR) //stylegate:ignore test capture: same
		b[0], b[1] = string(ob), ""
		done <- [2]string{a[0], b[0]}
	}()
	fn()
	os.Stdout, os.Stderr = oldOut, oldErr
	if err := outW.Close(); err != nil {
		t.Fatalf("close stdout pipe: %v", err)
	}
	if err := errW.Close(); err != nil {
		t.Fatalf("close stderr pipe: %v", err)
	}
	got := <-done
	return got[0] + got[1]
}
