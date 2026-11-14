<!-- doc-class: historical -->
# A pipe control's observable is a property of the HOST unless the arm pins its locale

**Task** 01a14179-5629-7960-a0ca-b5c7d0ef3d03 · **Landing** `internal/testutil/testbudget_report_pipe_gate_test.go` +
`internal/testutil/testbudget_report_pipe_sites_gate_test.go` · 2026-10-16 · base `adafe54`

## The reported symptom, and why it read as a contradiction

Two gates in `internal/testutil` were red in a whole-package run and green under a
`-run` filter:

```
--- FAIL: TestBudgetReportNeverPrintsPipeNoise (0.10s)
    testbudget_report_pipe_gate_test.go:158: the reverted shape did NOT reproduce the report
    noise on a 4096-test fixture (687018 bytes) — the control proves nothing, so this gate
    cannot see a regression. Raise budgetReportPipeFixtureRows so the producer
    out-writes the pipe buffer.
        stderr: "sort: 写入失败: 标准输出: 断开的管道\nsort: 写入错误\n"
--- FAIL: TestBudgetFailureListArmsAreSilentAndFaithful (0.07s)
    testbudget_report_pipe_sites_gate_test.go:187: the pre-fix shape did NOT reproduce the
    report noise on a 3000-row fixture — the control proves nothing, so this gate could not
    see a regression.
        stderr: "sort: 写入失败: 标准输出: 断开的管道\nsort: 写入错误\n"
```

The gate said "did NOT reproduce the noise" and then **printed the noise**. The
control HAD reproduced the defect; the assertion simply did not recognise it,
because it matched the English literal `write failed: 'standard output': Broken
pipe` while the producer spoke a Chinese catalogue. A failure message that
contradicts its own evidence is Rule 15's shape, one level up.

## 5-why, and the answer is NOT "the whole-package run"

1. Why did the assertion not recognise the noise? — It matches English; the
   text was Chinese.
2. Why was the text Chinese? — `sort` diagnoses through gettext, and the locale
   it inherited selected a Chinese catalogue.
3. Why did the arm inherit that locale? — `runBudgetReportPipeArm` /
   `runBudgetSiteArm` set only `cmd.Dir`; with `cmd.Env == nil` the child
   **inherits this process's environment verbatim**.
4. Why was the locale Chinese here but not under `-run`? — **It was not.** The
   difference is the SHELL, not the scope: the `bash` tool this run uses pins
   `LC_ALL=C` for its children (measured: this session's environment carries
   `LC_ALL=C`, `LANG=C`), so a command typed through it never reproduces the
   failure; the background `job` tool does not pin it, and the ambient
   environment there carries `LANG=zh_CN.UTF-8`. The whole-package run was
   merely the run that happened to be launched the other way.
5. Why could the gate not see this itself? — Its observable was a **translated
   message body**, i.e. a property of the host, and nothing in the arm pinned
   it or matched around it.

**This is a controlled A/B, not an inference** (same tree, same filter, only the
locale varying):

| command | result |
|---|---|
| `env -u LC_ALL LANG=zh_CN.UTF-8 go test ./internal/testutil/ -run TestBudgetReportNeverPrintsPipeNoise -count=1` | **FAIL**, `stderr: "sort: 写入失败: 标准输出: 断开的管道\nsort: 写入错误\n"` |
| `env LC_ALL=C LANG=zh_CN.UTF-8 go test ./internal/testutil/ -run TestBudgetReportNeverPrintsPipeNoise -count=1` | `ok` |
| `env LANG=zh_CN.UTF-8 go test ./internal/testutil/ -count=1` (whole package) | **FAIL**, both gates |
| `locale -a` | `zh_CN.utf8` installed — the catalogue is real on this host |

Raw artifacts, verbatim, under this run's session directory
(`~/.gopi/sessions/01a14179-5629-7960-a0ca-b5c7d0ef3d03/artifacts/`):
`whole-package-failure.log` (pre-fix whole-package red), `failing-run-env.txt`
(the failing run's own environment, carrying `LANG=zh_CN.UTF-8` and no `LC_ALL`).

**Scope of the claim.** The declaration "the locale decides this gate's verdict"
is what the five rows above measure. The further statement that *no other*
environment difference contributes is NOT measured here, and is not claimed.

## The fix: two halves, only one of which is a pin

**(a) A pin — the arm controls the catalogue it reads.** Both arm runners now
spawn their shell with `cmd.Env = budgetReportPipeChildEnv()`: this process's
environment with the four catalogue-selecting variables (`LANG`, `LC_ALL`,
`LC_MESSAGES`, `LANGUAGE`) **replaced** by `C`.

*Replaced, never appended*: a duplicated entry in a child's environment is
decided by the READER, not by one rule — glibc's gettext path and Go's os.Getenv
take the FIRST copy while dash and bash take the LAST, and a wrapper (GNU
`env`, a shell) collapses the pair before the next process sees it (measured
2026-10-16; the reading, its host and its date are in `internal/tools`'
`git_child_locale.go`, `ChildLocaleDuplicateKeyRule`). So
`append(os.Environ(), "LC_ALL=C")` is not a pin at all — the inherited Chinese
value still wins for whatever reader looks first. The same construction and the
same trap as `internal/tools`' `gitChildLocaleEnv` (measured there 2026-10-12,
epic `01a1306b`: a gettext catalogue turned a *product* census into a
clean-looking zero — `unreachable tips: 1` under `C`, `0` under `zh_CN.UTF-8`).
That helper is unexported in another package, so the reason is repeated here
rather than the defect.

**(b) A locale-independent observable — the program-name prefix.** gettext
translates the message BODY, never the program name. `sort:` and `grep:` are
byte-identical in every catalogue:

```
C:      sort: write failed: 'standard output': Broken pipe
zh_CN:  sort: 写入失败: 标准输出: 断开的管道
```

`budgetReportPipeMatchProducerNoise(stderr)` asserts exactly that: **every
non-empty stderr line carries a producer's own `sort:`/`grep:` prefix** (empty
stderr → false, so "nobody spoke" is not the noise). This is the assertion that
must never depend on the host; a catalogue the arm has never seen cannot hide
the defect from it.

The English literals are kept **beside** it, as the pin's own witness: with the
pin in force they are deterministic, and they turn red loudly if someone removes
the pin. The two halves are deliberately ordered — locale-independent first, so
a future locale regression cannot re-create the original message ("did NOT
reproduce the noise" printed next to the noise).

## The red arms are kept, and demonstrated

Silencing the assertion was not available: the controls are asserted, and three
mutation routes were run for real. Every mutation was reverted, and each restore
was verified against an **explicit revision** — `test "$(git hash-object <path>)"
= "$(git rev-parse HEAD:<path>)"` (the script) or `cmp` against the pre-mutation
snapshot (the test file) — never an index-relative diff.

1. **The shipped bytes go back to the defect** — `scripts/test-budget.sh`'s
   `list_executed_tests` body replaced with the pre-fix `… | sort -u | head -N`
   shape, run under `LANG=zh_CN.UTF-8` ⇒ RED twice over: the structural arm
   reports the early-exit consumer at helper line 4, and the shipped arm writes
   `"sort: write failed: 'standard output': Broken pipe\nsort: write error\n"` to
   stderr. **That stderr is ENGLISH under a Chinese locale** — an independent
   witness that the pin in force is what makes the literal match (artifact:
   `artifacts/redproof-1-shipped-reverted-zh.log`).
2. **The pin is removed from the arm's environment** —
   `cmd.Env = budgetReportPipeChildEnv()` deleted, run under
   `env -u LC_ALL LANG=zh_CN.UTF-8` ⇒ the English-literal witness fires while the
   locale-independent half still recognises the noise ⇒ RED with exactly the
   designed message: `the reverted shape reproduced a producer error (["sort:"
   "grep:"]) but NOT the English text this host's locale pin should have produced
   — … stderr: "sort: 写入失败: 标准输出: 断开的管道\nsort: 写入错误\n"`
   (artifact: `artifacts/redproof-2-pin-removed-zh.log`).
   *Note on the first attempt:* run without `-u LC_ALL`, this mutation did NOT
   bite — this session's own shell exports `LC_ALL=C`, which the unmutated
   inheritance then passed to the child. The mutation's red depends on the
   ambient catalogue actually being hostile, which is exactly the class of
   fragility the pin removes.
3. **The new unit guard's own arm** —
   `budgetReportPipeChildEnv` changed to the true append trap (the strip loop of
   catalogue names removed, the pin appended) ⇒ RED:
   `the arm's environment carries 1 LC_MESSAGES entries, want 0 … the pin must
   REPLACE LANG/LC_ALL and STRIP the selectors, never append`. *Honest note: a
   first version of this mutation appended a **second** `LANG`/`LC_ALL` while
   leaving the strip in place, and it stayed GREEN — that version was
   behaviourally equivalent to the original (the inherited poison had already
   been stripped, and this arm's child is spawned through `os/exec`, which
   DEDUPES `Cmd.Env` in favour of later values — `dedupEnv`,
   `$GOROOT/src/os/exec/exec.go` — so the inserted `C` is the only entry left),
   so it is not a
   valid red arm. The stripping removal is the lossy mutation.*

Mutation 1 proves the gate can still see a regression of the SHAPE; mutation 2
proves it can still see a regression of its own READABILITY; mutation 3 proves
the pin's construction is itself guarded rather than asserted in prose.

## Closing read-back at `ea2668b0f` (re-run by the reviewing session)

The measurements below were taken **again** after the commit, by a session that
had not written the fix — so the acceptance does not rest on the fixing run's own
transcript. Everything was run at `ea2668b0f` in the task's worktree.

- **Acceptance (1)** — whole package, no filter, hostile locale:
  `env -u LC_ALL LANG=zh_CN.UTF-8 go test ./internal/testutil/ -count=1`
  → `ok github.com/nick/gopi/internal/testutil 206.210s`, `EXIT=0`.
- **Acceptance (2)** — the two gates plus the new unit guard, unfiltered
  package's evidence narrowed to the names:
  `go test ./internal/testutil/ -run 'TestBudgetReportNeverPrintsPipeNoise|TestBudgetFailureListArmsAreSilentAndFaithful|TestBudgetReportPipeChildEnvPinsTheCatalogueAndThePredicateReadsAny' -count=1 -v`
  → `--- PASS` ×3, `ok … 4.887s`, `EXIT=0`.
- **Acceptance (3)** — red arm A re-run: `scripts/test-budget.sh`'s
  `list_executed_tests` reverted to `… | sort -u | head -N` (mutated blob
  `55eafc037abdc70e57b1fa238390e633ee516099`, HEAD's `433ce53f…`), then
  `env -u LC_ALL LANG=zh_CN.UTF-8 go test ./internal/testutil/ -run TestBudgetReportNeverPrintsPipeNoise -count=1`
  → **`EXIT=1`**, `--- FAIL: TestBudgetReportNeverPrintsPipeNoise`, structural arm
  at helper line 12 and the shipped arm writing
  `"sort: write failed: 'standard output': Broken pipe\nsort: write error\n"`.
  Restored, and the restore checked against an explicit revision
  (`git hash-object scripts/test-budget.sh` = `git rev-parse HEAD:scripts/test-budget.sh`
  = `433ce53f…`; `git diff HEAD` empty). Artifact:
  `artifacts/redproof-closing-mutation-A-zh.log`.
- **The shell A/B, re-measured** (this is the epic's central claim, so it was not
  inherited): the same probe run through the two tools —
  `bash` tool's child carries `LC_ALL=C` / `LANG=C`; the `job` tool's child carries
  `LANG=zh_CN.UTF-8` with **no** `LC_ALL`. That, not the `-run` filter, is the
  difference between the two environments the failure was reported in.
- **The catalogue is a real file on this host**:
  `/usr/share/locale-langpack/zh_CN/LC_MESSAGES/coreutils.mo` carries the body
  translations; the message *body* is what varies, `sort:` is not.

### Two review notes (honest — neither is fixed here)

1. **Mutation A does not, by itself, exercise the prefix predicate.** That
   mutation changes a production byte, so the reverted shipped arm runs it under
   the pin and speaks `sort: write failed: … Broken pipe` (the closing artifact
   shows exactly that). Its red therefore rests on the structural arm plus the
   English-literal witness — it distinguishes "the gate sees a regression of the
   SHAPE" from "the gate is asleep", not "the prefix predicate is load-bearing"
   from "only the literals matter". The claim that the locale-independent half is
   what discharges `the reverted shape did NOT reproduce … the control proves
   nothing` rests on the **pre-fix** artifact (`artifacts/whole-package-failure.log`,
   where the arm spoke zh_CN and the old literal-only assertion fired) and on the
   new unit guard, which pins the predicate against both catalogues directly.
2. **Mutation B's red has the same carrier as the defect it hunts.** With the pin
   deleted the fail message prints `stderr: %q` of the arm's stderr, and *that is
   the slice-captured channel that swallows a dead producer* — the fixing run
   measured this: the first mutant FAIL under that locale quoted an **empty**
   `stderr`, and only re-running with the pipe position valid again made the
   localized text appear (`artifacts/redproof-2-pin-removed-zh.log`). So "delete
   the pin ⇒ RED" is deterministic, while the *localized* text in that red is
   not: the path that yields it is the path whose validity is in question.

## Not Done (honest)

- The `report_package_failures` arm still has no control (its producer is the
  shell's own `printf` builtin, whose EPIPE bash swallows — measured
  2026-10-13), and the `fan_out_timeout_marker` control's observable is its
  STATUS, not stderr, because that helper redirects its producer's stderr to
  `/dev/null`. Both limits are unchanged by this fix and are still stated in the
  sites gate's header.
- `budgetSiteTimeoutMarkerFixture` (the fixture's marker prefix) is still a
  second spelling of the script's JSON-escaped constant; the gate asserts
  membership rather than reading it, for the reason its own comment gives.
- Only `internal/testutil` was re-run. Other packages' gates may carry the same
  translated-observable shape; nothing here audits them.
