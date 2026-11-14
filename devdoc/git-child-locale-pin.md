<!-- doc-class: historical -->
# The census that reported "no unreachable tip" because git answered in Chinese

Task `01a1306b-3fae-75d9-81e1-20fe1f46406b`, 2026-10-12, host `hostb-Laptop`
(hosta). Branch `task/01a1306b3fae75d981e120fe1f46406b`. Report for the epic
"diagnose the flake in `TestArchiveCLIUnreachableReportsTheKeepingWindow`":
the census reported `unreachable tips: 0` under the suite while the same test
passed in isolation, same worktree, same binary.

The flake was not a race, not state leakage, and not the fixture. **The verb
read git's own report in a language git is free to translate**, and the two
launch environments carried different locales.

---

## 1. Root cause, measured

`gopi task archive --unreachable` classifies `git fsck`'s report by matching the
literal first field `unreachable`
(`internal/tools/archive_unreachable.go`, `archiveUnreachableLinePrefix`). git's
diagnostics are **gettext-translated**, so the same command over the same object
store prints different bytes per locale. On this host, with a repo holding one
orphaned tip (`/dev/shm/locale-ab-01a1306b/repo`, tip `3e43155dabd6`):

```console
$ LANG=C            git -C "$R" fsck --no-progress --unreachable --no-reflogs
unreachable commit ca70f788134ebcf72606f9ab1d08e23768217f76
unreachable commit 3e43155dabd64ff03053d4a47b016a60fdc29c4c

$ LANG=zh_CN.UTF-8  git -C "$R" fsck --no-progress --unreachable --no-reflogs
不可达 commit ca70f788134ebcf72606f9ab1d08e23768217f76
不可达 commit 3e43155dabd64ff03053d4a47b016a60fdc29c4c
```

The classifier matched zero rows and rendered its **clean zero** — the same
sentence a repo with nothing to report produces. Raw output of the A/B, shipped
binary, one fixture, two binaries (`gopi-prefix` = the tree before the fix,
`gopi-fixed` = after), one locale each
(`artifacts/ab-binary-locale.log`, script `artifacts/ab-binary.sh`):

```
--- binary=prefix locale=LANG=C LC_ALL=C exit=0
  unreachable commits: 2
  unreachable tips:    1
--- binary=prefix locale=LANG=zh_CN.UTF-8 exit=0
  unreachable commits: 0
  unreachable tips:    0
--- binary=fixed locale=LANG=C LC_ALL=C exit=0
  unreachable commits: 2
  unreachable tips:    1
--- binary=fixed locale=LANG=zh_CN.UTF-8 exit=0
  unreachable commits: 2
  unreachable tips:    1
```

**This is a production defect, not a test symptom**: the shipped binary told an
operator "every commit is named by a ref" about a repo it had not read.

### Why it looked like a suite-vs-isolation flake

The two invocations inherited different locales, and nothing in the fixture or
the test was involved:

```console
$ bash -c 'echo LANG=$LANG LC_ALL=$LC_ALL'          # the foreground shell
LANG=C LC_ALL=C
$ # a background job's environment (job-14, same host, same minute)
LANG=zh_CN.UTF-8     ← and no LC_ALL, so LC_ALL cannot rescue it
```

`cmd/agent`'s suite inherits the process environment, and the test binary hands
that environment to the gopi child
(`runArchiveProbe` → `cliContractEnv`, which drops `GOPI_*`, `ANTHROPIC_*`,
`DEEPSEEK_*` and `DISPLAY` only). So the locale reached the verb, the verb
handed it to its own `git` child, and the child answered in Chinese. The
**exact** divergence, with the pre-fix binary, one arm per locale:

```console
$ env -u LC_ALL -u LC_MESSAGES -u LANGUAGE LANG=zh_CN.UTF-8 GOPI_TEST_BIN=…/gopi-prefix \
    go test ./cmd/agent/ -run TestArchiveCLIUnreachableReportsTheKeepingWindow -count=1
    cli_archive_unreachable_test.go:83: census output is missing "orphaned work part two":
          unreachable commits: 0
          unreachable tips:    0
          no unreachable tip — every commit is named by a ref
FAIL
$ env -u LC_ALL -u LC_MESSAGES -u LANGUAGE LANG=zh_CN.UTF-8 GOPI_TEST_BIN=…/gopi-fixed \
    go test ./cmd/agent/ -run TestArchiveCLIUnreachableReportsTheKeepingWindow -count=3
ok  	github.com/nick/gopi/cmd/agent	17.631s
```

The epic's premise — "passes in isolation, fails under the suite, same
worktree, same binary" — was therefore **half right**: the passing wrapper
carried `LC_ALL=C` and the failing one did not (the run's own first isolated
invocation was already red, `output.log:826`). The variable was never the suite;
it was the locale of whoever started the test binary.

---

## 2. The fix — pin the locale on the git child, in ONE helper

A parser that reads OUR strings out of git's output cannot accept the ambient
language: the locale is an **input**. Two seams in this repository already apply
exactly this rule to their own children — `internal/tools/bash.go`
(`bashChildEnv` / `bashDefaultLocale = "C"`) and `cmd/agent/merge_run.go`
(`mergeGitEnv`, whose comment records the identical 2026-09-26 incident on
`CONFLICT`). What was missing is the shared half.

New file `internal/tools/git_child_locale.go` — the ONE place this package
decides the message locale of a git child whose output a parser reads:

```go
func gitChildLocaleEnv() []string   // os.Environ() with LANG/LC_ALL REPLACED by "C",
                                    // LC_MESSAGES/LANGUAGE stripped
```

* **Replaced, never appended.** A duplicated entry in a child's environment is
  resolved by the READER, not by one order — glibc's gettext path and Go's
  `os.Getenv` take the FIRST copy while dash and bash take the LAST (re-measured
  2026-10-16; the reading, its host and its date are in
  `internal/tools/git_child_locale.go`'s `ChildLocaleDuplicateKeyRule`) — so
  `append(os.Environ(), "LC_ALL=C")` looks like a pin while the inherited value
  still wins for whichever reader looks first. The helper removes the inherited
  entry first; `TestGitChildLocaleEnvReplacesRatherThanAppends` asserts *exactly
  one* entry per name, not merely presence.
  *This paragraph originally read a one-word rule naming the *first* entry as
  the winner — the inversion corrected in task `01a14212` (measured 2026-10-16;
  refuted, not merely replaced: the FIRST-copy readers are glibc and Go, the
  LAST-copy readers are the two shells, so neither one-word rule survives).
  It is kept as a record and the rule above replaces it.*
* Wired into **(a)** the census's `git fsck` child
  (`archive_unreachable.go`, `archiveUnreachableFSCK`) and **(b)** the shared
  runner `repoSweepGitInput` (`repo_sweep.go`), whose ~20 callers include the
  archive namespace scans and the tip-holder scans.
* `childLocaleEnglish` is asserted equal to `bashDefaultLocale`: one spelling of
  "byte-stable English diagnostics" (Rule 14).

### 2b. The second defect this exposed: a test that skipped instead of measuring

The first version of the guard used `locale -a | grep`, and on this host it
returned **false** while the locale works perfectly:

```console
$ locale -a | grep zh_CN
zh_CN
zh_CN.gb18030
zh_CN.gbk
zh_CN.utf8            ← the listing spells it .utf8; the caller writes .UTF-8
$ LC_ALL=zh_CN.UTF-8 locale charmap
UTF-8                 ← glibc: the locale is usable
```

Both guards therefore **skipped**, the pin looked green, and only a manual
`LANG=zh_CN.UTF-8 go test` showed the red state — a guard that cannot fail is
not a guard. The probe now asks glibc (`locale charmap` under the pinned locale,
marker `Cannot set`), the same probe `cmd/agent/merge_locale_test.go`
(`mergeLocaleIsGeneratable`) already uses, and it SKIPS LOUDLY with the reason
when the host genuinely cannot translate git (Rule 15).

---

## 3. The third defect: a report the census could not read was dropped silently

`ArchiveUnreachableCensusAt` skipped every line that was not
`unreachable <type> <sha>` — which is *how* a translated report became a clean
zero. It now counts them (`ArchiveUnreachableCensus.Unrecognized`) and appends a
loud note, next to the pre-existing `Undescribed` note:

```
note: 4 line(s) of git's report were not `unreachable <type> <sha>` and could not be classified —
the counts below may UNDER-report; git's diagnostics are translated per the ambient locale, which
this census pins (git_child_locale.go)
```

The red proof (`artifacts/redproof-locale.log`, pin removed in the working tree;
the file is back on disk pinned):

```
--- FAIL: TestCensusReadsGitsReportWhateverTheLocale
      LANG=C:      4 commits / 2 tips
      zh_CN.utf8: 0 commits / 0 tips
      note: 4 line(s) of git's report were not `unreachable <type> <sha>` …
      no unreachable tip — every commit is named by a ref
```

---

## 4. Guards (all in this branch)

| guard | file | what it holds |
|---|---|---|
| `TestCensusReadsGitsReportWhateverTheLocale` | `internal/tools/git_child_locale_test.go` | census answers the SAME under `LANG=C` and under the translating locale; `Unrecognized == 0` |
| `TestGitChildLocaleEnvReplacesRatherThanAppends` | same | exactly one entry per written name, selectors stripped, value == `bashDefaultLocale` |
| `TestRepoSweepGitPinsTheLocale` | same | the shared runner's child answers in ENGLISH under the translating locale, with an unpinned control that must come back TRANSLATED |
| `TestArchiveCLIUnreachableIsIndependentOfTheAmbientLocale` | `cmd/agent/cli_archive_unreachable_test.go` | the REAL binary, same assertion, plus a printed control proving git localizes on this host |

The last one is the epic's own guard: it drives `gopi task archive --unreachable`
with `LANG=zh_CN.UTF-8` and **no `LC_ALL`** — the failing environment, not the
passing one. Against the pre-fix binary it is red
(`artifacts/guard-red.log`); against the fixed binary, green at `-count=3`.

---

## 5. Not done / honest limits

* **A sweep of the other parsers is not part of this change.** The pattern
  ("parse an English word out of git's own output") is now known to be a family.
  What this branch fixes is the census and the shared runner. A cheap grep
  suggests the remaining candidates are largely doc/comment hits
  (`memory_sync_task.go:921` reads SQLite's `already exists`, not git's), but
  that is an inspection, not a measurement — filed as
  `Rule 41`-style follow-up work, not claimed as verified.
* **The pin does not make the test independent of the operator's locale for
  non-git surfaces.** Only the git children whose output this package parses are
  pinned. A verb that renders its own Chinese text is unaffected (and correct).
* The measured host locale for the failing arm was `LANG=zh_CN.UTF-8` with **no**
  `LC_ALL`; a host with `LC_ALL=C` never saw the defect. That asymmetry is the
  whole reason this survived as a "flake".
