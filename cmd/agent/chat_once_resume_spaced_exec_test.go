package main

// The SPACED `--resume <target>` spelling of `gopi chat --once`, EXECUTED —
// in both orders, with and without `-p` — because the parser-level arm beside
// this file could not see the defect that was actually reported.
//
// THE DEFECT (measured, task 01a1ae04). The parser now carries a valued REPL
// flag's value into the next slot, and
// TestChatOnceValuedREPLFlagCarriesItsValueInTheNextSlot pins exactly that: the
// slice handed to the REPL is `[flag, value]` and the prompt is separate. What
// no arm covered is the whole command line the help page advertises WITH a
// prompt beside it:
//
//	$ gopi chat --once --resume <id> -p hi
//	gopi chat: more than one prompt given ("<id>" and "hi") — one turn takes ONE prompt
//
// A reader who follows the page writes a command line that cannot run, and every
// unit-level arm stayed green while it did.
//
// WHY AN EXECUTED ARM AND NOT A PARSER ASSERTION (Rule 31). The claim is about
// the FACE the caller gets, not about a return value: the report's acceptance
// criterion is "`--resume <id>` 报『找不到 session』，不是『more than one prompt』".
// So each argv is handed to the real binary and the binary's own words are read
// back. The probe is deliberately cheap and API-free — an unresolvable target
// stops the run at resolution, before any model call.
//
// STDIN CARRIES THE PROMPT, and that is not incidental: `--once` refuses to run
// without a prompt, and once `--resume <target>` stops being misread as the
// positional prompt there IS no prompt in those argv unless one arrives on stdin.
// The probe therefore feeds a line, which is what the report's own terminal runs
// had; the no-prompt ordering is pinned as its own case below, because a reader
// who runs these in a non-interactive shell meets exactly that sentence.

import (
	"os/exec"
	"strings"
	"testing"
)

// chatOnceSpacedProbe runs the real binary with args under cliContractEnv (an
// isolated GOPI_HOME, ambient GOPI_*/ANTHROPIC_*/DEEPSEEK_* and DISPLAY dropped)
// and returns its combined output. It exists because runCLIContractProbeRaw
// leaves the child's stdin at /dev/null, and the prompt these cases need arrives
// there.
func chatOnceSpacedProbe(t *testing.T, bin, home, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = cliContractEnv(home)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestChatOnceSpacedResumeRunsTheWholeCommandLine is the regression guard for the
// report's acceptance items, replayed against the shipped surface:
//
//	A. `--once --resume <id>` + prompt on stdin → the resolver's sentence,
//	   NOT "needs a target" (before the fix the id WAS the prompt)
//	B. `--once --resume <id> -p hi`             → the resolver's sentence,
//	                                              NOT "more than one prompt"
//	C. `--once -p hi --resume <id>`             → the same, order-independent
//	D. `--once --resume=<id> -p hi`             → the joined spelling keeps working
//	E. `--once --resume` + prompt on stdin      → a genuinely missing value still
//	                                              says "needs a target"
//
// Each case asserts BOTH halves of the criterion: the sentence it must contain
// AND the sentence it must not (Rule 15: a refusal names its own cause, so the
// positive half alone would pass on a run that printed both).
func TestChatOnceSpacedResumeRunsTheWholeCommandLine(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	makeSessionRoot(t, home)

	// The target resolves to nothing: the run stops at resolution, before a
	// checkpoint, a model call or a session artifact.
	const unknownTarget = "abc1234"
	const probePrompt = "hi"
	const promptOnStdin = probePrompt + "\n"

	cases := []struct {
		name       string
		args       []string
		stdin      string
		want       string
		forbidAlso []string
	}{
		{
			name:       "spaced, prompt on stdin",
			args:       []string{CliVerbChat, chatOnceFlag, chatFlagResume, unknownTarget},
			stdin:      promptOnStdin,
			want:       chatRunArgsNeitherRawLogPhrase,
			forbidAlso: []string{chatResumeNeedsTargetPhrase},
		},
		{
			name:       "spaced, prompt after the pair",
			args:       []string{CliVerbChat, chatOnceFlag, chatFlagResume, unknownTarget, chatOncePromptFlag, probePrompt},
			stdin:      "",
			want:       chatRunArgsNeitherRawLogPhrase,
			forbidAlso: []string{chatOnceTwoPromptsPhrase},
		},
		{
			name:       "spaced, prompt BEFORE the pair",
			args:       []string{CliVerbChat, chatOnceFlag, chatOncePromptFlag, probePrompt, chatFlagResume, unknownTarget},
			stdin:      "",
			want:       chatRunArgsNeitherRawLogPhrase,
			forbidAlso: []string{chatOnceTwoPromptsPhrase},
		},
		{
			name:       "joined, prompt after",
			args:       []string{CliVerbChat, chatOnceFlag, chatFlagResume + chatFlagValueSep + unknownTarget, chatOncePromptFlag, probePrompt},
			stdin:      "",
			want:       chatRunArgsNeitherRawLogPhrase,
			forbidAlso: []string{chatOnceTwoPromptsPhrase, chatResumeNeedsTargetPhrase},
		},
		{
			name:       "genuinely missing value",
			args:       []string{CliVerbChat, chatOnceFlag, chatFlagResume},
			stdin:      promptOnStdin,
			want:       chatResumeNeedsTargetPhrase,
			forbidAlso: []string{chatOnceTwoPromptsPhrase},
		},
		{
			// The ordering a reader meets in a non-interactive shell: no prompt
			// anywhere, so `--once` answers its own precondition FIRST. Pinned
			// here so the sentences above cannot be reached by silently treating
			// the resume target as the prompt again — which is the defect.
			name:       "no prompt at all",
			args:       []string{CliVerbChat, chatOnceFlag, chatFlagResume, unknownTarget},
			stdin:      "",
			want:       chatOnceNeedsPromptPhrase,
			forbidAlso: []string{chatResumeNeedsTargetPhrase},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := chatOnceSpacedProbe(t, bin, home, tc.stdin, tc.args...)
			if err == nil {
				t.Fatalf("gopi %s exited 0 — it did not reach the refusal path\n%s",
					strings.Join(tc.args, " "), out)
			}
			// The refusal LINE, not the whole page: `--once` prints its usage
			// page beside every refusal, and that page legitimately SAYS
			// "unknown flag" while explaining exit status 2. Searching the whole
			// output for the phrase therefore reported the page as a refusal
			// (measured: this arm failed on a case whose first line was the
			// correct "needs a target").
			if first := firstNonEmptyLine(out); strings.Contains(first, "unknown flag") || strings.Contains(first, "unknown option") {
				t.Fatalf("gopi %s: the face REFUSED the argv the help page advertises — first line:\n%s",
					strings.Join(tc.args, " "), first)
			}
			if !strings.Contains(out, tc.want) {
				t.Fatalf("gopi %s: the refusal does not carry %q — got:\n%s",
					strings.Join(tc.args, " "), tc.want, out)
			}
			for _, forbidden := range tc.forbidAlso {
				if strings.Contains(out, forbidden) {
					t.Fatalf("gopi %s: the refusal says %q, which is the WRONG cause for this argv\n%s",
						strings.Join(tc.args, " "), forbidden, out)
				}
			}
		})
	}
}

// firstNonEmptyLine is the first line that carries text — the line a refusal
// OPENS with, which is the one a reader (and this probe) treats as the answer.
func firstNonEmptyLine(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// The three sentences the criterion distinguishes, spelled once (Rule 13) so a
// reworded refusal fails in ONE place instead of leaving a case that can never
// match again — the vacuum shape that made a named check read green while it
// matched no test at all.
const (
	chatResumeNeedsTargetPhrase = "needs a target"
	chatOnceTwoPromptsPhrase    = "more than one prompt given"
	chatOnceNeedsPromptPhrase   = "--once needs a prompt"
)
