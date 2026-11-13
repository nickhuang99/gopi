package agent

// chat_surface_key_renewal_test.go — the KEY ceiling's renewal contract, driven
// through the REAL surfaces (the tool dispatcher and the carrier seam), not
// through the counter.
//
// WHY THIS FILE EXISTS. `action=key` and `action=type` are two halves of ONE
// self-driving seam, and the operator's ruling on their bound is one ruling
// (task 01a0e44a): a period is the stretch between two lines a HUMAN wrote, and
// a line the operator submits opens a fresh budget — the model can never open
// one itself. The turn half of that contract has been tested since it landed.
// The KEY half was not, and the two halves had drifted: the turn counter was
// reset by ChatSurfaceUserTurn while the injected-key counter only ever grew, so
// a session whose operator kept supervising still lost its whole interaction
// capability at the ceiling, permanently, in the middle of a long autonomous
// verification. A contract that holds for one half of a seam and not the other
// is a contract that disagrees with itself.
//
// WHAT IS PINNED, one claim per case, each with the failure mode it prevents:
//
//  1. THE OPERATOR'S LINE OPENS A FRESH KEY PERIOD. After the ceiling is spent,
//     the very next injection after a boundary is accepted and reports the NEW
//     period's count. Without it, one spent period ends the capability for the
//     session (the defect this file was written for).
//  2. THE MODEL CANNOT OPEN ONE. Every action the wire advertises, called
//     repeatedly past the ceiling, leaves the counter exactly where the ceiling
//     put it, and the ONLY method that can move it is never reached from a tool
//     action. THIS IS THE INVARIANT, and the RED PROOF is recorded in
//     devdoc/chat-surface-key-period.md: with a self-renewal planted in the key
//     path the case fails on its first loop.
//  3. A RENEWAL RE-ESTABLISHES THE DECLARED CEILING — never a period's tighter
//     value as the new normal, and never a larger one.
//  4. A `stop` IS NOT LIFTED BY AN OPERATOR LINE either: renewal re-opens a
//     BUDGET, it does not restart a loop the operator deliberately ended.
//  5. A DEFERRED BOUNDARY RENEWS NOTHING: when a self-driven line is still in
//     hand the operator's line is not the boundary, so neither counter moves.
//  6. A SESSION THAT NEVER INJECTED A KEY IS NOT "RENEWED": a boundary is
//     reported as a renewal only when something was actually reset, because a
//     console that announces a budget that does not exist is a console nobody
//     can trust.

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// keyStubCeiling is the period ceiling these cases establish through the tool's
// own `max_keys` operand, so the number the tool clamps and the number the
// counter is charged against are the same one.
const keyStubCeiling = 4

// keyStubSurface is the smallest surface that can carry a key counter honestly:
// it charges on acceptance exactly as internal/lineedit.Injector does (the
// counter the REAL path is charged against, in the shape the tool reads back),
// and it records every ResetKeys call so "no tool action can reach the reset" is
// observable rather than asserted by inspection.
type keyStubSurface struct {
	mu     sync.Mutex
	used   int
	bound  int
	resets []int
}

func (s *keyStubSurface) ScreenGrid() ChatScreenObservation { return ChatScreenObservation{} }

func (s *keyStubSurface) Transcript(string) (string, []string, error) {
	return "", nil, errStubNoTranscript
}

func (s *keyStubSurface) Enqueue(string, int) (bool, int, string) { return true, keyStubCeiling, "" }

func (s *keyStubSurface) InjectKey(key string, bound int) ChatKeyResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.used >= bound {
		return ChatKeyResult{Label: key, Bound: bound, Used: s.used,
			Why: fmt.Sprintf("this period's key ceiling is reached (%d of %d keys injected)", s.used, bound)}
	}
	s.used++
	return ChatKeyResult{Label: key, Accepted: true, ReadByEditor: true,
		Used: s.used, Bound: bound, Last: s.used >= bound}
}

func (s *keyStubSurface) ResetKeys(bound int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resets = append(s.resets, bound)
	s.used = 0
}

func (s *keyStubSurface) CancelLoop() bool { return false }

func (s *keyStubSurface) usedKeys() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.used
}

func (s *keyStubSurface) resetCalls() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.resets...)
}

// spendKeyPeriod drives ONE FULL key period through the real dispatcher, and
// fails if the period did not reach its ceiling. The FIRST call carries
// `max_keys`, which is how the session's period ceiling is established — the
// same operand the model uses.
func spendKeyPeriod(t *testing.T, a *Agent, s *keyStubSurface) {
	t.Helper()
	for i := 1; i <= keyStubCeiling; i++ {
		args := map[string]interface{}{"action": ChatSurfaceActionKey, "text": "down"}
		if i == 1 {
			args["max_keys"] = float64(keyStubCeiling)
		}
		got := a.runChatSurfaceTool(args)
		if !strings.Contains(got, "injected=") {
			t.Fatalf("key #%d of %d returned %q, want an acceptance", i, keyStubCeiling, got)
		}
		if !strings.Contains(got, fmt.Sprintf("keys_used=%d/%d", i, keyStubCeiling)) {
			t.Fatalf("key #%d reports %q, want keys_used=%d/%d", i, got, i, keyStubCeiling)
		}
	}
	got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionKey, "text": "down"})
	if !strings.Contains(got, "refused") {
		t.Fatalf("the key past the ceiling returned %q, want a refusal", got)
	}
	if used := s.usedKeys(); used != keyStubCeiling {
		t.Fatalf("the surface reports %d keys spent after a full period, want %d", used, keyStubCeiling)
	}
}

// TestChatSurfaceKeyRenewal_OperatorLineOpensAFreshKeyPeriod is claim 1: the
// operator's own line re-opens the KEY budget exactly as it re-opens the turn
// budget, and the next injection is accepted again.
func TestChatSurfaceKeyRenewal_OperatorLineOpensAFreshKeyPeriod(t *testing.T) {
	a := &Agent{}
	s := &keyStubSurface{}
	a.SetChatSurface(s)
	spendKeyPeriod(t, a, s)

	renew := a.ChatSurfaceUserTurn()
	if !renew.Renewed {
		t.Fatalf("a user line at a clean boundary reported %+v, want Renewed", renew)
	}
	if got := s.resetCalls(); len(got) != 1 || got[0] != keyStubCeiling {
		t.Fatalf("the boundary reset the key budget with %v, want exactly one call carrying this session's declared ceiling %d",
			got, keyStubCeiling)
	}
	// THE POINT OF THE WHOLE CHANGE: the interaction capability survives a long
	// run, because the operator's own supervision is what opens the periods.
	got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionKey, "text": "down"})
	if !strings.Contains(got, "injected=") {
		t.Fatalf("the first key after the renewal returned %q, want an acceptance — this is the long-run capability", got)
	}
	if !strings.Contains(got, fmt.Sprintf("keys_used=1/%d", keyStubCeiling)) {
		t.Fatalf("the fresh period's first key reports %q, want keys_used=1/%d (a period count, not a session total)", got, keyStubCeiling)
	}
}

// TestChatSurfaceKeyRenewal_TheModelCannotRenewTheKeyBudget is claim 2 — THE
// INVARIANT. No action the wire advertises moves the key counter, so a spent
// period stays spent however many times the model calls the tool.
//
// RED PROOF (recorded in devdoc/chat-surface-key-period.md, reproduced by
// planting a self-renewal in the key path — see the `RED PROOF` note there):
// with `a.chatSurface.surface.ResetKeys(0)` inserted at the top of
// chatSurfaceKey, this case fails on its first loop iteration with "action=key
// moved the key counter" — the self-renewal is caught, not reasoned about.
func TestChatSurfaceKeyRenewal_TheModelCannotRenewTheKeyBudget(t *testing.T) {
	a := &Agent{}
	s := &keyStubSurface{}
	a.SetChatSurface(s)
	spendKeyPeriod(t, a, s)
	resetsBefore := len(s.resetCalls())

	// EVERY action the wire advertises. A renewal reachable from any one of them
	// would be a bound the bounded party controls.
	for _, action := range ChatSurfaceActions() {
		for i := 0; i < keyStubCeiling+2; i++ {
			a.runChatSurfaceTool(map[string]interface{}{
				"action": action, "text": "down", "max_keys": float64(keyStubCeiling),
			})
			if used := s.usedKeys(); used != keyStubCeiling {
				t.Fatalf("action=%s moved the key counter to %d (from %d): the model can renew its own key budget",
					action, used, keyStubCeiling)
			}
		}
	}
	if got := s.resetCalls(); len(got) != resetsBefore {
		t.Fatalf("a tool action reached the key reset: %d call(s) %v after %d before — the reset must be unreachable from the tool",
			len(got)-resetsBefore, got[resetsBefore:], resetsBefore)
	}

	// `stop` also must not hand the model a new key budget: it ends the loop, it
	// does not open a period.
	a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionStop})
	if used := s.usedKeys(); used != keyStubCeiling {
		t.Fatalf("stop moved the key counter to %d, want %d", used, keyStubCeiling)
	}

	// The narrowly-scoped claim, stated as the tool's contract: the refusal names
	// the OPERATOR as the one who opens a fresh period, so a model reading it
	// knows the one action that opens a period is not its own.
	got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionKey, "text": "down"})
	if !strings.Contains(got, "refused") {
		t.Fatalf("the spent period accepted a key: %q", got)
	}
	if !strings.Contains(got, fmt.Sprintf("keys used %d/%d", keyStubCeiling, keyStubCeiling)) {
		t.Fatalf("the key refusal does not quote the enforced counters: %q", got)
	}
}

// TestChatSurfaceKeyRenewal_RenewalRestoresTheDeclaredCeiling is claim 3: the
// ceiling a renewed period runs against is the SESSION's own declaration — the
// value its first key established — never a larger number and never a period's
// private experiment. (`max_keys` is raised to the declared FLOOR before it is
// ever charged, so "tighter than the session" is a property of the injector's own
// per-call bound, tested next to it; what is pinned here is that the renewal
// carries the session's number and cannot exceed the tool's declared ceiling.)
func TestChatSurfaceKeyRenewal_RenewalRestoresTheDeclaredCeiling(t *testing.T) {
	a := &Agent{}
	s := &keyStubSurface{}
	a.SetChatSurface(s)

	// The session's ceiling is established by its FIRST key, from the operand the
	// model supplied (already clamped by the tool).
	session := keyStubCeiling + 2
	if session > ChatSurfaceKeyCeiling() {
		t.Fatalf("this case's ceiling %d exceeds the tool's declared %d", session, ChatSurfaceKeyCeiling())
	}
	if got := a.runChatSurfaceTool(map[string]interface{}{
		"action": ChatSurfaceActionKey, "text": "down", "max_keys": float64(session),
	}); !strings.Contains(got, fmt.Sprintf("keys_used=1/%d", session)) {
		t.Fatalf("the first key of the session reports %q, want keys_used=1/%d", got, session)
	}
	if renew := a.ChatSurfaceUserTurn(); !renew.Renewed {
		t.Fatalf("the boundary did not renew: %+v", renew)
	}
	if got := s.resetCalls(); len(got) != 1 || got[0] != session {
		t.Fatalf("the renewal passed %v, want the session's own ceiling %d (a period must not become the new normal)", got, session)
	}
	// After the renewal the period runs against the SAME declared number, and a
	// later key cannot raise it.
	if got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionKey, "text": "down"}); !strings.Contains(got, fmt.Sprintf("keys_used=1/%d", session)) {
		t.Fatalf("after the renewal the key reports %q, want the session's %d", got, session)
	}
	if ceil := a.chatSurfaceKeyCeiling(); ceil != session {
		t.Fatalf("the session's key ceiling is %d, want %d", ceil, session)
	}
	// A key asking for MORE than the session's ceiling cannot move it: the
	// session's value was fixed by its first key (one reader, see
	// chatSurfaceKeyCeilingArgs).
	if got := a.runChatSurfaceTool(map[string]interface{}{
		"action": ChatSurfaceActionKey, "text": "down", "max_keys": float64(ChatSurfaceKeyCeiling()),
	}); !strings.Contains(got, fmt.Sprintf("keys_used=2/%d", session)) {
		t.Fatalf("a key asking for a larger max_keys moved the ceiling: %q", got)
	}
}

// TestChatSurfaceKeyRenewal_StopIsNotLiftedByAUserLine is claim 4: a `stop`
// latches, and no operator line lifts it — for the key budget either.
func TestChatSurfaceKeyRenewal_StopIsNotLiftedByAUserLine(t *testing.T) {
	a := &Agent{}
	s := &keyStubSurface{}
	a.SetChatSurface(s)

	if got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionType, "text": "step"}); !strings.Contains(got, "typed=true") {
		t.Fatalf("type returned %q", got)
	}
	if got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionKey, "text": "down"}); !strings.Contains(got, "injected=") {
		t.Fatalf("key returned %q", got)
	}
	if got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionStop}); !strings.Contains(got, "pending_dropped=true") {
		t.Fatalf("stop returned %q, want the drop named", got)
	}
	before := len(s.resetCalls())
	ren := a.ChatSurfaceUserTurn()
	if ren.Renewed || ren.Deferred {
		t.Fatalf("a user line lifted a latched stop: %+v", ren)
	}
	if got := s.resetCalls(); len(got) != before {
		t.Fatalf("a latched stop still reset the key budget: %v", got[before:])
	}
	if used := s.usedKeys(); used != 1 {
		t.Fatalf("used=%d after a latched stop, want the 1 key the period had spent", used)
	}
}

// TestChatSurfaceKeyRenewal_DeferredBoundaryResetsNothing is claim 5: when a
// self-driven line is still in hand the operator's line is NOT the boundary, so
// neither counter moves (an inferred reset is the self-renewal the seam exists
// to prevent).
func TestChatSurfaceKeyRenewal_DeferredBoundaryResetsNothing(t *testing.T) {
	a := &Agent{}
	s := &keyStubSurface{}
	a.SetChatSurface(s)

	if got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionType, "text": "model's line"}); !strings.Contains(got, "typed=true") {
		t.Fatalf("type returned %q", got)
	}
	if got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionKey, "text": "down"}); !strings.Contains(got, "injected=") {
		t.Fatalf("key returned %q", got)
	}
	ren := a.ChatSurfaceUserTurn()
	if !ren.Deferred || ren.Renewed {
		t.Fatalf("a user line with a self-driven line in hand reported %+v, want Deferred", ren)
	}
	if got := s.resetCalls(); len(got) != 0 {
		t.Fatalf("a deferred boundary reset the key budget: %v", got)
	}
	if used := s.usedKeys(); used != 1 {
		t.Fatalf("used=%d after a deferral, want the 1 key still spent", used)
	}
	// The turn counter likewise: the model's line is still in hand, so the period
	// still counts it.
	turn := a.ChatSurfaceTakePending()
	if turn.Kind != ChatSurfaceSeamTyped || turn.Line != "model's line" {
		t.Fatalf("the deferred renewal changed whose line runs: %+v", turn)
	}
	if used := a.ChatSurfaceLoopUsed(); used != 1 {
		t.Fatalf("the turn counter moved on a deferral: %d", used)
	}
}

// TestChatSurfaceKeyRenewal_NoKeysNoRenewal is claim 6, the honest half: a
// boundary in a session that never injected a key (and never typed a turn) is
// NOT reported as a renewal. The console announcing "budget renewed" for a
// budget that does not exist would be exactly the kind of unfalsifiable notice
// this seam's comments promise not to print.
func TestChatSurfaceKeyRenewal_NoKeysNoRenewal(t *testing.T) {
	a := &Agent{}
	s := &keyStubSurface{}
	a.SetChatSurface(s)

	ren := a.ChatSurfaceUserTurn()
	if ren.Renewed || ren.Deferred {
		t.Fatalf("a boundary before anything was spent reported %+v, want neither renewed nor deferred", ren)
	}
	if got := s.resetCalls(); len(got) != 0 {
		t.Fatalf("a boundary with nothing to renew still reset: %v", got)
	}
}
