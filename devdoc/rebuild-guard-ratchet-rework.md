<!-- doc-class: normative -->
# The rebuild-guard merge vs the stylegate ratchet — what was actually red

Task: ledger row `01a092fc-1faa-7a2e-8bc7-00a4c360dd44` (the untracked-file
rebuild guard), reworked after its merge was reported to fail the project's own
stylegate gate.

## 1. The claim, and what measurement says

The briefing said: the merged fix fails `go test ./internal/stylegate -run
TestStyleGate` because the fix's own new test file discards an error at
`cmd/agent/cli_tasks_rebuild_guard_test.go:119`.

Both halves were checked on a real worktree, not argued:

| tree | rev | `go test ./internal/stylegate -run TestStyleGate` |
|---|---|---|
| pre-merge control (`fd92cc27^1`) | `7f56c049` | **PASS** |
| merged fix (`fd92cc27`) | `fd92cc27` | not run as the tip (superseded) |
| current master | `17ac5731` | **FAIL** |

and, with the scanner's own CLI as a deterministic instrument
(`go run ./internal/stylegate/cmd/stylegate -list`):

| tree | findings | ignored-error |
|---|---|---|
| control `7f56c049` | 189 | 182 (= ceiling) |
| master `17ac5731` | 227 | 199 |
| the fix's merge tip `fd92cc27` | 190 | 183 |

**The fix contributed exactly ONE site** (`cli_tasks_rebuild_guard_test.go:119`);
the other **47** net-new sites arrived via merges that landed *after* it. The
ratchet's numbers were never wrong — the sites were unreasoned. So "fix the
site, never raise the ceiling" is the whole fix, applied to the whole merged
state rather than to the one file the briefing named.

## 2. Why the merged tree can be 47 sites red without anyone seeing it

`gateCeilings[ignored-error][test]` was ratcheted **211 → 182** by `0dde9423`
("master's testutil refactor deleted 29 annotated sites"). Every later branch
that added an unreasoned `_ = f()` discard *locally* pushed the count back over
the ceiling — its own `go test ./internal/stylegate` would have said so — but
the branch was merged, not gated. This is the decay the gate exists to catch,
caught one merge too late: the failure is attributed to the branch that happens
to be the merge tip, not to the five branches that produced the sites.

## 3. The change

Every one of the 48 net-new sites gets the rule's **documented inline escape**
with a reason that names the mechanism (`//stylegate:ignore <why>` for the 40
that need it, `//stylegate:allow-sleep <why>` for the 10 sleeps; 8 unchecked
closes of `resp.Body`/pipes in one file share the same mechanism).

`internal/stylegate/gate_test.go` — the ceiling table — is **untouched**. That
is the deliverable's shape: the ratchet's declared numbers are met, not moved.

Two measurement corrections fell out of doing it:

- `internal/testutil/profileguard.go:97` is an `ignored-error` *production*
  site under this scanner's type information (the merge-time report conflated it
  with the 199 test count). Annotated like the rest.
- after the sweep, `ignored-error[test]` = **171 vs ceiling 182**, so the
  ratchet has headroom again and the next unreasoned discard still fails.

### Proof the change is comment-only

Zero behavioural risk was required (this is test-fixture code on a merged
state), so the proof is structural, not "it looks like comments": for **every
changed file**, the `go/scanner` token stream with comments skipped is
byte-identical to `HEAD`. A single non-comment byte would show up as a token
diff. All 16 files: identical.

## 4. Pitfalls found (worth reusing)

- **`//stylegate:allow-poll` is not a gate marker.** Three sites in the wild
  (and in the fix's own `cli_tasks_dead_holder_accept_test.go:240`) carry it;
  `RuleTestSleep` accepts only `//stylegate:allow-sleep`. A swept rule's escape
  spelling is the scanner's constant (`stylegate.AllowMarker`), not a plausible
  English variant — the site stays red until the exact spelling is used.
- **`test-skip` reads the literal, not the comment.** `checkSkip` accepts a
  reason comment only when the first argument is *not* a literal; a
  multi-line-concatenated literal (`t.Skipf("a"+"b")`) is reported before any
  comment is considered. A `//stylegate:ignore` line above it does nothing.
- **Reason spans**: `maxReasonCommentLines = 4`, so an annotation may be spread
  over up to 4 lines above the site; `commentSafe` decides whether the marker
  rides the statement's line or a line above it.
- **gofmt owns the alignment** of an annotated run of `_ = x()` lines — annotate
  first, `gofmt -w` second, then re-scan.

## 5. Method that made the diagnosis cheap

`internal/stylegate/cmd/stylegate -list` prints `file:line: [rule] detail` for
every finding with no `go test` wrapper; diffing two trees' lists (`comm -13`)
yields the **exact net-new site set**. That is the tool to use before believing
"this branch's change broke the gate".

## 6. Round 2: master moved under the merged state (and that was the real red)

The reproduction above was against master `17ac5731` | `4b156bd2`. Mid-task master
advanced to `449fda3a` — whose own `daa930a8` adds
`internal/tools/merge_contract_gate.go:230` (`stdin.Close()` in the batch
reader's `stop` func), a **`unchecked-close` production site** against a ceiling
of 0. On the re-merged state that one site is the *only* failing rule: the
branch's contribution to the gate is unchanged, master's new commit is not.

The lesson generalizes the one from §2: **a merged state is red against a moving
master for reasons that belong to neither side's branch tip.** Re-run
`TestStyleGate` after every master merge; the answer changes when master does.

Annotated in the same shape (documented inline escape, mechanism-naming reason,
comment-only — tokenproof identical), ceiling table still byte-identical to
master's. Rerun after the master merge: `go build ./...` OK, `go test
./internal/stylegate -run TestStyleGate` **ok 3.107s**, counts `errorf-verb 7 /
ignored-error 171 / TOTAL 178`.

## 7. Round 3: master moved AGAIN (worker --daemon) — and the shape held

Master advanced `449fda3a → 23e6da05` (`feat/worker-daemon-flag`, `9ab8cf17`).
Re-merged (`005723b5`); the merge had **one** conflict —
`cmd/agent/worktree_release_accept_test.go`, where *both* sides had annotated the
same per-call env flip. Resolution took master's form, because it carries both
the `//stylegate:ignore` reason and the `// env-leak-ok:` marker the Rule-17
standards gate reads (my round-1 form had only the first two lines of it) —
i.e. the conflict was between two spellings of the same decision.

Post-merge gate: `go build ./...` OK, `go test ./internal/stylegate -run
TestStyleGate` **ok 21.489s**, counts still `errorf-verb 7 / ignored-error 171 /
TOTAL 178` against ceilings `9 / 182`; `gofmt -l` clean on every changed file.

**The reusable rule, now twice confirmed:** merge master *before* measuring, then
measure the merged state; a branch whose own diff is green can still show a red
merged state, and the red belongs to master's new commits. Re-annotate *in the
merged state*, never in the branch tip — otherwise the fix is invisible to the
gate that runs on the merge.

## 8. Round 4: the final tip, and the suite differential that names master's reds

Re-measured at the branch's current tip `06a96129` (contains all of master
`8380ce9c`): `git status` clean, `go build ./...` OK, `go test
./internal/stylegate -run TestStyleGate` **ok**, `stylegate -list` → `errorf-verb
7 / ignored-error 171 / TOTAL 178` (ceilings `9 / 182`), **zero** net-new sites
against master, `git diff master -- internal/stylegate/` empty, `gofmt` clean,
tokenproof identical.

The full-suite differential (control = master without the branch, merged = the
branch's merged state, same env sanitisation, run overlapping on a host whose load
average was ~50 from other agents' suites) says:

```
control failing: cmd/agent  internal/stylegate  internal/testutil  internal/tools
merged  failing: cmd/agent                     internal/testutil  internal/tools
```

`TestStyleGate` is the **only** difference — every other red appears on both
trees. Two of them (`cmd/agent`, `internal/tools`) are the suite's own 10-minute
timeout under host load, not assertions; `internal/testutil`'s
`TestWorkerOwnershipPointerPresent` flags an **identical 22-file doc set on both
trees**, so the branch's report/devdoc add nothing to it. No test fails on the
merged state that does not also fail on master.

## 9. Not done / handed over


- 34 of the 47 foreign sites are other branches' unreasoned discards; they are
  annotated here (so the merged state is green) but the *debt* is theirs. A
  follow-up should file "[stylegate] ratchet headroom is 11 — the ceiling must
  be tightened after the next sweep", because a permanently-loose ceiling is the
  same silent permission the gate removed.
- `go test ./...` on the merged state was differenced against the control tree
  in the Rule-5 report; the pre-existing reds (ledger-DB/env-dependent
  `internal/agent` auto-task registration) are named there with their control
  counterparts rather than assumed.
