package testutil

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/boolvocab"
	"github.com/nickhuang99/gopi/internal/config"
)

// PinBareTerminalProcess pins the environment signals this process reads to
// decide WHO a row's writer is, so a fixture asserting a display contract
// measures the RENDERER and never the shell that launched `go test`.
//
// WHY IT IS NEEDED — and why ONE pin, not one per direction. internal/tasksource
// derives a row's provenance from the process's own spawn signature (be17a9bd:
// "only a person may claim human — derive provenance instead of defaulting it"),
// and config.AgentSpawnSignatureNow reads that signature from the ENVIRONMENT.
// The same fixture therefore renders differently in a developer's plain shell and
// in this suite's own runner. Measured 2026-09-25 on master 351736f9: the
// human-provenance fixture went red because a suite is often run from inside a
// gopi session whose shell exports GOPI_EXECUTOR_SESSION_ID — Derive then returns
// `agent` where the fixture asked for `human` — while the SESSION-briefing fixture
// asserts the derivation's NON-human branch and goes red under `env -i`, the other
// direction, where the same row renders `human`. Both reds are the same defect
// seen from opposite ends: an assertion anchored in ambient state. A pin that
// named only the human escape would MOVE the sensitivity instead of removing it,
// so this pin names every input the derivation consults.
//
// WHAT "BARE TERMINAL" MEANS HERE, exactly: the derivation sees the evidence a
// person's own terminal provides and none of the evidence an agent session
// provides. That is AgentRun() == false and HumanEvidence() == true — the only
// two questions internal/tasksource asks.
//
// THE THREE PINS, each with its reason:
//
//   - config.HumanTerminalEnv — the one signal that lets a process claim a PERSON
//     filed a row (tasksource.HumanEvidence). SET, so a `human` claim is
//     supported: the same escape the agent-spawn guard honours, so this pin makes
//     the fixture's process a plain operator terminal, which is what the fixture
//     is describing.
//   - config.AgentSessionEnv and config.HermesSessionIDEnv — the agent-session
//     evidence (AgentSpawnSignature.AgentSession / HermesSession). CLEARED so the
//     ambient agent chain cannot downgrade the claim above.
//   - the spelling is read from config's own constants (Rules 13/14), never typed.
//
// THE TWO SIGNALS DELIBERATELY NOT PINNED, and why leaving them alone is correct
// rather than an oversight:
//
//   - config.AgentSessionAncestorEnv (the pid-carrying anchor). It is read by PID
//     (AnchorSelf vs AnchorInherited), so it is structural: an INHERITED value
//     cannot be cleared by t.Setenv, because the suite's own parent chain
//     re-supplies it. It is nonetheless inert for everything this pin exists for —
//     AgentRun() returns false at its HumanTerminal branch, before the anchor is
//     consulted — and its real consumers live outside the row-provenance path
//     (internal/agent's ground-branch pick). Pinning a live pid to influence a
//     reader that no fixture here calls would add state without removing a
//     sensitivity; the honest move is to leave it and say so.
//   - AgentSpawnSignature.ParentIsWorker, read from procfs, never the environment.
//     The worker chain is the operator's own sanctioned path, so a test binary
//     launched by a worker reports ParentIsWorker and derives `worker` — which no
//     environment pin can change, by design. A fixture that must not depend on it
//     asserts the DERIVED value (taskWriteProvenance in internal/tools) instead of
//     a fixed spelling, which is the shape that cannot drift; this pin does not
//     pretend to cover it.
//
// SCOPE. t.Setenv (Rule 17) — the pin lasts the test and the testing package
// restores it, absence included, so a sibling test's derivation sees the ambient
// signature again. This is a PER-TEST pin, never a suite-wide one: a suite that
// pinned it globally would make EVERY fixture's human claim self-fulfilling and
// retire the coverage internal/tasksource/derive_test.go gives the synthesized
// signatures.
func PinBareTerminalProcess(t *testing.T) {
	t.Helper()
	t.Setenv(config.HumanTerminalEnv, envTruthyValue)
	t.Setenv(config.AgentSessionEnv, "")
	t.Setenv(config.HermesSessionIDEnv, "")
}

// envTruthyValue is the FIRST enabling spelling in the shared boolean
// vocabulary (internal/boolvocab, Rule 14) — config's own truthiness reader is
// that vocabulary, so this pin cannot drift from what agentguard honours. The
// pin writes a spelling the reader is guaranteed to accept instead of a copy of
// "1" that a future vocabulary change would silently invalidate.
var envTruthyValue = strings.SplitN(boolvocab.TrueValues, ",", 2)[0]
