<!-- doc-class: normative -->
# Rule 31's checked face: the `Ran:` / `Result:` presence check

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Status:** implemented on `feat/rule31-ran-check` (worktree
`~/workspace/gopi-rule31ran`), commit at the bottom.
**Epic:** make Rule 31's *"a command you did not run is not delivered"* a
CHECKED property — extend `internal/taskreport/` (no new package, tool or verb).

---

## 1. The measured case

> **Retired mechanism (2026-11-05).** The cross-node sync family this page
> describes — `gopi task sync`, `diff`, `harvest`, `push`, `send`, `receive`,
> `pull`, `sync-pass` — is DELETED. No verb in this binary opens a session
> against another node's ledger; each of those spellings still dispatches to a
> removal sentence, and work that must run on another host is FILED there with
> `gopi task submit`. The text below is the record of the mechanism as it was,
> kept verbatim because it is the evidence the removal is judged against.

`gopi task sync --push-only --task-id T` documented, in its own help text, that
*"with `--task-id T` exactly that row travels"*. It compiled, the suite was
green, and it was reported done. **One real invocation** found it broken twice
over:

```
$ ./bin/gopi task sync --push-only --task-id 01a0c2b1-...
gopi task sync: memsync push: no sync_peers declared

$ GOPI_SYNC_PEERS=hp ./bin/gopi task sync --push-only --task-id 01a0c2b1-...
push hp: unreachable: ssh hp: exit status 1
  (stderr: gopi task sync: claim-remote: want 1..8 row(s), got 2181)
```

Its report carried the pre-Rule-31 shape — `Build:` / `Tests:` only. Nothing in
it could have asked the question, so the operator asked it instead and filed two
bug tickets. Rule 31 (written the same day) states the standard; Rule 5's
skeleton gained the `Ran:` / `Result:` lines. **A rule is a sentence, and a
sentence is not a check** (原则无效，检查才有效).

## 2. What was built

`internal/taskreport/runcheck.go` — a leaf, stdlib-only, pure-text predicate
(Rule 14: the writer and the reader import ONE definition).

```
RunCheckVerdict(body string) RunCheck   // {Section, RanLine, Blocker}
RunCheck.Missing() bool                 // the ONE predicate
RunCheckRecord(RunCheck) string         // the digest fragment ("ran-line-missing: ")
RunCheckWarning(taskID, RunCheck) string// the operator line
```

**Three clauses, each decided once, with the failing direction chosen:**

1. **What is adjudicated** — the run's `## Verification` section, its heading
   read from `VerificationHeading()` (never re-spelled: Rules 1b/13). *No such
   section → no-op*: a missing section is `MissingSections`' finding, and
   re-deciding it here would double-report one defect (and would let a report
   dodge this check by deleting the section).
2. **What satisfies it** — a `Ran:` **token line** with a non-empty value. The
   token must sit at the start of the line (after indentation and ONE list
   marker) — the same line-prefix semantics `MissingSections` already uses. So
   `- Ran: ./bin/gopi task sync …` satisfies it, the word inside prose ("I did
   not run this") does not, and **an empty `Ran:` does not** (a hole is not a
   measurement; letting it count would be the same silent hole in a new
   costume). The skeleton's own declared no-invocation marker is recognised and
   read as *absent*.
3. **What exempts it (the honesty clause)** — a stated limit, in either accepted
   form: the declared `Blocked:` token (anywhere in the body — the exemption
   must not be lost because the author stated the limit beside the section it
   belongs to), or a `## Not Done` content line that is not a content-free
   placeholder (`nothing` / `(none)` / `n/a`, or the harness skeleton's own
   `(not recorded…)` hole, which is a hole and not a reason).

**The limit of the check, stated so it is not mistaken for more:** it
adjudicates **presence**, never **adequacy**. It cannot tell whether the
invocation on the `Ran:` line was the right one, whether it reached the surface
that changed, or whether its output was acceptable. That is a reviewer's
judgment. This is in `## Not Done` of the task report and here.

## 3. The action: **(A) record**, and the numbers that forced the choice

The epic asked for ONE action, justified, with option (B) — refuse the
writeback with an env escape hatch — allowed **only if its false-positive rate
over real historical reports could be driven to zero**.

**It cannot. Measured, not argued** (throwaway harness over the repo's own
reports; the predicate is `RunCheckVerdict`, unchanged — only the honesty clause
was varied):

| honesty clause | reports with a `## Verification` section that FIRE |
|---|---|
| **V1 — lenient (SHIPPED):** any non-placeholder `## Not Done` content, or `Blocked:` | **144 / 899 = 16.0 %** |
| V2 — strict vocabulary (could not / needs a / unavailable / skipped …) in `## Not Done` | **642 / 899 = 71.4 %** |
| V3 — declared `Blocked:` token only | **898 / 899 = 99.9 %** |

Corpus: 1310 `.md` reports under `tasks/` (176) and `devdoc/` (1134); 899 carry
a `## Verification` section, 411 do not (a non-report — correctly untouched).
**Every one of them was authored before Rule 31 existed**, i.e. none could have
carried the line, and 898 of the 899 carry none. A gate that refused on this
predicate would refuse ~99.9 % of this repository's real reports — reports
written by runs that DID run their surfaces. That is a gate that fires on honest
work, gets switched off within a day, and a disabled gate is worse than no gate.

The shipped V1 is deliberately the lenient end of the three, because the failure
this check must never make is flagging an honest report: it fires only when a
report **neither** says a surface was run **nor** states any reason it could not
be. And it is not vacuous — measured on the same corpus: 83.9 % of the
section-bearing reports state a limit (silent, correctly), 16.0 % state nothing
at all (recorded), 1 carries the line.

**Per-directory split (the shape of what fires):**

| corpus | files | with `## Verification` | `Ran:` | exempt | fires |
|---|---|---|---|---|---|
| `tasks/` (per-task reports) | 176 | 149 | 0 | 146 | 3 (**2.0 %**) |
| `devdoc/` (design/audit docs) | 1134 | 750 | 1 | 608 | 141 (**18.8 %**) |

The `tasks/` corpus — the per-task reports Rule 31 actually governs — fires on
3 files, every one of them with an **empty `## Not Done` section** (no reason
offered): `01a0b51d-…-report.md`, `git-merge-harness-research-report.md`,
`review-positional-fixes-independent-confirm.md`. The larger `devdoc/` share is
expected and not a false positive of the same kind: those files are *design and
audit documents* whose `## Verification` section states what was reviewed, not
what was executed, and they carry no `## Not Done` at all — the pre-Rule-31
document shape. They are recorded, never refused, which is exactly what the
record-and-warn action buys.

**Wiring (the seal, `internal/tools/task_report.go`):**

- **Record** — `taskReportRanCheckRecord` appends
  `ran-line-missing: ## Verification no `Ran:` line — …` to the run's
  append-only result digest (`writebackOutcome` → the terminal writeback), and
  prints the finding on **stderr** (`[task] ran-check <id> — under ## Verification: …`).
- **Never refuse**, **never rewrite** — the seal returns success and the report
  file is left byte-identical (the `gateclaim.go` invariant, reused rather than
  duplicated).
- **Read face** — `TaskReportShowBlock` renders `task_report_ran: …` in
  `gopi task show`, beside the authored body: a record an operator never meets
  is half a record.

## 4. Skeleton: an empty `Ran:` is never emitted as a measurement

`sectionPlaceholder` now special-cases the verification heading (by
`VerificationHeading()`, not a re-spelled string) and emits:

```
## Verification
* (not recorded — the model wrote no report; the harness recorded the facts below)
Ran: (no invocation — the harness executed no surface itself)
Result: (no observation)
```

The harness runs nothing, so it has no invocation to state. Emitting a bare
`Ran:` would hand every reader a line that *looks* like Rule 31's measurement
while measuring nothing (and the check would have to treat it as absent anyway) —
so the skeleton **names the tokens** (a model reading it learns what the contract
wants, which is the point of a skeleton) **behind an explicit marker**, and the
check reads that marker as absent. `TestSkeletonDoesNotSatisfyTheRanCheck` and
`TestSealSkeletonDeclaresNoInvocationInsteadOfAnEmptyRanLine` pin both halves.

> Note for reviewers: `FormatFact` renders `key: value`, so composing the token
> line through it produced `Ran:: value` — found by pointing the check at the
> skeleton it must not accept. The tokens are now composed by their own
> `tokenLine(token, value)` helper.

## 5. Files

| file | change |
|---|---|
| `internal/taskreport/runcheck.go` | **new** — the predicate, its constants, the digest/warning renderers |
| `internal/taskreport/runcheck_test.go` | **new** — red/green, the honesty case (both forms), the prose false-positive, the empty-line case, the empty-heading no-op, the skeleton |
| `internal/taskreport/taskreport.go` | `purposeNotDone` + `NotDoneHeading()` (read from the one section list, Rules 1b/13); `sectionPlaceholder` + `tokenLine` for the verification placeholder |
| `internal/tools/task_report.go` | seal-side record (`taskReportRanCheckRecord`, digest + stderr) and the read face (`TaskShowReportRanLabel`) |
| `internal/tools/task_report_rancheck_test.go` | **new** — the seam's red/green, the no-refusal assertion, the show face, the skeleton, the self-check on the sealed bytes |
| `cmd/agent/cli_tasks_rancheck_seam_test.go` | **new** — the E2E: a real `gopi worker` run through the production close-out, both arms |

## 6. Verification

- **Build:** `go build ./...` — OK (exit 0).
- **Tests:** `go test ./internal/taskreport/ -count=1` — OK;
  `go test ./internal/tools/ -run 'TaskReport|Seal|RanCheck|Skeleton|Missing' -count=1 -v` — **ok, exit 0** (21 tests, all PASS, incl. the pre-existing report-seam tests);
  the E2E pair in `cmd/agent` — see `Ran:` below.
- **Ran:** `./bin/gopi worker t-ran-seam-red` … (exact invocation and output in the task report's own `## Verification`, which is where Rule 5 puts it).
- **gofmt:** `gofmt -l` on the touched files: clean (`make fmt` deliberately not run).
- **Worktree:** `git status --porcelain` empty at close.

## 7. Rejected alternatives (recorded on purpose)

- **Refusing the writeback (B).** Refused by measurement: 16 % of real reports
  at the lenient clause, 71 % at a strict one, 99.9 % at a token-only one — see
  §3. An escape hatch would be flipped on permanently within a day.
- **Requiring `Result:` too.** The observation belongs to the same statement as
  the invocation; a `Result:`-only report has stated no invocation at all, so
  requiring both adds no discrimination — but `ResultLinePrefix` is still defined
  here so the contract's two tokens have one definition.
- **Gating on the CONTENT of what was run** (was the command the right one, did
  it reach the changed surface). Deliberately not attempted — it is a reviewer's
  judgment; a mechanical check pretending to make it would be lying.
- **A new tool / verb / package.** The epic forbids it and the section contract
  already lives here; `MissingSections` and `RequiredSections` are untouched (no
  required entry dropped, no weakening) so which body is inspected is unchanged.
