// sessionid_refusal.go — the OPERAND PROVENANCE clause of the id→path refusal.
//
// internal/sessionid answers the SHAPE question ("is this string a task-id
// spelling this tree uses") and holds the ONE definition of the refusal
// sentence. It is a LEAF (stdlib + internal/fullid) on purpose, so both sides
// of the agent/tools boundary can import it; it deliberately does NOT read the
// environment.
//
// What the shape gate cannot see is the question a diagnosis actually has to
// answer when it fires: WHICH CHANNEL handed the process this string. That
// question is answerable only where the caller's own environment is visible,
// and internal/tools is the package that owns both the seam
// (taskSessionDir, taskFindSessionJSONLByTask) and the wire contract the
// launcher exports into a child (internal/signalrec's SessionDirEnv /
// TaskIDEnv, set at spawn in cmd/agent/cli_tasks.go). So the clause lives
// here, beside the callers, and the seam wraps it — internal/sessionid keeps
// its one definition and gains no env reader.
//
// WHY THIS EXISTS (attribution of the extra-digit spelling, bug row
// 01a1472c-2919-7ac0-b9f8-edb08c65a88d, 2026-08-… measured 2026-10-17). A run
// whose container was keyed by a uuid with ONE EXTRA HEX CHARACTER (37 chars,
// 33 under the dashes) had no way of telling an operator whether the string
// came from the environment the launcher exported or from the process that
// built it. The measurement that decided that run's attribution was a
// hand-written comparison of the env's printed literal against the operand;
// that comparison is what this clause automates, so the next occurrence
// carries its own answer.
package tools

import (
	"fmt"
	"os"

	"github.com/nick/gopi/internal/sessionid"
	"github.com/nick/gopi/internal/signalrec"
)

// sessionidOperandSourceClause is the ONE provenance clause an id→path refusal
// carries. It reports the channel the CALLER DECLARED (the ledger id the
// launcher puts in the child's environment, signalrec.TaskIDEnv) whenever the
// process has it, and it states the two facts a diagnosis needs to split the
// possibilities:
//
//   - the operand EQUALS the declared channel value → the bad spelling arrived
//     through the environment, so the producer is upstream of this process;
//   - the operand DIFFERS from it (or the process declares nothing) → the
//     operand did not come from that channel, so this process's own argument
//     or command text is where to look;
//   - the channel is UNSET (a bare chat, a test, `task show` run by hand) →
//     the clause says the channel was unset rather than inventing a source
//     (Rule 15: a fabricated attribution is worse than none).
//
// The clause never claims more than it measured: it names the declared value
// and whether it matches, and leaves "who typed it" to the reader who has the
// process's argv in hand.
func sessionidOperandSourceClause(id string) string {
	declared := os.Getenv(signalrec.TaskIDEnv)
	if declared == "" {
		return fmt.Sprintf("operand source: the %s channel is unset in this process, so the operand did not arrive through it — the caller's own argument/command text is where this spelling came from (the env-published spelling would have been the ledger id)", signalrec.TaskIDEnv)
	}
	if declared == id {
		return fmt.Sprintf("operand source: the operand EQUALS the ledger id this process was handed (%s=%s) — the spelling arrived through the environment, so the producer is upstream of this process", signalrec.TaskIDEnv, declared)
	}
	return fmt.Sprintf("operand source: this process was handed ledger id %q via %s, and the operand %q DIFFERS from it — the spelling did not come from that channel; it came from this process's own argument/command text", declared, signalrec.TaskIDEnv, id)
}

// sessionidRefusal is the ONE call-site wrapper around internal/sessionid's
// refusal: the same sentence (one definition, in the leaf) plus the provenance
// clause above. Both seams that turn a task id into a path element use it, so
// the two cannot disagree about what an operator is told (Rules 13/14). The
// clause carries its own prefix, so the wrapper adds none.
func sessionidRefusal(id string) error {
	return fmt.Errorf("%w (%s)", sessionid.Refuse(id), sessionidOperandSourceClause(id))
}
