<!-- doc-class: historical -->
# Serve/Worker/Child Process-group Design Decision — process-group isolation DEFERRED (2026-09-03)

## Context
Two live incidents (2026-09-03): killing `gopi task serve` also killed its running child
(t-1788382802, t-1788382685 — same process group) → FinishSessionTask never ran → zombie
auto-rows. Proposal considered: spawn each child in its OWN process group so a serve restart
doesn't take running children down (they finish and self-report; serve restart becomes safe
anytime).

## The decision (nick, 2026-09-03) — DEFERRED, do NOT implement now
Child process-group isolation is REJECTED for now. Reasoning:

1. **It opens a zombie door to close a corner case.** Same-process-group (parent death kills
   children) is a DELIBERATE protection: a parent that dies never leaves orphaned children
   running unattended. Isolating the child group means: kill serve → children keep running as
   orphans — exactly the zombie class we've been fighting all week. We'd trade a guaranteed
   invariant (no orphans when the parent dies) for a singular convenience (restart serve while
   children run).
2. **"Restart serve on every new binary" is the wrong pattern.** Serve is a STABILITY design —
   it should rarely update. The binary-follows-master rebuild feature (t-1788383728) applies to
   TASK repos, not to the serve process itself (bootstrap: serve can't restart itself). The
   correct operation is: serve runs on a stable binary; when a new binary is wanted, restart at
   a natural window (no children running / maintenance), not eagerly after every merge.
3. **Cannot satisfy all use cases — weigh by frequency.** How often do we actually need to
   restart serve while children are mid-run? Rarely (restart at empty-queue windows instead).
   How often would isolated children become orphans? Every single parent kill from then on.
   The frequency argument is decisive: keep the invariant, handle the rare restart at a window.
4. When a parent IS killed with children mid-run (the 2026-09-03 incidents), the fallout is
   bounded: zombie auto-rows that the query-side stale sweep / serve-waitpid reconciliation
   (t-1788384486) cleans up — NOT orphaned processes running unbounded.

## What stands (still correct)
- **waitpid reconciliation at the runTaskRun layer (t-1788384486)** — child dies (crash /
  SIGKILL / non-zero) while the PARENT (serve tick / worker loop / manual run wrapper) is
  alive → the parent marks the row FAILED itself (never trusts child self-report). This works
  WITH same-process-group semantics: child death does not take the parent down, so the parent
  can reconcile. Only the "parent also dies" case (serve killed) escapes it — acceptable,
  covered by the stale sweep + human at the next look.
- Same-process-group stays: parent death = children die = no orphans. Deliberate.
- Signal handlers: SIGTERM/SIGINT are capturable (at-exit best-effort writeback is possible and
  worth adding — child catches SIGTERM → writes terminal state before exit); SIGKILL is NOT
  capturable (kernel semantics) — parent waitpid is the only backstop there.

## Future consideration (revisit only if)
- Hot-upgrade of serve becomes a real need (zero-downtime restart while tasks run), OR
- Tasks become long enough (hours+) that killing them on serve restart is costly, AND we build
  proper orphan adoption (a new serve re-parents and reconciles existing children) — without
  adoption, isolation is just zombie generation.

## Operation note
- Serve restart policy: restart at a natural window (queue empty / no children), NOT eagerly on
  every master merge. Rebuild-after-merge (t-1788383728) rebuilds the BINARY; activating it in
  serve waits for the window.
