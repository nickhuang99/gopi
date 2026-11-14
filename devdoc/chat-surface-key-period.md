<!-- doc-class: normative -->
# The chat_surface key ceiling: a PERIOD, renewed only by the operator's own line

Task: `01a0e80e-9e98-78c8-a4c6-77cee5c9de55`. Sibling work: `01a0e438` (the key
injection entry point, already on master) and the turn-side bound it was supposed
to match (the `RENEWAL` seam in `internal/agent/chat_surface.go`).

## What was wrong

`chat_surface action=key` and `action=type` are two halves of ONE self-driving
seam, and the operator's ruling on their bound is one ruling: **a period is the
stretch between two lines a HUMAN wrote, and a line the operator submits opens a
fresh budget — the model can never open one itself.** The turn half implemented
that (`ChatSurfaceUserTurn` zeroes `typed`). The key half did not: the injector's
`used` counter only ever grew, and `Agent.chatSurface.keys` was a session value
that no code path ever reopened.

The consequences were not cosmetic. A long autonomous verification spends keys —
that is what the action is FOR — and at the ceiling the capability died *for the
session*, however much the operator supervised. The two halves of one contract
disagreed, and the disagreement was invisible: the refusal sentence said "this
session's ceiling", which read like a deliberate design rather than the missing
half of a renewal.

A second, smaller defect lived in the same seam: `max_keys` was clamped by the
tool into `[4, 64]`, but the injector could be told a *larger* bound per call and
would use it. The tool's clamp was therefore the only thing standing between the
model and a raised ceiling — one line of defence where the design calls for two.

## The shape of the fix

**A period, decided by construction, never by inspecting a value.**

- `lineedit.Injector` now holds two numbers: `session` (the ceiling the
  constructor declared — immutable) and `bound` (the ceiling in force for the
  current period). `ResetKeys(periodBound)` zeroes `used` and re-establishes
  `bound` from `session`, optionally tightened; it can never raise it.
- `InjectKeyBounded`/`InjectBytesBounded` take the caller's period bound, and ONE
  resolver (`periodBound`) decides the number charged: `min(caller, injector)`.
  So a caller cannot raise the ceiling even if the tool's clamp were removed.
- `Agent.chatSurfaceKeyCeiling()` records the session's own value the first time a
  key establishes one (`sessionKeys`), so a renewal restores what the SESSION
  declared — not a period's tighter experiment, and never a larger number.
- `ChatSurfaceUserTurn` renews BOTH counters on the same boundary — and only when
  each actually has something to renew. A fresh line in a session that never typed
  a turn and never injected a key opens nothing, and reports so; a console that
  announces a budget that does not exist is a notice nobody can trust.

**Why the counter lives in the injector and not in the agent.** `used` is moved by
acceptance and read by refusal, and both of those are the injector's. Two owners
of one number is how a report and a refusal drift apart — the tool prints
`keys_used=N/BOUND` from the injector's own answer, and the injector is the object
that refuses on it.

**Why the reset is not a tool action.** The bound exists because a self-driving
seam without a ceiling is a loop nobody can stop (`devdoc/runaway-loop-20260923.md`).
A bound the bounded party can lift is not a bound, so there is deliberately **no
wire spelling** of `ResetKeys`: the only caller is the carrier, at the boundary
where it still knows WHICH arm produced the line. That is checked, not asserted —
`TestChatSurfaceKeyRenewal_TheModelCannotRenewTheKeyBudget` walks every action the
wire advertises and fails if the counter moves or if `ResetKeys` is ever reached.

## Files

| file | change |
|---|---|
| `internal/lineedit/inject.go` | `session` vs `bound`; `InjectKeyBounded`/`InjectBytesBounded`; `ResetKeys`; one `periodBound` resolver |
| `internal/agent/chat_surface.go` | `sessionKeys`; `keys` documented as the PERIOD's ceiling; `ResetKeys` on the `ChatSurface` contract; `ChatSurfaceUserTurn` renews both halves and reports `Renewed` only when something moved; `chatSurfaceKeyCeiling()`; `ChatSurfaceKeyUsed()`; tool text/refusal/notice say "this period" and name the operator as the one who opens one |
| `cmd/agent/chat_surface_surface.go` | the real surface forwards `ResetKeys` to its injector and reports `KeyUsed()`; renewal notice mentions the key budget |
| `internal/agent/chat_surface_key_renewal_test.go` | NEW — six acceptance cases through the real dispatcher and the carrier seam |
| `internal/lineedit/inject_reset_test.go` | NEW — four cases at the injector's own boundary |

## Verification

See `tasks/01a0e80e-report.md` for the full Rule 5 report (build, tests, the
verbatim live invocations and what they printed).

### The live loop, end to end

A real `gopi chat --tools --dump-screen` on a real pty (via `script`), in a
sandbox profile, with a scripted SSE stub as its model endpoint. The model's own
`chat_surface action=key` calls injected `/` and `down`; the model read its own
painted cells with `action=see`, and the same screen was read independently from
outside the process with `gopi ui_state --target chat`.

The model's OWN `see` result, after the injected `/` and before any `down`:

```
015|⚙ call chat_surface {"action":"key","text":"/"}
017|you> /
019|> /new (alias of /clear)
020|  /task (Usage: /task <subcommand> [args])
021|  /quit (alias of /exit)
022|  /exit (alias of /quit)
```

and after the injected `down` — the selection marker moved off `/new`:

```
017|you> /
019|  /new (alias of /clear)
020|  /task (Usage: /task <subcommand> [args])
021|  /quit (alias of /exit)
022|  /exit (alias of /quit)
```

The tool's own answers in the same session:

```
chat_surface action=key injected="/" read_by_editor=true pending_bytes=0 keys_used=1/64 (this period) — …
chat_surface action=key injected="down" read_by_editor=true pending_bytes=0 keys_used=2/64 (this period) — …
```

Nothing was submitted: the editing row still holds `/` when the turn ends, and no
`/new`, `/task`, `/quit` or `/exit` ever ran.

### Two things the live probe measured that the unit cases could not

1. **`ui_state --target chat` refuses to guess.** With two live chat surfaces it
   printed both pids and **no grid** — the probe read nothing until the stale
   chat was killed. A probe that ignored this would have "read" an empty screen
   and reported it as evidence.
2. **A line written in one `write(2)` is a PASTE, and its Enter is pasted
   content.** The first live attempt typed `hello\r` at once and the model was
   never asked anything: `you> hello` sat on the editing row. Separating the text
   and its CR by more than `PasteEnterSuppressWindow` (120ms) submits normally.
   This is worth knowing for every future probe of this surface.

## Not done (honest)

- **The RED PROOF lives in the session directory, not in the branch.** It is
  re-runnable —
  `~/.gopi/sessions/01a0e80e-9e98-78c8-a4c6-77cee5c9de55/evidence/red_proof.sh`
  backs the file up, plants the self-renewal, restores it, and verifies the
  restore with `cmp` — but it is a probe, not a committed gate, so a reviewer must
  run it from that path. The transcript is beside it in `red_proof.out`.
- **`--dump-screen` geometry is the pty's, not the operator's.** The probe runs
  at 80x24, so the menu's rows are the ones that fit there.
- **A live run reaches the ceiling only at or near `max_keys`'s floor.** The
  declared ceiling is 64 keys; 64 scripted model rounds at real network pacing
  outlast one session's budget, so the live ceiling is exercised at `max_keys: 4`
  and the unit cases cover both ends.
- The 2026-09-28 handoff that scoped this work claimed "no key-injection entry
  point exists in the code". That was stale: `internal/lineedit/inject.go`,
  `action=key` and `cmd/agent/chat_surface_key_e2e_test.go` are on master and
  green. This change is the period/renewal contract, not the entry point.

## The defect the live run found (and the unit cases could not)

`chatSurface.InjectKey` called `inj.InjectKey(key)` — the unbounded spelling — so
the bound the agent had computed never reached the injector. A live run asked for
`max_keys: 4` and was charged against 64.

This is worth recording because of *why* the tests missed it: every agent-level
case installs a **stub** surface, so they prove the dispatcher passes a bound and
a renewal on, and prove nothing about what the real surface does with either. The
regression case is therefore written against the real surface (`&chatSurface{input:
…}` over a real pty), not the agent, in `cmd/agent/chat_inject_pty_test.go`.

A second defect fell out of that case, and it is the more interesting one: with a
plain per-call `min` of the two bounds, a SPENT 4-key period answered a later call
carrying `max_keys: 64` by raising itself back to 64 and accepting the key — a
self-renewal the model could reach one operand at a time, in the exact system that
exists to prevent self-renewal. `latchPeriodBound` closes it: a period's ceiling
is decided at its FIRST key, and every later key in that period inherits it.

