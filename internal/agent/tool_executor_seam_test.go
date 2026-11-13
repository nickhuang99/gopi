package agent

// tool_executor_seam_test.go — the EXPORTED executor seam (Rule 31: the surface
// this task added is exercised through its real entry point, not only through the
// field the in-package tests already set).

import (
	"testing"

	"github.com/nick/gopi/internal/deepseek"
)

// TestSetToolExecutorReplacesDispatch pins the seam's contract: after
// SetToolExecutor the agent asks the caller for every result, the in-package
// field-literal construction keeps working unchanged, and a nil argument restores
// the real dispatch rather than leaving a nil call behind.
func TestSetToolExecutorReplacesDispatch(t *testing.T) {
	a := &Agent{}
	if a.ToolExecutorInstalled() {
		t.Fatal("a fresh agent reports an installed executor")
	}
	var seen []deepseek.ToolCall
	a.SetToolExecutor(func(tc deepseek.ToolCall) string {
		seen = append(seen, tc)
		return "from the seam"
	})
	if !a.ToolExecutorInstalled() {
		t.Fatal("ToolExecutorInstalled is false after SetToolExecutor")
	}
	if got := a.executeTool(deepseek.ToolCall{ID: "call_1", Name: "bash"}); got != "from the seam" {
		t.Fatalf("executeTool returned %q, want the seam's result", got)
	}
	if len(seen) != 1 || seen[0].ID != "call_1" {
		t.Fatalf("the seam saw %+v, want the one call that was executed", seen)
	}
	// nil RESTORES the real dispatch: the method is a switch, not a one-way door.
	a.SetToolExecutor(nil)
	if a.ToolExecutorInstalled() {
		t.Fatal("a nil executor left the seam installed")
	}
	// A nil RECEIVER is tolerated: the replay path installs the seam on an agent
	// it just built, and a panic there would be a crash in the middle of a live
	// chat surface.
	var nilAgent *Agent
	nilAgent.SetToolExecutor(func(deepseek.ToolCall) string { return "" })
	if nilAgent.ToolExecutorInstalled() {
		t.Fatal("a nil receiver reported an installed executor")
	}
}

// TestToolExecutorFieldLiteralStillCompiles is the CONSTRAINT this seam had to
// satisfy: the existing in-package construction (a struct literal setting the
// field) keeps working byte for byte — the exported method was added BESIDE the
// field, never instead of it.
func TestToolExecutorFieldLiteralStillCompiles(t *testing.T) {
	a := &Agent{executeToolFn: func(tc deepseek.ToolCall) string { return "literal" }}
	if got := a.executeTool(deepseek.ToolCall{ID: "call_1"}); got != "literal" {
		t.Fatalf("the field-literal executor returned %q, want %q", got, "literal")
	}
	if !a.ToolExecutorInstalled() {
		t.Fatal("ToolExecutorInstalled does not see the field-literal executor")
	}
}
