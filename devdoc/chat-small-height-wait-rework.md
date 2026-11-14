<!-- doc-class: historical -->
# Rework: landing `fix/chat-small-height-wait-from-master` on master

Task: `rework: fix/chat-small-height-wait-from-master — retiring waitDraws orphans
6 master call sites (vet red)`. Branch: `fix/chat-small-height-wait-from-master`
(commit `7ebf3ea5`, re-created from that commit — see "the branch ref was gone").
Base: master `0652b5db`.

## Completed

### The branch ref was gone; master had moved twice under it

The briefing described a branch tip `7ebf3ea5` at fork point `2fdc65e4`, with
master at `149b9bd9`. Measured on arrival:

- `git branch -l '*chat-small*'` → empty; `git cat-file -e
  fix/chat-small-height-wait-from-master` → `unknown revision`. The commit
  `7ebf3ea5` still existed, referenced by the commit `3e245d36` ("merge
  fix/chat-small-height-wait-from-master: a draw never overtakes queued input",
  2026-09-21 13:40) — an earlier merge attempt that is NOT an ancestor of master
  today (`git merge-base --is-ancestor 3e245d36 master` → NO), i.e. master was
  rewound past it and the branch was deleted without being archived (no
  `archive/*` tag names it).
- Master had moved on to `0652b5db`, and **master had landed its own fix for the
  same flake family**: `6ce7e92c` "fix(lineedit): a completion publish is a LEVEL,
  and the test source wakes through the pipe it owns" (adds
  `internal/lineedit/parkgate.go` + `menuNeedsDraw()`), plus `0fc25400`, which is
  one of the two commits that orphaned `waitDraws`.

So the re-created branch (`git branch fix/chat-small-height-wait-from-master
7ebf3ea5`) merges master `0652b5db`, not `149b9bd9`. The two sides are two
independent repairs of the same flake: master repairs the completion MENU's lost
wake (a level, `menuNeedsDraw`), this branch repairs the read loop's ORDERING (a
publish drawn ahead of queued input, `inputPending`).

### The merge: two conflicts, both resolved as a union

`git merge master` → `CONFLICT (content)` in exactly two files, as the briefing
predicted for DOC-INDEX (plus a second one the briefing's text probe could not
see, because it is a semantic conflict in the same arm):

1. **`devdoc/DOC-INDEX.tsv`** — an append race at the tail; both sides' rows kept,
   so the union is 2115 rows: this branch's `chat-small-height-flake.md` and
   master's `chat-duplicate-assistant-record.md`, `client-push-rebase-20260919.md`,
   `client-push-rebase-report.md`.
2. **`internal/lineedit/editor.go`, the `EventWake` arm** — the branch drew a
   publish only when
   `statusPending || menuUnread || menuDirty || menuUndrawn`, master only when
   `statusPending || menuNeedsDraw()`. `menuNeedsDraw()` IS that disjunction (the
   three-way OR of `menuUnread || menuDirty || menuUndrawn`, verbatim in its doc
   comment) — it is master's post-`6ce7e92c` consolidation of the very expression
   this branch added a fourth caller for. Keeping both would have spelled one
   predicate twice, so the resolution keeps master's level and this branch's new
   half: `statusPending || menuNeedsDraw()` plus the `inputPending() { continue }`
   ordering gate. Nothing was dropped: the gate is the branch's contribution, the
   level is master's.

### The orphaned call sites: 6 sites, ported to the assertion's own subject

After the merge, `waitDraws` had 6 call sites and 0 definitions
(`go vet ./cmd/agent` → `h.waitDraws undefined (type *streamHarness has no field
or method waitDraws)`, the briefing's exact failure). Both files master added
after the fork call it in the same shape — `h.in.PublishStatus()` followed by
`h.waitDraws(t, h.in.Answer().StatusPublishes()+1)`, i.e. "publish, then wait for
it to be DRAWN":

| file | lines | sites |
|---|---|---|
| `cmd/agent/chat_panel_rows_test.go` | 144, 179, 267, 324 (pre-edit) | 4 |
| `cmd/agent/chat_flow_rows_test.go` | 232, 278 (pre-edit) | 2 |

Each is ported to the idiom the branch established (`waitFor` polling the SCREEN
for the published text, which is the assertion's own subject):

```go
h.in.PublishStatus()
h.waitFor(t, "the published model on the status row", func() bool {
    h.replay()
    return strings.Contains(h.screen.Line(h.layout().StatusRow), panelStatusModel)
})
```

with `flowBandModel` in the flow file. This is not a weaker wait: the row text is
exactly what the tests below the wait assert on (`panelStatusModel` /
`flowBandModel`), and a real fault still fails with the timeout dumping the
screen. `waitDraws`'s retirement note in `chat_stream_test.go` is the only
remaining occurrence of the name.

## Not Done

1. **`internal/standards`' docclass COVERAGE gate is red — pre-existing on
   master, not caused by this merge, and deliberately not repaired here.**
   Measured A/B (two trees, same command):
   `go test -count=1 -run TestDocManifestCoversEveryDevdocFile ./internal/standards/`

   | tree | result |
   |---|---|
   | detached worktree of pure master `0652b5db` | FAIL — `2114 rows cover 2163 files` (49 unclassified) |
   | this branch (master merged) | FAIL — `2115 rows cover 2164 files` (49 unclassified) |

   The enumerated unclassified set is identical in both trees (the gate prints at
   most 25; `comm` on the two sorted lists → empty delta both ways): 9 devdoc
   markdown files (`chat-help-panel-rows.md`, `chat-turn-flow-band.md`,
   `chat-completion-lost-wake.md`, `brew-subcommand-test-hermetic.md`,
   `config-show-verb.md`, `cli-as-eyes-and-hands-feasibility.md`, …) and 40
   recorded artifacts under `devdoc/evidence/sgr-grid/` (tracked, and not ignored:
   `git check-ignore` → rc=1). The rows this branch's merge touched are all sound:
   0 duplicate paths, 0 rows for a file that does not exist, and each of the four
   unioned rows resolves to a real file. So this merge increases the hole by
   ZERO files (both sides' rows arrive with both sides' files). It is master's
   own coverage debt, filed separately rather than repaired inside a rework
   (Rule 26: dispatch it, do not fold it in).
2. **The banner half of the same gate WAS this merge's business and is fixed**:
   `devdoc/chat-small-height-flake.md` was classed `historical` in the manifest
   but carried no `<!-- doc-class: historical -->` banner, which master's
   `TestDocClassBannerMatchesManifest` requires (that gate landed on the master
   side after this branch forked). The banner is added; the gate is green on the
   merged tree.
3. **The flake's own re-verification is a different task.** Rule 27 puts the
   ORDER/DETERMINISM class on the `make flake-catch` opt-in, never on the merge
   path; the branch's own devdoc carries its 20-run acceptance transcript on pure
   master with the fix. What is verified here is the merge's own gate (build, vet,
   the scoped budget), not a re-measurement of the flake.

## Verification

- `go build ./...` → rc=0 (with the merge unresolved it also built — the orphan is
  a TEST-only symbol; `go build` was never the failing half).
- `go vet ./...` → **rc=0**, zero diagnostics (before the port: rc=1 with
  `cmd/agent/chat_flow_rows_test.go:232:4: h.waitDraws undefined`).
- `go test -count=1 ./internal/lineedit/` → `ok 24.773s` (master's `menuNeedsDraw`
  level and this branch's `inputPending` gate coexist).
- `go test -count=1 -run 'TestDocClassBannerMatchesManifest|TestDocManifestRowsAreWellFormed'
  ./internal/standards/` → `ok` (banner + row shape).
- `make test-budget PKGS=./cmd/agent/` → see the report's Verification section for
  the run's own output (this devdoc is committed before that number was known, so
  the number is not restated here).

## Evidence (self-filed)

- Branch reconstruction: `git branch fix/chat-small-height-wait-from-master
  7ebf3ea5` → OK; `git merge-base --is-ancestor 3e245d36 master` → exit 1 (NO);
  `git for-each-ref --contains 7ebf3ea5` → only `refs/tags/list`, a blob tag.
- The orphan proved by the merge itself: `git merge master` → the two conflicts
  above; `grep -c '^<<<<<<<'` → 0 in both files after the resolution.
- vet before/after the port: `h.waitDraws undefined … chat_flow_rows_test.go:232:4`
  → `go vet ./...` rc=0.
