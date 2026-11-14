<!-- doc-class: normative -->
# `push accepted` is now a MEASUREMENT, and the two "unverifiable completed" rows turned out to have DELIVERED

**Row**: `01a1a6a5-093c-7816-85ac-d85d7a38c837` · **ground**: `/home/user/workspace/01a1a6a5-093c-7816-85ac-d85d7a38c837` (CLONE, cut at `30b55d3`) · **measured at**: `30b55d381bfa40271e55dc94ece2ea01c50d5aaa` (+ this run's commit)

This page has two halves, and the SECOND one is the one a reader needs first:
**the epic's premise was wrong** — both rows it accused of an unverifiable
`completed` had in fact delivered — and the defect that remains after that
correction is real, narrower, and now closed by measurement.

---

## 1. The line, its file:line, and its criterion VERBATIM

`clone-channel: attempt N/3: push accepted` is printed in exactly one place:

```
internal/devworkspace/devworkspace.go:1003   (at cut 30b55d3; the arm is the `pushErr == nil` branch of AttemptDelivery's inner loop)
```

```go
		pushOut, pushErr := publishGround(ctx, dir, base)
		if pushErr == nil {
			res.Pushed = true
			res.Detail = fmt.Sprintf("pushed %s to origin after %d attempt(s)", base, attempt)
			res.printf(cfg.Log, "attempt %d/%d: push accepted", attempt, MaxPushAttempts)
			return res, nil, nil
		}
```

**The criterion, in one sentence: the exit status of the push subprocess and
nothing else.** `publishGround` is:

```go
func publishGround(ctx context.Context, dir, base string) (string, error) {
	args := []string{pushVerb, originRemoteName, gitHeadRef + ":" + refsHeadsPrefix + base}
	return runCommand(ctx, dir, gitBinary, args...)
}
```

and `runCommand` returns `cmd.CombinedOutput()`'s error — a *transport* verdict.
Nothing asked the peer what it kept. PROJECT.md Rule 6c reads that printed line
as the evidence of delivery, so the channel was manufacturing the evidence
rather than taking it.

## 2. The refspec, measured

`publishGround` builds **one** argv, and the loop calls it **once per attempt**
with two loop-invariant values (`base` is assigned once, before the loop, at
`devworkspace.go:831`):

```bash
$ sed -n '/^func publishGround/,/^}/p' internal/devworkspace/devworkspace.go
	args := []string{pushVerb, originRemoteName, gitHeadRef + ":" + refsHeadsPrefix + base}
$ grep -n "publishGround(ctx, dir, base)" internal/devworkspace/devworkspace.go
995:		pushOut, pushErr := publishGround(ctx, dir, base)
$ grep -n "base := baseBranchOf" internal/devworkspace/devworkspace.go
831:	base := baseBranchOf(cfg, branch)
```

⇒ **The push is always `git push origin HEAD:refs/heads/master`.** The row's own
address `task/<id>` is deliberately NOT a destination (commit `9ab17237e`); the
destination is the integration branch. A successful push therefore creates **no
`task/*` ref on the server at all** — which is why "`git ls-remote --heads origin`
carries no `task/01a1a647*`" was never evidence of anything (see §3).

## 3. Root cause: the premise, then the defect

### 3a. Both accused rows DELIVERED — measured by content, not by hash

The briefing's evidence was "the hash in the report does not exist here", and
that is true — but a rebase REPLAYS commits, so a replayed commit keeps its tree
and gets a new id. Content is the criterion:

**Row `01a1a647`** — its report claims five files; the commit on `origin/master`
carrying that row's `task:` trailer touches exactly those five:

```bash
$ git log --format='%H %s' --grep='01a1a647-a17b-7195-b9d1-5cd367333550' FETCH_HEAD
7296b3fba6aa3c1c05cb5caa2dec69c8034b9b7f fix(testutil): a suite's ledger is a file of ITS OWN profile, not a sibling of it
$ git show --stat --format='' 7296b3fb
 internal/testutil/dbfixture.go                |  15 +++-
 internal/testutil/ledgerpin.go                | 105 ++++++++++++++++++++++++--
 internal/tools/repo_sweep_report_only_test.go |  26 ++++++-
 internal/tools/taskzombie_test.go             |  12 ++-
 internal/tools/testmain_test.go               |   8 ++
 5 files changed, 157 insertions(+), 9 deletions(-)
```

The report's own list is the same five paths. `git rev-parse origin/master` and
`git rev-parse HEAD` in the main repo are both `7296b3fb` — the row's work IS
the base the briefing measured against.

**Row `01a1a67d`** — its report names `ef6aaee5` (+312/−56 on
`scripts/notify/notify_peer_resume_test.sh`) and `26960c82` (a devdoc). Neither
object exists in this clone (`fatal: Not a valid object name`), and yet:

```bash
$ git show --stat --format='' e99f11df
 scripts/notify/notify_peer_resume_test.sh | 368 +++++++++++++++++++++++++-----
 1 file changed, 312 insertions(+), 56 deletions(-)
$ git log --oneline -1 FETCH_HEAD -- devdoc/notify-peer-pull.md
59d5de44 docs(notify): the resume rig's RED arm now has three sources, and the peer-pull record says which
```

`+312/−56` is **the report's own figure**, and the devdoc commit is there with
that row's trailer. Both rows delivered; both hashes were rewritten by the
delivery loop's own `pull --rebase` — the mechanism Rule 38 already documents
for census citations.

⇒ **"The hash in the report is absent" is not a delivery verdict.** It is the
expected consequence of the rebase, and it cost this epic a wrong premise. The
correct reading is content (`git show --stat`, `patch-id`) or ancestry
(`merge-base --is-ancestor`), never object existence by hash.

### 3b. The four shapes, each measured — and (d) explained it

| shape | claim | measurement | verdict |
|---|---|---|---|
| **(a)** | the retry leg's `git push` exit is swallowed (a `\|\| true`, a discarded error) | `runCommand` → `run` returns `cmd.CombinedOutput()`'s error; `AttemptDelivery` branches on `pushErr == nil`; `TestLoopRetriesWhenThePushIsRefused` shows attempt 1 `REFUSED` then attempt 2 `push accepted`, and `TestBrakeStopsAfterMaxRefusalsAndKeepsTheWorkspace` shows three `REFUSED` with no acceptance | **excluded** — no swallow on the path |
| **(b)** | the retry pushes a DIFFERENT refspec | `publishGround` builds one argv from two loop-invariant values (§2); there is no second destination and no per-attempt branch mutation | **excluded** |
| **(c)** | a `--force-with-lease` whose lease check failed | `grep -rn "force-with-lease\|--force\|\"+\"" --include=*.go internal/devworkspace` returns **nothing**; the refspec carries no `+` | **excluded** |
| **(d)** | `HEAD`/the hash moved — a rebase replay — while the push itself was ordinary | both cases above: the pushed commit is the row's content under a new id | **ACCOUNTED FOR**, and it explains the *premise*, not a lie |

⇒ **The retry leg was never the liar.** Attempt 2 did exactly what attempt 1
did, with the same refspec, and the push really was accepted — by a peer that
kept the commit under a **rebased** hash. The report's `2c29ec94` was a
**pre-rebase** id that no longer names anything.

⇒ **But the defect the epic asked about is real and independent of all four
shapes**: nothing in the acceptance path proved the peer *kept* anything. A push
git calls successful while the receiving side discards the ref (a `post-receive`
hook that restores, a mirror that drops it) would have printed `push accepted`,
set `Pushed = true`, and closed the row `completed` — with Rule 6c pointing at
that line as its proof. Shape (a)–(d) are answers about *this* pair of rows; none
of them closes that hole.

## 4. The fix, and where the check belongs

`pushLandedOnPeer` (`internal/devworkspace/devworkspace.go`, immediately below
`publishGround`) is now the ONLY door to `res.Pushed = true`:

```go
			if landed, why := pushLandedOnPeer(ctx, dir, base); !landed {
				res.printf(cfg.Log, "attempt %d/%d: push reported success but the peer does not carry it — %s",
					attempt, MaxPushAttempts, why)
				return res, nil, fmt.Errorf(CloneChannelPrefix+"push reported success on attempt %d and the peer does not carry the commit (%s) (output: %s)",
					attempt, why, tail(pushOut))
			}
```

It reads the peer with the SAME primitive the empty gate already trusts
(`deliveryPeerOperand` + `deliveryPeerTipFrom`, i.e. `git ls-remote` of the ref
this push addressed — `base`, never the ground branch), then:

1. peer tip == our HEAD → landed (the ordinary case);
2. peer tip is a descendant — a concurrent push landed on top between the push
   and the read → **landed** (the race must not read as a lost delivery); it
   needs one `fetch` of that tip into `FETCH_HEAD` only, because the peer's
   commit is normally an object this store has never seen;
3. anything else → **an ERROR naming both sides**, never a blocker (retrying the
   inner loop repeats the same transport, and the brake's "the remote kept moving
   first" would be a story about a race nobody ran).

**Why the leg and not the END hook** — the answer to deliverable ⑤:
`Reclaim` deletes the ground the moment the delivery returns (`cmd/agent`'s
`finish()`), so *after* the loop there is no local HEAD left to compare. A
close-out check could only ask "does some commit with this row's trailer exist
on the peer?", which is a different and weaker question (it cannot tell this
attempt's push from an earlier one). The leg is the last moment both sides exist.

**False positives in the normal case**: none measured. The two green arms are the
ordinary delivery (`TestPushAcceptedStillDeliversOnAnOrdinaryPush`) and the
concurrent-push race (`TestPushLandedToleratesAPeerThatMovedOnTopOfOurPush`);
the whole package (103 arms) stays green, including every e2e that ends in a real
push. An unmeasured answer (no `origin`, a peer that does not answer, an
unreadable HEAD) is reported as FAILURE with its own sentence — deliberate: the
one thing this function may never do is let "I could not look" read as "landed".

## 5. Evidence

```
Build:  go build ./...                                        → exit 0
Tests:  make test-budget PKGS='./internal/devworkspace'
        → stamp: 2026-11-04T21:24:51+08:00 host=host-b pkg=./internal/devworkspace
          budget=45m elapsed=25s load_start=8.74 10.65 12.37 load_end=9.90 10.74 12.36
          cores=32 exit=0 outcome=pass stream=run-1793798691-2489991.json
        → (floor line) github.com/nick/gopi/internal/devworkspace: 103 finished arm(s)
Ran:    go test ./internal/devworkspace/ -run 'TestPushAccept|TestPushLanded' -count=1 -v
Result: === RUN   TestPushAcceptedRequiresThePeerToCarryTheCommit
        --- PASS: TestPushAcceptedRequiresThePeerToCarryTheCommit (0.08s)
        === RUN   TestPushAcceptedStillDeliversOnAnOrdinaryPush
        --- PASS: TestPushAcceptedStillDeliversOnAnOrdinaryPush (0.07s)
        === RUN   TestPushLandedToleratesAPeerThatMovedOnTopOfOurPush
        --- PASS: TestPushLandedToleratesAPeerThatMovedOnTopOfOurPush (0.07s)
        === RUN   TestDeliveryDoesNotPrintPushAcceptedWhenThePeerKeptNothing
        --- PASS: TestDeliveryDoesNotPrintPushAcceptedWhenThePeerKeptNothing (0.09s)
```

**The RED proof** — the loop arm is the one that bites, because it drives
`AttemptDelivery` rather than the gate:

```bash
# the mutation: the gate bypassed in the acceptance arm only
-			if landed, why := pushLandedOnPeer(ctx, dir, base); !landed {
+			if landed, why := true, ""; !landed {
$ go test ./internal/devworkspace/ -run 'TestDeliveryDoesNotPrintPushAccepted' -count=1 -v
=== RUN   TestDeliveryDoesNotPrintPushAcceptedWhenThePeerKeptNothing
    push_landed_peer_test.go:195: the channel printed "clone-channel: attempt 1/3: push accepted"
    although the peer never kept the commit; its whole log:
--- FAIL: TestDeliveryDoesNotPrintPushAcceptedWhenThePeerKeptNothing (0.09s)
FAIL
# restore, then verify by CONTENT-IDENTITY against a revision (not an index diff)
$ cp /tmp/dw.good.go internal/devworkspace/devworkspace.go
$ go test ./internal/devworkspace/ -run 'TestDeliveryDoesNotPrintPushAccepted' -count=1 -v
--- PASS: TestDeliveryDoesNotPrintPushAcceptedWhenThePeerKeptNothing (0.09s)
```

The red arm prints **the defect's own sentence verbatim** — that is what makes
the proof non-vacuous. Note the first attempt at this test was a red herring: it
called `pushLandedOnPeer` directly, so bypassing the loop's call left it green.
A test that does not exercise the CALL SITE cannot prove the call site.

The fixture is a real repository, not a mock: `post-receive` in the origin's own
`.git/hooks/` reads `$old` and puts the ref back, so git's push **exits 0** and
the peer keeps nothing — the shape a mock cannot produce.

## 6. The operator-visible chain, driven for real

The leg's ERROR is what the row and the operator's stream become — measured by
driving the PRODUCTION chain (`a.finishSessionTask(...)` with the real error text
the arm above produced, then reading the ledger back):

```
OPERATOR_STREAM: task ledger: registered 01a1a6bd-97db-73e5-ac0e-14a9361b95c2 (in_progress, auto, session 9500af61-c813-4968-a7fb-0abfadefefb2)
ROW: status:         failed
ROW: reason:         clone channel: delivery did not land — push reported success on attempt 1 and the peer does
                     not carry the commit (the peer's refs/heads/master is at 79118a2a17c386baf84b683ed2bbb2b1968320e9,
                     and the 4d464dd42f3a661062f5e0feb7c4cbedc903dff8 this attempt pushed is not in its history)
                     (output: To /dev/shm/suite-devworkspace-run-441256931/TestDeliveryDoesNotPrintPushAcceptedWhenThePeerKeptNothing2133235040/001 |    79118a2..4d464dd  HEAD -> master)
```

⇒ The row lands `failed` (not `completed`), its reason is LINE-ANCHORED at
`clone channel: delivery did not land — `, and the refusal names **both** commits.
`push accepted` does not appear in the stream at all for a refused delivery. The
row's own output line is git's, quoted verbatim — including the `79118a2..4d464dd
HEAD -> master` that git itself printed on a push the peer then discarded, which
is the whole reason an exit code could not carry this answer.
