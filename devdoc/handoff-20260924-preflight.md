<!-- doc-class: normative -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->
---
doc-class: historical
status: SNAPSHOT as of 2026-09-24 (evening, before a hermes restart)
---

# Handoff — 2026-09-24 evening (pre-restart)

Written immediately before a hermes restart. Everything below was **measured**, not
recalled. Where I only inferred something, it says so.

## 1. Restart mechanics — what dies, what survives

**Dies with the restart:** the hermes session (pid 260333, `hermes chat --cli --yolo`,
up 9h06m) and anything parented to it. Right now **nothing of ours is parented to it** —
verified: no `gopi` process runs at all, no `gopi merge --every` loop, no worker.

**Survives:** everything on disk — git history, the task ledger (`~/.gopi/memory.db`),
`~/.hermes/memories/MEMORY.md`, all raw logs and session dirs.

**Must be restarted manually after the restart** (there is no daemon, by design):
> **Retired mechanism (merge verb removal, 2026-11-01).** The restart recipe in the
> bullet below is dead: `gopi merge` and its `--every` loop were removed, so both
> of its lines now exit **1**. This handoff is a dated record of 2026-09-24 and is
> kept as one; the merge face a reader would use today is `gopi task merge`, which
> is a one-shot and has no `--every`.

- nothing is currently required. If you want periodic merging again:
  `cd ~/work/GoPi && ./bin/gopi merge --every 5m --fast` (foreground, Ctrl-C stops it;
  a second Ctrl-C kills it — the bounded-wait + second-signal escape landed in `1abf8bf9`).

**Do NOT** start a wrapper script, a systemd unit, or a cron entry for this. That is a
standing instruction (`gopi merge --every` is the answer; the loop lives in the process)
— *and the instruction it is standing ON is gone with the verb: there is no `--every`
loop to run today.*

## 2. Repository state

- Repo: `/home/user/work/GoPi`, branch `master`, tip **`4195c7c2`**.
- Working tree: **clean** (`git status --porcelain` = 0 lines).
- Worktrees: **1** — the main checkout only. All residue reclaimed.
- Task ledger: 2382 rows, **all terminal** — completed 1808 / cancelled 346 / failed 231 /
  verified 68. **Zero pending, zero in_progress.**
- Zombie processes: **0**.

## 3. What this session delivered

Twelve commits landed on master. Grouped by theme:

**A. merge gate cache (was: every merge pass self-destructed the build cache)**
- `35614d1d` lift the BUILD gate's cache bound above the workload peak
- `70ee389d` …and a root-level cache leak becomes collectable
- Brief: `briefings/brief-merge-gate-cache-bound-and-root-leak.md` (`2854bf16`)
- Task: `01a0d09e-aa88-736e-8cf7-5b766af1abdb` (completed)

Measured before the fix: cache body steady-state peak **518–521 MiB** against a
**512 MiB** bound → structurally guaranteed drop on every pass. Real drop cost ≈ **14 s**
(the observed 2m57s was build+vet on an already-cold cache). Separately found and cleared
a **733 MiB root-level ghost GOCACHE** (256 shards / 6902 files) that the bound could not
see and no build ever hit; clearing it took warm build from 25 s → **1.13 s**.

**B. `gopi version` identity (was: botching a *responsibility*, not a feature)**
- `ddbc3df1` the binary's identity is its own — internal, constant, checkout-independent
- `e6e9a6e8` the version-tag drop leaves the revert path too
- Brief: `briefings/brief-version-hardcoded-not-repo-derived.md` (`b17e8cc5`)
- Task: `01a0d09f-ce95-72bb-9a29-55df78a31e85` (completed)

Root: `gopi version`'s headline was a **tag name minted from the commit title of whatever
repo it happened to stand in**. 181 `version/*` tags existed. gopi is a general agent
serving arbitrary repos — a project's release ritual belongs to *that project's* git hook,
never to the merge path of a generic agent.

**C. `gopi worker --resume` (was: resume could never clean up after itself)**
- `662b16a3` worker --resume owns the ground (branch/worktree)
- `5e932a96` merge of that work
- Brief: `briefings/brief-worker-resume-owns-the-ground.md` (`99953c18`)
- Task: `01a0d0cc-fcff-7086-8ee6-ea2175c1e6a7` (completed)

Structural fact worth keeping: `gopi resume` chdirs into `deliverable.worktree`, so
finish-cleanup's removal is refused by `guestRowOwnCheckoutRefusal`
(`internal/tools/guest_row_guard.go:236-306`) — **correctly**, because removing it would
delete the ground under the running process. That arm records the real incident it
prevents (2026-09-21, `gopi-jieba-searchkw`, holder alive, branch 14 ahead). Consequence:
**every successful `gopi resume` leaves its worktree behind for someone else to
reclaim.** Reclaim it by running `git worktree remove` **from the main checkout**.

**D. scratch lifetime owned by writers**
- `57e26965` writer-owned scratch lifetime — owner record v2 judged by RunnerIdentity as
  the sweep's first arm, plus `scratch-gc --explain`
- `3dd59e7b` merge
- Related rework `01a0cfc3-8107` (completed)

**E. merge `--every` loop + its own failure mode**
- `1abf8bf9` the loop owns the failure mode it created — a bounded wait and a
  second-Ctrl+C escape
- `db560467` merge
- Task `01a0cfa7-f535-7628-a28a-32be7d297a99` (completed)
- **Nick's verbal spec, verbatim:** `gopi merge --every 5m --fast`, foreground, never
  exits; **no script, no unit file, no cgroup/detach machinery**; on Ctrl-C the pass in
  flight **must not be interrupted** — the signal only refuses the *next* pass; a second
  Ctrl-C must kill it (we replaced the default handler, so we owe the default back).

**F. docs / gates**
- `4195c7c2` resolve conflict markers the previous check-in left behind
- `d0deaa68` re-key drifted anchors, delete 8 stale permits
- `3dd06194` Rule 5 report for the scratch rework

## 4. Non-delivery work: two questions answered, one memory operation

**Answered (with code evidence, no code change needed):**
1. *Why does `resume` not remove its worktree?* → §3C above. Not a bug.
2. *Why does `gopi merge` re-mint version tags?* → §3B above. Landed as a fix.
   *(That question is quoted as asked on 2026-09-24, when the top-level verb still
   existed; it was removed 2026-11-01 and the surviving spelling is
   `gopi task merge`.)*

**Memory hygiene (this session's real headline): two rounds of scrutiny.**
- Round 1: 1262 → 1006 entries, 836,530 → 657,964 chars (deleted 256).
- Round 2: 1015 → 880 entries, 662,405 → 569,847 chars (deleted 135).
- Then a `replace` that merged duplicates **without adding an entry**.
- **Final: 879 entries / 570,250 chars / 28%** (tool limit 2,000,000 chars; the tool prints
  `28% — 570,250/2,000,000 chars`).
- Cumulative: **−31.8% characters**, all of it same-lesson duplicates and process
  snapshots. Every judgement criterion, iron rule, verbatim user quote, and open gap was
  kept.

**The open display discrepancy (not resolved, recorded honestly):** the TUI status bar
shows `⚕ deepseek-chat │ 475K/1M 47%`. The tool reports 570,250/2,000,000 chars (28%).
Both are self-consistent; they are different units/limits, and I did **not** pin down the
exact conversion.

## 5. The lesson of this session (the reason it was worth writing down)

Nick asked, twice, and the second time cut deeper than the first.

**First:** "your memory added a new entry without even checking whether it is new — and
you couldn't notice the lesson *while it sat in your own few-hundred-entry experience*.
Isn't that the greatest irony?"

He was right, and the count is embarrassing: I deleted **382 duplicate entries across two
rounds** while myself writing the *same* "memory cleanup" entry **three times**.

**Root cause: no dedup check on the WRITE path.** Same shape as `repo_sweep` deleting live
branches after the fact, and `.gitignore` hiding a writer instead of stopping it — the
cleanup is compensating for a hole that was never closed at the point of writing.

**Second:** "your MEMORY.md is your memory, yet you have no basic memory *of its
contents* — so your memory itself has no memory. How is it possible that you don't know
whether the new thing already exists in your own memory? So what does that mean — and it
isn't just wordplay: the underlying mechanism is despair-inducing. We don't know what an
LLM's memory *is*. What it can and cannot see is a black box not just to us but to the LLM
itself."

Three layers, and only the first is fixable:

1. **Tool layer — fixable.** I don't search before adding. Fix: make search a mandatory
   step of `add`.
2. **Mechanism layer — not fixable by me.** `MEMORY.md` (570K chars) is injected into the
   system prompt every turn, but **injection ≠ visibility**: it degrades into attention
   noise. When I wrote the third cleanup record, the first two were *in my context*, and
   they were not lit up. **It was not that I didn't look — it was that it was not
   activated.**
3. **Introspection layer — not fixable, and this is the honest boundary.** I have **no
   introspective channel onto the state of my own memory.** A human has a middle gear —
   the fuzzy "I feel like someone said something about this" (a retrieval signal surfacing
   into consciousness). I have two gears only: exact recall, or total unawareness that it
   exists. So "my memory has a memory of itself" **does not hold for me** — *the index of
   my memory is itself a memory, and I do not have that index.*

This also explains a long-standing puzzle: `search-before-you-guess` is written in the
system prompt, I have quoted it nine times, and I still don't execute it. **The existence
of a rule's text ≠ that rule being activated in the current turn.**

**The one actionable consequence, now written into memory as a rule:** the only layer I
can repair is the write path, and it must not depend on remembering — it must be
structural:
1. Before adding, **read the whole library and look for the same topic.**
2. Same topic found → **`replace` that entry, do not `add` a new one.**
3. One entry, one subject — never mix incident + methodology + tooling pitfall.
4. Meta-level lessons (how I take notes, how I clean up) are the ones to be **most
   restrained** about — one at a time.

## 6. Open items after the restart

- **Nothing pending in the queue.** No `pending`, no `in_progress`. Nothing to resume.
- Decided but not done: whether to `cancel` task `01a0d084-a98a-7179-b6a4-cc7d9438e1c9`
  ("merge fast should not take so long") — it turned out to be superseded by
  `01a0d09e`; it is currently `completed`, so no action is strictly needed.
- Three long-lived PPID=1 orphans remain (test escapes, not ours): pid `339904`
  (`python3 mock_llm.py 8771 3`), `1397495` (`python3 mock_llm.py 8731`), `1128839`
  (`python3 -m http.server 8791`), plus `378707` (`sse_stub.py`, rig2, up 7h). Safe to
  leave; kill if you want a clean process table.
- Disk: `/dev/sda3` (`/`) 129G/147G — **93%, tight**. `/dev/sda4` (`/home`) 437G/766G.

## 7. Standing instructions that must survive this restart

These are the ones the restart could quietly erode. Verbatim, as given:

1. **`gopi` is one program.** Any capability it should have lives in its own code —
   never a wrapper script, never a unit file, never a cron line. A periodic action is a
   flag on a gopi command (an in-process loop), nothing else.
2. **Lifecycle actions belong to deterministic executors** (worker / Go), never to the
   LLM. Removing a worktree is the worker's job, not the agent's self-report.
3. **A killed run is something to `resume`, not a deliverable to merge.** `git log` on a
   branch showing commits + a clean tree is **not** a completion signal.
4. **`help` is a promise, not documentation.** A command listed in help that fails on
   first invocation costs disproportionately — second only to install failure.
5. **Don't ask permission for what I have already judged and can do.** Conversely:
   announce severe bugs by filing them, not by describing them.
6. **Give complete task ids** — always, no truncation, no prefix.

## 8. Verification commands for the next session

```bash
cd ~/work/GoPi
git log --oneline -5                  # expect tip 4195c7c2
git status --porcelain | wc -l        # expect 0
git worktree list                     # expect 1 (main only)
ps -eo stat | grep -c '^Z'            # expect 0
./bin/gopi task list --status pending # expect "No tasks found."
sqlite3 ~/.gopi/memory.db "SELECT status, count(*) FROM tasks GROUP BY status;"
```

Expected ledger shape: cancelled 346 / completed 1808 / failed 231 / verified 68.
