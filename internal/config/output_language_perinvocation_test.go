package config

// output_language_perinvocation_test.go — the FAIL-CLOSED half of the
// reply-language knob: the two per-invocation channels (the CLI flag, and the
// env var exported for this run) are FATAL when the value is unusable, while
// the settings.json channel keeps warn-and-continue.
//
// WHY THIS FILE IS SEPARATE from output_language_test.go: that file pins the
// RESOLUTION (which channel wins, what an unset knob emits, what a refused
// declaration reports). This one pins the POLICY — which channel refuses the
// process and with what status. The two defects the knob has had are one of
// each: the first was caller-side (nothing validated on the early-return
// surfaces), the second was policy-side (the validation reported and the
// process exited 0 anyway).
//
// The exit itself (os.Exit) is observed by EXECUTING the binary — the cases
// live in cmd/agent/cli_output_language_validation_test.go, which drives the
// real process through $GOPI_TEST_BIN. What is unit-testable here is every
// INPUT to that decision: the argv scan, and the contract on the exit code
// constant. Splitting them this way keeps each test on the instrument that can
// actually see the fact.

import (
	"testing"
)

// TestOutputLanguageFlagWasGiven pins the argv scan that decides whether the
// operator made an EXPLICIT per-invocation declaration. It is the fact that
// makes the refusal legitimate: a value that merely reached the environment
// from settings.json is not a command line and must keep the softer policy.
//
// The three shapes matter individually — the equals form, the space form, and
// the DANGLING flag (`--output-language` with nothing after it): the last one
// is a declaration with no value at all, and reading it as "no flag given"
// would be exactly the silence this task removes.
func TestOutputLanguageFlagWasGiven(t *testing.T) {
	flag := "--" + OutputLanguageFlagName
	cases := []struct {
		name      string
		args      []string
		wantValue string
		wantGiven bool
	}{
		{"space form", []string{flag, "zh-CN", "chat"}, "zh-CN", true},
		{"equals form", []string{flag + "=zh-CN", "chat"}, "zh-CN", true},
		{"equals form, empty value", []string{flag + "=", "chat"}, "", true},
		{"dangling flag", []string{"version", flag}, "", true},
		{"unusable value", []string{flag, "not a lang!!"}, "not a lang!!", true},
		{"absent", []string{"version"}, "", false},
		{"absent, other flag first", []string{"--tmpdir", "/x"}, "", false},
		{"absent, another flag's equals form", []string{"--db=/x/memory.db"}, "", false},
		{"absent, empty argv", nil, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value, given := OutputLanguageFlagWasGiven(tc.args)
			if given != tc.wantGiven {
				t.Fatalf("OutputLanguageFlagWasGiven(%v) given = %v, want %v", tc.args, given, tc.wantGiven)
			}
			if value != tc.wantValue {
				t.Fatalf("OutputLanguageFlagWasGiven(%v) value = %q, want %q", tc.args, value, tc.wantValue)
			}
		})
	}
}

// TestOutputLanguagePerInvocationExitCodeContract pins the CONSTANT the
// refusal exits with, and it is not a formality.
//
// The executed-binary test asserts `code == OutputLanguagePerInvocationExitCode`,
// which is self-referential: constant 0 and a warn-and-continue binary would
// satisfy it while the defect stayed live. So the constant's own contract is
// asserted here — non-zero (a refusal a caller can detect), and distinct from
// the two neighbouring classes a caller must be able to tell apart: 1 ("the run
// itself failed" — here nothing ran) and AgentGuardRefusalExitCode ("this
// launch is forbidden" — a different operator action).
func TestOutputLanguagePerInvocationExitCodeContract(t *testing.T) {
	if OutputLanguagePerInvocationExitCode == 0 {
		t.Fatal("the per-invocation refusal must exit NON-ZERO — a refusal a caller cannot detect " +
			"by exit status is the very defect this policy exists to remove")
	}
	if OutputLanguagePerInvocationExitCode == 1 {
		t.Errorf("the refusal must not reuse 1 (the generic run failure): a command line the operator "+
			"can fix must be distinguishable from a run that broke, got %d",
			OutputLanguagePerInvocationExitCode)
	}
	if OutputLanguagePerInvocationExitCode == AgentGuardRefusalExitCode {
		t.Errorf("the refusal must not collide with the spawn guard's refusal class (%d): "+
			"'your command line is wrong' and 'this launch is forbidden' are two different "+
			"operator actions", AgentGuardRefusalExitCode)
	}
}

// TestOutputLanguageFatalLabelIsTheFatalSignature pins the ONE phrase every
// fatal opens with, which is the only part of the message a caller can rely on
// being present in BOTH channels' refusals: the declared value differs, the
// channel differs, the hint differs — the label does not.
//
// It exists in a test at all because the executed-binary cases in cmd/agent
// assert the label on a CHILD PROCESS's output, while the child is a different
// build: a label reworded in production and asserted from a stale copy would go
// unnoticed here and fail there (Rule 14 — one definition, every reader imports
// it). The label is EXPORTED for that reason; the prefix that wraps it is not,
// and the assertion below is in-package so the two halves are still checked
// against each other.
func TestOutputLanguageFatalLabelIsTheFatalSignature(t *testing.T) {
	if OutputLanguageFatalLabel == "" {
		t.Fatal("the fatal label must be a non-empty constant — it is what tells a reader of a log " +
			"that the process did not proceed")
	}
	if outputLanguageFatalPrefix != "gopi: "+OutputLanguageFatalLabel+": " {
		t.Fatalf("the fatal prefix (%q) must be built from the label (%q) — two spellings of the "+
			"signature would let one surface state it and another drift",
			outputLanguageFatalPrefix, OutputLanguageFatalLabel)
	}
}
