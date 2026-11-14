<!-- doc-class: normative -->
# The editing model's write ownership — one owner and a published copy

Task `01a0e67f` (epic: close the third seam of the `internal/lineedit` two-writer
family, after the FLOW seam `01a0e608` — `devdoc/lineedit-band-write-ownership.md`
— and the ANSWER RENDERER `01a0e630` — `devdoc/lineedit-answer-write-ownership.md`).

Code: `internal/lineedit/valueowner.go` (the invariant, `publish`, `snapshot`,
`road`, the road names), `value.go` (`Value.pub`, `tick` → `publish`, the readers
`Text`/`Len`/`Cursor`), `editor.go` (`noteAudience` records the loop's identity
INTO the value), `internal/lineedit/valueowner_foreign_test.go` (the guard).

## What was wrong

`Value` is the editing buffer. Its contract in this package is stated for the
layout ("the read loop is the layout's only writer and the editing model's only
reader"), and the two earlier epics moved the band and the answer renderer onto
publish queues so that contract could hold. The model itself was never given an
owner, and the measured pair on master `9aba625b` was:

```
WARNING: DATA RACE
Write at 0x00c000a121b0 by goroutine 73:
  lineedit.(*Value).Reset()             value.go:108
  lineedit.(*Editor).Run()              editor.go:1204
  cmd/agent.(*chatInput).ReadLine()     lineedit_wire.go:165
  cmd/agent.runChatWithInput.func8()    cli_chat.go:1338        <- the READ LOOP
Previous read at 0x00c000a121b0 by goroutine 61:
  lineedit.(*Value).Text()              value.go:85
  cmd/agent.keyE2EEditorText()          chat_surface_key_e2e_test.go:200
  cmd/agent.keyE2EWaitEnterGap()        chat_surface_key_e2e_test.go:187
```

**The census is the design input, so it is a re-runnable command rather than a
number in prose** (this repo's ruling for census questions). Every *production*
reader of the model, from the branch tip:

```bash
grep -rn "e\.val\.\|\.Value()\." --include=*.go internal cmd | grep -v _test
```

and every hit is `internal/lineedit/editor.go` reached from `Run` (`repaint` →
`clampedMenu`/`paintMenu`/`submit`/`handleKey`/`spillAppend`/`CompleterRequest`).
The ONE reader outside the loop was the harness's own buffer poll:
`keyE2EEditorText` asks "has the run's editor taken the typed line yet?" while the
loop is between lines, inside `Run`'s opening `Value.Reset()`.

So this seam is not the shape of its two predecessors. Those had a foreign
PRODUCER to route; this one has a foreign READER, and the read is legitimate — a
caller may ask what the model holds. What it may not do is sample the buffer while
its owner is editing it.

## The fix

**A mutation publishes; a read is served by that copy.** `Value.tick` — the funnel
every mutation already ends in: the splice, the delete, the kill, the cursor motion,
the history recall — stores the buffer into one `atomic.Value` slot, so the slot
holds the contents as of the last completed mutation and "did you remember to
publish?" has no call site to get wrong (Rule 14). `Text` then reads that slot, and
`Len`, `Cursor`, `Lines` and `RowsFor` are all defined in terms of `Text`, so they
inherit the road instead of restating it.

**One exception, and it is the loop's own read.** `publish()` reports whether the
CALLER is the read loop; on the loop, `Text` returns the live `v.runes` with no copy
and no snapshot in the way. The loop is the only writer, and its `repaint` reads the
model it just edited — paying for a copy on every frame would be a cost the fix
does not need to impose.

**There is NO owner until a read loop takes the buffer up.** `ownerGoroutine()`
answers the loop's recorded id, and `0` while none has recorded itself. A caller
that is not that id — which is every caller before the first `Run` — is served the
published copy. Concretely: a goroutine does not own the editing model by virtue of
having CONSTRUCTED the editor around it.

**The copy is the point.** A splice that does not overflow the capacity moves the
same backing array (`spliceRunes` → `copy`), so a snapshot holding `v.runes` would
keep changing under its reader — a window, not a snapshot. `publish` therefore
copies into fresh storage every time, on the writer's goroutine, beside an edit that
already moved the same bytes (one memmove for a 64 KiB line: the
`paste_cost_test.go` budget — 15 s for 64 reads × 4 KiB — is untouched and the whole
package suite passes unchanged).

**Rule 15 was the constraint, not a preference.** No mutex over the buffer: it would
leave both goroutines' passes interleaved exactly as before — a read landing inside
the loop's splice — and merely stop the detector from seeing it. The fields stay
unguarded on purpose, so an interleaved pass remains *visible*; the only atomic in
the design holds the copy that two goroutines legitimately share.

**A rejected alternative: moving the harness's read.** Making `keyE2EEditorText`
poll something else would have removed this measurement without restoring the
invariant. The read is legitimate, it is the shape every future foreign reader
takes, and the fix closes the class rather than the instance.

**A rejected alternative: reading the composer's parked draft**
(`ComposerDraft`, `internal/lineedit/composer.go`, sibling task `01a0e635`). It is a
different concern — a fullscreen-mode draft slot — and it is not a snapshot of the
live editing model, so a reader that used it would read something that is not the
buffer.

## Two measured wrong answers (the road to the rule)

Both were caught by this task's own acceptance loop, and both are the same mistake
seen from two sides: **giving a goroutine ownership of the buffer because it built
the editor around it.**

1. **A per-Run owner.** First cut: the value recorded the loop and cleared the
   record when `Run` returned. `ReadLine` calls `Run` ONCE PER LINE, so the model
   was ownerless BETWEEN two lines — exactly when a caller polls. The fallback
   owner was then the goroutine that built the editor, which in cmd/agent's pty
   harness is the TEST's own goroutine and the one that polls: `Value.Reset` vs
   `Value.Text`/`publish`, 3 runs of the first 4.
2. **The builder as a fallback owner.** Second cut: keep the loop's record sticky,
   but fall back to the constructing goroutine. That left the same window at the
   OTHER end — before the loop's first `Run` noted itself — and the caller that
   built the editor read the live fields while the loop entered `Reset`: 4 blocks in
   each of 2 consecutive runs.

`TestValueOwner_TheBufferOwnerIsSticky` is the first defect's deterministic repro
(the zero id must not release the buffer), and the no-owner-before-the-loop rule
closes the second structurally rather than by timing.

## The guard (deterministic, no `-race`, no sampling)

`valueowner_foreign_test.go`, the same shape as `bandwriter_foreign_test.go` and
`answer_writer_foreign_test.go`. The read loop is PARKED on the source's gate
(`guardedOrderingSource`), a whole line is scripted through it, and then:

1. `TestValueOwner_ForeignReaderReadsTheSnapshot` — a read from the TEST's own
   goroutine must take the `snapshot` road, return the model's contents, and leave
   the copy it took UNCHANGED by the owner's next edit — while the model's own
   publication MOVES in the same breath, so a snapshot nothing updates cannot pass.
2. `TestValueOwner_TheOwnerReadsTheLiveBuffer` — the other road, pinned: the same
   question asked from inside the loop's own released read (`afterRead` runs on the
   loop's goroutine) must report the `owner` road, so the guard cannot be satisfied
   by routing everybody to a copy.
3. `TestValueOwner_TheBufferOwnerIsSticky` — mechanism: no id recorded ⇒ the caller
   is not an owner; the loop's id ⇒ a different goroutine is not; a zero id (the
   per-Run release) ⇒ still the loop's.
4. `TestValueOwner_GuardIsDeterministic` — the sequence 5×; the barrier is a gate
   and the observable is a road, never a sampled window.

**Red-proof, out-of-tree, with a one-line revert.** A detached worktree at the
branch base `9aba625b` (`/home/user/workspace/gopi-redproof-01a0e67f`) takes
`internal/lineedit/` from the fix commit, and then the ownership test is inverted —
`ownerGoroutine()` answered with the calling goroutine, i.e. "everyone is on the
loop", which is the shape of the defect (`-race`'s `Value.Text` on the harness's
goroutine reading fields `Value.Reset` was writing). The failure is in the task
report's red-then-green table.

## Measured result

Acceptance: `go test -race ./cmd/agent/ -run 'TestChatSurfaceKey_' -count=1`, 12
consecutive runs — exit 0, 0 `WARNING: DATA RACE`, 0 failing cases. Per-run numbers
and the pre-fix baseline are in the task report (session dir
`~/.gopi/sessions/01a0e67f-7eb3-788a-b13d-ddba2ab5fc37/`).

## Not done / honest residue

- **`Value`'s paste-burst state is not in the snapshot.** `BurstActive()` returns
  `v.burstActive` and `suppressEnterAt` reads `burstEnd`/`burstActive`; neither is
  read by a goroutine outside the loop today (the renderer and `Value.Enter` are
  loop-side or direct-drive). Recorded as **not covered**, not as fixed: widening
  `valueSnapshot` with the burst clock is a behaviour change (the burst window is a
  timing decision) and belongs with a caller that needs it.
- **The publish costs one allocation per mutation**, bounded by the same buffer
  bound the sieve already enforces (`valueMaxBytesDefault`). It does not show
  against the existing budget test, and it buys the property the whole fix rests on
  (a copy, not a window).
- **The pty FIXTURE's own race is not this seam.** Master carries its fix
  (`d2fda946`, merged into `master` after this branch's base) and this branch's
  acceptance runs include it. A block naming `chat_pty_fixture_test.go` /
  `closeQuietly` is the harness's, not the model's.
- **No new dependency, no new knob, no new process (Rule 35)**: the change is one
  atomic slot and one copy inside the package that already owns the buffer.
