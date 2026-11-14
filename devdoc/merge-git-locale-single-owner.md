<!-- doc-class: historical -->
# One list, two owners — the merge lane's git locale pin now CALLS the shared helper

Task `01a141e6-4ca7-72b8-812c-9ac0a01374ac`, 2026-10-14, branch
`task/01a141e64ca772b8812c9ac0a01374ac`, cut at `ab19708`.

## 1. The defect, as measured

`cmd/agent/merge_run.go`'s `mergeGitEnv()` built its OWN strip-and-append of the
message-locale names:

```go
if strings.HasPrefix(kv, "LANG=") || strings.HasPrefix(kv, "LC_ALL=") ||
    strings.HasPrefix(kv, "LC_MESSAGES=") || strings.HasPrefix(kv, "LANGUAGE=") {
    continue
}
return append(env, "LANG="+locale, "LC_ALL="+locale)
```

while `internal/tools/git_child_locale.go` owned the same four names
(`ChildLocaleLocalePinnedNames` = LANG, LC_ALL; `ChildLocaleSelectorNames` =
LC_MESSAGES, LANGUAGE). **Two owners of one list** (Rule 13/14).

The two agreed *by coincidence*, not by construction. Concretely: the day a
fifth gettext selector is added to the helper, this lane's git children keep
inheriting it, and **no test in `cmd/agent` can see it** — the lane's own test
(`TestMergeGitEnvPinsTheLocale`) asserted the four names the lane had typed, so
it agreed with itself. That is the failure mode, and it is a SILENT one: the
classification that reads `CONFLICT` out of git's output
(`mergeConflictMarker`) then matches zero lines on a translated host, which is
exactly the Rule 15 shape already measured in this repository
(`devdoc/git-child-locale-pin.md`, task `01a1306b`: the unreachable census
reported `unreachable tips: 0` under `zh_CN`).

## 2. The fix — the VALUE moves, the POLICY does not

* `internal/tools/git_child_locale.go`: new exported entry point

  ```go
  func ChildLocaleEnvPinning(locale string) []string
  ```

  which owns the whole policy: which names are stripped
  (`ChildLocaleEnvName`), which are written
  (`ChildLocaleLocalePinnedNames`), and that the inherited entries are
  **replaced rather than appended beside** — a duplicate key in an exec
  environment resolves to the FIRST entry, so `append(os.Environ(), "LC_ALL=C")`
  looks like a pin while the inherited translation still wins.

  `ChildLocaleEnv()` is now `ChildLocaleEnvPinning(ChildLocaleEnglish)`, so every
  existing caller (bash.go's children, the voice recorder in
  `cli_voice_client.go`) is behaviourally unchanged and pinned to the same list.

* `cmd/agent/merge_run.go`: `mergeGitEnv()` keeps the Rule 2 seam and nothing
  else — it reads `GOPI_MERGE_LOCALE` (that read exists in exactly one place,
  `mergeGitEnv`) and hands the VALUE to the helper. No locale name is spelled in
  this package's production code any more.

An earlier alternative was rejected: callers re-applying the override on top of
`ChildLocaleEnv()`'s result would have to re-decide the strip/append policy at
the call site — which is precisely how a SECOND spelling of "byte-stable
diagnostics" appears. The parameter exists so the caller supplies a value that
the ONE policy then applies.

## 3. The gate, and why it is a MIRROR gate

`cmd/agent/merge_locale_mirror_test.go` carries two arms that are different
instruments:

* `TestMergeLocaleMirrorIsPinnedToTheSharedHelper` — parses every non-test `.go`
  file in `cmd/agent` with `go/parser` and fails if any of the four names appears
  as a STRING LITERAL (the precedent is `TestUnknownNode_MirrorLiteralGate`,
  Rule 13). This is the arm a fifth selector turns red. Comments are exempt by
  construction (they are parsed as comments, not as literals): the package's
  other files narrate the same measurements (`LANG=zh_CN.UTF-8 gopi …`) and a
  gate that forbade the evidence would be deleting the record to enforce the rule
  (Rule 38). A literal exemption is available as `locale-mirror-ok: <reason>`,
  reviewed on the assignment's line. The arm also asserts a read floor
  (`mergeLocaleMirrorFloor`) — a scan that read nothing is not a pass (Rule 15) —
  *and* that `merge_run.go` still calls `tools.ChildLocaleEnvPinning(`, so a
  dropped call cannot make the arm green for the wrong reason.

* `TestMergeGitEnvPinsExactlyTheHelpersNames` — the SHAPE arm, anchored on the
  HELPER's exported lists rather than on four names typed in the test. It sets a
  hostile ambient environment through `t.Setenv` (Rule 17) and asserts: each
  pinned name appears **exactly once** carrying this lane's pin, each selector
  appears **zero** times, and `GOPI_MERGE_LOCALE` reaches the pin as the VALUE.
  "Exactly once" is what rules out the naive `append(os.Environ(), "LC_ALL=C")`
  shape, which a mere "is C in there" check would accept while the inherited
  translation won. This is why the helper EXPORTS the lists.

The day a fifth selector is added to the helper, both arms start covering it with
no edit — that is the whole point of routing the lane through the helper.

## 4. Red proof, restore, and the Rule 31 run

* **Red proof.** `merge_run.go` was mutated back to the pre-fix shape (inline
  four-name strip/append, no helper call) and
  `go test ./cmd/agent -run TestMergeLocaleMirrorIsPinnedToTheSharedHelper` went
  **RED with 6 findings**, naming `merge_run.go:3222` / `:3223` (the strip) and
  `:3228` (`return append(env, "LANG="+locale, "LC_ALL="+locale)`) plus
  `merge_run.go does not call tools.ChildLocaleEnvPinning(`. Log:
  `<session>/artifacts/redproof-mirror.log`.
* **Restore.** Revision-anchored, per `devdoc/redproof-restore-criterion.md` /
  `redproof-restore-content-identity.md`: the reading is content identity against
  a **revision**, not an index-relative diff —

  ```console
  $ test "$(git hash-object cmd/agent/merge_run.go)" = "$(git rev-parse HEAD:cmd/agent/merge_run.go)" && echo OK
  OK
  ```

  and it was taken AFTER the last write to the file, so it is timely.
* **Rule 31 — the changed surface, executed.** `go build ./...` and a green
  suite do not show that `mergeGitEnv` pins anything; the real binary was built
  and the real verb run against a real repo under a real hostile locale. Inside a
  non-git directory, ambient `LANG=zh_CN.UTF-8` with **no** `LC_ALL` (the exact
  pair that defeats a naive pin — gettext consults `LANG`, `LC_ALL` beats it, and
  an inherited `LANGUAGE` overrides both), the lane printed git's diagnostic in
  **English** (`not a git repository`), while bare `git` in the same environment
  printed `不是 git 仓库`; and with `GOPI_MERGE_LOCALE=zh_CN.UTF-8` over ambient
  `LANG=C` the same line printed **Chinese**, proving the override reaches the
  child environment through the ONE helper. Verbatim transcript (commands, output
  and the red control): `<session>/artifacts/invivo-rule31-locale-pin.txt`.

## 5. Not done / honest limits

* The mirror arm's exemption escape is textual and per-line: a deliberately
  written `locale-mirror-ok:` comment is honoured without review beyond the
  reader's eye. The escape count is not ratcheted in a baseline table the way
  Rule 39's hygiene permits are.
* The arm scans **this package** (`.`), which is where the second owner was. It
  does not scan the rest of the repository for a third spelling of the same list;
  today the only other consumer, `internal/tools/bash.go`, reaches the helper
  itself.
* An unrelated observation from the Rule 31 run is filed as its own bug row
  rather than fixed here (Rule 26/41): `internal/removalaudit` refuses to write a
  record when `<GOPI_HOME>/.gopi/logs/` does not exist, printing
  `… record for … not written: open …/logs/removal-audit.log: no such file or
  directory` on stderr and going on — a fresh profile therefore silently loses
  the removal audit. Evidence: `<session>/artifacts/invivo-merge-locale.txt`.
