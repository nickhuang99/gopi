// chat_budget_line.go renders the SELF-DRIVE BUDGET into the chat context block,
// BEFORE it is spent — the flaw this closes is that the remaining budget was
// visible only in the tool result AFTER a successful action=type, so a model
// planning a long task could not see the ceiling until it hit it (task
// 01a1e101, defect ③).
//
// Two properties are deliberate and both are load-bearing:
//
//   - It is rendered at ASSEMBLY time from the SAME state the arming call reads
//     (chatSurfaceLoopBound + the loop's own `typed` counter), because a number
//     produced anywhere else would be a second source of truth for a bound whose
//     whole point is that it cannot be raised from this surface (Rule 14).
//   - It states the UNIT out loud — per PERIOD, a period being the stretch
//     between two lines a HUMAN wrote. A budget that names its number but not
//     its unit is what made a long task ("one operator line, then silence")
//     look like a 20-step task, which it is not.

package agent

import (
	"strconv"
	"strings"
)

const (
	// chatSurfaceBudgetMarker is the block's first token. It is exported as a
	// prefix-free literal check in the tests rather than a bare string there,
	// so a rename cannot silently stop the tests from finding the block.
	chatSurfaceBudgetMarker = "[chat budget]"

	// chatSurfaceBudgetUnit is the unit sentence, kept out of the format string
	// because it is the half a reader needs and it must never be compressed
	// away by an edit that only touches the numbers.
	chatSurfaceBudgetUnit = "the count is per PERIOD — a period is the stretch between two lines a HUMAN wrote, so it resets when the operator submits a line and NEVER when you do"
)

// chatSurfaceBudgetLine renders "turns_used=k/N" plus the remaining turns and
// the unit. A nil Agent renders nothing: the block is a property of a chat run,
// and a task run has no such budget to state.
//
// A CHAT TURN THE OPERATOR WROTE HAS NO SELF-DRIVE BUDGET TO STATE, so it
// renders nothing there. The ceiling bounds the turns a self-driven loop may
// ADD; a turn a human wrote is not one of them, and RenewChatSurfacePeriod
// resets the counter on exactly that event. So "nothing typed, nothing pending"
// IS "no loop in flight" — and rendering "0/20, 20 left" into every ordinary
// chat request tells the model it is on a turn budget it must conserve, which
// is the opposite of what this console is: the chat surface is long-horizon and
// has no turn budget until a loop the model itself started is running
// (operator's ruling, 2026-11-21). The block still appears the moment a loop is
// in flight — the case task 01a1e101 exists for — because then the number is
// spendable and worth planning against.
func chatSurfaceBudgetLine(a *Agent) string {
	if a == nil {
		return ""
	}
	if a.chatSurface.typed == 0 && strings.TrimSpace(a.chatSurface.pending) == "" {
		return ""
	}
	bound := a.chatSurfaceLoopBound()
	used := a.chatSurface.typed
	return chatSurfaceBudgetMarker + " self-driven turns_used=" + strconv.Itoa(used) + "/" + strconv.Itoa(bound) +
		" (" + strconv.Itoa(chatSurfaceTurnsLeft(used, bound)) + " left in this period); " +
		chatSurfaceBudgetUnit + ". Spend them on steps the task already fixes; when they run out, say which step is next instead of asking the operator to type it."
}

// chatSurfaceTurnsLeft is kept separate so the arithmetic has one home; a
// negative remainder is clamped to 0 because an exhausted period is exhausted,
// not "over-drawn".
func chatSurfaceTurnsLeft(used, bound int) int {
	left := bound - used
	if left < 0 {
		return 0
	}
	return left
}
