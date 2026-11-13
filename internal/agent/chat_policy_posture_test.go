package agent

// chat_policy_posture_test.go — the STARTUP POSTURE's four rungs, plus the two
// properties that make it safe: a yolo run allows EVERYTHING (evidence
// included), and the resolved value cannot be revised afterwards.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/chatpolicy"
)

// envOf builds a getenv for a fixed map — the ladder is testable without
// touching this process's environment (Rule 32).
func envOf(pairs map[string]string) func(string) string {
	return func(k string) string { return pairs[k] }
}

// writeSettings writes a settings.json into a scratch dir and returns its path.
func writeSettings(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	return path
}

// RUNG 1 — the command line outranks BOTH other channels.
func TestResolvePostureCLIWinsOverEnvAndSettings(t *testing.T) {
	settings := writeSettings(t, `{"chat_policy":"forbidden"}`)
	got := ResolveChatPolicyPosture("yolo",
		envOf(map[string]string{ChatPolicyEnv: "on-request"}), settings)
	if d := got.Posture.Decision(); d != chatpolicy.DecisionAllow {
		t.Errorf("--policy yolo with env=on-request and settings=forbidden resolved %v, want allow (the FLAG decides)", d)
	}
	if got.Posture.Source() != ChatPolicyFlag {
		t.Errorf("Source = %q, want %q — the startup line must name the channel", got.Posture.Source(), ChatPolicyFlag)
	}
}

// RUNG 2 — the environment outranks the settings file.
func TestResolvePostureEnvWinsOverSettings(t *testing.T) {
	settings := writeSettings(t, `{"chat_policy":"forbidden"}`)
	got := ResolveChatPolicyPosture("",
		envOf(map[string]string{ChatPolicyEnv: "allow"}), settings)
	if d := got.Posture.Decision(); d != chatpolicy.DecisionAllow {
		t.Errorf("GOPI_CHAT_POLICY=allow with settings=forbidden resolved %v, want allow (the ENV decides)", d)
	}
	if got.Posture.Source() != ChatPolicyEnv {
		t.Errorf("Source = %q, want %q", got.Posture.Source(), ChatPolicyEnv)
	}
}

// RUNG 3 — the settings file decides when the two higher channels are silent.
func TestResolvePostureSettingsDecidesWhenSilent(t *testing.T) {
	settings := writeSettings(t, `{"chat_policy":"deny"}`)
	got := ResolveChatPolicyPosture("", envOf(nil), settings)
	if d := got.Posture.Decision(); d != chatpolicy.DecisionForbidden {
		t.Errorf("settings chat_policy=deny resolved %v, want forbidden", d)
	}
	if !strings.Contains(got.Posture.Source(), ChatPolicySettingsKey) {
		t.Errorf("Source = %q, want it to name the settings key", got.Posture.Source())
	}
}

// RUNG 4 — nothing declared: the DEFAULT, which is ON-REQUEST, and the source
// says "default" (a different sentence than a file that said on-request).
func TestResolvePostureDefaultIsOnRequest(t *testing.T) {
	// No settings file at all.
	got := ResolveChatPolicyPosture("", envOf(nil), filepath.Join(t.TempDir(), "absent.json"))
	if d := got.Posture.Decision(); d != chatpolicy.DecisionPrompt {
		t.Errorf("with no channel set the posture = %v, want prompt (on-request)", d)
	}
	if got.Posture.AllowsEverything() {
		t.Error("the default posture AllowsEverything — the default must never be yolo")
	}
	if got.Posture.Source() != chatPolicyDefaultSource {
		t.Errorf("Source = %q, want %q so a run that declared nothing reads differently from one that said on-request",
			got.Posture.Source(), chatPolicyDefaultSource)
	}
	if len(got.Notes) != 0 {
		t.Errorf("Notes = %v, want none for a silent, readable settings path", got.Notes)
	}
}

// An unusable value is REPORTED and the next channel decides — never silently
// read as the default (Rule 15).
func TestResolvePostureRefusesUnknownWordsAndFallsThrough(t *testing.T) {
	settings := writeSettings(t, `{"chat_policy":"forbidden"}`)
	got := ResolveChatPolicyPosture("ture", envOf(nil), settings)
	if d := got.Posture.Decision(); d != chatpolicy.DecisionForbidden {
		t.Errorf("an unknown --policy word resolved %v, want the SETTINGS answer (the next channel decides)", d)
	}
	if len(got.Notes) == 0 || !strings.Contains(got.Notes[0], "ture") {
		t.Errorf("Notes = %v, want the refused word named", got.Notes)
	}
	// An unknown ENV value behaves the same way.
	got = ResolveChatPolicyPosture("", envOf(map[string]string{ChatPolicyEnv: "sure"}), settings)
	if d := got.Posture.Decision(); d != chatpolicy.DecisionForbidden {
		t.Errorf("an unknown env word resolved %v, want the settings answer", d)
	}
	if len(got.Notes) == 0 || !strings.Contains(got.Notes[0], "sure") {
		t.Errorf("Notes = %v, want the refused env value named", got.Notes)
	}
	// An unknown SETTINGS value falls to the default, announced.
	bad := writeSettings(t, `{"chat_policy":"maybe"}`)
	got = ResolveChatPolicyPosture("", envOf(nil), bad)
	if d := got.Posture.Decision(); d != chatpolicy.DecisionPrompt {
		t.Errorf("an unknown settings value resolved %v, want the default", d)
	}
	if len(got.Notes) == 0 || !strings.Contains(got.Notes[0], "maybe") {
		t.Errorf("Notes = %v, want the refused settings value named", got.Notes)
	}
}

// THE COMPATIBILITY MAPPING: chat_op_auto=true maps to allow and SAYS SO; a
// false value needs no announcement (it already means on-request), and a
// declared chat_policy key silences the mapping entirely.
func TestResolvePostureMapsLegacyChatOpAuto(t *testing.T) {
	legacy := writeSettings(t, `{"chat_op_auto":true}`)
	got := ResolveChatPolicyPosture("", envOf(nil), legacy)
	if d := got.Posture.Decision(); d != chatpolicy.DecisionAllow {
		t.Errorf("chat_op_auto=true resolved %v, want allow", d)
	}
	if !strings.Contains(got.Posture.Source(), chatOpAutoKey) {
		t.Errorf("Source = %q, want it to name the legacy key as MAPPED", got.Posture.Source())
	}
	joined := strings.Join(got.Notes, "\n")
	if !strings.Contains(joined, chatOpAutoKey) || !strings.Contains(joined, ChatPolicySettingsKey) {
		t.Errorf("Notes = %v, want the mapping explained (the old key AND its replacement)", got.Notes)
	}

	// false = the same answer as the default: nothing to say.
	off := writeSettings(t, `{"chat_op_auto":false}`)
	if got := ResolveChatPolicyPosture("", envOf(nil), off); got.Posture.Decision() != chatpolicy.DecisionPrompt || len(got.Notes) != 0 {
		t.Errorf("chat_op_auto=false resolved %v with notes %v, want prompt and NO note", got.Posture.Decision(), got.Notes)
	}

	// The NEW key wins when both are present (one knob, one precedence).
	both := writeSettings(t, `{"chat_op_auto":true,"chat_policy":"forbidden"}`)
	got = ResolveChatPolicyPosture("", envOf(nil), both)
	if d := got.Posture.Decision(); d != chatpolicy.DecisionForbidden {
		t.Errorf("with both keys set the posture = %v, want forbidden (the new key decides)", d)
	}
	if strings.Contains(got.Posture.Source(), chatOpAutoKey) {
		t.Errorf("Source = %q names the legacy key although chat_policy was declared", got.Posture.Source())
	}
}

// YOLO EVIDENCE, end to end through the layer the CLI resolves: a yolo posture
// answers allow to a destructive command even against the most restrictive rule
// table — the posture has no exception branch to reach.
func TestYoloPostureAllowsForcePushThroughTheResolver(t *testing.T) {
	settings := writeSettings(t, `{"chat_policy":"forbidden"}`)
	// The rule table is irrelevant under yolo; build one that forbids everything.
	pol, perr := chatpolicy.Parse("test", "```chat-policy\nforbidden *\n```\n")
	if perr != nil {
		t.Fatalf("Parse: %v", perr)
	}
	yolo := ResolveChatPolicyPosture("yolo", envOf(nil), settings)
	for _, words := range [][]string{
		{"git", "push", "--force"},
		{"git", "reset", "--hard"},
		{"rm", "-rf", "/"},
	} {
		m := yolo.Posture.Resolve(pol, words)
		if !m.Matched || m.Decision != chatpolicy.DecisionAllow {
			t.Errorf("%v under --policy yolo = %+v, want a Matched allow", words, m)
		}
	}
	// The SAME table under the default posture still refuses: the evidence above
	// is the posture's, not the table's.
	ask := ResolveChatPolicyPosture("", envOf(nil), filepath.Join(t.TempDir(), "absent.json"))
	if m := ask.Posture.Resolve(pol, []string{"git", "push", "--force"}); m.Decision != chatpolicy.DecisionForbidden {
		t.Errorf("the same table under the default posture = %v, want forbidden", m.Decision)
	}
}

// FROZENNESS through the resolved value: two resolutions do not share state and
// a note cannot revise an answer.
func TestResolvedPostureIsFrozen(t *testing.T) {
	settings := writeSettings(t, `{"chat_policy":"forbidden"}`)
	first := ResolveChatPolicyPosture("", envOf(nil), settings)
	second := ResolveChatPolicyPosture("yolo", envOf(nil), settings)
	if first.Posture.Decision() != chatpolicy.DecisionForbidden {
		t.Errorf("the first resolution = %v, want forbidden (a later resolution must not revise it)", first.Posture.Decision())
	}
	if second.Posture.Decision() != chatpolicy.DecisionAllow {
		t.Errorf("the second resolution = %v, want allow", second.Posture.Decision())
	}
	_ = first.Posture.WithNote("changed")
	if first.Posture.Decision() != chatpolicy.DecisionForbidden || first.Posture.Note() != "" {
		t.Error("WithNote mutated a resolved posture")
	}
}

// The rule table loads from the project context file the run already sends, and
// a fence-less file is a VALID EMPTY table rather than an error.
func TestLoadChatPolicyTableReadsTheProjectFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "GOPI.md")
	body := "# project\n\n```chat-policy\nallow git status\nprompt *\n```\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write GOPI.md: %v", err)
	}
	pol, from, err := LoadChatPolicyTable(dir)
	if err != nil {
		t.Fatalf("LoadChatPolicyTable: %v", err)
	}
	if from != path {
		t.Errorf("loaded from %q, want %q", from, path)
	}
	if pol == nil || pol.Len() != 2 {
		t.Fatalf("rules = %v, want 2", pol)
	}

	// A fence-less document: 0 rules, no error, and Fenced=false is reported.
	bare := t.TempDir()
	if err := os.WriteFile(filepath.Join(bare, "GOPI.md"), []byte("# nothing here\n"), 0o644); err != nil {
		t.Fatalf("write GOPI.md: %v", err)
	}
	pol, _, err = LoadChatPolicyTable(bare)
	if err != nil {
		t.Fatalf("a fence-less GOPI.md was an error: %v", err)
	}
	if pol == nil || pol.Len() != 0 || pol.Fenced {
		t.Errorf("fence-less table = %+v, want 0 rules and Fenced=false", pol)
	}

	// A MALFORMED fence is reported with its line, never silently half-read.
	broken := t.TempDir()
	if err := os.WriteFile(filepath.Join(broken, "GOPI.md"),
		[]byte("# p\n\n```chat-policy\nnope git push\n```\n"), 0o644); err != nil {
		t.Fatalf("write GOPI.md: %v", err)
	}
	if _, _, err := LoadChatPolicyTable(broken); err == nil {
		t.Error("a malformed rule was not reported")
	}
}
