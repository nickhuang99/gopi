package config

// agentguard_bareprompt_test.go — the BARE-PROMPT face of the agent-spawn guard.
//
// THE DEFECT (bug row 01a1ac14, measured 2026-11-05): `gopi <word>` where <word>
// is not a verb and not a near-miss falls through the dispatch chain to the
// bare-prompt path (cmd/agent/main.go's ag.RunPrompt) and starts a REAL paid
// agent run — a session directory, a ledger row marked `[auto] <word>` and a
// clone ground under the SHARED ~/workspace. Nothing refused it: the guard that
// protects `gopi worker` and `gopi task run` had NO call site on this face, so
// an AGENT that ran a bare word grew another agent, with no depth limit.
//
// WHAT THIS FILE PINS — the DECISION, cheaply, so the expensive end-to-end half
// (cmd/agent/bare_prompt_guard_e2e_test.go) only has to prove the wiring:
//
//	① a human terminal is allowed and is not called an override;
//	② an inherited agent signature is REFUSED, and the refusal names the word,
//	   the face, the three correct entry points and the exit code (Rule 15: a
//	   refusal that does not say what to do instead is a dead end);
//	③ the agent's OWN re-exec (AnchorSelf) is allowed — the anchor is what keeps
//	   this guard from refusing the very process that legitimately owns the
//	   signature;
//	④ the two documented escapes still work, and each says it was an override
//	   (never silent);
//	⑤ BarePromptWord — the SCREEN — answers "" for every shape that is not the
//	   bare word (no positionals, an explicit prompt channel), so the guard can
//	   never refuse a run whose text the caller deliberately supplied.

import (
	"strconv"
	"strings"
	"testing"
)

// barePromptSig human is the operator's own terminal: the negative signature
// wins before any other fact is consulted.
func barePromptHumanSig() AgentSpawnSignature {
	return AgentSpawnSignature{HumanTerminal: true}
}

// barePromptAgentSig is the recursing shape: an agent session above us whose
// anchor carries ANOTHER pid (an inherited marker).
func barePromptAgentSig() AgentSpawnSignature {
	return AgentSpawnSignature{
		AgentSession:    true,
		HermesSession:   true,
		AnchorInherited: true,
	}
}

// TestGuardBarePromptRefusesInheritedAgentSession is ②: the refusal exists, is
// named, and points at the three doors an agent may actually use.
func TestGuardBarePromptRefusesInheritedAgentSession(t *testing.T) {
	const word = "submit-x"
	d := GuardBarePrompt(word, barePromptAgentSig())
	if d.Allowed {
		t.Fatalf("a bare word (%q) from inside an agent session must be refused — allowing it starts a real paid run:\n%+v", word, d)
	}
	for _, want := range []string{
		word,                        // the word itself
		BarePromptFace,              // the surface by name
		AutoTaskRowPrefix + word,    // the ledger row it would have written
		BarePromptTaskSubmitAddress, // the queue door
		BarePromptChatAddress,       // the conversation door
		BarePromptVerbHelpAddress,   // the usage door
		AgentSpawnGuardExitCodeLabel + strconv.Itoa(AgentGuardRefusalExitCode),
		HumanTerminalEnv,          // the operator's escape, named
		AgentSpawnGuardOffEnv,     // the wholesale switch, named
		"inside an agent session", // the signature, rendered
	} {
		if !strings.Contains(d.Reason, want) {
			t.Errorf("the bare-prompt refusal does not carry %q — a refusal that hides a fact is the silent failure Rule 15 bans:\n%s", want, d.Reason)
		}
	}
	if d.Override {
		t.Errorf("a REFUSED decision must not report Override=true:\n%+v", d)
	}
	// The three addresses must be runnable gopi command lines, not fragments.
	for _, addr := range []string{BarePromptTaskSubmitAddress, BarePromptChatAddress, BarePromptVerbHelpAddress} {
		if !strings.HasPrefix(addr, "gopi ") {
			t.Errorf("entry-point address %q is not a gopi command line", addr)
		}
	}
}

// TestGuardBarePromptAllowsTheHumanTerminal is ①: the documented usage
// `gopi "<prompt>"` from a person's shell is untouched — and NOT reported as an
// override, because an override notice on every interactive run is noise.
func TestGuardBarePromptAllowsTheHumanTerminal(t *testing.T) {
	d := GuardBarePrompt("summarize this file", barePromptHumanSig())
	if !d.Allowed {
		t.Fatalf("a person's terminal must keep the bare-prompt face:\n%+v", d)
	}
	if d.Override {
		t.Errorf("a human terminal is not an override — reporting one would print a warning on every interactive run:\n%+v", d)
	}
}

// TestGuardBarePromptAllowsTheAgentsOwnReexec is ③: the anchor's self case. This
// is the property that makes the guard safe to put on a face the agent itself
// legitimately runs (its own session re-exec exports its own pid).
func TestGuardBarePromptAllowsTheAgentsOwnReexec(t *testing.T) {
	d := GuardBarePrompt("own prompt", AgentSpawnSignature{AgentSession: true, AnchorSelf: true})
	if !d.Allowed {
		t.Fatalf("a process that IS the agent session (AnchorSelf) must not be refused — refusing it would break the agent's own run:\n%+v", d)
	}
}

// TestGuardBarePromptHonoursTheDocumentedEscapes is ④: both knobs still work,
// and each is reported as an override so nothing is silent.
func TestGuardBarePromptHonoursTheDocumentedEscapes(t *testing.T) {
	for _, knob := range []string{AgentSpawnGuardOverrideEnv, AgentSpawnGuardOffEnv} {
		t.Run(knob, func(t *testing.T) {
			t.Setenv(knob, "1") // Rule 17: scoped to this test, restored with absence
			d := GuardBarePrompt("submit-x", barePromptAgentSig())
			if !d.Allowed {
				t.Fatalf("%s=1 must clear the bare-prompt refusal:\n%+v", knob, d)
			}
			if !d.Override {
				t.Errorf("%s=1 allowed the run but did not report Override — an escape must never be silent:\n%+v", knob, d)
			}
		})
	}
}

// TestBarePromptWordScreensOnlyTheUnresolvedWord is ⑤: the screen answers "" for
// every shape whose text the caller supplied deliberately, so the guard cannot
// fire on a prompt that was asked for.
func TestBarePromptWordScreensOnlyTheUnresolvedWord(t *testing.T) {
	cases := []struct {
		name string
		cfg  *Config
		want string
		why  string
	}{
		{
			name: "nil config",
			cfg:  nil,
			want: "",
			why:  "a failed resolution is reported elsewhere; the guard has nothing to rule on",
		},
		{
			name: "no positionals",
			cfg:  &Config{},
			want: "",
			why:  "a stdin-only or --prompt-only run has no unresolved word",
		},
		{
			name: "explicit --prompt",
			cfg:  &Config{Prompt: "submit-x", promptFrom: promptSourceFlag, Positionals: []string{"data.txt"}},
			want: "",
			why:  "with an explicit prompt the positionals are input FILES, not verbs",
		},
		{
			name: "settings prompt",
			cfg:  &Config{Prompt: "submit-x", promptFrom: promptSourceSettings, Positionals: []string{"data.txt"}},
			want: "",
			why:  "the profile's prompt key is an explicit channel too",
		},
		{
			name: "bare word",
			cfg:  &Config{Positionals: []string{"submit-x"}},
			want: "submit-x",
			why:  "this is the defect's exact shape — it must be screened",
		},
		{
			name: "ambient GOPI_PROMPT does not disarm the screen",
			cfg:  &Config{Prompt: "1", promptFrom: promptSourceEnv, Positionals: []string{"submit-x"}},
			want: "submit-x",
			why:  "an inherited GOPI_PROMPT says a prompt applied to SOME invocation (task 01a0de86), never to this one",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := BarePromptWord(tc.cfg); got != tc.want {
				t.Fatalf("BarePromptWord = %q, want %q — %s", got, tc.want, tc.why)
			}
		})
	}
}
