package tools

// chat_long_spelling_harness_test.go — the LONG class's group spellings are
// commands that EXIST, and the proof is the classifier's own verdict plus the
// binary's own answer rather than a grep (task 01a1958f; Rule 31: execute the
// surface, do not read it).
//
// WHAT WENT WRONG. internal/chatpolicy's built-in category table named three
// spellings that dispatch nothing, and the classifier reads that table, so the
// defect was live on the chat surface rather than cosmetic:
//
//	gopi merge       RETIRED — the word exits 1 with a refusal naming its
//	                 survivor, `gopi task merge`, which was itself ABSENT from
//	                 the table. The live spelling therefore escaped
//	                 classification entirely while the dead one held a row.
//	gopi serve       NOT A VERB — `gopi serve -h` prints the top-level help
//	                 verbatim (measured: 170 lines, byte-identical to `gopi -h`),
//	                 so no `gopi serve` command line can ever arrive to be
//	                 classified.
//	gopi scratch-gc  RETIRED — the flat word exits 1 naming `gopi tool
//	                 scratch-gc`, which is the word that runs.
//
// WHY THE CRITERION IS BEHAVIOUR AND NOT THE WORD LIST. "The row names a
// command that exists" is a claim about the BINARY, and a text scan of the table
// cannot answer it — deleting a row and renaming a verb leave the same trace. So
// each group row is put through the real classifier AND the real binary
// (testutil.RunGopi), and a failure names which half disagreed.

import (
	"os"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/cliverbs"
	"github.com/nickhuang99/gopi/internal/runcmd"
	"github.com/nickhuang99/gopi/internal/taskverbs"
	"github.com/nickhuang99/gopi/internal/testutil"
)

const (
	// chatLongHelpFlag is the one flag every probe uses: a verb's usage page
	// runs nothing and takes no lock, which is what makes it a safe probe of
	// "does this word dispatch at all". The merge page says so itself
	// (`gopi task merge -h  this page: prints, runs nothing, takes no lock`).
	chatLongHelpFlag = "-h"
	// chatLongHelpExitCode is what a dispatching verb's help page exits with.
	chatLongHelpExitCode = 0
	// chatLongMergeLiveFlag is a real flag of the LIVE merge spelling, taken
	// from its own page (`gopi task merge --all`: sweep every local branch
	// except the integration branch) — a probe names an argument shape the
	// command actually documents rather than the bare word.
	chatLongMergeLiveFlag = "--all"
	// chatLongScratchGCLiveArm is one arm of the live scratch sweep, from its
	// page (`gopi tool scratch-gc sandboxes`).
	chatLongScratchGCLiveArm = "sandboxes"
	// chatLongRetiredMarker is the exact fragment a RETIREMENT REFUSAL carries
	// (`gopi: \`merge\` is RETIRED — use \`gopi task merge\` …`). It is the WORD
	// plus its punctuation, not the bare adjective: the live `gopi task merge`
	// help page mentions the retired verb in prose ("'gopi merge' was removed"),
	// and a predicate matching the adjective alone reported that LIVE page as a
	// refusal — measured on this tree, 2026-11-01. The fragment is spelled here
	// because the sentence lives in `package main` (unimportable from here); the
	// arm that drives the binary is what keeps it honest.
	chatLongRetiredMarker = "RETIRED — use "
	// chatLongProgramWordOffset is how many leading fields of a composed command
	// line are the PROGRAM WORD. `gopi` is argv[0] of a shell command line and
	// must never be passed as an argument: handing it to the binary as argv[0]
	// makes it a PROMPT positional, which turned every probe in this file into
	// "gopi gopi <verb> -h" — the top-level help, exit 0, for every spelling.
	chatLongProgramWordOffset = 1
)

// chatLiveSpelling is one group row: the command line the classifier and the
// binary are driven with, and WHY that is the live spelling.
type chatLiveSpelling struct {
	cmd string
	why string
}

// chatRetiredSpelling is one flat word the table must no longer carry as long,
// with the measurement that retired it and the SHAPE its refusal takes.
type chatRetiredSpelling struct {
	cmd  string
	why  string
	kind string
}

const (
	// chatRefusalRetired is a word that refuses BY NAME and prints where the
	// capability went: non-zero exit AND a retirement sentence.
	chatRefusalRetired = "retired"
	// chatRefusalNotAVerb is a word that was never a verb: it does not refuse,
	// it prints the TOP-LEVEL help, which is measurable as byte-identity with
	// `gopi -h`.
	chatRefusalNotAVerb = "not-a-verb"
)

// chatLiveGroupSpellings is the ONE list of group-shaped long operations the
// tests in this package drive. Every command line is COMPOSED from the leaf
// constants that own its words (Rules 13/14 — the discipline the table itself
// follows), so a rename reaches the tests by construction instead of leaving a
// stale literal behind.
//
// It is a TEST-ONLY selection, not a second vocabulary: the table's own
// contents are asserted against ChatOpLongOperationVocabulary() by
// TestChatOpCategoryClassPredicateIsExhaustiveAndDisjoint, so this list can
// only ever name words the table already declares.
func chatLiveGroupSpellings() []chatLiveSpelling {
	gopi := runcmd.BinaryName
	return []chatLiveSpelling{
		{
			cmd: gopi + " " + cliverbs.Worker,
			why: "`gopi worker` dispatches — it runs the queued tasks",
		},
		{
			cmd: gopi + " " + cliverbs.Task + " " + taskverbs.Merge + " " + chatLongMergeLiveFlag,
			why: "`gopi task merge` is the surviving merge trigger (`gopi merge` was retired)",
		},
		{
			cmd: gopi + " " + cliverbs.Task + " " + taskverbs.Sync,
			why: "`gopi task sync` is the cross-node reconciliation face",
		},
		{
			cmd: gopi + " " + cliverbs.Task + " " + taskverbs.Push,
			why: "`gopi task push` is the push half of that face",
		},
		{
			cmd: gopi + " " + cliverbs.Tool + " " + cliverbs.ScratchGC + " " + chatLongScratchGCLiveArm,
			why: "`gopi tool scratch-gc` is the surviving scratch sweep (`gopi scratch-gc` was retired)",
		},
	}
}

// chatRetiredFlatSpellings is the other half: the flat words the table used to
// name, and the measurement that retired each one.
func chatRetiredFlatSpellings() []chatRetiredSpelling {
	gopi := runcmd.BinaryName
	return []chatRetiredSpelling{
		{
			cmd:  gopi + " " + taskverbs.Merge,
			why:  "the word is RETIRED: it exits 1 with '`merge` is RETIRED — use `gopi task merge`'",
			kind: chatRefusalRetired,
		},
		{
			// The bare word has no constant of its own because it is not a
			// top-level verb at all — that absence IS the evidence. The word is
			// spelled from the family constant that owns it rather than typed.
			cmd:  gopi + " " + taskverbs.Serve,
			why:  "it is NOT a verb: `gopi serve -h` prints the top-level help verbatim, so nothing dispatches here",
			kind: chatRefusalNotAVerb,
		},
		{
			cmd:  gopi + " " + cliverbs.ScratchGC,
			why:  "the word is RETIRED: it exits 1 with '`scratch-gc` is RETIRED — use `gopi tool scratch-gc`'",
			kind: chatRefusalRetired,
		},
	}
}

// TestChatOpLongSpellingsNameCommandsThatExist is the STANDING assertion: the
// live group spellings are LONG and are refused unauthorised, and no flat word
// the table dropped is still classified long (a row that matches nothing is a
// row the prompt still RENDERS — the model would be taught to ask about a
// command that does not exist).
func TestChatOpLongSpellingsNameCommandsThatExist(t *testing.T) {
	for _, live := range chatLiveGroupSpellings() {
		admission := ChatOpAdmit(live.cmd, "")
		if got := admission.Class(); got != ChatOpClassLong {
			t.Errorf("%q (%s) classified as %s — the table no longer reaches the live spelling, so the "+
				"operator is never asked about the operation", live.cmd, live.why, got)
		}
		if admission.Allowed() {
			t.Errorf("%q (%s) was ADMITTED with nothing declared — a long operation is the one class the "+
				"operator must authorise", live.cmd, live.why)
		}
	}
	for _, dead := range chatRetiredFlatSpellings() {
		if got := ChatOpClassify(dead.cmd); got == ChatOpClassLong {
			t.Errorf("%q is still classified LONG, but %s — the table still carries a word that dispatches "+
				"nothing", dead.cmd, dead.why)
		}
	}
}

// TestChatOpLongSpellingsLiveBinary drives the REAL binary (Rule 31): each
// claimed-live spelling must answer `-h` with exit 0 and no retirement
// sentence, and each claimed-dead one must fail exactly the way its own
// measurement says it does — by name, or by printing the top-level help.
func TestChatOpLongSpellingsLiveBinary(t *testing.T) {
	for _, live := range chatLiveGroupSpellings() {
		out, code := chatProbeHelp(t, live.cmd)
		if chatIsRetiredSentence(out) {
			t.Errorf("%q is claimed LIVE (%s) but the binary answers with a RETIREMENT sentence:\n%s",
				live.cmd, live.why, out)
			continue
		}
		if code != chatLongHelpExitCode {
			t.Errorf("%q -h exited %d (want %d) — a long row must name a command whose help page answers:\n%s",
				live.cmd, code, chatLongHelpExitCode, out)
		}
	}
	topLevelHelp, topCode := chatProbeHelp(t, runcmd.BinaryName)
	for _, dead := range chatRetiredFlatSpellings() {
		out, code := chatProbeHelp(t, dead.cmd)
		switch dead.kind {
		case chatRefusalRetired:
			if code == chatLongHelpExitCode {
				t.Errorf("%q is claimed RETIRED (%s) but `-h` exited 0 — the word still dispatches, so the "+
					"measurement behind its removal is wrong:\n%s", dead.cmd, dead.why, out)
				continue
			}
			if !chatIsRetiredSentence(out) {
				t.Errorf("%q exited %d but printed no retirement sentence, so a reader is not told where the "+
					"capability went:\n%s", dead.cmd, code, out)
			}
		case chatRefusalNotAVerb:
			// The measurement IS the identity: a word that dispatches nothing
			// hands the argv to the top-level help, so its `-h` page is the
			// top-level page, byte for byte. A word that answered its OWN page
			// would be a verb, and the row that names it would be legitimate.
			if out != topLevelHelp || code != topCode {
				t.Errorf("%q is claimed NOT A VERB (%s) but its `-h` output differs from `%s -h` "+
					"(exit %d vs %d) — the word has a page of its own, so it is a command and the row's "+
					"removal was wrong:\n%s", dead.cmd, dead.why, runcmd.BinaryName, code, topCode, out)
			}
		default:
			t.Fatalf("%q carries the unknown refusal kind %q (want %q|%q)", dead.cmd, dead.kind,
				chatRefusalRetired, chatRefusalNotAVerb)
		}
	}
}

// chatProbeHelp runs one shell command line with the help flag in a fresh
// scratch directory and returns its combined output and exit code.
//
// THE PROGRAM WORD IS ARGV[0], NOT AN ARGUMENT — see chatLongProgramWordOffset
// for the measured reason: passing `gopi` through to the binary made the binary
// read the word as a PROMPT positional and print the top-level help with exit 0
// for EVERY spelling, which made the retired-word arms of this test pass for the
// wrong reason. Dropping it is what makes the probe a probe.
func chatProbeHelp(t *testing.T, shellCommandLine string) (string, int) {
	t.Helper()
	argv := strings.Fields(shellCommandLine)[chatLongProgramWordOffset:]
	stdout, stderr, code := testutil.RunGopi(t,
		append(argv, chatLongHelpFlag), "", chatProbeEnv(t), t.TempDir())
	return stdout + stderr, code
}

// chatIsRetiredSentence reports whether the output is a RETIREMENT REFUSAL —
// the sentence the binary prints when it refuses a removed spelling and names
// the one that runs.
func chatIsRetiredSentence(output string) bool {
	return strings.Contains(output, chatLongRetiredMarker)
}

// chatProbeEnv is the environment a probe runs under: the caller's own, copied
// so nothing a probe does can leak into a sibling test (Rule 17's spirit: a test
// owns its inputs).
func chatProbeEnv(t *testing.T) []string {
	t.Helper()
	return append([]string(nil), os.Environ()...)
}
