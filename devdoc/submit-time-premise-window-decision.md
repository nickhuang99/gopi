<!-- doc-class: normative -->
# The SUBMIT-time premise window (instance #4): decision — no mechanism, and the measured reason is that #4 cost nothing

Row `01a135ff-5f04-7ad8-9196-925d8857afb8` · branch
`task/01a135ff5f047ad89196925d8857afb8` · base `e63a80a5c` · host **peerbox** · worktree
`/home/user/workspace/gopi-01a135ff5f047ad89196925d8857afb8` · evidence
`~/.gopi/sessions/01a135ff-5f04-7ad8-9196-925d8857afb8/artifacts/`

This is a **decision document**, not a fix. The row asked: *which existing seam let
instance #4 through, does a mechanism belong there, and if so ship the smallest one* —
with "no mechanism is justified" named in advance as an acceptable outcome. Both answers
are below, each with the command that produced it (Rule 31). Measurements and inferences
are kept apart (Rule 41 ③); every inference is marked `⇒ INFERENCE`.

## 0. What this document corrects

The epic's framing was **"the first instance whose premise was already FALSE AT SUBMIT
TIME"**. That framing is refuted by measurement (§2): the premise was **true in the tree it
was measured in** — the filer's own branch tip, `ca698ed37`, `23:22:17` — and the repair
landed on master **3 m 34 s later**, on a branch the filer never merged. Instance #4 is not a
new class of window: it is the *same* divergence as #1–#3, with the gap being **the filer's
own bound branch against a moving master**, not a submit-seam blind spot. The prior decision
(`devdoc/claim-to-ground-premise-expiry-decision.md`) is therefore **not** extended here,
because it does not need extending.

The second correction (§4) is about cost: the epic assumed #4 burned a session
re-deriving "already fixed". It did not — the run triaged the reversal correctly within
its first commands, delivered a real guard, and filed no duplicate row.

## 1. The premise-detector chain, run on row 01a135ec's ACTUAL briefing text

Not paraphrased: the row's briefing body was extracted from the ledger and fed to the
running predicates.

```
$ gopi task show 01a135ec-58c4-77c1-bdb2-3e8c8e1eb303 > artifacts/m1-row-01a135ec-show.out
$ awk '/^briefing:$/{f=1;next} f&&/^(result|task_report|claimed_by):/{f=0} f' \
    artifacts/m1-row-01a135ec-show.out > artifacts/m2-01a135ec-briefing-body.txt
      # 1504 bytes
```

A temporary probe test (deleted before close-out; `artifacts/m3-predicate-probe.out`,
`artifacts/m3b-subpredicate-probe.out`) called the REAL `UnlandedClaimsIn` and its real
sub-predicates over that body:

```
PROBE section="" claims=1
PROBE claim[0] commit="01a12e51"
PROBE claim[0] sentence="Found 2026-10-13 during the orphan-branch landing run (task 01a12e51-e2bf-7b7e-8bd1-11ea098ceaee).\n The file is IDENTICAL on master and on my branch — this is NOT introduced by the merge."
```

```
PROBE[1] namesBranch=true assertion=true landedMarker=false FIRES=true commit="01a12e51"
  sent="Found 2026-10-13 during the orphan-branch landing run (task 01a12e51-…).\n The file is
        IDENTICAL on master and on my branch — this is NOT introduced by the merge."
  matched assertion phrase: "not in"
PROBE[4]  namesBranch=false assertion=true  FIRES=false   ← "NOT PERMITTED (drifts, absent from the baseline)"   matched: "absent from"
PROBE[7]  namesBranch=true  assertion=false FIRES=false   ← "git rev-parse master:… == git rev-parse HEAD:…"
PROBE[11] namesBranch=false assertion=true  FIRES=false   ← "The drift is NOT in the baseline table …"           matched: "not in"
```

**Measured, three facts, each independently decisive:**

1. **The predicate fired — on the WRONG sentence.** The single claim the detector found is
   the *incidental* sentence `the file is IDENTICAL on master and on my branch — this is
   NOT introduced by the merge`. It names the branch (`master`, literally), and it carries
   `not in` as a substring of `NOT introduced`. It is not a premise about the integration
   branch at all; it is the filer stating the opposite (this is not my change).

2. **`unlandedClaimCommit` extracted a ROW ID, not a revision**: `01a12e51` — the eight
   hex characters of `01a12e51-e2bf-…`, straight out of the parenthesised row reference
   `(task 01a12e51-e2bf-…-7b7e-8bd1-11ea098ceaee)`. The `(…)` enclosing shape wins over
   the rest of the sentence (`internal/taskreport/unlandedclaim.go`, `unlandedClaimCommit`).

3. **The three sentences that CARRY the false premise are invisible to the predicate, and
   this is a vocabulary boundary, not a bug.**
   - `PROBE[4]`, the recorded doccheck output, carries `absent from` (a listed assertion)
     but **does not name the branch** — `unlandedClaimNamesBranch` is false → no fire.
   - `PROBE[11]`, `The drift is NOT in the baseline table …`, carries `not in` but also
     **names no branch** → no fire.
   - The row's summary sentence (*"absent from the baseline table"*) names no branch
     either. The claim's subject is **the freeze table**, never master.

   `unlandedClaimSentence` requires **branch ∧ assertion ∧ ¬landed-marker**; the row's real
   premise supplies the assertion and never the branch. **There is no revision in the
   briefing to measure** — `absent from the baseline table` is a statement about a *file in
   the tree*, not about ancestry — so even a detector that caught it could not have
   re-measured it with `MasterClaimEvidenceAt` (Rule 8/13: that function's whole subject is
   `git merge-base --is-ancestor <rev> <branch>`).

The dispatch seam then reported exactly that, and its record is in instance #4's own run:

```
$ grep -ao 'briefing premise[^\\]\{0,60\}' ~/.gopi/sessions/01a135ec-…/*.raw.log
briefing premise        ← ×4: the arm lines, one of them
```

and the probe's independent replay of the same call against this repo:

```
PROBE clause=""
PROBE record: briefing premise → UNMEASURED (git rev-parse could not resolve "01a12e51") — the premise was NOT checked, which is not a pass: Found 2026-10-13 during the orphan-branch landing run (task 01a12e51-…).
PROBE evidence(da8d4e72a)={Rev:da8d4e72a AncestorExit:0 ClaimRef:refs/heads/master}
PROBE evidence(01a12e51)={ShortCommit: AncestorExit:-1 Err:git rev-parse could not resolve "01a12e51"}
```

⇒ INFERENCE: the detector's `namesBranch` gate is what keeps it from being *noisy*, and it
is also exactly what makes it blind to this class. Relaxing it (fire on `absent from` /
`not in` alone) would fire on ordinary prose describing a file, a gate, a table, a value —
the false-positive budget of the whole seam. That trade is refused in §3.3.

## 2. The timeline, re-measured — and it is NOT a submit-time window

### 2.1 The two instants the epic named

```
$ git log -1 --format='%h committer=%cd' --date=iso da8d4e72a
da8d4e72a 2026-10-13 23:25:51 +0800
$ git merge-base --is-ancestor da8d4e72a master; echo rc=$?
rc=0                                  # the repair IS on master
$ gopi task show 01a135ec-… | grep created_at
created_at:     2026-10-13 23:40:44   # +14m53s after the repair existed
```

So far the epic's arithmetic is right.

### 2.2 But the TREE the observation was taken in was cut 3m34s BEFORE the repair existed

The filer is task `01a12e51-e2bf-7b7e-8bd1-11ea098ceaee` (named in the row's first line). Its
own branch and its own tree are where the red `doccheck` was observed, so the observation's
revision is that branch's tip — **not** any other task's merge:

```
$ git log -1 --format='%h %cd parents=%p' --date=iso ca698ed37
ca698ed37 2026-10-13 23:22:17 +0800 parents=c136ccc27 352ba4726
$ T=internal/docclass/testdata/anchor-number-baseline.tsv
   c136ccc27   2026-10-13 23:10:26 +0800  table=6ce4373348cf  smokegate-rows=0  has_repair=no
   352ba4726   2026-10-12 12:13:43 +0800  table=3871cbfa89b5  smokegate-rows=0  has_repair=no
   ca698ed37   2026-10-13 23:22:17 +0800  table=6ce4373348cf  smokegate-rows=0  has_repair=no
$ git merge-base --is-ancestor da8d4e72a ca698ed37; echo rc=$?
rc=1                                   # the repair is NOT in the filer's branch, at any point
$ git show ca698ed37:internal/config/config.go | sed -n '413p' -> // "sync_peers"): ssh config aliases this node exchanges
$ git show ca698ed37:internal/config/config.go | sed -n '429p' -> // whitelist): a client node with task_sync=false still h
$ git show ca698ed37:devdoc/DOC-INDEX.tsv | grep -o 'config.go:[0-9]* at // whitelist' | head -1
config.go:413 at // whitelist
```

**Measured:** in the filer's own tree the table carried **zero** rows for
`smokegate-scratchroot-pin-unmerged.md`, the repair was **not an ancestor** of its branch (at
its tip or at either parent), and the anchor cell `config.go:413` pointed at the *wrong* text
while the recorded text sat at `:429`. **Every proposition the row asserts was true in that
tree.** The repair `da8d4e72a` (`23:25:51`) did not exist until **3 m 34 s after** that tree
was last synced, and the filer's branch never contained it.

```
$ gopi task show 01a12e51-e2bf-... | grep -E '^status:|ended_at'
status:         completed
ended_at:       2026-10-13 23:56:34
$ gopi task show 01a135ec-... | grep created_at
created_at:     2026-10-13 23:40:44
```

```
filer branch tip   23:22:17   table 6ce43733, 0 rows, repair absent  => the premise is TRUE here
repair lands       23:25:51   (+3m34s) - on master only; the filer never re-syncs
record filed       23:40:44   (+18m27s after the tip, +14m53s after the repair)
filer ends         23:56:34   - the observer was STILL RUNNING when its observation was filed
```

=> INFERENCE: **the premise was true in the tree it was measured in and false only on a
branch that tree had never merged.** The window is *filer's branch tip -> submit* (18 m 27 s),
covered by a bound master that moved once at 23:25:51. There is no "already false at submit
time" to catch: at every instant the filer could legitimately have looked, the measurement was
correct, and the divergence is the ordinary consequence of a bound branch and a moving
master - the pipeline's normal state, already measured as such.

**Correction of record (Rule 41 3).** A first pass of this section attributed the observation
to the merge `790387280` (`23:24:18`). That commit is **another task's** merge (`into
task/01a135122d067f2db81a3b06f88fec4f`) and is not part of the filer's branch; the attribution
is withdrawn and the figures above are re-measured against the filer's own revs. The
conclusion is unchanged - the filer's tip (`23:22:17`) is 3 m 34 s before the repair, against
the mis-attributed rev's 93 s - and the corrected figure is the weaker one.

### 2.3 The repair's first-parent arrival, for completeness

```
$ git log --first-parent --format='%h %cd %s' --date=iso master | grep -n da8d4e72a
9:da8d4e72a 2026-10-13 23:25:51 +0800 fix(docclass): regenerate the freeze table for the drift master made by itself (frozen=167), …
$ git log --first-parent --format='%h %cd %s' --date=iso --since='2026-10-13 23:20' --until='2026-10-13 23:45' master
0e7b4aaae 23:26:10  docs(docclass): name the second bug row (§7.3), filed with the unattended reproduction
da8d4e72a 23:25:51  fix(docclass): regenerate the freeze table …
790387280 23:24:18  Merge remote-tracking branch 'origin/master' into task/01a135122d067f2db81a3b06f88fec4f
ffd3ba169 23:22:09  docs(docclass): record the two bug rows this run filed …
```

The repair is `+3 m 34 s` past the filer's branch tip (`ca698ed37`, `23:22:17`) and `+14 m 53 s`
before the record was filed. The observation was taken in a tree that was bound 3 m 34 s
earlier, and the filer never re-synced afterwards — so the tree that carried the red stayed
red for the whole session, while master had already been repaired. (`790387280` in the list
above is the arrival of the fix's *predecessor* work and belongs to another task's branch; it
is not the filer's tree — see §2.2's correction note.)

### 2.3b This run's OWN suite hit a pre-existing red at its base — and the §3.2 remedy closed it

Directly on-topic, and measured while writing this document: the branch-scoped suite this run
was told to run is RED at its base, for a reason that has nothing to do with this diff.

```
$ go test ./internal/standards/ -run 'CommandsDoc' -count=1
--- FAIL: TestCommandsDocGeneratedBlockMatchesSource (5.52s)
        first difference:
          got:  line 40: | `gopi ui_state` | argv[0] | `cmd/agent/cli_uistate.go:370` | … |
          want: line 40: | `gopi ui_state` | argv[0] | `cmd/agent/cli_uistate.go:391` | … |
FAIL

$ git rev-parse e63a80a5c:<f> ; git rev-parse HEAD:<f>      # base vs this run's own tip
  COMMANDS.md              identical  9ce6d2ac43f6
  cmd/agent/cli_uistate.go identical  0fea0cc82e5c
  cmd/agent/cli_verbs.go   identical  1aad0f102cbc
$ git diff --name-only e63a80a5c..HEAD
  devdoc/DOC-INDEX.tsv
  devdoc/submit-time-premise-window-decision.md
```

**Measured:** the failing assertion's three inputs are **byte-identical** before and after this
run, and none is in its diff — so the red is pre-existing at the base, and it reproduces at
master (`fea030924`: cell `cli_uistate.go:370` vs source `:391`; the regeneration
`05c2a3a36` *is* an ancestor of master, but `master's COMMANDS.md has never carried :391`,
`git log -S'cli_uistate.go:391' -- COMMANDS.md` → empty).

⇒ INFERENCE: this is the same condition the prior decision's §3.5 measured for **its own**
ground (a `COMMANDS.md` cell stale against its source, "a red base costs one triage"), and the
triage is the same read — `git diff --name-only <base>..HEAD` against the failing test's named
inputs. **The standing remedy worked here too**, which is the fourth independent observation of
it in two days. The live instance is filed as row `01a13627-002c-7b45-8ac3-b1847cad2d39`
(`tags=bug`) per Rule 41, with the five prior rows for this class named in its payload so the
executor can judge duplication itself.

### 2.4 Which of the three existing seams could have seen it — measured, all three, no

| seam | its inputs | could it see instance #4? |
|---|---|---|
| **claim guard** (`internal/tools/task_staleness_guard.go`, via `TaskNextFiltered`) | the row's **`deliverable` JSON only** — `mergeVerifyStaleRowBranch` reads `d["repo"]` and `d["branch"]`, both of which came back empty for this row (`deliverable.repo` and `.branch` are set for this row, but the guard's entry predicate is `TaskIsMergeVerifyReworkRow`) | **No.** Measured below. |
| **dispatch premise refresh** (`TaskBriefingPremiseRefresh`, `cmd/agent/cli_tasks.go:2791`) | the briefing **prose**, for sentences asserting the integration branch *lacks* a revision | **No** — it RAN, fired on the wrong sentence, extracted a row id, and stated UNMEASURED (§1). |
| **submit** (`TaskSubmit`) | the payload text + the repo path + this process's hostname | **No.** Nothing in the submit path reads a revision out of the payload. Measured below. |

The claim guard's scope, verbatim from its own file:

```
$ grep -c 'briefing' internal/tools/task_staleness_guard.go
13                      # every hit is PROSE — the string never appears as a read of the row's text
$ sed -n '820,832p' internal/tools/task_staleness_guard.go
func mergeVerifyStaleRowBranch(t *Task) (repo, branch string) {
	if t == nil || strings.TrimSpace(t.Deliverable) == "" { return "", "" }
	var d map[string]string
	json.Unmarshal([]byte(t.Deliverable), &d)
	return strings.TrimSpace(d["repo"]), strings.TrimSpace(d[mergeVerifyReworkBranchKey])
}
```

**The claim guard never reads a briefing.** It judges the merge-verify rework lane from the
row's *stamp*, and its three git facts are about a **branch ref**, not a sentence. A
docclass bug row with a repo and no rework branch is outside its entry predicate by
construction.

The submit path's existing warnings (the only two, both non-refusing):

```
$ grep -n 'Warning\|warn' internal/tools/tasks.go | grep -in 'submit\|hermes\|attachment'
6687:  out += "\n" + TaskSubmitAttachmentClaimWarningText(phrase)
6698:  if w := TaskHermesHostWarning(repo); w != "" {
```

⇒ INFERENCE: there is **no third seam to extend**, and the two that exist are structurally
incapable of this class — one reads a stamp, the other reads prose for an ancestry claim.

## 3. QUESTION — does a mechanism belong at the submit seam?

### DECISION

**No mechanism. No new warning, no vocabulary widening, no `premise_commit` key.** Three
independent reasons, in order of weight.

### 3.1 The epic's premise is false, so the mechanism would serve a defect nobody measured

The window the epic asked to close ("already false at submit time") **does not exist** in
this instance (§2.2). What exists is: *an observation 93 s old, carried into a submit 16 min
later, by a session that merged master once at 23:24:18 and re-measured nothing*. A
mechanism placed at submit time would be answering a question the data does not ask.

### 3.2 More decisively: instance #4 cost nothing, and its run closed a real gap

The epic's cost model ("costs a full agent session re-deriving a measurement that says
'already fixed'") is refuted by instance #4's own record. Its run:

```
$ git log --oneline master..task/01a135ec58c477c1bdb23e8c8e1eb303
42381f6ce test(docclass): pin the anchor-NUMBER census GENERATOR's face on the real tree
```

and its report's own `## Completed` opens with the reversal, correctly:

> "**Established the briefing's premise is stale, by measurement, both halves.** … the row is
> present at `internal/docclass/testdata/anchor-number-baseline.tsv:152` as `number-drifted` …
> repaired by commit `da8d4e72a` … which is on master."

> "**Closed the one real gap this run found**: the census GENERATOR's face of the freeze
> question had no real-tree test, while its sibling rung already had one for its own
> generator. Added `internal/docclass/anchor_number_shipped_face_test.go` — the twin guard,
> with its own red half."

and it **filed no duplicate row**, giving its reason:

> "I filed no fifth row: `01a135ff`'s summary **already names this task id**, so the bug-flow
> entry for this report exists and this run's contribution is evidence, not a duplicate."

⇒ INFERENCE: the churn this decision must weigh is **not** a wasted session — it is one
triage at the top of a run, which is exactly the disposition the prior decision prescribed
(§2 of `claim-to-ground-premise-expiry-decision.md`: *"the preflight belongs on the
EXECUTOR … exit 0 ⇒ the ask is already delivered ⇒ report that, never manufacture a diff"*).
**The executor did it.** The prescribed remedy was observed working, on the very instance
that was supposed to prove it insufficient.

### 3.3 The cheap form is contract-compatible but has no teeth; the effective form is refused

- A **warning** at submit (Rule 40's precedent) is contract-compatible — but it can only warn
  about a premise the submitter *already measured correctly*. §2.2's measurement was true
  when taken; a submit-time warning saying "master moved since your measurement" fires on
  **every** row submitted from a long session, and the prior decision already measured that
  condition as the pipeline's normal state, not a rare race: *"a per-row comparison at claim
  time fires constantly"* (`claim-to-ground-premise-expiry-decision.md` §2(c), 143
  first-parent commits in the window, 102 of them merges).
- The form with **teeth** (refuse, or auto-close) is refused for the reason the prior
  decision gives and this run re-confirms: the row exists *because* an acceptance command is
  red, so a green acceptance is what **success** looks like — refusing on "the premise no
  longer holds" refuses the honest outcome (§2(d) there, unchanged).
- Widening `unlandedClaimAssertions` to fire without a branch token (§1.3) is the third
  cheap form, and it is refused because `absent from` / `not in` are ordinary prose about
  files, tables, gates and values. The detector's `namesBranch` gate is the thing that keeps
  the seam from stamping honest briefings; the price of removing it is paid by every
  briefing, not by this class.

### 3.4 What this decision obliges, and its honest limit

- **It obliges nothing new on the machinery.** It records that the executor-side preflight
  (already the standing remedy) was observed to work on instance #4, and that the detector
  chain behaved *correctly* given its vocabulary — it reported UNMEASURED and never claimed
  a pass (Rule 15), which is the behaviour the seam was built for.
- **Limit, stated plainly:** this is one instance. §3.2's evidence is a single run's report,
  read at `artifacts/m14-instance4-deliverable.out`; a population of one cannot establish
  that the executor preflight *always* works — only that it is not the failure the epic
  assumed. If a future instance shows a run that **did** manufacture a diff or a duplicate
  row from a #4-shaped briefing, that is new evidence and the decision is re-opened.
- **No gate exists for this and none is proposed.** "The submitter re-measured before
  submitting" is not observable in the general case; what the ledger can show is the
  outcome, and #4's outcome is in §3.2.

## 4. Diffs against the prior decision (`devdoc/claim-to-ground-premise-expiry-decision.md`)

| # | prior decision | this document | why |
|---|---|---|---|
| 1 | "at submit-and-claim time there was nothing to catch" (its §2, limit) | **Confirmed, and now measured for #4 specifically** (§1.3, §2.2) — there was nothing to catch because the premise was TRUE at the observation instant | the prior decision asserted it for #1–#3; #4 is the first instance where the *observation* instant could be located in the object graph, and it vindicates the assertion |
| 2 | the window is **claim→ground** | **Not the only window.** #4's window is **observation→submit** (93 s of tree motion + 16 min of session), a *different* pair of instants | the prior decision's §3.5 already found a third shape (repair 2m25s after the ground's base stamp); this names the fourth: the **filer's own session** is a window |
| 3 | remedy = executor runs the acceptance command first ("a writer discipline with no gate") | **Unchanged, and observed working** on the very instance the epic called uncaught (§3.2) | the epic's cost model was the thing under test; the remedy survived it |
| 4 | No mechanism for the `c37219666` drift class (accepted churn) | **Unchanged.** #4's row reports that same drift class, and the drift itself is not a defect — `number-drifted` means recorded text present, line stale by design | no new population, no new predicate (§3.3) |
| 5 | — | **NEW: the detector's blind spot is named and pinned** (§1.3): a premise whose subject is a *file in the tree* ("absent from the baseline table") rather than an *ancestry* is outside `UnlandedClaimAssertion`'s vocabulary, and no widening is justified | this is the part of #4 that was genuinely not known before |

## 4.5 How to reproduce §1 (the probes were deleted from the tree — Rule 30)

The three probes ran as temporary test files and were removed before close-out; their source
is kept OUTSIDE the worktree, in the evidence directory, so the tree stays clean:

```
$ cp ~/.gopi/sessions/01a135ff-5f04-7ad8-9196-925d8857afb8/artifacts/probe/zz_probe_stale4_taskreport_test.go internal/taskreport/
$ cp ~/.gopi/sessions/01a135ff-5f04-7ad8-9196-925d8857afb8/artifacts/probe/zz_probe_stale4_tools_test.go    internal/tools/
$ PROBE_BRIEFING=…/artifacts/m2-01a135ec-briefing-body.txt \
    go test ./internal/taskreport/ -run TestZZProbeSubPredicates -v
$ PROBE_BRIEFING=…/artifacts/m2-01a135ec-briefing-body.txt PROBE_REPO=/home/user/work/GoPi \
    go test ./internal/tools/ -run TestZZProbePremiseRefreshOnRow01a135ec -v
```

`m2-01a135ec-briefing-body.txt` is the row's briefing verbatim, extracted by the `awk` in §1,
so the reproduction takes no dependency on the ledger row still being readable.

**No regression test is shipped with this document, and that is deliberate**: a decision of
"no mechanism" has no behaviour to pin. The regression pin the epic asked for is only owed
*if* a mechanism is justified (§3), and none is.

## 5. What this document is NOT

- **Not a fix.** No code changed, no constant added, no gate, no script, no new key. Rule 35's
  four questions are answered by *not creating the dependency*.
- **Not a rejection of #4 as a bug row.** The row was filed honestly and its observational
  evidence was correct when taken. What is rejected is the epic's inference from it.
- **Not a claim that stale briefs are harmless.** The cost is real (§3.4 limit) and the
  standing remedy is the executor's first command.

## 6. Evidence index

`~/.gopi/sessions/01a135ff-5f04-7ad8-9196-925d8857afb8/artifacts/`

| file | what it holds |
|---|---|
| `m1-row-01a135ec-show.out` | row 01a135ec's own ledger fields |
| `m2-01a135ec-briefing-body.txt` | the briefing body fed verbatim to the predicates (1504 bytes) |
| `m3-predicate-probe.out` | `UnlandedClaimsIn` on that body: section, one claim, its sentence |
| `m3b-subpredicate-probe.out` | per-sentence `namesBranch` / `assertion` / `landedMarker` / FIRES + the matched phrase for each |
| `m4-timeline-and-tree.out` | repair vs `created_at`; blob identity master ↔ ground |
| `m5-population-submit-vs-repair.out` | the four instances' submit instants vs their repairs' committer dates |
| `m7-filer-tree.out` | the filer (01a12e51) and its recorded ground |
| `m8-master-arrival.out` | `da8d4e72a`'s first-parent position and the master commits around it |
| `m9-table-diff.out` | the merge-tree table blob and its `smokegate` row count |
| `m10-seam-ran.out` | instance #4's own session: the seam's record lines, the tool histogram |
| `m11-seam-record-and-commands.out` | the seam record lines verbatim + the memory record the run itself ranked |
| `m12-executor-commands.out` | the 70 commands instance #4's run actually executed, in order |
| `m13-cost-of-instance4.out` | the run's report head (`## Completed`) |
| `m14-instance4-deliverable.out` | its branch's commits vs master + `## Not Done` + `## Verification` |
| `m15-drift-live-at-observation.out` | §2.2 — the three revs: table blob, smokegate row count, ancestry, and the anchor cell vs the recorded text |
| `probe/zz_probe_stale4_taskreport_test.go` | §1's sentence-level probe source (kept outside the tree) |
| `probe/zz_probe_stale4_tools_test.go` | §1's seam-replay probe source |
| `v1-verify.out` | the build + the branch-scoped suite for the packages this diff touches |
| `v2-red-triage.out` | §2.3b — the diff vs the failing test's named inputs |
| `v3-pre-existing-proof.out` | §2.3b — the three inputs byte-identical base..HEAD + the conclusion |
| `v4-red-provenance.out` | the live cell vs source line at HEAD, and the existing rows for the class |
| `v5-red-at-master.out` | §2.3b — the red measured at master `fea030924`, and the mover chain |
| `bug-commandsdoc-uistate-anchor-drift.md` | the Rule 41 payload filed as row `01a13627-002c` |
