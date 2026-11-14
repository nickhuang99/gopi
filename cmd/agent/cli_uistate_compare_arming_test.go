package main

// cli_uistate_compare_arming_test.go — THE LOOP, as a test: an agent must be able
// to arm its own screen, find the artifact, and run the one-call check against
// the same input — and the same input must FAIL when the arming is missing.
//
// WHY THIS FILE EXISTS. Every part of the self-observation loop shipped and the
// loop had still never been run, because the dump was never ARMED before the run
// that produced the display defect. The check therefore reported on nothing, and
// nothing in the suite could notice: the compare tests all build their dump
// fixture directly, so no test ever depended on the arming being reachable. This
// file is that dependency, in the order the agent lives it:
//
//	1. `gopi chat --dump-screen` asks for the dump — through the SAME resolver the
//	   REPL uses (chatDumpRequest), so a help text that promised a place the run
//	   does not write to fails HERE;
//	2. `gopi ui_state --target chat` finds it — through the verb's own discovery;
//	3. `gopi ui_state --compare "<line>" --session <id>` answers — exit 0 when the
//	   line is on the painted grid and in the transcript.
//
// The RED leg is the same three steps with the arming removed, and it must exit 1
// ("could not run"), NOT 0: an unarmed screen is not a verified one.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/uiclient"
)

// TestChatDumpArming_MakesTheCheckRunAndUnarmedDoesNot is the RED-then-GREEN proof
// on ONE input: the same line, the same transcript, the same profile layout —
// only the arming differs.
func TestChatDumpArming_MakesTheCheckRunAndUnarmedDoesNot(t *testing.T) {
	path := compareFixture(t, compareLine, uiclient.GeometrySourceLayout, compareLine)
	session := filepath.Join(filepath.Dir(path), "..", compareSessionFileName)

	// GREEN: an armed run's artifact is discovered by the verb (no --pid, no
	// path) and the check runs to a verdict.
	out, code := uiStateRun(t, []string{
		uiStateFlagCompare, compareLine,
		uiStateFlagSession, session,
		uiStateFlagTarget, string(uiclient.SurfaceChat),
	})
	if code != 0 {
		t.Fatalf("GREEN leg: exit = %d, want 0 for a line that is on the grid and in the transcript\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, string(uiclient.CompareMatch)) {
		t.Fatalf("GREEN leg: the answer is not %q:\n%s", uiclient.CompareMatch, out)
	}

	// RED: remove the arming — the artifact the loop reads is simply not there —
	// and the SAME call must refuse to answer instead of reporting agreement.
	if err := os.Remove(path); err != nil {
		t.Fatalf("RED leg: remove the armed artifact: %v", err)
	}
	if err := os.Remove(path + dumpJSONTwinSuffixForTest); err != nil && !os.IsNotExist(err) {
		t.Fatalf("RED leg: remove the artifact's twin: %v", err)
	}
	outRed, codeRed := uiStateRun(t, []string{
		uiStateFlagCompare, compareLine,
		uiStateFlagSession, session,
		uiStateFlagTarget, string(uiclient.SurfaceChat),
	})
	if codeRed != 1 {
		t.Fatalf("RED leg: exit = %d, want 1 — an unarmed surface has no screen to check, and agreeing with it would be the false pass this whole change exists to remove\noutput:\n%s",
			codeRed, outRed)
	}
	if strings.Contains(outRed, string(uiclient.CompareMatch)) {
		t.Fatalf("RED leg: an unarmed surface was reported as %q:\n%s", uiclient.CompareMatch, outRed)
	}
	if !strings.Contains(outRed, uiclient.DumpFileName) || !strings.Contains(outRed, CliVerbUIState) {
		t.Fatalf("RED leg: the refusal does not name what is missing and which verb to run:\n%s", outRed)
	}
}

// dumpJSONTwinSuffixForTest is the machine-checkable twin's suffix, spelled here
// so this RED leg removes exactly the pair the sink writes. The producer's own
// spelling lives in internal/uiclient (unexported); a test that asked the product
// for it could not notice the twin moving, which is the failure this leg is about.
const dumpJSONTwinSuffixForTest = ".json"

// TestChatDumpArming_TheHelpTextCarriesTheCommandThatArms is the DISCOVERABILITY
// half of the remedy, asserted where an agent actually reads it: the chat verb's
// own page must spell the loop (arm → find → check), because the measured
// obstruction was that nothing said "arm the dump first" before the run whose
// screen no longer existed to be read.
func TestChatDumpArming_TheHelpTextCarriesTheCommandThatArms(t *testing.T) {
	// The exact three commands an agent must be able to copy out of `gopi chat -h`.
	for _, want := range []string{
		"gopi chat " + chatDumpFlag,                                  // step 1: arm this run
		CliVerbUIState + " --target " + string(uiclient.SurfaceChat), // step 2: is a dump on hand
		uiStateFlagCompare,                                           // step 3: the one-call check
		uiStateFlagSession,
		"exit 1 = the check could NOT", // the exit-code contract
	} {
		if !strings.Contains(chatUsage, want) {
			t.Errorf("chatUsage does not carry %q — an agent that suspects its own display cannot find the arming command, which is how the loop went unrun:\n%s", want, chatUsage)
		}
	}
	// The choice must be an informed one: the default and its cost are stated, and
	// so is the way OUT — a default without a documented off switch is a decision
	// taken from the operator, and the loop's bytes are only unchanged on the path
	// that declines it.
	if !strings.Contains(chatUsage, "ON BY DEFAULT") {
		t.Error("chatUsage does not state that the dump is on by default (an operator who was not told cannot decide whether to decline it)")
	}
	if !strings.Contains(chatUsage, chatNoDumpFlag) {
		t.Error("chatUsage does not name the OFF switch — the default's way out is undocumented")
	}
	if !strings.Contains(chatUsage, "byte-identical") {
		t.Error("chatUsage does not state the no-dump path's byte-identity — the reason a run that declines the dump is unchanged")
	}
}

// TestChatDumpOffSwitch_ParsesAndWinsOverEverything is the OFF SWITCH's own
// contract, on the ONE parser (there is no second argv walk to keep in step): a
// bare `--no-dump-screen` disables the dump that is otherwise armed by default, a
// value beside it is refused BY NAME (a flag that silently discarded its value
// would teach a caller something false about their own command line), and the
// flag outranks a truthy environment — the same precedence direction the arm flag
// has.
func TestChatDumpOffSwitch_ParsesAndWinsOverEverything(t *testing.T) {
	parsed, matched, err := parseChatDumpArg(chatNoDumpFlag, "", false)
	if !matched || err != nil {
		t.Fatalf("%s did not parse as a dump word: matched=%v err=%v", chatNoDumpFlag, matched, err)
	}
	if !parsed.disabled || parsed.requested || parsed.dumped() {
		t.Errorf("%s parsed as %+v — it must DISABLE the dump (requested=false disabled=true dumped()=false)", chatNoDumpFlag, parsed)
	}

	// A value beside the off switch is refused, and the message names the fix.
	if _, matched, err := parseChatDumpArg(chatNoDumpFlag, "some.dump", true); !matched || err == nil {
		t.Errorf("%s with a following value was accepted (matched=%v err=%v) — the off switch takes no value and must say so", chatNoDumpFlag, matched, err)
	}

	// The DEFAULT is armed, and the off switch is the one state that is not.
	if !chatDumpDefault().dumped() {
		t.Error("the default dump options are not armed — the whole change is an opt-out")
	}

	// PRECEDENCE, on the real parse order (env first, flag overrides): a truthy
	// environment arms the dump, and the flag on the command line turns it off.
	t.Setenv(chatDumpEnv, "1")
	fromEnv := chatDumpFromEnv()
	if !fromEnv.dumped() {
		t.Fatalf("%s=1 did not arm the dump: %+v", chatDumpEnv, fromEnv)
	}
	if parsed.dumped() {
		t.Error("the flag did not override the environment: the run would write the artifact the caller declined")
	}

	// A falsey environment turns the default OFF, and the state is STATED (so the
	// resume chain knows this run made a choice, and the default cannot masquerade
	// as one).
	t.Setenv(chatDumpEnv, "off")
	offEnv := chatDumpFromEnv()
	if offEnv.dumped() || !offEnv.stated {
		t.Errorf("%s=off parsed as %+v — a falsey value must disable AND count as a stated choice", chatDumpEnv, offEnv)
	}
	// ...while a run that says NOTHING is armed and has stated nothing.
	t.Setenv(chatDumpEnv, "")
	quiet := chatDumpFromEnv()
	if !quiet.dumped() || quiet.stated {
		t.Errorf("a silent environment parsed as %+v — the default must be armed and must NOT count as a stated choice (it would supersede a resumed session's own record)", quiet)
	}
}

// TestChatDumpFlags_AreInTheVerbsOwnFlagList pins the ONE cross-check that
// caught a real drift on this change (2026-10-01): the flag LIST the verb
// prints in an unknown-flag refusal is not the same thing as the words its
// parser READS, and a new flag missing from the list makes the usage page
// describe a command line that cannot express the capability. The off switch is
// the case: the default's way out has to be a word the page admits exists.
func TestChatDumpFlags_AreInTheVerbsOwnFlagList(t *testing.T) {
	listed := chatREPLFlags()
	for _, flag := range []string{chatDumpFlag, chatNoDumpFlag} {
		found := false
		for _, l := range listed {
			if l == flag {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s is read by the verb's parser but is NOT in chatREPLFlags() — the usage page and every unknown-flag refusal would omit it", flag)
		}
		if !strings.Contains(chatUsageLine, flag) {
			t.Errorf("%s is missing from chatUsageLine — the one-line usage does not admit the flag exists", flag)
		}
	}
	// The off switch is not merely listed: the parser must actually READ it, so a
	// run that passes it is not refused as an unknown flag.
	if _, matched, err := parseChatDumpArg(chatNoDumpFlag, "", false); !matched || err != nil {
		t.Errorf("%s is listed as a flag but the parser does not read it (matched=%v err=%v)", chatNoDumpFlag, matched, err)
	}
}

// TestChatDumpDefaultArmsWithoutAnyFlag is the DEFAULT, end to end through the
// resolver the REPL calls: with the environment silent and no flag, the run is
// ARMED and gets a destination of its own — and the OFF switch resolves to no
// path at all, which is what makes the paint stream byte-identical (there is no
// sink to tee into).
func TestChatDumpDefaultArmsWithoutAnyFlag(t *testing.T) {
	t.Setenv(chatDumpEnv, "")
	path, err := chatResolveDumpPath(chatDumpFromEnv(), nil)
	if err != nil {
		t.Fatalf("the default dump request did not resolve: %v", err)
	}
	if !strings.HasSuffix(path, uiclient.ChatDumpFileName) {
		t.Errorf("the default destination is %q — want a path ending in %s beside this run's own artifacts", path, uiclient.ChatDumpFileName)
	}

	down, err := chatResolveDumpPath(chatDumpOptions{disabled: true, stated: true}, nil)
	if err != nil {
		t.Fatalf("the off switch's request did not resolve: %v", err)
	}
	if down != "" {
		t.Errorf("the off switch resolved to %q — a declined dump must resolve to NO path", down)
	}
	// And that path is what makes the writer byte-identical: no sink, the editor's
	// own writer back, unchanged (the same contract chat_dump_writer_order_test.go
	// pins on the object).
	sink, err := chatArmDump(down, 117, 26, false, nil, nil)
	if err != nil {
		t.Fatalf("arming a declined dump failed: %v", err)
	}
	if sink != nil {
		t.Errorf("a declined dump armed a sink (%v) — the run would pay the cost it declined", sink)
	}
}

// TestChatDumpArming_TheTopLevelIndexNamesTheSurfaces pins the DISCOVERABILITY
// ladder's FIRST rung, deliberately — not the loop's text.
//
// The top-level page is an INDEX BY CONTRACT: `gopi -h` is capped at 160 lines
// (TestTopLevelUsageDelegatesEnvVarsToHelpEnv) and states "each accepts -h for its
// full help". Measured here: the baseline page renders 159 of those 160 lines, so
// adding the loop's three commands there breaks the budget the help-format epic
// established. The loop therefore lives in the verb pages — `gopi chat -h` carries
// the ARMING flag and the three-command loop, `gopi ui_state -h` carries the check
// and its exit contract (both pinned above) — and the job of THIS page is that an
// agent can FIND those verbs without knowing their flags already.
func TestChatDumpArming_TheTopLevelIndexNamesTheSurfaces(t *testing.T) {
	usage := config.FormatUsage()
	for _, want := range []string{
		// The ruled line for the flag-bearing surface: an agent that cannot see
		// `gopi chat` here never reaches `gopi chat -h`, where the arming lives.
		"gopi chat",
		// The check's own verb, in the index.
		"gopi " + CliVerbUIState,
		// The pointer that makes the verb pages reachable at all.
		"accepts -h for its full help",
	} {
		if !strings.Contains(usage, want) {
			t.Errorf("`gopi -h` does not name %q — the verb pages carrying the loop would be unreachable from the page an agent reads first", want)
		}
	}
}

// TestChatDumpArming_TheVerbPagesSplitTheLoop pins WHERE the loop's two halves
// live, so the split cannot silently collapse into one page (an agent reads the
// ARMING page when arming and the CHECK page when checking; neither page alone is
// the loop).
func TestChatDumpArming_TheVerbPagesSplitTheLoop(t *testing.T) {
	if !strings.Contains(chatUsage, chatDumpFlag) {
		t.Errorf("`gopi chat -h` does not carry the arming flag %s — the page an operator reads before starting the surface is where arming must be visible", chatDumpFlag)
	}
	if !strings.Contains(uiStateUsage, uiStateFlagCompare) {
		t.Errorf("`gopi ui_state -h` does not carry %s — the check's own page is where its contract belongs", uiStateFlagCompare)
	}
	if !strings.Contains(uiStateUsage, chatDumpFlag) {
		t.Errorf("`gopi ui_state -h` does not name %s — the check is unreadable without a dump, and its page must say what makes one exist", chatDumpFlag)
	}
}
