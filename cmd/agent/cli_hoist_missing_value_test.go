package main

// cli_hoist_missing_value_test.go — the PRE-VERB HOIST's value discipline,
// observed on the REAL process (Rule 31).
//
// THE DEFECT (measured on the built binary, 2026-10-30, before this test):
//
//	$ ./bin/gopi --chat-surface-turns chat </dev/null   ; echo $?
//	<174 lines of the top-level usage>
//	0
//	$ ./bin/gopi --chat-ideal-geometry chat </dev/null  ; echo $?
//	<174 lines of the top-level usage>
//	0
//
// Both exited ZERO. config.BootstrapEnvFromArgs hoisted the flag with an
// UNCONDITIONAL peek, so the word `chat` was taken as the value, published into
// the knob's env and DELETED from the dispatch remainder — no verb was
// dispatched, the run fell through to the bare-prompt path, and a command that
// was typed with a missing operand came back as a DIFFERENT command that
// "succeeded". That is Rule 15's exact shape: the operator's typo was rewritten
// into a plausible run instead of being named.
//
// WHY THIS TEST EXECUTES THE BINARY. The refusal calls os.Exit and the silence it
// replaces was a property of main()'s dispatch chain — neither is visible to a
// unit test, and the unit test that DOES exist (internal/config's
// TestHoistedFlagVerdict) can only see the predicate. The two instruments are
// split on purpose; the arrangement, and its precedent, are stated in
// output_language_perinvocation_test.go's header.
//
// RED PROOF:
//
//	GOPI_TEST_BIN=<pre-fix binary> go test ./cmd/agent/ -run TestHoistRefusesAValueLessFlag
//	go test ./cmd/agent/ -run TestHoistRefusesAValueLessFlag     # the fixed tree
//
// Pre-fix, every `wantRefusal` case fails with "exited 0" plus 174 lines of the
// top-level page, and every `wantRuns` case PASSES (that half is the compatibility
// contract — it is asserted so a "fix" that refuses too much cannot pass).

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
	gopiversion "github.com/nick/gopi/internal/version"
)

// topLevelUsageMarker identifies the top-level `gopi -h` page. It is the page's
// own first line, taken from the SAME renderer main() prints (config.FormatUsage)
// rather than re-typed: a substring copied into this file would keep passing after
// the page was reworded, asserting nothing (Rule 14).
func topLevelUsageMarker() string {
	page := config.FormatUsage()
	if i := strings.IndexByte(page, '\n'); i > 0 {
		return page[:i]
	}
	return page
}

// hoistCase is one invocation of the real binary and what must be true of it.
type hoistCase struct {
	// name is the subtest name — the command line, so a failure is readable.
	name string
	// argv is the child's argument vector, WITHOUT the binary path.
	argv []string
	// wantRefusal marks a command line gopi must REFUSE: exit
	// config.CLIUsageExitCode, a diagnosis on stderr naming the flag, and NO
	// top-level usage page (nothing ran, so there is no page to print).
	wantRefusal bool
	// wantFlag is the flag spelling the refusal must name. Required when
	// wantRefusal is set: a refusal that does not name the flag is noise.
	wantFlag string
	// wantRuns marks a command line that must keep working: exit 0 and the
	// VERB's own output — the compatibility half.
	wantRuns bool
	// wantOutput is a substring the successful run must contain. Required when
	// wantRuns is set.
	wantOutput string
}

// TestHoistRefusesAValueLessFlag is the acceptance test for the silent-rewrite
// defect: a hoisted flag whose value is missing or unusable is refused BY NAME
// with a non-zero status, and the commands that were always legitimate still run.
func TestHoistRefusesAValueLessFlag(t *testing.T) {
	bin := gopiTestBinPath(t)
	usagePage := topLevelUsageMarker()

	cases := []hoistCase{
		// --- THE TWO MEASURED INVOCATIONS: a value-less flag in front of its verb ---
		{
			name: "--chat-surface-turns chat", argv: []string{"--" + config.ChatSurfaceTurnsFlagName, "chat"},
			wantRefusal: true, wantFlag: "--" + config.ChatSurfaceTurnsFlagName,
		},
		{
			name: "--chat-ideal-geometry chat", argv: []string{"--" + config.ChatIdealGeometryFlagName, "chat"},
			wantRefusal: true, wantFlag: "--" + config.ChatIdealGeometryFlagName,
		},
		// --- the same class with an unusable (not absent) value ---
		{
			name:        "--chat-surface-turns banana chat",
			argv:        []string{"--" + config.ChatSurfaceTurnsFlagName, "banana", "chat"},
			wantRefusal: true, wantFlag: "--" + config.ChatSurfaceTurnsFlagName,
		},
		{
			name:        "--chat-ideal-geometry 117 chat",
			argv:        []string{"--" + config.ChatIdealGeometryFlagName, "117", "chat"},
			wantRefusal: true, wantFlag: "--" + config.ChatIdealGeometryFlagName,
		},
		// --- nothing after the flag at all ---
		{
			name:        "dangling --chat-surface-turns",
			argv:        []string{"--" + config.ChatSurfaceTurnsFlagName},
			wantRefusal: true, wantFlag: "--" + config.ChatSurfaceTurnsFlagName,
		},
		// --- the empty INLINE value, which used to publish nothing and exit 0 ---
		{
			name:        "--chat-surface-turns= chat",
			argv:        []string{"--" + config.ChatSurfaceTurnsFlagName + "=", "chat"},
			wantRefusal: true, wantFlag: "--" + config.ChatSurfaceTurnsFlagName,
		},

		// --- THE COMPATIBILITY HALF: these must keep working, verbatim ---
		{
			name:     "--chat-surface-turns 12 chat -h",
			argv:     []string{"--" + config.ChatSurfaceTurnsFlagName, "12", "chat", HelpFlag},
			wantRuns: true, wantOutput: "Usage: gopi " + CliVerbChat,
		},
		{
			name:     "--chat-surface-turns=12 chat -h",
			argv:     []string{"--" + config.ChatSurfaceTurnsFlagName + "=12", "chat", HelpFlag},
			wantRuns: true, wantOutput: "Usage: gopi " + CliVerbChat,
		},
		{
			name:     "--chat-ideal-geometry 120x30 chat -h",
			argv:     []string{"--" + config.ChatIdealGeometryFlagName, "120x30", "chat", HelpFlag},
			wantRuns: true, wantOutput: "Usage: gopi " + CliVerbChat,
		},
		{
			// THE VALUE-OPTIONAL SWITCH: the conditional peek this file must not
			// disturb. `chat -h` proves the verb survived the hoist.
			name:     "--memory-prefetch chat -h",
			argv:     []string{"--" + config.MemoryPrefetchFlagName, "chat", HelpFlag},
			wantRuns: true, wantOutput: "Usage: gopi " + CliVerbChat,
		},
		{
			// A path-valued hoist, whose value may legitimately BE a verb word.
			name:     "--settings <path> version",
			argv:     []string{"--" + "settings", config.DefaultSettingsPath(), CliVerbVersion},
			wantRuns: true, wantOutput: gopiversion.Version,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// A sandbox HOME: the child must never read (or write) the operator's
			// live profile, and these cases return from main() long before a model
			// is involved — the endpoint is the named never-reached placeholder.
			env := testutil.MockEnv(t, t.TempDir(), hoistNeverReachedEndpoint)
			out, code := runGopiIn(t, bin, env, "", tc.argv...)

			switch {
			case tc.wantRefusal:
				if code != config.CLIUsageExitCode {
					t.Fatalf("`gopi %s` exited %d, want %d — a value-less/unusable hoisted flag used to be "+
						"silently taken from the NEXT TOKEN (usually the verb), which deleted the verb and "+
						"exited 0 having printed the top-level usage. Output:\n%s",
						tc.name, code, config.CLIUsageExitCode, out)
				}
				if tc.wantFlag != "" && !strings.Contains(out, tc.wantFlag) {
					t.Fatalf("the refusal must NAME the flag %q — a refusal that names nothing is noise. Output:\n%s",
						tc.wantFlag, out)
				}
				if !strings.Contains(out, config.HoistedFlagRefusalPrefix) {
					t.Fatalf("the refusal must be gopi's own diagnosis (%q). Output:\n%s",
						config.HoistedFlagRefusalPrefix, out)
				}
				if strings.Contains(out, usagePage) {
					t.Fatalf("`gopi %s` must NOT print the top-level usage page — that page means "+
						"'no verb was dispatched', which is the defect (nothing ran). Output:\n%s", tc.name, out)
				}
			case tc.wantRuns:
				if code != 0 {
					t.Fatalf("`gopi %s` exited %d, want 0 — this command line was always legitimate and "+
						"must keep working. Output:\n%s", tc.name, code, out)
				}
				if !strings.Contains(out, tc.wantOutput) {
					t.Fatalf("`gopi %s` must print %q (the verb must have RUN). Output:\n%s",
						tc.name, tc.wantOutput, out)
				}
			default:
				t.Fatalf("case %q asserts nothing — every hoist case is either a refusal or a run", tc.name)
			}
		})
	}
}

// TestHoistRefusalDoesNotConsumeTheVerb is the sharper statement of the same fact,
// and it is the one that would have caught the original bug directly: the word
// after the flag is NOT published into the knob's env. A refusal that still
// hoisted the verb as the value would satisfy every assertion above while leaving
// the knob carrying a value no human wrote.
//
// It is observable from outside because the refusal's own sentence says so, and
// because the post-fix process exits before the value could be used anywhere: the
// internal/config unit test (TestHoistedFlagVerdict) pins the predicate, this pins
// the sentence the operator actually reads.
func TestHoistRefusalDoesNotConsumeTheVerb(t *testing.T) {
	bin := gopiTestBinPath(t)
	env := testutil.MockEnv(t, t.TempDir(), hoistNeverReachedEndpoint)

	out, code := runGopiIn(t, bin, env, "", "--"+config.ChatSurfaceTurnsFlagName, CliVerbChat)
	if code != config.CLIUsageExitCode {
		t.Fatalf("exit = %d, want %d. Output:\n%s", code, config.CLIUsageExitCode, out)
	}
	// The MISSING-value refusal carries the extra clause; it is what distinguishes
	// "you forgot the value" from "the value you wrote is not one" — the two used
	// to be indistinguishable because both were silent.
	if !strings.Contains(out, config.HoistedFlagNextWordKeptClaim) {
		t.Fatalf("a value-less flag's refusal must state that the next word was NOT taken as its value "+
			"(%q). Output:\n%s", config.HoistedFlagNextWordKeptClaim, out)
	}
}

// hoistNeverReachedEndpoint is a placeholder model endpoint. Every case in this
// file returns from main() at the subcommand chain (or at the hoist's refusal)
// before a session is built, so no request is ever made; the value is named
// (Rule 1b) rather than inlined at the call site, like the sibling file's
// mockLLMURLForKnob.
const hoistNeverReachedEndpoint = "http://127.0.0.1:1"
