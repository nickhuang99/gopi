package main

// The REPLY-LANGUAGE KNOB'S VALIDATION, measured by executing the built binary
// — the only instrument that can see this defect at all.
//
// WHY A SUBPROCESS AND NOT A UNIT TEST (the reason this file exists in this
// shape): the knob's Rule 15 validation (config.resolveOutputLanguage) was
// always correct and always tested — internal/config/output_language_test.go
// asserts it directly. The DEFECT was CALLER-SIDE: the resolution had exactly
// ONE caller, internal/agent's systemPromptWithOutputLanguage, and every other
// surface that exposes the knob returns from main() BEFORE that path. So the
// surfaces a user actually meets — `gopi -h`, `gopi --help-env`, `gopi config
// show`, `gopi chat`, a task run — read a declaration the operator wrote with
// NOTHING: measured on the built binary (2026-09-27), both the env case and the
// flag case exited 0 with ZERO bytes on stderr. The validation was dead code on
// the real path, and the help page advertising the knob disagreed with the
// behaviour of the very process that printed the page.
//
// A unit test of resolveOutputLanguage CANNOT see that — the function is
// reachable and correct; it is the PROCESS that never calls it. Nothing
// in-process reproduces it, which is why the observation is the child's stderr.
//
// WHAT IS ASSERTED, and it is the pair that must hold together:
//
//   - a REFUSED declaration is REPORTED, on every surface that exposes the
//     knob (the refusal names the channel AND the value it refused, and the
//     chain's verdict that nothing was adopted) — a declaration the operator
//     made is never silent;
//   - a REFUSED PER-INVOCATION declaration (the CLI flag, or the env var
//     exported for this run) is FATAL — a NON-ZERO exit status, and the
//     subcommand does NOT run. This half was the SECOND defect (2026-09-27,
//     measured on master 6936c787): the first fix reported the refusal and
//     still exited 0 having run the verb, so `gopi --output-language "not a
//     lang!!" version` printed the refusal lines, printed the version, and
//     returned 0 — a typo and a correct declaration were indistinguishable to
//     any caller. A text-only assertion cannot see that: the text WAS present
//     and correct. Only the exit code can, which is why every case below pins
//     it.
//   - the SETTINGS channel keeps warn-and-continue (exit 0, with the report).
//     The asymmetry is the pinned decision and it is asserted here as a
//     DECISION: it is what keeps one stale line in the settings file from
//     failing every command run on that host;
//   - an UNSET or USABLE knob stays SILENT and exits 0. This half is not
//     decoration: the knob's contract is that configuring nothing emits
//     nothing, and a "fix" that printed a line — or refused — for every run
//     would break the compatibility promise the whole knob rests on.
//     Asserting only the refusal half would pass on a binary that simply
//     chattered (or refused) on every launch.
//
// RED PROOF (recorded in the report): the suite's ONE build is overridable with
// GOPI_TEST_BIN (cmd/agent/gopibin_test.go), so this same test run against a
// binary built BEFORE the fix fails with ZERO bytes of stderr where a refusal
// is required:
//
//	GOPI_TEST_BIN=<pre-fix binary> go test ./cmd/agent/ -run TestOutputLanguageValidation
//	go test ./cmd/agent/ -run TestOutputLanguageValidation   # the fixed tree

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/testutil"
	gopiversion "github.com/nickhuang99/gopi/internal/version"
)

// The refusal's two signature phrases come from the PRODUCTION constants
// (config.OutputLanguageRefusalPhrase / config.OutputLanguageNotAdoptedClaim),
// never from local copies: a test matching on its own copy of the sentence
// keeps passing after the binary is reworded, asserting nothing (Rule 14 — one
// definition, every reader imports it). BOTH the positive cases (the text must
// appear) and the silence cases (the text must NOT appear) match on them.
const (
	outputLanguageRefusalPhrase    = config.OutputLanguageRefusalPhrase
	outputLanguageNotAdoptedPhrase = config.OutputLanguageNotAdoptedClaim
	// outputLanguageMissingValuePhrase is the flag-with-no-value refusal, and
	// it comes from production for the same reason as the two above.
	outputLanguageMissingValuePhrase = config.OutputLanguageMissingValuePhrase
)

// outputLanguageSurface is one surface that exposes the knob: how to invoke it,
// what the knob is set to, and what the child's output must contain.
//
// The surfaces matter individually rather than collectively: each one is a
// DIFFERENT early return from main() (`-h` and `--help-env` return from the
// help scan, `config show` and `version` return at the subcommand chain), and
// the defect was that ALL of them bypassed the resolution. A single case would
// leave the others as an assumption.
type outputLanguageSurface struct {
	// name is the subtest name (the surface, for a failure message).
	name string
	// argv is the child's argument vector.
	argv []string
	// knob is the GOPI_OUTPUT_LANGUAGE value exported to the child, "" for
	// none — the env channel.
	knob string
	// flag is the --output-language value passed on argv, "" for none.
	flag string
	// settings is the settings.json body pointing at the OUTPUT_LANGUAGE key,
	// "" when the fixture must not carry the key at all.
	settings string
	// want are substrings the child's combined output MUST contain.
	want []string
	// silent marks a case whose output must contain NEITHER refusal phrase —
	// the knob is unset or usable and must therefore say nothing.
	silent bool
	// fatal marks a case whose declaration is EXPLICIT for this invocation
	// (the flag, or the env exported for this run) and therefore unusable
	// input: the child must exit config.OutputLanguagePerInvocationExitCode
	// and must NOT have run the verb. Unusable input that must NOT be fatal
	// (the settings channel) leaves this false and is asserted at exit 0 —
	// the two are distinguished by this field alone, so a case cannot be
	// silently moved from one policy to the other.
	fatal bool
	// wantAbsent are substrings the child's output must NOT contain. It is
	// how "did not proceed" is observed: `gopi version` prints its version
	// line, and a fatal must come BEFORE that.
	wantAbsent []string
}

// TestOutputLanguageValidationIsReachedOnEverySurface is the acceptance test
// for the dead-validation defect: every surface that exposes the knob must
// REPORT a declaration it cannot understand, and stay quiet otherwise.
func TestOutputLanguageValidationIsReachedOnEverySurface(t *testing.T) {
	bin := gopiTestBinPath(t)

	// versionBanner is the marker that the `version` verb RAN. The fatal cases
	// assert its ABSENCE: "printed the error and then did the thing anyway" is
	// exactly the pre-fix behaviour, and an exit-code assertion alone would
	// once again miss a run that refused loudly and proceeded.
	versionBanner := gopiversion.Version

	cases := []outputLanguageSurface{
		// --- a REFUSED env value, on each surface that exposes the knob ---
		{
			name: "env refused / top-level help",
			argv: []string{"-h"}, knob: "chinese", fatal: true,
			want: []string{
				config.OutputLanguageEnv + `="chinese"`,
				outputLanguageRefusalPhrase,
				config.OutputLanguageFatalLabel,
			},
		},
		{
			name: "env refused / help-env",
			argv: []string{"--help-env"}, knob: "chinese", fatal: true,
			want: []string{
				config.OutputLanguageEnv + `="chinese"`,
				outputLanguageRefusalPhrase,
			},
		},
		{
			name: "env refused / config show",
			argv: []string{CliVerbConfig, configShowVerb}, knob: "chinese", fatal: true,
			want: []string{
				config.OutputLanguageEnv + `="chinese"`,
				outputLanguageRefusalPhrase,
			},
		},
		{
			// A second dispatch verb: the chain returns early for the whole
			// group, so the refusal must not be attached to one verb's path.
			// The banner assertion is what proves the refusal PRECEDES it.
			name: "env refused / version",
			argv: []string{CliVerbVersion}, knob: "chinese", fatal: true,
			want:       []string{config.OutputLanguageEnv + `="chinese"`},
			wantAbsent: []string{versionBanner},
		},
		// --- a REFUSED flag value (the hoist is what makes it visible) ---
		{
			name: "flag refused / top-level help",
			argv: []string{"-h"}, flag: "chinese (simplified)", fatal: true,
			want: []string{
				// The refusal names the channel the declaration was made ON —
				// the FLAG here, not the env name the hoist publishes it as —
				// because "which of my declarations is wrong" is the question
				// the operator has to answer from this message.
				"--" + config.OutputLanguageFlagName + ` "chinese (simplified)"`,
				outputLanguageRefusalPhrase,
			},
		},
		{
			// THE OPERATOR'S OWN MEASURED CASE (epic 01a0e2e6-5300): the exact
			// command from the briefing, which used to exit 0 and print the
			// version.
			name: "flag refused / version",
			argv: []string{CliVerbVersion}, flag: "not a lang!!", fatal: true,
			want: []string{
				`"not a lang!!"`,
				outputLanguageRefusalPhrase,
				"--" + config.OutputLanguageFlagName,
				config.OutputLanguageValueQualifier,
				config.OutputLanguageExampleTags,
			},
			wantAbsent: []string{versionBanner},
		},
		{
			// A MISSING value (`--output-language` with no operand) is a
			// declaration with no usable content — the same class, its own
			// sentence, and it must not be read as "no flag at all".
			name: "flag with no value",
			argv: []string{CliVerbVersion, "--" + config.OutputLanguageFlagName}, fatal: true,
			want: []string{
				"--" + config.OutputLanguageFlagName,
				outputLanguageMissingValuePhrase,
			},
			wantAbsent: []string{versionBanner},
		},
		{
			// The `--flag=` spelling of the same missing value.
			name: "flag with empty value",
			argv: []string{"--" + config.OutputLanguageFlagName + "=", CliVerbVersion}, fatal: true,
			want:       []string{outputLanguageMissingValuePhrase},
			wantAbsent: []string{versionBanner},
		},
		// --- a REFUSED settings value (the LAST channel): warn-and-continue,
		// the PINNED asymmetry. Exit 0, reported, and the verb still runs.
		{
			name:     "settings refused / top-level help",
			argv:     []string{"-h"},
			settings: "{\"" + config.OutputLanguageSettingsKey + `":"??"}`,
			want: []string{
				config.OutputLanguageSettingsKey + `="??"`,
				outputLanguageRefusalPhrase,
				outputLanguageNotAdoptedPhrase,
			},
		},
		{
			// The other half of the asymmetry, and the reason it needs its own
			// case: the SAME unusable value in the settings file must let the
			// verb run — a settings fatal would break every command on the
			// host, which is exactly what this policy refuses to do.
			name:     "settings refused still runs the verb",
			argv:     []string{CliVerbVersion},
			settings: "{\"" + config.OutputLanguageSettingsKey + `":"??"}`,
			want: []string{
				config.OutputLanguageSettingsKey + `="??"`,
				versionBanner,
			},
		},
		// --- a declaration discarded while a LATER channel supplied the value:
		// the report must name the refusal AND what the chain adopted instead,
		// which is the "next channel decides" contract observed from outside.
		// A fatal env declaration still refuses — the settings fallthrough is
		// the RESOLUTION's contract, and it is not a licence for a command line
		// the operator got wrong to exit 0.
		{
			name: "env refused, settings adopted", fatal: true,
			argv: []string{"-h"}, knob: "chinese",
			settings: "{\"" + config.OutputLanguageSettingsKey + `":"zh-CN"}`,
			want: []string{
				config.OutputLanguageEnv + `="chinese"`,
				config.OutputLanguageFatalLabel,
			},
		},
		// --- SILENCE: the compatibility contract, which a chatty "fix" breaks ---
		{name: "unset stays silent", argv: []string{"-h"}, silent: true},
		{
			name: "usable env stays silent", argv: []string{"-h"},
			knob: "zh-CN", silent: true,
		},
		{
			name: "usable flag stays silent", argv: []string{"-h"},
			flag: "en-GB", silent: true,
		},
		{
			// `GOPI_OUTPUT_LANGUAGE=` (exported, but empty) is NOT a
			// declaration: the knob's documented "absent or empty means no
			// directive" contract, and the reason an empty variable must not
			// be read as an unusable one.
			name: "empty env stays silent", argv: []string{"-h"},
			knob: " ", silent: true,
		},
		{
			// The end-to-end half of the same contract: a USABLE declaration
			// must still let the verb run, exit 0 and say nothing.
			name: "usable flag runs the verb", argv: []string{CliVerbVersion},
			flag: "zh-CN", silent: true, want: []string{versionBanner},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := outputLanguageChildEnv(t, tc.knob, tc.settings)
			argv := append([]string{}, tc.argv...)
			if tc.flag != "" {
				argv = append(argv, "--"+config.OutputLanguageFlagName, tc.flag)
			}
			out, code := runGopiIn(t, bin, env, "", argv...)
			joined := strings.Join(argv, " ")
			if tc.fatal {
				if code != config.OutputLanguagePerInvocationExitCode {
					t.Fatalf("`gopi %s` exited %d, want %d — an EXPLICIT per-invocation declaration "+
						"that is unusable must be FATAL (before the fix it exited 0 and ran the verb, "+
						"which made a typo indistinguishable from a correct declaration):\n%s",
						joined, code, config.OutputLanguagePerInvocationExitCode, out)
				}
			} else if code != 0 {
				t.Fatalf("`gopi %s` exited %d (only the per-invocation channels are fatal; "+
					"a refused value from the settings file is REPORTED and the run continues)\noutput:\n%s",
					joined, code, out)
			}
			for _, absent := range tc.wantAbsent {
				if strings.Contains(out, absent) {
					t.Fatalf("`gopi %s` must NOT have proceeded (found %q — the refusal must fire "+
						"BEFORE the subcommand chain dispatches):\n%s", joined, absent, out)
				}
			}
			if tc.silent {
				for _, phrase := range []string{
					outputLanguageRefusalPhrase,
					outputLanguageNotAdoptedPhrase,
					config.OutputLanguageFatalLabel,
				} {
					if strings.Contains(out, phrase) {
						t.Fatalf("`gopi %s` must say NOTHING about a knob that is unset or usable "+
							"(found %q):\n%s", joined, phrase, out)
					}
				}
				return
			}
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Fatalf("`gopi %s` with %s=%q must report %q — the declaration the operator "+
						"wrote was read by nobody (the dead-validation defect):\n%s",
						joined, config.OutputLanguageEnv, tc.knob, want, out)
				}
			}
		})
	}
}

// outputLanguageChildEnv is the child's environment: the suite's mocked-run base
// (which strips every GOPI_* the test process inherited, so a case that sets no
// knob really has none) plus this case's channels.
//
// A settings fixture is written ONLY when the case carries a settings value: an
// ABSENT file is the honest "nobody asked" (FromSettingsFile documents
// defaults-on-missing), and pointing GOPI_SETTINGS at nothing would make the
// settings channel's behaviour a property of the fixture rather than of the
// resolver.
func outputLanguageChildEnv(t *testing.T, knob, settings string) []string {
	t.Helper()
	env := testutil.MockEnv(t, t.TempDir(), mockLLMURLForKnob)
	if settings != "" {
		p := filepath.Join(t.TempDir(), "settings.json")
		if err := os.WriteFile(p, []byte(settings), 0o600); err != nil {
			t.Fatalf("write the settings fixture: %v", err)
		}
		env = append(env, config.SettingsEnv+"="+p)
	}
	if knob != "" {
		env = append(env, config.OutputLanguageEnv+"="+knob)
	}
	return env
}

// mockLLMURLForKnob is a placeholder endpoint. These cases never talk to a
// model — every one of them returns from main() before a session is built — and
// the value is named (Rule 1b) rather than inlined at the call site for the
// same reason cli_config_show_tmpdir_test.go names its own.
const mockLLMURLForKnob = "http://127.0.0.1:1"
