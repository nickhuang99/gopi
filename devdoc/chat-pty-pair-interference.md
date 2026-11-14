<!-- doc-class: historical -->
# The chat pty acceptance pair: two flaky failures, two root causes, no shared resource

Class: historical — a past run's record and the correction of the premise it was
dispatched under. (`gopi doccheck` classifies it so; the banner above matches the
manifest. Its standing content is the *reasoning*, which the code comments in
`cmd/agent/chat_surface_key_e2e_test.go` carry forward.)

Task: `01a0e5f2`. Cases: `cmd/agent/chat_surface_help_menu_e2e_test.go`
(`TestChatSurfaceKey_OperatorHelpMenuActDrivenOnlyByTheModel`) and
`cmd/agent/chat_surface_key_e2e_test.go`
(`TestChatSurfaceKey_DrivesTheRealREPLEditorOnARealPty`).

## The premise was wrong: there is no interference between the two cases

The dispatch (task `01a0e442`) reported the PAIR failing 1-in-8 when run in one
`go test ./cmd/agent/` invocation, "each case passing 12/12 alone", and asked for
the shared resource — pty allocation, mock SSE port, `settle()` wait race.

Measured (this task, base tip `5a22254b`, 48 runs):

| what was run | result |
|---|---|
| the pair, 24 runs | 6 FAIL — runs 1, 8, 10, 11, 20, 21 (25%) |
| the menu case ALONE, 12 runs | 2 FAIL — runs 4, 7 (17%) of the 10 that COMPILED |
| the key case ALONE, 12 runs | **not measured in this batch — the 12 logs are BUILD FAILURES, not runs** |
| the key case in the pair | 1 FAIL of those 24 (run 21) |

**Correction, measured 2026-09-28 by re-reading every log rather than the tally.**
The "key case ALONE, 12 runs, 0 FAIL" row was read out of a `pass=0 fail=12`
count and is FALSE as an experiment: `isolated/key-*.log` all carry
`cmd/agent/chat_surface_help_menu_e2e_test.go:420: s.interaction undefined
[build failed]` — the package had a transient compile error while the isolated
loop ran, so the key case never executed there. The same batch's
`isolated/menu-12.log` is a build failure too, so the menu tally is 2 real FAILs
out of 10 runs that ran (20%), not 2 of 12. Nothing above turns on the missing
row — the falsification rests on `isolated/menu-{4,7}.log`, two REAL red runs with
their sibling case absent from the process (verbatim transcript below, and the
same text on a real red pair run) — but the row is withdrawn rather than left
standing on a tally of a compile failure. The fix's own acceptance loop is
unaffected: `merged/`, `final/` and `stability/` each checked
`grep -l 'build failed'` → 0.

```
--- FAIL: TestChatSurfaceKey_OperatorHelpMenuActDrivenOnlyByTheModel (0.41s)
    chat_surface_help_menu_e2e_test.go:467: the Down changed the menu/editing rows on a ONE-item menu (it must clamp, not mangle)
        --- before ---
        you> /h
        > /help (help for one command)
          nothing to move to — the only match
        deepseek-v4-p… ·  iter 1  ·  ⚙ chat_surface {"action"… ·  ctx 30≡ ·↑0↓10               responding 
        --- after ---
        you> /h
        > /help (help for one command)
          nothing to move to — the only match
        deepseek-v4-p… ·  iter 1  ·  ⚙ chat_surface {"action"… ·  ctx 31≡ ·↑0↓20               responding 
FAIL
```

(`grep -l FAIL isolated/menu-*.log` → `menu-4`, `menu-7`, and the build-failure
`menu-12`; the before/after blocks below the assertion are byte-identical except
the status row, which is the stopwatch.)

### The red proof — each fix, made red on purpose, measured (Rule 10.5)

A fixed flake is only evidence of a fix if the graded run FAILS when the fix is
undone. Both halves were reverted in turn on the final tree and the pair command
re-run. **The samples were sized to the measured base rate, not guessed**: the
unfixed tip failed 6/24 (25%), so an 8-run sample can come back green by luck —
and the first attempt at the menu half did exactly that (8/8 green, a false
negative). Everything below is a counted result.

| what was reverted on the final tree | runs | result |
|---|---|---|
| `menuBottom` back to the pre-fix `MenuTop+l.MenuRows+1` (no clamp) | 40 | **3 RED** — runs 1, 38, 40, every one `chat_surface_help_menu_e2e_test.go:487: the Down changed the menu/editing rows on a ONE-item menu`, before/after byte-identical except `ctx 30≡ ·↑0↓10` → `ctx 31≡ ·↑0↓20` |
| the Down DELETED from `keyE2EInjectedKeys()` | 3 | **3 RED** — i.e. the case is red on 3 of 3 whenever the key it claims to test is absent. It reddens as a PANIC, not as the assertion, and the panic is worth naming: `chat_surface_key_e2e_test.go:526: index out of range [0] with length 0`, because `gates := make([]chan struct{}, len(act)-1)` derives its round count from the injected keys, so removing one leaves `gates` empty at `close(gates[0])`. That is the harness refusing to run a corrupt injection rather than quietly passing — but a reader should read the 3/3 as "the case cannot pass without its Down", not as an assertion firing. |
| the marker-ROW wait reverted to the old proxy ("the grid differs from a snapshot") | 6 | **0 RED — inconclusive, and reported as such.** A proxy wait reddens only under a timing draw this sample did not hit. The v1 file's own measurement (7/7 red, `postfix/`, `postfix2/`, on the taller 6-candidate menu) is the evidence for this leg, not this table. |

The first row is the correction of a false negative I produced myself: the 8-run
version of it was green 8/8 and would have "disproved" a real defect.

```
--- FAIL: TestChatSurfaceKey_OperatorHelpMenuActDrivenOnlyByTheModel (0.42s)
    chat_surface_help_menu_e2e_test.go:487: the Down changed the menu/editing rows on a ONE-item menu (it must clamp, not mangle)
        --- before ---
        you> /h
        > /help (help for one command)
          nothing to move to — the only match
        deepseek-v4-p… ·  iter 1  ·  ⚙ chat_surface {"action"… ·  ctx 30≡ ·↑0↓10               responding 
        --- after ---
        you> /h
        > /help (help for one command)
          nothing to move to — the only match
        deepseek-v4-p… ·  iter 1  ·  ⚙ chat_surface {"action"… ·  ctx 31≡ ·↑0↓20               responding 
```

**The reverted files were restored with `git checkout --` inside the same script,
and the restore is checked, not assumed**: each script's `summary.txt` ends with
`git status --porcelain` printing only the devdoc (`M devdoc/…`) — no test file
left modified. Transcripts: `redproof-a/`, `redproof-a2/`, `redproof-b/`.

**The menu case also fails with its sibling absent from the process**, so there is
no cross-test interference to find: with `-count=1` and no `t.Parallel`, a single
test is the only thing running.

Checked and excluded as shared resources: `GOPI_HOME` (both cases `t.Setenv`
their own `t.TempDir()`), the pty (each case opens its own pair and closes it),
the mock SSE server (`httptest.NewServer` per case, ephemeral port). Nothing in
either file is package-level mutable state.

Note on `t.Setenv`: it makes a test **ineligible for parallel execution** and
`t.Parallel` is called in neither file, so the two cases run serially in one
process — which is why neither a port nor a pty can be shared.

## Defect A — the reading range reached one row PAST the menu

`helpMenuSession.rowsRange(top, bottom)` renders `[top, bottom)`. Both callers
passed `l.MenuTop + l.MenuRows + 1`, one row past the last menu row — on a
layout where the row after the menu is the **status row**, which carries the
elapsed clock and the `↑N↓M` key counter by design.

The range was introduced (commit `e749a04a`, the commit that ADDED both cases) to
keep a stopwatch out of an equality claim; the `+1` put it back in. The failure is
verbatim the stopwatch:

```
--- FAIL: TestChatSurfaceKey_OperatorHelpMenuActDrivenOnlyByTheModel (0.41s)
    chat_surface_help_menu_e2e_test.go:467: the Down changed the menu/editing rows
    on a ONE-item menu (it must clamp, not mangle)
    --- before --- / --- after ---
    (every menu and editing row byte-identical; the ONLY differing line:
     " ctx 30≡ ·↑0↓10 · responding"  ->  " ctx 31≡ ·↑0↓20 · responding")
```

Both readings on the status row differ in the elapsed second AND in the key
counter — `↓1` is the Down this very assertion is about, arriving between the
baseline and the reading.

The layout is readable straight out of the log: `rowsRange(l.EditorTop,
l.MenuTop+l.MenuRows+1)` printed the editor line (`you> /h`), the blank, the menu
row and the note row AND then the status row, because the status row sits
immediately after the menu's last row — exactly where the range reached.

The same off-by-one allows a second, silent shape — `MenuTop + MenuRows ==
EditorTop`, so both readings come back the EMPTY string — which was NOT observed
in any of the runs captured here (it needs the menu closed at BOTH readings, and
this assertion site runs only while the menu is up). The form that IS observed,
in every red log (`baseline/pair-1`, `baseline/pair-8`, `isolated/menu-4`), is the
clock form above. The fix removes the whole class: neither the real status row nor
the fake one can be reached by `[EditorTop, MenuTop+MenuRows)`.

**Fix**: the two callers pass `l.MenuTop + l.MenuRows`. The documented intent
(the menu plus the editing line, never the status row) is now what the code does.
The Down's claim keeps its full scope and is now *dead-on* rather than
clock-lucky: two readings of the same cells from ONE layout read.

## Defect B — the Down was delivered BEFORE its own baseline was read

The case fired BOTH injected keys from the session's first model round. The
injector's pump keeps only the pacing gap between two reads and
`startInjectPump` returns once its queue is empty, so by the time the round ended
and the test read its pre-Down baseline, the Down had already been spent. The
dump from the first attempt (marker on the second row, the baseline showing the
first immediately after) is conclusive:

```
--- FAIL: ...DrivesTheRealREPLEditorOnARealPty
    timed out waiting for the injected Down to move the selection
    ⚙ call chat_surface {"action":"key","text":"/"}
    ⚙ call chat_surface {"action":"key","text":"down"}
    ▌ the keys were injected.
    you> /
      /log ...  > /new ...  /task ...  /quit ...
```

A first fix attempt changed the comparison from "the whole grid differs" to "the
selection MARKER moved", which is the right cell-level fact but does not help:
`marker == marker` is still a comparison against a **snapshot taken too
late**, and it failed 7 runs out of 7 from that moment (the process in which the
snapshot was introduced).

**Fix**: the Down is now injected in its OWN model round and that round **parks
until the test releases it** (`gates[0]`, closed after the baseline read) — the
one-key-per-round, test-gated arrangement the sibling case already used for the
same reason. The comparison itself is unchanged and keeps its full scope: the
baseline is a snapshot of the marker's row taken while the motion provably has
not happened yet.

Two ordering hazards this gate creates, both measured while landing it:

1. **The bootstrap barrier.** The key case used to wait for the session's TEXT
   answer to prove "a round happened" before touching the grid. With gated keys
   that answer is only sent after the last release, so the wait deadlocked the
   test (`timed out waiting for the session to run its first turn`). Round 1 is
   in flight as soon as the bootstrap line is submitted, so the menu opening IS
   round 1's effect — the barrier is now the positive outcome this leg asserts.
   The round ORDER is deliberately not assumed: the gate parks rounds 2..N but
   never the round that carries the tool RESULTS (each key's own request), so the
   positive-half replay of `injected=` still sees every key.
2. **The release must precede `srv.Close`**, i.e. `t.Cleanup(func(){ close(done) })`
   registered AFTER `t.Cleanup(srv.Close)` (cleanups run LIFO). A parked handler
   keeps its connection open, and `httptest.Server.Close()` waits for it.

Assertion strength, before → after (nothing weakened; one item REMOVED as
surplus, listed honestly):

| claim | before | after |
|---|---|---|
| menu opened | `menuRows() > 0` | unchanged |
| rune on the editing row | `contains("/")` | unchanged |
| Down moved the selection | whole grid differs from a snapshot | **the marker's ROW moved** (cell-level, name-stable via `lineedit.MenuMarkerActive()`) |
| exactly one marker (dense index, no clamp) | not asserted | **added** (`len(selectionBefore) == 1`) |
| menu still up | `menuRows() != 0` | unchanged |
| Down was decoded as a key, not text | (implied) | `editingLine()` carries no `down` text — **added** |
| line survived the interaction | `TrimSpace(editingLine())` contains `"/"` | unchanged |
| injection really happened | `len(recorded) >= 2`, count `injected=` in the LAST request | same count, read from the request that carries the last key's result |

The sibling case's (`chat_surface_help_menu_e2e_test.go`) assertions are entirely
untouched — its whole diff is the two `rowsRange` bounds — and that case is the
one that owns the `menuRows() == 1 + CompletionNoteRow` claim, the one-item-menu
note row and the Enter-accepts leg.

The rune assertion `editingLine() contains "/"` was retained although it makes
the following `TrimSpace(...)` check partly redundant: it is the cheapest direct
statement that the rune landed, and removing whole-string assertions is exactly
the kind of "tightening" this task forbids.

The `trimSpace(line) contains rune` vs `contains line "down"` pair is NOT
redundant: independent of the menu, it distinguishes "the arrow was decoded"
from "the arrow was typed as text".

## The cleanup-order trap (measured while landing this fix)

A model round parked on a channel holds its HTTP handler open; `httptest.Server.Close()`
waits for outstanding requests. `t.Cleanup(srv.Close)` therefore must be
registered BEFORE the `close(done)` release, because cleanups run **LIFO** —
release first, then close. Registered the other way round, a red run hangs in
Close until the binary's own test timeout: measured on the first attempt at this
gate, `go test -count=1 -run 'TestChatSurfaceKey' ./cmd/agent/` produced no
output for over two minutes with both assertions pending. That is the same
hazard the sibling case documents in its own comment, and the reason the release
in BOTH cases is registered after `srv.Close`.

## After the fact: a sibling task landed a stronger fix on master

This branch measured, fixed and proved the pair, and then found that task
`01a0e5d2` had fixed the SAME case on master in parallel (`23d673f5`, ~15 minutes
earlier). Its key-case fix is a **superset** of this branch's:

| | this branch | `23d673f5` on master |
|---|---|---|
| one key per model round, test-held gate | yes | yes |
| cleanup order (`srv.Close` then `close(done)`) | yes | yes |
| no separate "first turn" wait | yes | yes |
| baseline from a SETTLED frame (settle-polls) | no | yes — the menu paints over several writes, so a mid-draw baseline is satisfied by the rest of the same draw |
| waits on the SUBJECT (marker's ROW) | no — waited on "the grid differs" | yes — the answer band streams into that same grid, so "differs" was satisfiable by the answer's own text |
| red-proof (delete the Down ⇒ the case must fail) | no | yes — and it failed, i.e. the unfixed wait was hollow |
| range off-by-one in the help-menu case | yes (2 lines) | yes (`menuBottom()`, with an explicit status-row clamp) |

So the merge (this branch) **took master's version of both test files verbatim**:
combining them would have restored the weaker legs — this branch's grid-differs
wait and its mid-draw baseline. `git diff master -- cmd/agent/` on the merged tree
prints nothing. What this branch keeps, and what neither of those commits carries,
is the **falsification of the interference premise** and the **measured per-case
failure rates** — the reason the dispatch's "shared resource" framing was wrong.

Two honest notes. (1) This branch's `fbf3e6ef` fix was itself measured 12/12 green
on the base tip before the merge, so it was not known to be insufficient when it
was committed; the merge is a supersession, not a correction of a red result.
(2) The `-race` question is NOT settled here: `23d673f5`'s own report says `-race`
on this selection is 12/12 red for an **unrelated PRODUCT race** (the flow seam's
repaint reads `Value` while the read loop's `Value.Reset` writes it) — that is a
product defect in a different layer, reported by that task and not addressed by
this one.

## Evidence

- Baseline + isolated + postfix transcripts:
  `/home/user/.gopi/sessions/01a0e5f2-0a1d-76db-9a69-9cb5c7706d91/{baseline,isolated,postfix,postfix2}/`
- Provenance: both failing lines were introduced by `e749a04a`
  (`git log -S 'menuBefore' -- cmd/agent/chat_surface_key_e2e_test.go`,
  `git log -S 'MenuTop+l.MenuRows+1' -- cmd/agent/chat_surface_help_menu_e2e_test.go`).

## Verification

```
go build ./...                                   # OK
go vet ./cmd/agent/                              # OK
go test -count=1 -run 'TestChatSurfaceKey' ./cmd/agent/   # 12/12 PASS
```

As merged (`8897c6d3`), re-run on the merged tree — `merged/summary.txt` and its
12 logs under the session directory; see the task report for the counted result.
