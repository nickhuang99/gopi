package main

// chat_replay_test.go — the `--replay` vocabulary, its refusals, and the ONE
// environment contract the verb depends on.

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
)

// TestChatParseReplayArgReadsBothSpellings pins the parser shape the argv walk
// relies on: a bare switch consumes the NEXT word, the joined form consumes its
// own, and a switch with nothing after it is a refusal (never a silent skip that
// would leave the run behaving as an ordinary chat).
func TestChatParseReplayArgReadsBothSpellings(t *testing.T) {
	cases := []struct {
		name     string
		arg      string
		next     string
		hasNext  bool
		want     string
		consumed bool
		wantErr  bool
	}{
		{name: "bare with value", arg: chatFlagReplay, next: "abc-123", hasNext: true, want: "abc-123", consumed: true},
		{name: "joined form", arg: chatFlagReplay + "=" + "abc-123", hasNext: false, want: "abc-123", consumed: true},
		{name: "bare with no value", arg: chatFlagReplay, hasNext: false, consumed: true, wantErr: true},
		{name: "bare before another flag", arg: chatFlagReplay, next: chatFlagTools, hasNext: true, consumed: true, wantErr: true},
		{name: "joined with empty value", arg: chatFlagReplay + "=", hasNext: false, consumed: true, wantErr: true},
		{name: "not the flag", arg: chatFlagResume, next: "abc-123", hasNext: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, consumed, err := chatParseReplayArg(tc.arg, tc.next, tc.hasNext)
			if consumed != tc.consumed {
				t.Fatalf("consumed = %v, want %v", consumed, tc.consumed)
			}
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				if !strings.Contains(err.Error(), chatFlagReplay) {
					t.Fatalf("the refusal does not name the flag: %v", err)
				}
				return
			}
			if got != tc.want {
				t.Fatalf("target = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestChatReplayBaseURLEnvMatchesTheResolver is the ONE contract that makes the
// stub reachable: the names this verb writes must be the names the client
// resolves, in the same order. A rename on either side would leave the replay
// talking to the operator's REAL provider — the exact failure this verb exists to
// prevent — and it would do so silently, because a real provider answers.
func TestChatReplayBaseURLEnvMatchesTheResolver(t *testing.T) {
	const stub = "http://127.0.0.1:65535"
	restore, err := chatApplyReplayEnv(stub)
	if err != nil {
		t.Fatalf("bind the stub: %v", err)
	}
	defer restore()

	if got := deepseek.ResolveClaudeBaseURL(nil); got != stub {
		t.Fatalf("the client resolved base URL %q, want the stub %q — chatReplayBaseURLEnv is not the name the resolver reads", got, stub)
	}
	// The FALLBACK spelling is what the replay must SUPPRESS: with it left in
	// force it would win over nothing and the stub would be unreachable on a
	// machine that has it set.
	restore()
	if got := deepseek.ResolveClaudeBaseURL(nil); got == stub {
		t.Fatalf("the override survived its restore: %q", got)
	}
}

// TestChatReplayEnvironmentIsRestoredOnEveryExitPath pins the restore, including
// the case the fallback name exists in the environment: an override that leaked
// would leave the operator's next chat pointed at a closed loopback port.
func TestChatReplayEnvironmentIsRestoredOnEveryExitPath(t *testing.T) {
	// The environment this test runs in may carry EITHER spelling (a gopi chat
	// session publishes them to its children), so both are pinned: an EMPTY
	// value reads as absent to the resolver, and t.Setenv restores whatever was
	// there — which is what makes the assertions below about THIS test's own
	// writes rather than about the operator's environment.
	t.Setenv(chatReplayBaseURLEnv, "")
	t.Setenv(chatReplayBaseURLFallbackEnv, "http://fallback.example")
	before := deepseek.ResolveClaudeBaseURL(nil)
	if before != "http://fallback.example" {
		t.Fatalf("precondition: the resolver read %q, want the fallback spelling", before)
	}
	restore, err := chatApplyReplayEnv("http://127.0.0.1:12345")
	if err != nil {
		t.Fatalf("bind the stub: %v", err)
	}
	restore()
	if got := deepseek.ResolveClaudeBaseURL(nil); got != before {
		t.Fatalf("after restore the resolver reads %q, want the original %q", got, before)
	}
}

// TestChatReplayChunksAreRuneSafe is the bounded-read arm: a chunk that cut a
// multi-byte rune would hand the editor's decoder half a character, which is a
// byte sequence no keyboard can produce.
func TestChatReplayChunksAreRuneSafe(t *testing.T) {
	text := strings.Repeat("中", chatReplayCharsPerKey+3) + "abc"
	chunks := chatReplayChunks(text)
	if len(chunks) < 2 {
		t.Fatalf("chunks = %d, want the text split across reads", len(chunks))
	}
	if got := strings.Join(chunks, ""); got != text {
		t.Fatalf("chunks do not reassemble the text: %q", got)
	}
	for i, c := range chunks {
		if !strings.ContainsRune(text, []rune(c)[0]) {
			t.Fatalf("chunk %d does not start on a rune boundary: %q", i, c)
		}
	}
}
