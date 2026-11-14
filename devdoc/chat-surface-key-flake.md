<!-- doc-class: normative -->
# The intermittent `TestChatSurfaceKey_` failure — a proxy wait, and a reading range that included a stopwatch

Task `01a0e5f0`. Two `cmd/agent` e2e cases failed intermittently **only when selected
together** by one `-run` regex. The cause was not shared fixture state (no shared
state exists — each case has its own `t.TempDir()` `GOPI_HOME`, its own pty pair and
its own `httptest` server) and not the product. It was **two independent defects in
the cases' own assertions**, plus a third found in the sibling while fixing them.

## Defect 1 — a wait on a PROXY instead of on the claim's subject

`chat_surface_key_e2e_test.go`'s LEG 2, pre-fix:

```go
menuBefore := h.rows()                                    // baseline
h.waitFor(t, "the injected Down to move the selection",
          func(string) bool { return h.rows() != menuBefore })
```

`h.rows() != menuBefore` reads as "the selection moved". It is not: it is "the
painted grid differs from a snapshot". **The answer band streams into that same
grid during the turn**, so the predicate is satisfied by the answer's own text,
with no relation to the Down. And the baseline is captured as soon as
`h.menuRows() > 0` — the instant the menu's published *height* becomes nonzero,
while the editor paints the menu over several writes — so the *remainder of the
same draw* satisfies it too.

**The measurement that settles it.** Delete the key and see whether the case
notices:

```go
func keyE2EInjectedKeys() []string { return []string{keyE2EMenuRune} }  // Down DELETED
```

On the pre-fix case this **PASSES** (`ok … 3.002s`). A case that passes with the key
it claims to be testing deleted is not testing the key. This is the general trap: a
predicate of the form *"something about the screen changed"* is only as strong as
the control that proves it fails when the act does not happen — run that control.

The sibling act case (`chat_surface_help_menu_e2e_test.go`) had already learned this
and recorded it: *"nothing changed is not evidence the key arrived"*. The lesson did
not transfer to the case that needed it.

## Defect 2 — both keys in ONE model round

The mock injected `[/, down]` in a single round. **The menu is published by the
completer's worker goroutine**, so whether the menu appears before or after the Down
is a scheduling question, not a property of the editor. When the Down lands first,
the test baselines a frame that already has the Down applied — and then *no* later
repaint can differ from it, so the wait burns its entire `keyE2EWait` (8s) budget.
That is the observed form of the intermittent: **an 8s timeout, not a wrong value.**

The fix is the pacing the sibling already uses: **one key per model round, with the
test holding the gate.** The SSE body of round `n` blocks on `<-gates[n-2]` until the
test has asserted act `n-1`. The test's `close(gates[0])` is what lets the Down's
round run.

**The cleanup order is load-bearing.** Register the gate release in `t.Cleanup`
**before** the server's `t.Cleanup(srv.Close)`, so a red run runs them in the order
teardown → release → close. Otherwise a red run reaching `t.Fatalf` leaves a mock
handler parked on a gate forever, `httptest.Server.Close` never returns, and the
failure surfaces as `panic: test timed out` instead of the failing assertion's own
message.

## Defect 3 — the reading range ended on the status row

`helpMenuSession.interaction()` read `rowsRange(l.EditorTop, l.MenuTop+l.MenuRows+1)`.
The `+1` ends **one row past the menu** — on the **status row**, whose entire content
is an elapsed clock, a live token count and a spinner. The claim "the Down left the
grid intact" was therefore, in part, "the stopwatch read the same twice" — true or
false with **no key involved**.

A real red run of that case printed this before/after pair (evidence
`accept2/run-7.txt`, failure `the Down changed the menu/editing rows on a ONE-item
menu (it must clamp, not mangle)`):

```
--- before ---
you> /h
> /help (help for one command)
  nothing to move to — the only match
deepseek-v4-p… ·  iter 1 ·  ⚙ chat_surface {"action"… ·  ctx 30≡ ·↑0↓10   responding
--- after ---
you> /h
> /help (help for one command)
  nothing to move to — the only match
deepseek-v4-p… ·  iter 1 ·  ⚙ chat_surface {"action"… ·  ctx 31≡ ·↑0↓20   responding
```

The editing line and both menu rows are byte-identical. The only difference is
`ctx 30≡ ·↑0↓10` → `ctx 31≡ ·↑0↓20`. The Down had clamped exactly as a one-item menu
says it must — **the range was what lied**, and the failure message blamed the key.
The fix clamps the range at `l.StatusRow`.

The sibling's own comment shows the same lesson was learned twice in one file and
applied to the wrong end: it says *"the claim is about the menu and the editing line,
so it is read there"* — while reaching one row past those rows.

## What the fix actually asserts now

Nothing was weakened; every change made an assertion stronger:

| claim | before | after |
|---|---|---|
| the menu | `MenuRows > 0` | `MenuRows > 0` **and `> 1` candidate** (a premise, asserted: on a one-item menu a Down can only clamp, so a selection-move assertion is unfalsifiable) |
| the Down arrived | "the grid differs from a snapshot" | `MenuMarkerActive()` **changes ROW** — the claim's own subject |
| the motion is a motion | `rows != menuBefore` (grid, incl. streaming answer + clock) | reading range = editing line + menu rows **clamped at the status row**, read ONCE from one live layout |
| the line is undisturbed | *not asserted* | the editing line is **unchanged** by the motion |
| baselines | taken mid-draw | taken after `settle` (`keyE2ESettlePolls` consecutive identical readings) |

## The product race this surfaced (reported, not fixed here)

`go test -race ./cmd/agent/ -run 'TestChatSurfaceKey_'` is **12/12 red on the
pre-fix tree**, with a genuine data race on `lineedit.Value`:

```
Write by the READ LOOP:      (*Value).Reset()   value.go:101  ← (*Editor).Run()  editor.go:895
Previous read by the TURN:   (*Value).Text()    value.go:79   ← RowsFor → clampedMenu
                                                               → paintMenu → repaint
                                                               → Editor.WriteLines  editor.go:470
                                                               ← chatInput.WriteLines
```

`internal/lineedit/editor.go` documents that the read loop is the layout's **only**
writer and that a caller never writes. **The flow seam violates that**:
`Editor.WriteLines` calls `e.repaint()` on whatever goroutine calls it, and
`repaint → paintMenu → clampedMenu → Value.RowsFor → Value.Text` *reads* the mutable
`Value` that `Editor.Run → Value.Reset` writes. Two writers on the editor's model
state — not merely on the paint stream. Filed as task `01a0e608` for a fresh session;
it is a product change needing its own regression test, not a fixture edit.

**A flake investigation is a cheap moment to run `-race` on the same selection.** It
cost one loop here and found a defect the flake itself never showed.

## Follow-up: the Enter gap was a bare `time.Sleep` (stylegate test-sleep)

Task `01a0e64e`. The helper above shipped with a fixed wait between the typed line and
its Enter; `internal/stylegate`'s `test-sleep` sweep (ceiling 0) flags exactly that, so
master was red at `cmd/agent/chat_surface_key_e2e_test.go:125`. Removing it is not a
formality, and the two attempts are worth recording because the first one was plausible
and wrong.

**What the gap is really waiting for — two facts, not one duration.**

1. The characters must have been READ. The paste burst is armed by the read loop
   (`Value.NoteKeys`, `internal/lineedit/editor.go`), never by the pty write; until that
   read happens the burst state is simply absent.
2. The window must be OUT since that read. `Value.Enter` suppresses an Enter that arrives
   within `PasteEnterSuppressWindow` of the burst's last key.

**Attempt 1 — poll `Value.BurstActive()`. Wrong, and the read was in the source.**
`BurstActive` reads like the predicate, and `Value.Enter`'s doc says a suppressed Enter
needs "a burst that carried at least `PasteBurstMinChars` characters". But `BurstActive()`
is a **sticky flag**: armed at `PasteBurstMinChars` and cleared only by a real
key/Enter — it carries no clock at all. The window lives in the unexported
`suppressEnterAt`, which `Value.Enter` consults. Measured: the flag was still `true`
after 360 ms of polling **with the line already in the buffer**, so that poll could only
ever time out (`the editor still classifies "hello" as a PASTE after 360ms`). The
comment on the accessor names it correctly — "whether the input currently looks like a
paste" is the HINT a renderer shows, not the decision `Enter` makes.

**Attempt 2 — wait for the read, then wait the window out. Shipped.**
Two waits, in the order the facts can become true:

- a bounded **poll** on the buffer (`Value.Text()` carries the line) — a real observable
  that proves the read loop consumed the characters;
- a bounded **wait** for the editor's own `PasteEnterSuppressWindow` — because **nothing
  public exposes the window**. The only public route to the same answer is calling
  `Value.Enter`, which *submits the line this helper is about to submit*. So the half
  that cannot be polled is waited out against the editor's own constant, with the reason
  on the `//stylegate:allow-sleep` marker.

**The red-proof (the control that makes the second wait a fact rather than a guess).**
With only the buffer poll and the window wait deleted, the Enter is swallowed as pasted
content and the acceptance case fails by its own name at the menu leg:
`timed out waiting for the completion menu to open below the editing line` (8.0 s). The
wait is load-bearing, and its removal is what the case notices.

**A shared helper's pause needs the SESSION, not a global.** `keyE2ETypeLine` is called by
three pty cases (the acceptance case, the surface-bound case, the help-menu case). The
first version reached the live editor through a package-level variable published by
`t.Cleanup`-registered helper calls — which regressed the two siblings, whose sessions
never published it (`keyE2ETypeLine("start") ran before publishKeyE2EHarness`). Each
caller already held its own session, so the pause takes it as a required parameter
(`keyE2ESession`, satisfied by each session type's own `input()` / `render()`). A pause
that cannot name the editor it is waiting on cannot be correct for it.

**Same sweep, two more sites (the gate message named only one).** Master was red on
three: the `test-sleep` above, `unchecked-close` at
`internal/tools/memory_cjk_infix_route_test.go:137` (a bare `rows.Close()`, fixed by
checking it — together with `rows.Err()`, which was never checked there), and
`ignored-error` at `internal/testutil/scratch_placement.go:147`. The last is **not**
mechanically fixable: the arm is chosen on the FILESYSTEM KIND (`ScratchRootFilesystem`),
and `freeBytes` is a number *reported* to the caller — the room check for a caller's own
root is `assertScratchRootHasRoom`, which runs on the pinned root and reports its own
measurement. Its discard therefore carries `//stylegate:ignore` with that reason rather
than a fabricated error branch. **Never reason from the failing count**: the gate reports
one rule per run.

The ratchet is untouched: `git diff master -- internal/stylegate/` is empty.

## Method notes worth keeping

- **`git merge-base --is-ancestor <my-tip> master` BEFORE the repro loop.** The
  briefing said "both cases are selected by one regex"; the worktree was 4 commits
  behind master and **did not contain the second case at all**. 24 runs of a
  one-case selection proved nothing. Merging master in (fast-forward) was step one —
  without it the whole investigation, including the sibling's own flake, was
  invisible.
- **A flake that will not reproduce is not a dead end.** 54 pre-fix runs were green
  on this host. The defects were established by **mutation** — delete the key, remove
  the gate — which is deterministic and is also the red-proof the fix owes.
- **When a wait times out, first ask whether its predicate could ever be false.**
  Both defects here are of that shape: a predicate satisfied by unrelated activity
  (Defect 1) or one that can never become true (Defect 2).

## Fourth defect (2026-10-27) — a reading taken BETWEEN the layout publish and the redraw

Reported as: the whole-tree suite under load, `chat_surface_key_e2e_test.go:658` red with
`the editing row does not show the injected rune: ""`, green in isolation on the same HEAD.
Then reproduced **without any load** at 11/12 on `-count=12` (`go test ./cmd/agent/ -run
TestChatSurfaceKey_DrivesTheRealREPLEditorOnARealPty -count=12`) — the failure is a race on
the paint stream, not on wall-clock, so contention only changes how often it lands.

**Root cause: a half-painted frame, not a missing rune.** LEG 1 waited for
`h.menuRows() > 0` — the LAYOUT's published reservation — and then read the editing row
off the grid. Those are two different writes on the read loop, in this order:

```
region.layout()      ← SetMenuRows(5) publishes EditorTop 22 → 17   (write A)
render.Redraw(...)   ← ONE write: placement + `you> /` on row 17    (write B)
paintMenu()          ← the candidates, below it                     (write C)
```

`h.editingLine()` reads `Layout().EditorTop` (17, the NEW layout) and then grid row 17 —
which between A and B is STILL BLANK, because the previous `you> /` was painted on row 22
and the band's own erase has already cleared the rows it gained.

**The measurement** (`keyE2EPaint.probe`, a hook that renders the grid at EVERY write
boundary — installed temporarily, removed before the fix landed): 6 runs, identical shape,
3 of 3 sampled transitions in every run:

```
bytes=4868 menu=0 row=22 line="you> /"   ← line on the OLD row, layout not yet moved
bytes=4880 menu=5 row=17 line=""         ← layout published; the redraw has NOT landed
bytes=4946 menu=5 row=17 line="you> /"   ← the redraw that fills it
```

So the empty string the case reported was **"painted halfway"**, never "the rune never
arrived": the buffer held `/` the whole time — it is what opened the menu.

**The fix (test side, no assertion weakened):** `h.waitEditingLinePainted` waits until the
editing row the CURRENT layout names begins with `chatPrompt`. The prompt is the check and
not the buffer, deliberately: `Redraw` composes placement + row text into ONE write, so a
row starting with the prompt is a row this frame FINISHED, while asking the buffer would
read `*Value` across goroutines (the race recorded above). The assertion after it is byte
for byte the old one.

**Red proof (that the case still fails when the key does not arrive):** changing the
assertion's needle to a rune the injection never sends makes it red 2/2 with the same
message — `the editing row does not show the injected rune: "you> /"` — i.e. the wait does
not swallow the claim.

**Verification:** `-count=20` by hand and `make test-budget PKGS='-run <case> -count=40
./cmd/agent/'` → 20/20 and 40/40 PASS (exit 0, stamped `outcome=pass`), against 11/12 on
the pre-fix tree.
