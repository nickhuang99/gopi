<!-- doc-class: historical -->
# task report — `session list --json`: the REPL-turn wire key is `repl_turns`, not the bare word `turns`

Task `01a174c5-478d-768a-be95-b4aa783bff3a` · ground `master` (clone-channel) ·
2026-10-26.

## Completed

### 1. The wire key is a constant in the leaf that already owns the unit

`internal/sessionevent/counters.go` — beside `CounterUnitReplTurn`:

| constant | value | role |
|---|---|---|
| `JSONKeyReplTurn` | `repl_turns` | the LIVE wire key of `chats.turn_count` |
| `DeprecatedJSONKeyTurns` | `turns` | the retired key, kept for one migration window |
| `DeprecatedJSONKeyTurnsRemoveAfter` | `2026-12-31` | its stated removal date |
| `JSONKeyDeprecatedTurns` | `deprecated_keys` | the block that carries the alias |
| `DeprecatedTurnsKeyNote` | one sentence | the notice printed on every row |
| `JSONKeyOperatorTurns` / `JSONKeyOperatorTurnsKnown` / `JSONKeyTurnsNote` | `operator_turns` / `operator_turns_known` / `turns_note` | the operator-row count and its stated absence |

The live key is derived from the unit a human already reads:
`JSONKeyReplTurn == strings.ReplaceAll(CounterUnitReplTurn, " ", "_")` — asserted
by the test, so the two faces cannot spell one quantity two ways.

### 2. `sessionJSONRow`: bare `json:"turns"` is gone from the top level

`cmd/agent/cli_session.go`:

- `Turns int \`json:"turns"\`` → **`ReplTurns int \`json:"repl_turns"\``**, with the
  comment block that used to declare the debt closed ("the JSON KEY keeps its
  historic spelling") replaced by one that states what the key is, why it is
  qualified, and that a struct tag cannot hold a constant so a test pins it.
- A new **`DeprecatedKeys deprecatedSessionKeys \`json:"deprecated_keys"\``**
  field carries `{turns, replaced_by, remove_after, note}`. The alias is a NUMBER
  (a script that read `.turns` still gets 18) but it is **nested**, so it can
  never be mistaken for the live field and it states its own replacement and
  deadline on every row.
- **The bare top-level `"turns"` is deliberately NOT published.** Emitting
  `"turns": 18` beside `"repl_turns": 18` would preserve the exact collision this
  task removes — two keys, one number, one of them wearing the operator's word —
  and would make the wire shape depend on which spelling a reader happened to
  pick. The migration survives; the ambiguity does not.
- Both encoders (`jsonSessionRows`, `jsonSessionDetail`) fill the new fields, so
  `session list --json` and `session show --json` cannot diverge.

The `turns` comment block also records why `operator_turns` kept its own key
instead of re-pointing `turns`: **changing a key's MEANING is the silent breakage
Rule 15 forbids**, so the new quantity arrived under a new name.

### 3. Help text, and the man page's new JSON KEYS section

The `-h` page ROUTES; the man page carries the contract — and the repo's own
usage-budget gate enforced that here rather than letting the first draft through:
the first version of `sessionUsage` spelled out every JSON key inline, measured
**89 lines against the 83-line budget for `gopi session`**
(`TestPublishedHelpPagesStayWithinTheirUsageBudget/session`), so the detail moved
to `man/gopi-session.md`, which gained a **`# JSON KEYS`** section (the table of
`repl_turns` / `operator_turns` / their `_known` + `_note` pair, the retired bare
key, and the exact `deprecated_keys` object). The usage line now names the two
keys and routes to that section; `gopi session -h` measures **82 lines**.
`make man` regenerated the synopsis from the usage constant and `make man-check`
is green.

### 4. Test — `cmd/agent/session_json_wire_test.go`

| test | pins |
|---|---|
| `TestSessionJSONReplTurnKeyIsTheConstant` | the struct tag equals `sessionevent.JSONKeyReplTurn` (checked against the CONSTANT, so a tree where both drifted fails), the key is the underscore form of `CounterUnitReplTurn`, and the live key ≠ the deprecated one |
| `TestSessionJSONRowCarriesReplTurnsNotBareTurns` | by MARSHALLING the real encoder: `repl_turns == 18`, **no top-level `turns`**, `deprecated_keys` present with the same value + `replaced_by` + `remove_after` + a note naming the new key, and `operator_turns == 11` — with a guard that fails if the fixture's two counts coincide (a fixture where they agree cannot see the defect) |
| `TestSessionJSONRowsLeaveTheDeprecatedKeyUnparseableAsTheLiveField` | a script still reading top-level `turns` decodes nothing |

## Migration story for external scripts (Rule 15)

1. **What changes.** `gopi session list --json` / `gopi session show --json`
   publish the REPL-turn count under **`repl_turns`**, not `turns`. Its VALUE and
   MEANING are unchanged — it is still `chats.turn_count`, the turns the model
   finished. Only the name moved, because the bare word is what every human
   surface spends on the operator's own rows.
2. **What a reader must do.** `row.turns` → `row.repl_turns`. If the script wanted
   the operator's own asks (the number `session log <id> --turns` prints), it
   wanted `operator_turns` all along — that key exists and is the correct one.
3. **What still works, for how long.** A script that keeps reading `turns` finds
   the same number at **`row.deprecated_keys.turns`** until **2026-12-31**
   (`sessionevent.DeprecatedJSONKeyTurnsRemoveAfter`), announced on every row by
   `deprecated_keys.note`. A script that indexes the TOP LEVEL (`row["turns"]`)
   gets nothing — deliberately: a missing value is a loud break, while a present
   value under a word that means something else is the silent misread this task
   exists to end.
4. **After the deadline.** `deprecated_keys` is deleted in one commit; the report
   of that task names this date. No key ever silently changes MEANING — a
   re-pointed key is exactly the failure mode the alias is not.
5. **The human faces do not change.** `gopi session list` still prints
   `turns 11 (repl turns 18)`; `session show` still prints `repl turns 18`;
   `session log --turns` still prints `11 turn(s)`.

## Ran (Rule 31 — real invocations, real arguments, pasted output)

Binary: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → `BUILD_OK`.

**`gopi session list --json` (the changed wire surface).** Top-level keys of row
0 and the absence of the bare key:

```
$ ./bin/gopi session list --json
[bash] 27 rows / exit=0 (stderr: "[hidden] 23 session(s) hidden — their session log is not on this machine")
top keys: ['session_id', 'mode', 'source', 'state', 'model', 'repl_turns', 'operator_turns',
           'operator_turns_known', 'input_tokens', 'output_tokens', 'cache_tokens',
           'tokens_reported', 'started_at', 'ended_at', 'file_present', 'deprecated_keys']
turns top-level present: False
deprecated_keys: {"turns": 3, "replaced_by": "repl_turns", "remove_after": "2026-12-31",
  "note": "`turns` here is the retired bare key: it still holds this row's REPL-turn count
           (chats.turn_count) and is NOT the operator's own rows. Read `repl_turns` instead;
           this block is removed after 2026-12-31"}
```

**The epic's own session, the two quantities distinguishable on the wire:**

```
$ ./bin/gopi session list --json --all | (row 80c07a7d-…)
{ "session_id": "80c07a7d-bb6b-48b4-be66-ea1867f403f2",
  "repl_turns": 18, "operator_turns": 11, "operator_turns_known": true,
  "deprecated_keys": { "turns": 18, "replaced_by": "repl_turns",
                       "remove_after": "2026-12-31", "note": "…" } }
```

**`gopi session list` table — unchanged, still qualified:**

```
$ ./bin/gopi session list --mode chat
80c07a7d-bb6b-48b4-be66-ea1867f403f2  [chat]  human  …  (interrupt, model deepseek-flash,
  turns 11 (repl turns 18), tok in=21395024 out=56636 cache=21395024, started …, ended …)
```

**`gopi session show` table — the qualified word beside the unmeasured neighbour:**

```
$ ./bin/gopi session show 80c07a7d-bb6b-48b4-be66-ea1867f403f2
  turns       —
  repl turns  18
```

**`gopi session show --json` — same shape as the list row:**

```
$ ./bin/gopi session show 80c07a7d-… --json
repl_turns = 18 ; deprecated_keys = {"turns": 18, "replaced_by": "repl_turns", …}
```

**`gopi session log <id> --turns` — the operator's own quantity, unchanged:**

```
$ ./bin/gopi session log 80c07a7d-bb6b-48b4-be66-ea1867f403f2 --turns
SESSION TURNS — 80c07a7d-bb6b-48b4-be66-ea1867f403f2 (11 turn(s))
```

**Red-then-green proof of the pin**, two arms (job output, verbatim):

```
== RED 1: revert the tag to the retired bare key ==
--- FAIL: TestSessionJSONRowsLeaveTheDeprecatedKeyUnparseableAsTheLiveField (0.00s)
    session_json_wire_test.go:217: a top-level "turns" still decodes structurally (18):
    the retired key must not sit beside the live one
FAIL	github.com/nick/gopi/cmd/agent	0.383s
--- restore ---
ok  	github.com/nick/gopi/cmd/agent	0.359s
RESTORE_IDENTICAL   (diff -q against the pre-mutation copy: no drift)

== RED 2: rename the tag to "repl_turns_v2", leave the constant alone ==
--- FAIL: TestSessionJSONReplTurnKeyIsTheConstant (0.00s)
    session_json_wire_test.go:96: sessionJSONRow carries no field with wire key "repl_turns"
== RESTORE ==  RESTORE_IDENTICAL ; ok on the restored file
```

RED 2 is the arm that proves the pin bites on the TAG-vs-CONSTANT relation itself
(not only on a bare-key regression): the constant stays `repl_turns`, the tag
moves, and the test names the drift.

## Verification

- Build: `go build ./...` — clean (and the target binary `BUILD_OK`).
- Tests: `make test-budget PKGS='./cmd/agent ./internal/tools ./internal/sessionevent'`
  — **the final run's honest verdict**, three runs in total:

  | run | outcome | why |
  |---|---|---|
  | 1 (`run-1792978332`) | `fails` (17) | **my own doing**: the red-proof job mutated `cmd/agent/cli_session.go` WHILE the suite ran, so the tested binary no longer contained the tree and every worker/ground test refused (`the tested binary no longer contains the tree`) |
  | 2 (`run-1792979046`) | `fails` (20) | same class — the tree was still uncommitted, so the built binary's revision (`d1f2b23`) did not match the working tree; the gate named the two files that differed, which is exactly the refusal it should give |
  | 3 (`run-1792979677`, committed tree) | `sessionevent` **PASS**, `tools` **PASS**, `agent` **FAIL on ONE test** | `TestSigintDocsCiteTheLineNumbersTheSourceActuallyHas` — a PRE-EXISTING doc-citation drift, unrelated to this diff (below) |
  | 4 (`run-1792980385`, **re-run at the delivered commit `006eea0`**) | `sessionevent` **PASS**, `tools` **PASS**, `agent` **FAIL on the SAME ONE test, same message** | independent confirmation: the failure is reproducible at the landed revision, not an artefact of run 3 |

  Tests this change adds or touches, from run 3's own stream: `PASS
  TestSessionJSONReplTurnKeyIsTheConstant`, `PASS
  TestSessionJSONRowCarriesReplTurnsNotBareTurns`, `PASS
  TestSessionJSONRowsLeaveTheDeprecatedKeyUnparseableAsTheLiveField`, `PASS
  TestSession80c07a7dTurnCounts`, `PASS TestSessionListAndLogAgreeOnTurns` —
  8191 test-level `pass` events across the three packages, 1 failure.

  **Run 4's own numbers** (`stamp: 2026-10-26T10:06:25+08:00 ... elapsed=529s
  exit=1 outcome=fail stream=run-1792980385-3559642.json`), read out of that
  run's stream — the same three packages, the same single failure:

  ```
  8189 test-level pass fail events resolved; failing tests: 1
    TestSessionJSONReplTurnKeyIsTheConstant                              = pass
    TestSessionJSONRowCarriesReplTurnsNotBareTurns                      = pass
    TestSessionJSONRowsLeaveTheDeprecatedKeyUnparseableAsTheLiveField   = pass
    TestPublishedHelpPagesStayWithinTheirUsageBudget                    = pass
    TestSigintDocsCiteTheLineNumbersTheSourceActuallyHas                = FAIL
      worker_control_signal_origin_test.go:232: ...cites main.go:657 where
      "signalrec.Install" is the subject, but main.go puts that call on line 667
  ```

  (The stream is JSONL with one `pass`/`fail` event per test ARM, and a test
  with subtests ends several; the "1 failing test" figure is the budget script's
  own tally over that stream, and the package verdict it printed is
  `FAIL: github.com/nick/gopi/cmd/agent` — `internal/tools` and
  `internal/sessionevent` both passed.)

  **The one failure, and why it is not this diff's** (measured, both sides):

  ```
  worker_control_signal_origin_test.go:232: ../../devdoc/worker-control-sigint-attribution.md
    cites main.go:657 where "signalrec.Install" is the subject, but main.go puts that
    call on line 667
  $ grep -n "signalrec.Install" cmd/agent/main.go                        -> 667
    $ grep -n "main.go:657" devdoc/worker-control-sigint-attribution.md  -> 119, 138
    $ git show d1f2b23:cmd/agent/main.go | grep -n signalrec.Install     -> 667
    $ git show d1f2b23:devdoc/worker-control-sigint-attribution.md | ... -> 657
  ```

  Neither `cmd/agent/main.go` nor that devdoc is in this commit, and BOTH sides
  read the same at the base commit `d1f2b23` this ground started from — the drift
  predates this run. Filed as bug row **`01a175f4-0449-73b1-bab9-920b6dd4110e`**
  (unlinked from `01a175d6`, they are independent) with the raw evidence at
  `/home/user/.gopi/sessions/01a174c5-…/evidence/sigint-doc-line-drift.txt`.
- `make man` — `man/gopi-session.md` regenerated from the usage text; `make man-check` green.
- `gopi session -h` = **82 lines** (budget 83) — `TestPublishedHelpPagesStayWithinTheirUsageBudget`
  green after the keys moved out of the usage page (the first draft measured 89).
- `go test ./internal/standards/ -run TestEveryCorpusDocDeclaresItsClassInItsHeader`
  — green (this report carries `<!-- doc-class: historical -->` on line 1).
- Worktree: `git status --porcelain` prints nothing after the commit.

## The grounds this ran on, and the one before it

The clone channel's delivery loop, not a hand-made push (Rule 6c): the commit is
on this ground's branch, `git status --porcelain` is empty, and the loop
publishes it.

## Not Done (honest)

- **`gopi session show <id>` reports `turns —`** for a session whose operator
  count IS measurable (11 in its log). This is a PRE-EXISTING defect, not
  introduced here: `tools.ChatShow` never fills `Turns`/`TurnsKnown`/`TurnsNote`
  — only the LIST path measures them — so the single-row face has always had no
  operator count, and on the JSON face `operator_turns` is absent while
  `operator_turns_known` is `false`. Filed as bug task
  **`01a175d6-a055-751d-b083-fff5329cfa16`** with the raw evidence at
  `~/.gopi/sessions/01a174c5-…/evidence/show-operator-turns-gap.txt`. It is NOT
  hidden by this change (the migration story above tells readers to use
  `operator_turns`, and for `show` that key is absent — the row states it).
- `internal/gopilog/stats.go` keeps its own `Turns int \`json:"turns"\`` — that
  key is the OPERATOR-row count, i.e. the quantity the bare word legitimately
  owns; it is not this collision and was left alone.
- `cmd/agent/web_identity.go` publishes `Turns int \`json:"turns"\`` in the web
  identity payload (the engine's own REPL-turn counter, printed to humans under
  `sessionevent.CounterUnitReplTurn`). Renaming a WEB API field its clients poll
  is a separate compatibility decision with its own consumers — not in this
  task's scope, and doing it silently in this diff would be the very silent wire
  change this report documents how to do.

## Evidence (self-filed)

- wire key present / bare key absent: `./bin/gopi session list --json` → top key
  list above, `turns top-level present: False`.
- both quantities distinguishable: row `80c07a7d` → `repl_turns 18`,
  `operator_turns 11`, `operator_turns_known true`.
- human faces unchanged: `session list --mode chat` → `turns 11 (repl turns 18)`;
  `session log … --turns` → `11 turn(s)`.
- pin fails on the old shape: the RED block above (test name + message + exit).
- tag ↔ constant: `TestSessionJSONReplTurnKeyIsTheConstant` compares the field's
  tag against `sessionevent.JSONKeyReplTurn` (not against a literal).
- re-run at the delivered revision: `run-1792980385` (
  `stamp: 2026-10-26T10:06:25+08:00 ... exit=1 outcome=fail`) → the three new
  wire tests `pass`, `TestPublishedHelpPagesStayWithinTheirUsageBudget` `pass`,
  and exactly one failure, `TestSigintDocsCiteTheLineNumbersTheSourceActuallyHas`,
  whose message names a doc this commit does not touch. Identical to run 3 —
  the reproduction, not a claim.
- the failing test's drift predates this branch: at the base `d1f2b23`,
  `git show d1f2b23:cmd/agent/main.go | grep -n signalrec.Install` → `667` while
  `git show d1f2b23:devdoc/worker-control-sigint-attribution.md | grep -n main.go:657`
  → `119, 138`; the same two readings at HEAD.
