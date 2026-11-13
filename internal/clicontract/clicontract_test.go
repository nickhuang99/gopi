package clicontract

import (
	"strings"
	"testing"
)

// mustParseChain / mustParseMirror fail the test on a shape error: an
// assertion over a NIL parse result (the silently-ignored-error form) can
// pass vacuously, so every fixture is parsed through these.
func mustParseChain(t *testing.T, src string) []string {
	t.Helper()
	got, err := ParseDispatchChain(src)
	if err != nil {
		t.Fatalf("ParseDispatchChain: %v", err)
	}
	return got
}

// mirrorFixtureRepo prefixes a mirror fixture the way the real file holds it
// (`var cliContractDispatchPredicates = []string{`), so a fixture can never be
// parsed by a substring the production anchor would reject.
func mirrorFixtureRepo(body string) string {
	return "var cliContractDispatchPredicates = []string{" + body + "}\n"
}

func mustParseMirror(t *testing.T, src string) []string {
	t.Helper()
	got, err := ParseContractMirror(mirrorFixtureRepo(src))
	if err != nil {
		t.Fatalf("ParseContractMirror: %v", err)
	}
	return got
}

// Realistic fixtures: a main.go dispatch chain (the shape bd3d34a5 produced —
// `|| isVoiceSubcommand(dispatchArgs)` appended) and the contract-suite
// mirror, both at master's pre-incident and the incident's post states.

const chainBase = `	if isCompletionSubcommand(dispatchArgs) || isVersionSubcommand(dispatchArgs) || isWorkerSubcommand(dispatchArgs) || isMemorySubcommand(dispatchArgs) || isMemorySyncSubcommand(dispatchArgs) || isWebSearchSubcommand(dispatchArgs) || isWebFetchSubcommand(dispatchArgs) || isVisionSubcommand(dispatchArgs) || isText2imgSubcommand(dispatchArgs) || isCodeSearchSubcommand(dispatchArgs) || isLogSubcommand(dispatchArgs) || isStatsSubcommand(dispatchArgs) || isRawSubcommand(dispatchArgs) || isResumeSubcommand(dispatchArgs) || isCheckpointSubcommand(dispatchArgs) || isProxySubcommand(dispatchArgs) || isTasksSubcommand(dispatchArgs) || isChatSubcommand(dispatchArgs) || isWebSubcommand(dispatchArgs) || isBrewSubcommand(dispatchArgs) || isMemsyncSubcommand(dispatchArgs) || isShotSubcommand(dispatchArgs) || isScreenCaptureSubcommand(dispatchArgs) || isActSubcommand(dispatchArgs) || isScratchGCSubcommand(dispatchArgs) {
		dispatchSubcommand(dispatchArgs)
	}
`

// chainVoice is bd3d34a5's main.go: the chain with VoiceSubcommand appended.
const chainVoice = `	if isCompletionSubcommand(dispatchArgs) || isVersionSubcommand(dispatchArgs) || isWorkerSubcommand(dispatchArgs) || isMemorySubcommand(dispatchArgs) || isMemorySyncSubcommand(dispatchArgs) || isWebSearchSubcommand(dispatchArgs) || isWebFetchSubcommand(dispatchArgs) || isVisionSubcommand(dispatchArgs) || isText2imgSubcommand(dispatchArgs) || isCodeSearchSubcommand(dispatchArgs) || isLogSubcommand(dispatchArgs) || isStatsSubcommand(dispatchArgs) || isRawSubcommand(dispatchArgs) || isResumeSubcommand(dispatchArgs) || isCheckpointSubcommand(dispatchArgs) || isProxySubcommand(dispatchArgs) || isTasksSubcommand(dispatchArgs) || isChatSubcommand(dispatchArgs) || isWebSubcommand(dispatchArgs) || isBrewSubcommand(dispatchArgs) || isMemsyncSubcommand(dispatchArgs) || isShotSubcommand(dispatchArgs) || isScreenCaptureSubcommand(dispatchArgs) || isActSubcommand(dispatchArgs) || isScratchGCSubcommand(dispatchArgs) || isVoiceSubcommand(dispatchArgs) {
		dispatchSubcommand(dispatchArgs)
	}
`

const mirrorBase = `var cliContractDispatchPredicates = []string{
	"CompletionSubcommand", "VersionSubcommand", "WorkerSubcommand",
	"MemorySubcommand", "MemorySyncSubcommand", "WebSearchSubcommand",
	"WebFetchSubcommand", "VisionSubcommand", "Text2imgSubcommand",
	"CodeSearchSubcommand", "LogSubcommand", "StatsSubcommand",
	"RawSubcommand", "ResumeSubcommand", "CheckpointSubcommand",
	"ProxySubcommand", "TasksSubcommand", "ChatSubcommand",
	"WebSubcommand", "BrewSubcommand", "MemsyncSubcommand",
	"ShotSubcommand", "ScreenCaptureSubcommand", "ActSubcommand",
	"ScratchGCSubcommand",
}
`

// mirrorVoice is the contract suite bd3d34a5 SHOULD have carried (and did
// not): the mirror with the new predicate tracked.
const mirrorVoice = `var cliContractDispatchPredicates = []string{
	"CompletionSubcommand", "VersionSubcommand", "WorkerSubcommand",
	"MemorySubcommand", "MemorySyncSubcommand", "WebSearchSubcommand",
	"WebFetchSubcommand", "VisionSubcommand", "Text2imgSubcommand",
	"CodeSearchSubcommand", "LogSubcommand", "StatsSubcommand",
	"RawSubcommand", "ResumeSubcommand", "CheckpointSubcommand",
	"ProxySubcommand", "TasksSubcommand", "ChatSubcommand",
	"WebSubcommand", "BrewSubcommand", "MemsyncSubcommand",
	"ShotSubcommand", "ScreenCaptureSubcommand", "ActSubcommand",
	"ScratchGCSubcommand", "VoiceSubcommand",
}
`

func TestParseDispatchChain_RealShape(t *testing.T) {
	got, err := ParseDispatchChain(chainBase)
	if err != nil {
		t.Fatalf("ParseDispatchChain: %v", err)
	}
	if len(got) != 25 {
		t.Fatalf("parsed %d predicates, want 25: %v", len(got), got)
	}
	if got[0] != "CompletionSubcommand" || got[len(got)-1] != "ScratchGCSubcommand" {
		t.Fatalf("chain endpoints wrong: first=%q last=%q", got[0], got[len(got)-1])
	}
	voice, err := ParseDispatchChain(chainVoice)
	if err != nil {
		t.Fatalf("ParseDispatchChain(voice): %v", err)
	}
	if len(voice) != 26 || voice[25] != "VoiceSubcommand" {
		t.Fatalf("voice chain = %d preds, last=%q; want 26 ending VoiceSubcommand", len(voice), voice[len(voice)-1])
	}
}

func TestParseDispatchChain_RefactoredChainErrorsNotRefuses(t *testing.T) {
	// A chain that lost its anchor or shrank must be an ERROR (the caller
	// abstains), never parsed as a valid chain with 0 predicates.
	cases := map[string]string{
		"no anchor":     "func main() { dispatch(dispatchArgs) }\n",
		"unterminated":  "if isCompletionSubcommand(dispatchArgs)\n",
		"too few preds": "if isCompletionSubcommand(dispatchArgs) { x() }\n",
	}
	for name, src := range cases {
		if _, err := ParseDispatchChain(src); err == nil {
			t.Errorf("%s: ParseDispatchChain = nil error, want a shape error (abstention)", name)
		}
	}
}

func TestParseContractMirror_RealShape(t *testing.T) {
	got, err := ParseContractMirror(mirrorFixtureRepo(mirrorBase))
	if err != nil {
		t.Fatalf("ParseContractMirror: %v", err)
	}
	if len(got) != 25 {
		t.Fatalf("parsed %d mirror entries, want 25: %v", len(got), got)
	}
	if got[0] != "CompletionSubcommand" || got[len(got)-1] != "ScratchGCSubcommand" {
		t.Fatalf("mirror endpoints wrong: first=%q last=%q", got[0], got[len(got)-1])
	}
}

func TestParseContractMirror_MissingListErrors(t *testing.T) {
	// No declaration at all.
	if _, err := ParseContractMirror("package main\n"); err == nil {
		t.Fatal("ParseContractMirror = nil error for a source with no mirror list, want error")
	}
	// Declared but tracking NOTHING: an empty parse is the shape error, not a
	// valid "mirror of zero predicates" (which would silently satisfy a slice
	// comparison and bless every wired predicate).
	if _, err := ParseContractMirror("var cliContractDispatchPredicates = []string{}\n"); err == nil {
		t.Fatal("ParseContractMirror = nil error for an empty mirror list, want a shape error")
	}
	// Declared, unterminated.
	if _, err := ParseContractMirror("var cliContractDispatchPredicates = []string{"); err == nil {
		t.Fatal("ParseContractMirror = nil error for an unterminated mirror list, want error")
	}
}

// TestCompare_VoiceInputIncidentIsCaught is the RED proof at the pure-parser
// level: the exact bd3d34a5 shape (main.go gains VoiceSubcommand, the mirror
// does not) must be reported as drift with the predicate named.
func TestCompare_VoiceInputIncidentIsCaught(t *testing.T) {
	baseChain := mustParseChain(t, chainBase)
	baseMirror := mustParseMirror(t, mirrorBase)
	voiceChain := mustParseChain(t, chainVoice)

	// tip = the incident branch (new chain, UNCHANGED mirror)
	// target = master before it (base chain + base mirror)
	d := Compare(voiceChain, baseMirror, baseChain, baseMirror, nil)
	if d.Empty() {
		t.Fatal("Compare = empty drift for the bd3d34a5 shape, want WiredUntracked=[VoiceSubcommand]")
	}
	if len(d.WiredUntracked) != 1 || d.WiredUntracked[0] != "VoiceSubcommand" {
		t.Fatalf("WiredUntracked = %v, want [VoiceSubcommand]", d.WiredUntracked)
	}
	if len(d.TrackedUnwired) != 0 {
		t.Fatalf("TrackedUnwired = %v, want none", d.TrackedUnwired)
	}
	if !strings.Contains(d.Detail(), "VoiceSubcommand") ||
		!strings.Contains(d.Detail(), ContractSuiteFile) {
		t.Errorf("Detail = %q, want it to name the predicate and the contract file", d.Detail())
	}
}

// TestCompare_FixedBranchIsClean is the positive half: the SAME branch with
// its contract mirror updated AND an inventory token routed to the new
// predicate must pass — the gate refuses the drift, not the feature. (The
// mirror entry alone is NOT enough: the project's guard needs the token map
// entry to find the contract case, which the live demonstration on the real
// incident confirmed.)
func TestCompare_FixedBranchIsClean(t *testing.T) {
	baseChain := mustParseChain(t, chainBase)
	baseMirror := mustParseMirror(t, mirrorBase)
	voiceChain := mustParseChain(t, chainVoice)
	voiceMirror := mustParseMirror(t, mirrorVoice)

	if d := Compare(voiceChain, voiceMirror, baseChain, baseMirror, []string{"VoiceSubcommand"}); !d.Empty() {
		t.Fatalf("Compare = %v, want clean for a branch that carries both halves", d)
	}
}

// TestCompare_InheritedDriftIsNotRefused: a branch that changed nothing about
// the dispatch chain must not be refused for drift master already carries
// (rebasing master's own main.go into a branch is not the branch's fault).
func TestCompare_InheritedDriftIsNotRefused(t *testing.T) {
	voiceChain := mustParseChain(t, chainVoice)
	baseMirror := mustParseMirror(t, mirrorBase)
	// tip == target == the already-drifted tree; the branch changed nothing.
	if d := Compare(voiceChain, baseMirror, voiceChain, baseMirror, nil); !d.Empty() {
		t.Fatalf("Compare = %v, want clean when the branch introduces no drift of its own", d)
	}
}

// TestCompare_MirrorOnTargetSideCounts: the merge target's own mirror entry
// covers the predicate, so a branch relying on master's already-tracked
// mirror is NOT refused (the union rule — a false negative beats a false
// refusal on the merge path).
func TestCompare_MirrorOnTargetSideCounts(t *testing.T) {
	baseChain := mustParseChain(t, chainBase)
	voiceChain := mustParseChain(t, chainVoice)
	voiceMirror := mustParseMirror(t, mirrorVoice) // target already tracks it

	if d := Compare(voiceChain, baseChain, baseChain, voiceMirror, []string{"VoiceSubcommand"}); !d.Empty() {
		t.Fatalf("Compare = %v, want clean when the target's mirror already tracks the predicate", d)
	}
}

// TestCompare_RemovedPredicateIsCaught is the other direction: a branch that
// stops dispatching a predicate while the mirror still tracks it is the same
// count mismatch (the test goes red either way), so it is refused too.
func TestCompare_RemovedPredicateIsCaught(t *testing.T) {
	baseChain := mustParseChain(t, chainBase)
	voiceMirror := mustParseMirror(t, mirrorVoice) // tracks VoiceSubcommand
	if d := Compare(baseChain, voiceMirror, baseChain, voiceMirror, nil); !d.Empty() {
		t.Fatalf("Compare = %v, want clean (tip tracks everything it wires)", d)
	}
	// tip wires fewer predicates than the target, while the mirrors still
	// list the removed one → a count mismatch on the merged tree.
	baseMirror := mustParseMirror(t, mirrorBase)
	voiceChain := mustParseChain(t, chainVoice)
	d := Compare(baseChain, baseMirror, voiceChain, voiceMirror, nil)
	if len(d.TrackedUnwired) != 1 || d.TrackedUnwired[0] != "VoiceSubcommand" {
		t.Fatalf("TrackedUnwired = %v, want [VoiceSubcommand]", d.TrackedUnwired)
	}
}

// TestParseDispatchChain_RenamedArgumentStillParses guards the live-demo
// footgun: pinning the literal `(dispatchArgs)` made the gate abstain (merge
// unchanged, forever) the moment main.go renamed that local variable. The
// parser must key on the predicate, not the argument's spelling.
func TestParseDispatchChain_RenamedArgumentStillParses(t *testing.T) {
	renamed := strings.ReplaceAll(chainVoice, "(dispatchArgs)", "(a)")
	got, err := ParseDispatchChain(renamed)
	if err != nil {
		t.Fatalf("ParseDispatchChain(renamed arg): %v — the gate must not go blind on a local rename", err)
	}
	if len(got) != 26 || got[25] != "VoiceSubcommand" {
		t.Fatalf("renamed-arg chain = %d preds (last %q), want 26 ending VoiceSubcommand", len(got), got[len(got)-1])
	}
	// And the drift is still detected through the renamed chain.
	baseChain := mustParseChain(t, strings.ReplaceAll(chainBase, "(dispatchArgs)", "(a)"))
	baseMirror := mustParseMirror(t, mirrorBase)
	d := Compare(got, baseMirror, baseChain, baseMirror, nil)
	if len(d.WiredUntracked) != 1 || d.WiredUntracked[0] != "VoiceSubcommand" {
		t.Fatalf("WiredUntracked = %v, want [VoiceSubcommand] through a renamed argument", d.WiredUntracked)
	}
}

// TestParseDispatchChain_MixedArgumentsAbstains: a chain that dispatches on
// two DIFFERENT variables is a shape change — abstain, never guess.
func TestParseDispatchChain_MixedArgumentsAbstains(t *testing.T) {
	mixed := strings.ReplaceAll(chainVoice, "isLogSubcommand(dispatchArgs)", "isLogSubcommand(otherArgs)")
	if _, err := ParseDispatchChain(mixed); err == nil {
		t.Fatal("ParseDispatchChain = nil error for a mixed-argument chain, want a shape error")
	}
}

// TestCompare_TokenMapArmIsRequired: the SECOND red shape the live
// demonstration found on the real incident — the mirror tracks the predicate
// but NO inventory token routes to it (dispatcherOfToken has no entry), which
// the project's guard reports as "predicates without a contract case". The
// gate must refuse that too, or it lands a half-fixed branch that is still
// red on master.
func TestCompare_TokenMapArmIsRequired(t *testing.T) {
	voiceChain := mustParseChain(t, chainVoice)
	baseChain := mustParseChain(t, chainBase)
	voiceMirror := mustParseMirror(t, mirrorVoice)

	// Mirror updated, token map NOT: still drift.
	d := Compare(voiceChain, voiceMirror, baseChain, mustParseMirror(t, mirrorBase), nil)
	if len(d.WiredUntracked) != 1 || d.WiredUntracked[0] != "VoiceSubcommand" {
		t.Fatalf("WiredUntracked = %v, want [VoiceSubcommand] when no token routes to it", d.WiredUntracked)
	}
	// Both halves: clean.
	if d := Compare(voiceChain, voiceMirror, baseChain, mustParseMirror(t, mirrorBase), []string{"VoiceSubcommand"}); !d.Empty() {
		t.Fatalf("Compare = %v, want clean with the token-map entry present", d)
	}
	// A token map that routes OTHER predicates does not blanket-cover.
	if d := Compare(voiceChain, voiceMirror, baseChain, mustParseMirror(t, mirrorBase), []string{"ActSubcommand"}); d.Empty() {
		t.Fatal("Compare = empty drift, want VoiceSubcommand still untracked (token coverage is per-predicate)")
	}
}

// TestParseDispatcherTokenMap_RealShape pins the map parser against the real
// map's shape (a gofmt-aligned literal with bare string values) and its
// abstention when the map is absent.
func TestParseDispatcherTokenMap_RealShape(t *testing.T) {
	const src = `var dispatcherOfToken = map[string]string{
	CliVerbCompletion:           "CompletionSubcommand",
	toolnames.ToolVisionAnalyze: "VisionSubcommand",
}

var unrelated = 1
`
	got, err := ParseDispatcherTokenMap(src)
	if err != nil {
		t.Fatalf("ParseDispatcherTokenMap: %v", err)
	}
	if len(got) != 2 || got[0] != "CompletionSubcommand" || got[1] != "VisionSubcommand" {
		t.Fatalf("parsed %v, want [CompletionSubcommand VisionSubcommand]", got)
	}
	if _, err := ParseDispatcherTokenMap("package main\n"); err == nil {
		t.Fatal("ParseDispatcherTokenMap = nil error for a source with no map, want error")
	}
}
