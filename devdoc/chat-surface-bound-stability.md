<!-- doc-class: historical -->
# The self-driven bound must not move under the model (measurement, 2026-09-28)

**Class:** measurement / bug fix. **Task:** `01a0e44a46d27f38b672c3deecf86351`.
**File:** `internal/agent/chat_surface.go` (`chatSurfaceType`).

## The claim that was wrong

The code said, in its own comment:

> The bound is set at the FIRST type and is stable for the session, so "how many
> self-driven turns are left" cannot move under the model.

It tested "first" as **the seam being idle**:

```go
if a.chatSurface.pending == "" {
    a.chatSurface.bound = chatSurfaceLoopBoundArgs(args)
}
```

The seam is idle at **every turn boundary** — the REPL consumes the pending line
before the next model request — so that predicate is true on *every* `type`, not
only the first. The sentence above was a description of intent, not of behaviour.

## What that let happen (measured, not reasoned)

`chatSurfaceLoopBoundArgs` accepts a **lower** `max_turns` (clamped into
`[chatSurfaceRestartFloor, chatSurfaceLoopBound]`), so a model that asked
`max_turns: 2` on its second turn of a period **cut its own period's bound from 8
to 2**. Nothing failed, nothing was said: the leash simply got shorter, and how
short was the model's choice.

Red run on the real surface (pre-fix binary, a mock model that asks
`max_turns: 2` from its second turn on, one operator line, no other input):

```
bound_in_force=8   turns_used=1/8
bound_in_force=2   turns_used=2/2      <- the period's bound is now 2
refused: this period's self-driven loop is at its bound (2 of 2 turns used)
console: self-driven turn 1/8, then self-driven turn 2/2
```

That it is a *leash* and not an *escape* is worth stating exactly: the clamp is
downward-only, so the hard ceiling never rose. The defect is category (b) of the
runaway-loop invariants read the other way round — **the model may not set the
bound**, in either direction, and a bound the operator cannot predict is not the
bound the operator authorized.

## The fix

```go
bound := a.chatSurfaceLoopBound()
sessionBoundSet := a.chatSurface.bound != 0   // set at the session's FIRST type
if !sessionBoundSet {
    a.chatSurface.bound = chatSurfaceLoopBoundArgs(args)
    bound = a.chatSurface.bound
}
```

The test for "first" is now **the bound being unset**, which is what the comment
always meant and what the sibling `action=key` has always done
(`a.chatSurface.keys == 0`). Later `type` calls ignore `max_turns` — and **say
so** (Rule 15: a silent no-op is a failure the caller cannot see), on both the
acceptance and the refusal string:

```
action=type typed=true bound_in_force=8 turns_used=3/8 pending=true (max_turns=2 was
IGNORED: this session's bound is 8, set at its first type and stable after that — only
a NEW session moves it)
```

Green run, same mock, same operator line, post-fix binary:

```
turns_used 1/8 … 8/8, every one bound_in_force=8; 7 x "max_turns=2 was IGNORED";
then "refused: … at its bound (8 of 8 turns used)"
console: self-driven turn 1/8 … 8/8, "this is the last self-driven turn of this period (bound 8)"
```

## The lesson worth keeping

**A predicate that happens to be true at the moment you first observe it is not
the same as a predicate that is true only once.** `pending == ""` was a *seam
state*, available and convenient, and it was reused as a *session-progress
state* — a category error that compiles, reads correctly, and is invisible until
a caller passes a different argument on its second call. The durable test for
"already initialised" is the initialised VALUE (`bound != 0`), never a proxy that
resets between calls. Corollary for review: when a field is set "once per
session/period", the guard must name that period's own marker.

## Reproduce

```bash
# unit (red pre-fix, green post-fix)
go test ./internal/agent/ -run TestChatSurfaceRenewal_ModelCannotMoveThePeriodBoundDown -count=1
# real surface, both directions: realrun2/ (post-fix green) and realrun3/ (pre-fix red)
python3 realrun2/drive_chat2.py realrun2/gopi . "$(cat realrun2/port2)" raw2.bin console2.log
```

Evidence under `/home/user/.gopi/sessions/01a0e44a-46d2-7f38-b672-c3deecf86351/`
(`realrun2/`, `realrun3/`) — outside git by the run's briefing.
