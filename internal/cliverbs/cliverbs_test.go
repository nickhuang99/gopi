package cliverbs

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestSenseVerbsMatchCmdAgentDispatch is the drift guard that makes this leaf
// package safe: every constant here is the SAME spelling as the cmd/agent
// cli_verbs.go constant that DISPATCHES it, and each one has a live
// `isXxxSubcommand` predicate gating on it. A rename on either side fails
// here instead of silently splitting the prompt's vocabulary from the CLI's.
func TestSenseVerbsMatchCmdAgentDispatch(t *testing.T) {
	// Scan the WHOLE cmd/agent package, not only cli_verbs.go: the primary
	// SSOT block lives there, but a few top-level verbs are declared beside
	// their own handler (CliVerbAct in cli_act.go) — a single-file read would
	// report a false "no dispatch-side owner".
	matches, err := filepath.Glob(filepath.Join("..", "..", "cmd", "agent", "*.go"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("glob cmd/agent/*.go: %v", err)
	}
	var b strings.Builder
	for _, f := range matches {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		b.Write(src)
		b.WriteString("\n")
	}
	text := b.String()
	want := map[string]string{ // sense verb value -> cmd/agent constant name
		UIState:       "CliVerbUIState",
		Log:           "CliVerbLog",
		Raw:           "CliVerbRaw",
		Stats:         "CliVerbStats",
		Task:          "CliVerbTask",
		Capture:       "CliVerbCapture",
		ScreenCapture: "CliVerbScreenCapture",
		Act:           "CliVerbAct",
		Sandbox:       "CliVerbSandbox",
		// Chat/UI are the two SURFACES the eye reads (the epic that added them
		// exists because the prompt named only `ui`): a rename on either side
		// must fail here, or the prompt teaches a surface the CLI does not have.
		Chat: "CliVerbChat",
		UI:   "CliVerbUI",
		// Notify is reached from OUTSIDE cmd/agent (the cross-peer hop composes
		// `gopi notify --notify-remote` as a remote argv), so a rename on either
		// side must fail here rather than silently put a word on the wire that
		// the peer's dispatch does not know.
		Notify: "CliVerbNotify",
	}
	for verb, constName := range want {
		// `const X = "v"` (a verb declared beside its handler) and `\tX = "v"`
		// (the const block in cli_verbs.go) are both a declaration.
		re := regexp.MustCompile(`(?m)^\s*(?:const\s+)?` + constName + `\s*=\s*"([^"]*)"`)
		m := re.FindStringSubmatch(text)
		if m == nil {
			t.Errorf("cmd/agent/cli_verbs.go declares no %s — the leaf constant %q has no dispatch-side owner",
				constName, verb)
			continue
		}
		if m[1] != verb {
			t.Errorf("%s = %q but cliverbs spells %q — the prompt would teach a verb the CLI does not dispatch",
				constName, m[1], verb)
		}
	}
}

// cliverbsAliasTargets are the constants declared in THIS package that a
// cmd/agent constant may legally be an alias of. The flag drift guard resolves
// through them, so `rawFlagFile = cliverbs.RawFileWord` (Rule 13's
// one-definition shape, the same one `CliVerbMerge = cliverbs.Merge` uses) is
// checked as rigorously as a literal would be — and a rename on EITHER side
// still fails.
var cliverbsAliasTargets = map[string]string{
	"RawFileWord":    RawFileWord,
	"FileFlag":       FileFlag,
	"DumpScreen":     DumpScreen,
	"ScreenStream":   ScreenStream,
	"NoScreenStream": NoScreenStream,
	"ScreenReplay":   ScreenReplay,
	// The pre-rename aliases are legal alias TARGETS too: a cmd/agent constant
	// that still says `cliverbs.SgrStream` (the deprecated spelling kept alive
	// for the alias parse path) must resolve through the one definition, so a
	// rename of ScreenStream still fails here instead of drifting.
	"SgrStream":    SgrStream,
	"NoSgrStream":  NoSgrStream,
	"SgrReplay":    SgrReplay,
	"TargetFlag":   TargetFlag,
	"Compare":      Compare,
	"SessionFlag":  SessionFlag,
	"TaskTagsWord": TaskTagsWord,
	"TaskTagsFlag": TaskTagsFlag,
}

// declaredConstantRHS returns the right-hand side of a `const NAME = …`
// declaration in text (the leading `const` optional, both the block and the
// single-const spellings), unquoted when it is a string literal and stripped of
// the `cliverbs.` qualifier when it is an alias into this package. The second
// result is false when no declaration was found at all.
func declaredConstantRHS(text, name string) (string, bool) {
	re := regexp.MustCompile(`(?m)^\s*(?:const\s+)?` + name + `\s*=\s*(.+?)\s*$`)
	m := re.FindStringSubmatch(text)
	if m == nil {
		return "", false
	}
	rhs := strings.TrimSpace(m[1])
	if i := strings.Index(rhs, "//"); i >= 0 {
		rhs = strings.TrimSpace(rhs[:i])
	}
	if strings.HasPrefix(rhs, `"`) {
		if unq, err := strconv.Unquote(rhs); err == nil {
			return unq, true
		}
	}
	return strings.TrimPrefix(rhs, "cliverbs."), true
}

// TestSenseFlagsMatchCmdAgentDispatch is the same drift guard for the FLAGS the
// prompt renders: each leaf constant must equal the dispatch-side constant the
// parser actually reads, so a renamed flag cannot leave the prompt teaching an
// invocation the CLI rejects with "unknown flag".
//
// TWO LEGAL SHAPES, both accepted and both resolved to a value before the
// comparison: a literal (`chatDumpFlag = "--dump-screen"`) and an ALIAS of a
// constant in this package (`rawFlagFile = cliverbs.RawFileWord`, the Rule 13
// shape that keeps one definition of the word). The alias arm is not a
// loophole — the resolved value must still equal the expected flag, so a rename
// on either side fails — and TestRawFileFlagIsAnAliasNotALiteral below pins
// that the shape is actually exercised rather than merely permitted.
func TestSenseFlagsMatchCmdAgentDispatch(t *testing.T) {
	text := readCmdAgentSources(t)
	want := map[string]struct{ constName, value string }{ // expected flag -> dispatch-side owner
		DumpScreen:  {"chatDumpFlag", DumpScreen},
		TargetFlag:  {"uiStateFlagTarget", TargetFlag},
		Compare:     {"uiStateFlagCompare", Compare},
		SessionFlag: {"uiStateFlagSession", SessionFlag},
		// RawFileWord rather than FileFlag: the raw verb's parser matches BARE
		// words, so the dispatch-side constant holds the word and the parser
		// prepends `--` at the boundary. The prompt's rendered flag is DERIVED
		// from the same word (FileFlag = "--" + RawFileWord), so pinning the word
		// here is what makes a rename on either side fail.
		RawFileWord: {"rawFlagFile", RawFileWord},
		// TaskTagsFlag is derived from TaskTagsWord, so the dispatch-side owner
		// holds the WORD (`taskTagsFlagName = cliverbs.TaskTagsWord`) and the
		// renderer holds the dashed flag — the same split, for the same parser
		// shape, as the raw verb's --file above. Pinning the word here is what
		// makes a rename on EITHER side fail: the chat PA-duty paragraph renders
		// `--tags <bug>`, and a drifted word would teach a flag the CLI refuses.
		TaskTagsFlag: {"taskTagsFlagName", TaskTagsWord},
	}
	for flag, c := range want {
		got, ok := declaredConstantRHS(text, c.constName)
		if !ok {
			t.Errorf("cmd/agent declares no %s — the leaf flag constant %q has no dispatch-side owner",
				c.constName, flag)
			continue
		}
		if resolved, isAlias := cliverbsAliasTargets[got]; isAlias {
			got = resolved
		}
		if got != c.value {
			t.Errorf("%s resolves to %q but cliverbs spells %q — the prompt would teach a flag the CLI rejects",
				c.constName, got, c.value)
		}
	}
}

// TestRawFileFlagIsAnAliasNotALiteral pins the SHAPE, not the value: the raw
// verb's `--file` word must be an alias of this package's constant rather than a
// second literal. Without this, "the alias arm is accepted" would be a licence —
// a re-typed `rawFlagFile = "file"` would pass the drift guard above (the values
// agree today) while reintroducing exactly the mirror-literal defect Rules 1b/13
// forbid, and the two would then drift apart silently.
func TestRawFileFlagIsAnAliasNotALiteral(t *testing.T) {
	text := readCmdAgentSources(t)
	re := regexp.MustCompile(`(?m)^\s*(?:const\s+)?rawFlagFile\s*=\s*(.+?)\s*$`)
	m := re.FindStringSubmatch(text)
	if m == nil {
		t.Fatal("cmd/agent declares no rawFlagFile at all")
	}
	rhs := strings.TrimSpace(m[1])
	if strings.HasPrefix(rhs, `"`) {
		t.Fatalf("rawFlagFile is the bare literal %s again — the word must have ONE definition (%s), "+
			"which the prompt and this file both reference", rhs, "cliverbs.RawFileWord")
	}
	if want := "cliverbs.RawFileWord"; rhs != want {
		t.Fatalf("rawFlagFile = %s, want %s — an alias of the one definition", rhs, want)
	}
}

// readCmdAgentSources concatenates the whole cmd/agent package source (not one
// file): the primary SSOT block lives in cli_verbs.go, but a flag is declared
// beside its own handler elsewhere (uiStateFlagTarget in cli_uistate.go,
// rawFlagFile in cli_raw.go), so a single-file read would report a false
// "no dispatch-side owner".
func readCmdAgentSources(t *testing.T) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join("..", "..", "cmd", "agent", "*.go"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("glob cmd/agent/*.go: %v", err)
	}
	var b strings.Builder
	for _, f := range matches {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		b.Write(src)
		b.WriteString("\n")
	}
	return b.String()
}

// TestNoSenseVerbIsEmptyOrBareLiteral guards the two ways a constant package
// rots: an empty value (a prompt line that reads `gopi ` and means nothing)
// and a value that is not the word the CLI types.
func TestNoSenseVerbIsEmptyOrBareLiteral(t *testing.T) {
	flags := map[string]bool{Compare: true, SessionFlag: true, DumpScreen: true, TargetFlag: true, PolicyFlag: true}
	all := []string{UIState, Log, Raw, Stats, Task, Capture, ScreenCapture, Act, Sandbox, Chat, UI, Compare, SessionFlag, DumpScreen, TargetFlag, PolicyFlag}
	for _, v := range all {
		if strings.TrimSpace(v) == "" {
			t.Errorf("a sense verb constant is empty")
		}
		// A FLAG carries a leading dash and is still one lowercase word; a verb
		// is bare. Both must be a single token with no whitespace — that is the
		// property that makes the constant usable in an invocation.
		word := strings.TrimPrefix(v, "--")
		if word != strings.ToLower(word) || strings.ContainsAny(v, " \t") {
			t.Errorf("constant %q is not a lowercase single CLI word — it can never be a dispatch token", v)
		}
		if strings.HasPrefix(v, "--") != flags[v] {
			t.Errorf("constant %q is not spelled as its kind demands (a FLAG leads with --, a verb does not)", v)
		}
	}
	sorted := append([]string{}, all...)
	sort.Strings(sorted)
	for i := 1; i < len(sorted); i++ {
		if sorted[i] == sorted[i-1] {
			t.Errorf("duplicate verb value %q — two constants, one CLI word", sorted[i])
		}
	}
}
