package config

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// agentguard_test.go — the decision table of the agent-spawn guard (operator
// ruling 2026-09-12: hermes/PA never runs `gopi worker` / `gopi task run`).
//
// The predicate is a PURE function of AgentSpawnSignature, so the whole table
// is exercised without a live environment; the end-to-end proof through the
// real binary lives in cmd/agent/agent_spawn_guard_test.go.

// clearGuardEnv removes every guard-relevant variable from this process so a
// case starts from a known state (the test binary may itself have been started
// from an agent session — this very repo's agents do exactly that).
func clearGuardEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		AgentSessionEnv, HermesSessionIDEnv, AgentSessionAncestorEnv,
		HumanTerminalEnv, AgentSpawnGuardOverrideEnv, AgentSpawnGuardOffEnv,
	} {
		t.Setenv(name, "")
	}
}

func TestAgentSpawnGuardDecisionTable(t *testing.T) {
	clearGuardEnv(t)
	cases := []struct {
		name string
		sig  AgentSpawnSignature
		want bool // refused?
	}{
		{
			name: "operator terminal: no signature at all",
			sig:  AgentSpawnSignature{},
			want: false,
		},
		{
			name: "agent bash: gopi session inherited",
			sig:  AgentSpawnSignature{AgentSession: true},
			want: true,
		},
		{
			name: "agent bash: hermes session inherited",
			sig:  AgentSpawnSignature{HermesSession: true},
			want: true,
		},
		{
			name: "agent bash: anchor inherited (another pid)",
			sig:  AgentSpawnSignature{AnchorInherited: true},
			want: true,
		},
		{
			name: "agent bash: anchor inherited + both ids",
			sig:  AgentSpawnSignature{AnchorInherited: true, AgentSession: true, HermesSession: true},
			want: true,
		},
		{
			name: "gopi agent session itself (anchor is ours)",
			sig:  AgentSpawnSignature{AnchorSelf: true, AgentSession: true, HermesSession: true},
			want: false,
		},
		{
			name: "worker's own task-run child (parent is the worker)",
			sig:  AgentSpawnSignature{ParentIsWorker: true, HermesSession: true},
			want: false,
		},
		{
			name: "operator escape claimed in his shell",
			sig:  AgentSpawnSignature{HumanTerminal: true, AnchorInherited: true, HermesSession: true},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := GuardAgentSpawn("worker", tc.sig)
			if refused := !d.Allowed; refused != tc.want {
				t.Fatalf("refused=%v want %v (reason=%q)", refused, tc.want, d.Reason)
			}
			if tc.want {
				// A refusal must SAY WHICH signal fired and name the remedy —
				// a bare "refused" in a log is the Rule 15 silent-failure trap.
				for _, want := range []string{
					"inside an agent session",
					"gopi-worker-up.sh",
					"gopi task submit",
					HumanTerminalEnv,
					AgentSpawnGuardOverrideEnv,
				} {
					if !strings.Contains(d.Reason, want) {
						t.Errorf("refusal message missing %q:\n%s", want, d.Reason)
					}
				}
			}
		})
	}
}

// TestAgentSpawnGuardSignatureNowReadsTheEnvironment pins the env→signature
// capture, including the anchor's self-vs-inherited discriminator (the move
// that lets the operator run a worker from a terminal opened inside hermes).
func TestAgentSpawnGuardSignatureNowReadsTheEnvironment(t *testing.T) {
	clearGuardEnv(t)

	if sig := AgentSpawnSignatureNow(); sig.AgentSession || sig.HermesSession || sig.AnchorInherited || sig.AnchorSelf {
		t.Fatalf("clean env must carry no signature, got %+v", sig)
	}

	t.Setenv(AgentSessionEnv, "6a31f480-a2e9")
	t.Setenv(HermesSessionIDEnv, "20260912_040123_ec45a1")
	sig := AgentSpawnSignatureNow()
	if !sig.AgentSession || !sig.HermesSession {
		t.Fatalf("session env not captured: %+v", sig)
	}
	if !sig.AgentRun() {
		t.Fatal("an inherited signature must count as an agent run")
	}

	// The anchor with OUR pid is a self-claim (this gopi IS the agent session).
	t.Setenv(AgentSessionAncestorEnv, strconv.Itoa(os.Getpid()))
	if sig := AgentSpawnSignatureNow(); !sig.AnchorSelf || sig.AnchorInherited {
		t.Fatalf("own pid must read as AnchorSelf, got %+v", sig)
	} else if sig.AgentRun() {
		t.Fatal("a self-anchored agent session must not be refused")
	}

	// Any OTHER pid is an ancestor claim.
	t.Setenv(AgentSessionAncestorEnv, strconv.Itoa(os.Getpid()+1))
	if sig := AgentSpawnSignatureNow(); !sig.AnchorInherited || sig.AnchorSelf {
		t.Fatalf("another pid must read as AnchorInherited, got %+v", sig)
	}

	// A malformed value still claims a chain — read conservatively.
	t.Setenv(AgentSessionAncestorEnv, "not-a-pid")
	if sig := AgentSpawnSignatureNow(); !sig.AnchorInherited {
		t.Fatalf("malformed anchor must read as inherited, got %+v", sig)
	}
}

// TestAgentSpawnGuardOverrideAndOffAreExplicit: both escapes let the spawn
// through, and BOTH report Override so the caller can never do it silently.
func TestAgentSpawnGuardOverrideAndOffAreExplicit(t *testing.T) {
	clearGuardEnv(t)
	agentSig := AgentSpawnSignature{AnchorInherited: true, AgentSession: true}

	t.Setenv(AgentSpawnGuardOverrideEnv, "1")
	if d := GuardAgentSpawn("worker", agentSig); !d.Allowed || !d.Override {
		t.Fatalf("override must allow with Override=true, got %+v", d)
	}

	clearGuardEnv(t)
	t.Setenv(AgentSpawnGuardOffEnv, "1")
	if d := GuardAgentSpawn("worker", agentSig); !d.Allowed || !d.Override {
		t.Fatalf("guard-off must allow with Override=true, got %+v", d)
	}

	// An empty/whitespace value is ABSENT, never a claim.
	clearGuardEnv(t)
	t.Setenv(AgentSpawnGuardOverrideEnv, "  ")
	if d := GuardAgentSpawn("worker", agentSig); d.Allowed {
		t.Fatal("a whitespace override must not count as a claim")
	}
}

// TestAgentSpawnGuardDoesNotFireOnTheOperatorTerminal is the concrete negative
// test the ruling demands: the guard must not touch a plain shell. The check is
// on the live signature of THIS process — started without any agent signature
// (see clearGuardEnv) it must decide "allowed", and the only thing that can
// make it refuse is a signature that is genuinely present.
func TestAgentSpawnGuardDoesNotFireOnTheOperatorTerminal(t *testing.T) {
	clearGuardEnv(t)
	sig := AgentSpawnSignatureNow()
	if sig.ParentIsWorker {
		t.Skip("parent is a gopi worker — the operator-terminal case needs a plain shell")
	}
	if d := GuardAgentSpawn("worker", sig); !d.Allowed {
		t.Fatalf("operator-style launch refused (reason=%s)", d.Reason)
	}
}
