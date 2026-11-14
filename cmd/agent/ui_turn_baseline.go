package main

// ui_turn_baseline.go — THE STATUS BAR'S TURN COUNTER STARTS WHERE THE LEDGER IS.
//
// THE DEFECT (measured twice on 2026-10-27, in the operator's own words): the bar
// under `gopi ui` counts the turns THIS SURFACE ran, starting at 1. That is the
// right count for a fresh chat and the WRONG number for a session that was
// resumed, or that `gopi notify` deliveries opened turns on — the operator
// reported it as "重启之后这个数字又归零了" while `gopi session log`, `gopi
// session show` and `chats.turn_count` all said 16, and the resume notice one
// screen earlier said "the next turn is turn 17".
//
// WHY THE COUNT IS SUPPLIED FROM HERE. `chats.turn_count` is a column in the chat
// index (internal/tools). internal/uiclient knows nothing about the ledger and
// must not learn: it renders a screen, and the ONE producer of this number is the
// ledger (Rule 14). So the caller — this layer, which already imports both —
// reads it and hands it over (uiclient.Config.TurnBaseline), exactly as it hands
// over the geometry, the palette and the interrupt hook.
//
// AN UNMEASURED COUNT IS ANNOUNCED, NOT SUBSTITUTED (Rule 15). If the index has
// no row for the session yet (the run has started no turn), or the read failed,
// the bar starts at 0 and this function says so on stderr — printed BEFORE the
// display takes the terminal, which is the only moment anything can. It never
// invents a number and never leaves the reader to discover the mismatch by
// comparing two screens.

import (
	"fmt"
	"os"
	"strings"

	"github.com/nickhuang99/gopi/internal/gopilog"
	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// uiSessionIDFor resolves the id of the session this surface is attached to: the
// in-process engine's own, or — for `--server` — the one the remote engine
// reports over its gated identity route (the same route the identity block
// prints, so there is ONE answer to "which session is this?").
//
// An unreachable peer is reported by the caller's own identity step; here it is
// simply no id, and the baseline says so once.
func uiSessionIDFor(engine *uiEngine, base, token string) string {
	if engine != nil {
		return engine.srv.identityPayload().SessionID
	}
	p, err := webFetchIdentity(base, token)
	if err != nil {
		return ""
	}
	return p.SessionID
}

// uiTurnBaselineFor resolves the turn ORDINAL anchor the session already holds,
// from the ONE producer that numbers the ordinals themselves, and reports an
// unmeasured anchor on stderr.
//
// IT READ `chats.turn_count` AND THAT WAS ONE PRODUCER BEHIND (2026-11-04). The
// ledger counts FINISHED turns; the ordinal a reader hands to `replay --turn N`
// is a POSITION in the log, so anchoring the bar on the ledger made `gopi ui`'s
// bar and the session's own row numbers differ by whatever had not finished —
// and on session e1e22e9f… the gap was a constant 1 across 16 of 16 readable
// turns. chatTurnOrdinalBaseline states the full measurement and why the older
// ledger ruling is superseded.
//
// A session id that cannot be resolved at all is not an error here: the surface
// still works and its counter still counts, so the run continues with 0 and one
// line saying why. Nothing downstream depends on the anchor for correctness —
// only for the number matching the other surfaces.
func uiTurnBaselineFor(sessionID string) int {
	if strings.TrimSpace(sessionID) == "" {
		fmt.Fprintf(os.Stderr, "%s: the engine reported no session id, so the %s counter starts at 0 — it will not match %s for this session\n",
			webCarrierUI, sessionevent.CounterUnitTurn, gopilog.TurnCountHomeSurface)
		return 0
	}
	n, ok, err := chatTurnOrdinalBaseline(sessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: cannot anchor the %s counter to this session's log — the bar starts at 0: %v\n",
			webCarrierUI, sessionevent.CounterUnitTurn, err)
		return 0
	}
	if !ok {
		fmt.Fprintf(os.Stderr, "%s: no record for this session yet, so the %s counter starts at 0\n",
			webCarrierUI, sessionevent.CounterUnitTurn)
		return 0
	}
	return n
}
