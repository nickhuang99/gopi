package main

// Contract for the NO-VERB resume face: `gopi --session <id> ["<prompt>"]`
// (epic resume-surface convergence, step 1).
//
// What this file pins, and why each arm exists:
//
//  1. `gopi --session -h` answers with the FLAG's own page — not the top-level
//     one. The top-level help scan runs earlier in main(), so an unguarded scan
//     would swallow the query (the hand-rolled-arm defect this repo has paid
//     for). The page is also the proof that no verb was added: `gopi session
//     -h`'s family page stays the address for the verb spelling.
//  2. A uuid in a NON-CANONICAL spelling is refused by internal/sessionid's own
//     sentence (with the canonical id it meant), exit != 0, and NOTHING is
//     created — joining such an id is exactly what mints a second session
//     container beside the row's own.
//  3. An unknown id is a resolution failure naming what was looked at, exit != 0
//     — never an agent run (that is Rule 15's failure mode: a silent
//     fall-through into a paid session).
//  4. NO session artifact is created by any of the refusal arms: the assertions
//     read the profile's sessions directory, so a regression that lets the flag
//     leak into the agent path fails here even if the exit code is right.
//
// Every arm drives the REAL binary (Rule 31): the routing this face adds is
// main()'s, and only a process can answer whether main() reached it.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/runcmd"
)

// sessionFlagSessionsDir is the profile path a leaked run would populate
// (<GOPI_HOME>/.gopi/sessions). Asserting on it — rather than on
// <GOPI_HOME>/sessions — is what makes the no-run claim mean anything:
// config.GopiDir resolves the profile leaf, so a run writes THERE.
func sessionFlagSessionsDir(t *testing.T, home string) string {
	t.Helper()
	return filepath.Join(home, ".gopi", "sessions")
}

// firstUsageLine is the LEAD line of a help page's output: the first non-empty
// line, trimmed. It exists so a help assertion can compare against the page's
// own synopsis exactly — "the answer STARTS with the page's synopsis" — instead
// of testing whether the synopsis occurs somewhere in the text.
func firstUsageLine(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return strings.TrimSpace(out)
}

func assertNoSessionArtifactsUnder(t *testing.T, what, home string) {
	t.Helper()
	entries, err := os.ReadDir(sessionFlagSessionsDir(t, home))
	if os.IsNotExist(err) {
		return // nothing materialised — the good case
	}
	if err != nil {
		t.Fatalf("%s: read sessions dir: %v", what, err)
	}
	if len(entries) > 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("%s: created session artifact(s) %v — the argv reached the agent path", what, names)
	}
}

// TestSessionResumeFlagHelpIsItsOwnPage pins the help query: the flag's page,
// exit 0, and no run.
func TestSessionResumeFlagHelpIsItsOwnPage(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	out, err := runCLIContractProbeRaw(t, bin, home, sessionResumeFlag, "-h")
	if err != nil {
		t.Fatalf("gopi %s -h: exit error: %v\n--- output ---\n%s", sessionResumeFlag, err, out)
	}
	if !strings.Contains(out, "Usage: gopi "+sessionResumeFlag+" <session-id|raw-log> [--task <id>]") {
		t.Fatalf("gopi %s -h did not print the flag's own page — got:\n%s", sessionResumeFlag, out)
	}
	// The two things this face can be asked to continue are stated on the page:
	// a session id AND a raw-log path (the spawned-child shape uses the path),
	// plus the row linkage the worker/merger recovery children carry.
	for _, want := range []string{"--task <id>", "resolved", "Examples:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("gopi %s -h: the page does not state %q — got:\n%s", sessionResumeFlag, want, out)
		}
	}
	// The page must not advertise a VERB (Rule 3): the usage line names the flag.
	if strings.Contains(out, "Usage: gopi session resume") {
		t.Fatalf("gopi %s -h printed the verb's page — this face adds no verb:\n%s", sessionResumeFlag, out)
	}
	assertNoSessionArtifactsUnder(t, "gopi "+sessionResumeFlag+" -h", home)
}

// TestSessionResumeFlagRefusesNonCanonicalUUID pins the SHAPE gate: the refusal
// is internal/sessionid's, verbatim, and it names the canonical id it meant.
func TestSessionResumeFlagRefusesNonCanonicalUUID(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	// The branch/worktree spelling of a uuid: 32 hex characters, no grouping.
	const spelled = "01a0ae5ff4d073479b952382a4a7d352"
	const canonical = "01a0ae5f-f4d0-7347-9b95-2382a4a7d352"

	out, err := runCLIContractProbeRaw(t, bin, home, sessionResumeFlag, spelled)
	if err == nil {
		t.Fatalf("gopi %s %s exited 0 — a non-canonical uuid must be refused\n%s", sessionResumeFlag, spelled, out)
	}
	for _, want := range []string{"non-canonical spelling", "did you mean", canonical} {
		if !strings.Contains(out, want) {
			t.Fatalf("gopi %s %s: refusal does not carry %q — got:\n%s", sessionResumeFlag, spelled, want, out)
		}
	}
	assertNoSessionArtifactsUnder(t, "gopi "+sessionResumeFlag+" <non-canonical>", home)
}

// TestSessionResumeFlagUnknownSessionIsAResolutionFailure pins the second
// refusal class: a canonical id with no session behind it fails to LOCATE a raw
// log, names what it looked at, exits non-zero, and buys no session.
func TestSessionResumeFlagUnknownSessionIsAResolutionFailure(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	const unknown = "01a096c9-6f7d-7e43-8e46-9ecca58e9cd0"
	// The sessions ROOT must exist for the failure to be the interesting one:
	// with no root at all the resolver stops one layer earlier ("cannot resolve
	// session id … no such file or directory"), which is a different (also
	// legitimate) sentence. This arm pins the sentence an operator sees on a
	// profile whose session store is there and simply holds no such id.
	if err := os.MkdirAll(sessionFlagSessionsDir(t, home), 0o755); err != nil {
		t.Fatalf("mkdir sessions root: %v", err)
	}

	out, err := runCLIContractProbeRaw(t, bin, home, sessionResumeFlag, unknown)
	if err == nil {
		t.Fatalf("gopi %s %s exited 0 — an unresolvable session must fail\n%s", sessionResumeFlag, unknown, out)
	}
	if !strings.Contains(out, "is neither a raw log path nor an existing session id") {
		t.Fatalf("gopi %s %s: the failure does not name the resolution — got:\n%s", sessionResumeFlag, unknown, out)
	}
	if !strings.Contains(out, "gopi "+sessionResumeFlag+":") {
		t.Fatalf("gopi %s %s: the diagnostic does not name THIS face — got:\n%s", sessionResumeFlag, unknown, out)
	}
	assertNoSessionArtifactsUnder(t, "gopi "+sessionResumeFlag+" <unknown>", home)
}

// TestSessionResumeFlagRequiresAnID pins the presence arm.
func TestSessionResumeFlagRequiresAnID(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	out, err := runCLIContractProbeRaw(t, bin, home, sessionResumeFlag)
	if err == nil {
		t.Fatalf("gopi %s with no id exited 0\n%s", sessionResumeFlag, out)
	}
	if !strings.Contains(out, "requires a session id") {
		t.Fatalf("gopi %s: refusal does not name the missing id — got:\n%s", sessionResumeFlag, out)
	}
	assertNoSessionArtifactsUnder(t, "gopi "+sessionResumeFlag, home)
}

// TestSessionResumeFlagScanTakesTheIDAndLeavesThePrompt pins the argv split: the
// id comes from the flag (both spellings), the tail is the prompt, and a help
// word is NOT taken as an id (`gopi --session -h` asks a question).
func TestSessionResumeFlagScanTakesTheIDAndLeavesThePrompt(t *testing.T) {
	const id = "01a096c9-6f7d-7e43-8e46-9ecca58e9cd0"
	cases := []struct {
		name     string
		argv     []string
		wantID   string
		wantTail []string
		wantFind bool
	}{
		{"space form with prompt", []string{"--session", id, "run the tests"}, id, []string{"run the tests"}, true},
		{"equals form with prompt", []string{"--session=" + id, "run the tests"}, id, []string{"run the tests"}, true},
		{"space form, no prompt", []string{"--session", id}, id, nil, true},
		{"help is not an id", []string{"--session", "-h"}, "", []string{"-h"}, true},
		{"bare flag", []string{"--session"}, "", nil, true},
		{"not this face", []string{"task", "list"}, "", nil, false},
		{"not this face (bare prompt)", []string{"write a README"}, "", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, tail, found := scanSessionResumeFlag(tc.argv)
			if found != tc.wantFind || id != tc.wantID {
				t.Fatalf("scanSessionResumeFlag(%v) = (%q, %v, %v), want id %q found %v",
					tc.argv, id, tail, found, tc.wantID, tc.wantFind)
			}
			if strings.Join(tail, "\x00") != strings.Join(tc.wantTail, "\x00") {
				t.Fatalf("scanSessionResumeFlag(%v) tail = %v, want %v", tc.argv, tail, tc.wantTail)
			}
			// The prompt is the POSITIONAL remainder; a flag in it is named back.
			prompt, configArgs, _, bad := sessionResumePrompt(tail)
			if bad != "" && bad != "-h" {
				t.Fatalf("sessionResumePrompt(%v) refused %q unexpectedly", tail, bad)
			}
			if bad == "" && len(configArgs) != 0 {
				t.Fatalf("sessionResumePrompt(%v) collected config args %v from a plain tail", tail, configArgs)
			}
			if bad == "" && tc.wantID != "" && len(tc.wantTail) > 0 && prompt != tc.wantTail[0] {
				t.Fatalf("sessionResumePrompt(%v) = %q, want %q", tail, prompt, tc.wantTail[0])
			}
		})
	}
}

// TestSessionResumeFlagIsTheOtherAddressOfOneImplementation pins the STRUCTURE
// the epic demands: the no-verb face hands its work to resumeSession — the core
// extracted from runResume — instead of carrying a second copy of the resume
// pipeline. A source pin is the right instrument here: the defect it prevents
// (a mirrored implementation drifting from the one the verb uses) is visible
// only as a second body, never as a wrong exit code.
func TestSessionResumeFlagIsTheOtherAddressOfOneImplementation(t *testing.T) {
	src, err := os.ReadFile("cli_resume_session_flag.go")
	if err != nil {
		t.Fatalf("read cli_resume_session_flag.go: %v", err)
	}
	body := string(src)
	if !strings.Contains(body, "resumeSession(sessionResumeProgName,") {
		t.Fatalf("the --session face no longer calls resumeSession — the resume pipeline must have ONE implementation (runResume's body was extracted for exactly this)")
	}
	for _, forbidden := range []string{"agent.LoadCheckpoint(", "newAgent(", "ag.Resume("} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("the --session face carries its own %s call — that is a SECOND resume implementation beside resumeSession", forbidden)
		}
	}

	core, err := os.ReadFile("cli_resume.go")
	if err != nil {
		t.Fatalf("read cli_resume.go: %v", err)
	}
	if !strings.Contains(string(core), "func resumeSession(progName, target, continuation, taskID, prompt string) int {") {
		t.Fatalf("resumeSession's signature changed — update this pin and the --session call site together")
	}
	// The TARGET operand is named by resumeTargetAndTail (the family criterion:
	// a flag's side of the operand never changes which token is the operand),
	// so the pin names that variable instead of the old unconditional args[0].
	// What it pins is unchanged: the verb routes through the ONE core, passes
	// its continuation-file positional, and passes NO inline prompt.
	if !strings.Contains(string(core), "return resumeSession(resumeProgName, target, continuation, taskID, \"\")") {
		t.Fatalf("runResume no longer routes through resumeSession with its continuation-file contract — the extraction's whole point is that the verb face keeps its positional semantics")
	}
	if !strings.Contains(string(core), "target, tail := resumeTargetAndTail(args)") {
		t.Fatalf("runResume no longer splits its target through resumeTargetAndTail — reading args[0] as the target is the defect that made `resume --task T <id>` address \"--task\"")
	}
}

// TestSessionResumeFlagSymmetryWithTheBarePromptFace is the epic's OWN contract:
// the two same-shaped faces must agree on the CONFIGURATION knobs.
//
// The defect this pins was measured on the built binary before the change: the
// bare-prompt run accepted `--settings X` and the no-verb resume refused it as
// an unknown option — an asymmetry in configuration alone, since both faces run
// the same agent stack with the same data root.
//
// The accepted set is NOT restated here: every positive row is driven twice —
// once through this face's argv, once through the configuration resolution the
// run path uses — and the assertion is that the resume face CONSUMED the knob
// (it never reaches the prompt, it reaches config.FlagOverrides) rather than
// that some particular word is on a list. A catalog knob added tomorrow is
// therefore covered by construction, and a hand-written list in the face would
// fail the negative arm (`--bogus` must still be refused).
func TestSessionResumeFlagAcceptsCatalogConfigKnobs(t *testing.T) {
	const (
		id     = "01a096c9-6f7d-7e43-8e46-9ecca58e9cd0"
		prompt = "run the suite"
	)
	cases := []struct {
		name       string
		tail       []string
		wantPrompt string
		wantConfig []string
		wantBad    string
	}{
		{
			name:       "--settings BEFORE the positional prompt",
			tail:       []string{"--settings", "/tmp/zz.json", prompt},
			wantPrompt: prompt,
			wantConfig: []string{"--settings", "/tmp/zz.json"},
		},
		{
			name:       "--home AFTER the positional prompt",
			tail:       []string{prompt, "--home", "/tmp/zzhome"},
			wantPrompt: prompt,
			wantConfig: []string{"--home", "/tmp/zzhome"},
		},
		{
			name:       "--tmpdir in the inline form",
			tail:       []string{"--tmpdir=/tmp/zz-tmp", prompt},
			wantPrompt: prompt,
			wantConfig: []string{"--tmpdir=/tmp/zz-tmp"},
		},
		{
			name:       "a catalog boolean keeps its boolean operand out of the prompt",
			tail:       []string{"--debug", "false", prompt},
			wantPrompt: prompt,
			wantConfig: []string{"--debug", "false"},
		},
		{
			name:    "a non-catalog option is still refused BY NAME",
			tail:    []string{"--bogus", prompt},
			wantBad: "--bogus",
		},
		{
			name:    "a single-dash option is refused too",
			tail:    []string{"-z"},
			wantBad: "-z",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prompt, configArgs, _, bad := sessionResumePrompt(tc.tail)
			if bad != tc.wantBad {
				t.Fatalf("sessionResumePrompt(%v) badFlag = %q, want %q", tc.tail, bad, tc.wantBad)
			}
			if tc.wantBad != "" {
				if configArgs != nil || prompt != "" {
					t.Fatalf("sessionResumePrompt(%v) returned prompt %q / config %v beside a refusal", tc.tail, prompt, configArgs)
				}
				return
			}
			if prompt != tc.wantPrompt {
				t.Fatalf("sessionResumePrompt(%v) prompt = %q, want %q (a config knob must never become prompt text)", tc.tail, prompt, tc.wantPrompt)
			}
			if strings.Join(configArgs, "\x00") != strings.Join(tc.wantConfig, "\x00") {
				t.Fatalf("sessionResumePrompt(%v) configArgs = %v, want %v", tc.tail, configArgs, tc.wantConfig)
			}
			// THE OTHER HALF OF THE SYMMETRY: the collected words are exactly what
			// the run path's OWN parser accepts. Driven, not assumed — a name this
			// face consumed and config.FlagOverrides rejects would be a refusal
			// the operator meets only at runtime.
			if _, err := config.FlagOverrides(configArgs); err != nil {
				t.Fatalf("the resume face accepted %v but the run path's parser rejects them: %v", configArgs, err)
			}
		})
	}
	_ = id
}

// TestSessionResumeFlagRealBinaryConsumesTheKnob is the Rule 31 arm: the argv
// reaches main(), and main() resolves the knob. The observation is the RESOLVED
// PATH, not an exit code — `--home /nonexistent` proves the knob was consumed by
// making the session lookup happen under that home (the sentence names the
// directory the resolution used), and `--help` proves the help query still wins
// over a knob's presence.
func TestSessionResumeFlagRealBinaryConsumesTheKnob(t *testing.T) {
	bin := gopiTestBinPath(t)
	const id = "01a096c9-6f7d-7e43-8e46-9ecca58e9cd0"

	t.Run("help still answers after a config knob", func(t *testing.T) {
		home := t.TempDir()
		out, err := runCLIContractProbeRaw(t, bin, home, sessionResumeFlag, id, "--settings", filepath.Join(home, "zz-nonexistent.json"), HelpFlagLong)
		if err != nil {
			t.Fatalf("gopi %s <id> --settings … --help exited non-zero: %v\n%s", sessionResumeFlag, err, out)
		}
		// THE CONTRACT: the help query answered FIRST — the page, not the knob's
		// resolution. The comparison is the page's OWN synopsis constant
		// (sessionResumeUsageHead), never a re-typed copy of it: a copy is what
		// drifted here before, and it drifted SILENTLY (the page was right; the
		// assertion was quoting an older one). Comparing the LEAD line exactly
		// also keeps the check non-vacuous — a substring test passes on any page
		// that merely repeats the synopsis somewhere, while this one demands the
		// answer START with it, which is what "help still answers" means.
		if head := firstUsageLine(out); head != sessionResumeUsageHead {
			t.Fatalf("the help page did not answer first — its lead line is %q, want the page's own synopsis %q — got:\n%s",
				head, sessionResumeUsageHead, out)
		}
	})

	t.Run("--home is consumed and moves the session lookup", func(t *testing.T) {
		home := t.TempDir()
		elsewhere := filepath.Join(home, "elsewhere")
		out, err := runCLIContractProbeRaw(t, bin, home, sessionResumeFlag, id, "--home", elsewhere)
		if err == nil {
			t.Fatalf("gopi %s <id> --home <fresh> exited 0 — an id with no session must still fail\n%s", sessionResumeFlag, out)
		}
		if !strings.Contains(out, filepath.Join(elsewhere, ".gopi", "sessions")) {
			t.Fatalf("the refusal does not name the profile --home selected (%s) — the knob was NOT consumed:\n%s",
				filepath.Join(elsewhere, ".gopi", "sessions"), out)
		}
	})

	t.Run("a non-catalog option is refused by that name", func(t *testing.T) {
		home := t.TempDir()
		out, err := runCLIContractProbeRaw(t, bin, home, sessionResumeFlag, id, "--bogus")
		if err == nil {
			t.Fatalf("gopi %s <id> --bogus exited 0 — a stray option must be refused\n%s", sessionResumeFlag, out)
		}
		if !strings.Contains(out, `unknown option "--bogus"`) {
			t.Fatalf("the refusal does not name the offending option — got:\n%s", out)
		}
	})
}

// TestSessionResumeFlagConsumesTheRowLinkage pins the arm that made the whole
// resume face unusable from a SPAWNED child (measured 2026-10-18): `--task <id>`
// is printed in this face's own usage page and composed by the ONE resume
// renderer (runcmd.ResumeArgv), yet the face refused it by name — exit 1, before
// the first instruction — so every `gopi worker --resume` run (and every
// dead-holder recovery child) reported nothing and left its row unclosed.
//
// The observation is the PARSER's answer, in both spellings, plus the refusal
// that must survive beside it: an option the face does not carry is still named
// back (Rule 15), and `--task` with no operand is refused by the OPERAND, never
// swallowed as the prompt.
func TestSessionResumeFlagConsumesTheRowLinkage(t *testing.T) {
	const (
		task   = "t-worker-resume-e2e"
		prompt = "run the suite"
	)
	cases := []struct {
		name       string
		tail       []string
		wantTask   string
		wantPrompt string
		wantBad    string
	}{
		{
			name:       "space form, before the prompt",
			tail:       []string{runcmd.FlagTask, task, prompt},
			wantTask:   task,
			wantPrompt: prompt,
		},
		{
			name:       "inline form, after the prompt",
			tail:       []string{prompt, runcmd.FlagTask + "=" + task},
			wantTask:   task,
			wantPrompt: prompt,
		},
		{
			name:    "no operand is a refusal about the operand",
			tail:    []string{runcmd.FlagTask},
			wantBad: runcmd.FlagTask,
		},
		{
			name:    "an operand that is another flag is not eaten",
			tail:    []string{runcmd.FlagTask, "--bogus"},
			wantBad: runcmd.FlagTask,
		},
		{
			name:    "an inline empty operand is the same refusal",
			tail:    []string{runcmd.FlagTask + "="},
			wantBad: runcmd.FlagTask,
		},
		{
			name:    "a still-unknown option is refused BY NAME beside the linkage",
			tail:    []string{runcmd.FlagTask, task, "--bogus"},
			wantBad: "--bogus",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prompt, _, taskID, bad := sessionResumePrompt(tc.tail)
			if bad != tc.wantBad {
				t.Fatalf("sessionResumePrompt(%v) badFlag = %q, want %q", tc.tail, bad, tc.wantBad)
			}
			if tc.wantBad != "" {
				return
			}
			if taskID != tc.wantTask {
				t.Fatalf("sessionResumePrompt(%v) taskID = %q, want %q — a spawned child's row linkage was dropped", tc.tail, taskID, tc.wantTask)
			}
			if prompt != tc.wantPrompt {
				t.Fatalf("sessionResumePrompt(%v) prompt = %q, want %q — the row id must not leak into the prompt", tc.tail, prompt, tc.wantPrompt)
			}
		})
	}
}

// TestSessionResumeFaceExecutesTheRowLinkage is the Rule 31 arm for the same
// fix: the argv runcmd.ResumeArgv renders (the SHAPE a spawned child is given)
// is handed to the REAL binary, and the proof that the row linkage was CONSUMED
// is the face's own answer about the row — not an "unknown option" refusal.
//
// The target is unresolvable on purpose, so the run stops at resolution: what is
// being pinned is the ARGV CONTRACT (the face accepts what the renderer emits),
// not a happy path.
func TestSessionResumeFaceExecutesTheRowLinkage(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	makeSessionRoot(t, home)

	const unknownTarget = "01a096c9-6f7d-7e43-8e46-9ecca58e9cd0"
	argv := runcmd.ResumeArgv(unknownTarget, "t-worker-resume-e2e")
	out, err := runCLIContractProbeRaw(t, bin, home, argv[1:]...)
	if err == nil {
		t.Fatalf("%s exited 0 on an unresolvable target\n%s", strings.Join(argv, " "), out)
	}
	if strings.Contains(out, "unknown option") || strings.Contains(out, "unknown flag") {
		t.Fatalf("%s: the face REFUSED the argv the renderer emits — a spawned child cannot reach its row:\n%s",
			strings.Join(argv, " "), out)
	}
	if !strings.Contains(out, "is neither a raw log path nor an existing session id") {
		t.Fatalf("%s: the run did not reach the resume resolution — got:\n%s", strings.Join(argv, " "), out)
	}
	// The CONTRAST that makes the assertion above evidence: the same face still
	// refuses a word it does not carry, so "accepted" cannot mean "accepts
	// anything".
	bad, _ := runCLIContractProbeRaw(t, bin, home, sessionResumeFlag, unknownTarget, "--bogus") //stylegate:ignore the contrast arm asserts the refusal text; both a zero and a non-zero status are legitimate outcomes for a refused option
	if !strings.Contains(bad, `unknown option "--bogus"`) {
		t.Fatalf("%s --bogus: a stray option was no longer refused by name — got:\n%s", sessionResumeFlag, bad)
	}
}
