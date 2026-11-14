<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# Task Report: the undispatched-tool-word gate read every row as "refuses itself" (master b7b39a57f)

## Completed

- **The defect, measured on master.** `go test ./cmd/agent/ -run TestUndispatchedToolWordsNameADispatchingCanonical`
  failed with exactly **8 red lines**, one per row of `undispatchedToolWords`, every one reading
  `"<word>" refuses itself` at `cli_undispatched_tool_words_test.go:62` — the whole table, not one bad row.
- **Root cause = field-semantics drift, not a code defect.** commit `844d1f3d2` re-pointed each row's
  `canonical` field from *the command word that dispatches this spelling* to *an address fragment*
  (`code_search` → `code-search`, `web-search` → `web_search`), because the address the refusal prints is
  now the `tool` group line carried in `homeProg`. The assertion was still comparing the two FIELDS:
  once `canonical` is an address fragment, `dashTwin(canonical) == dashTwin(word)` is **true by
  construction** for every row — a tautology, so all 8 lines went red while the behaviour stayed correct.
- **The refusal behaviour is correct and was left alone** — `cli_undispatched_tool_words.go` is untouched
  by this branch (verified: the file is not in the diff). Each refusal exits 1 and prints an address that
  really dispatches; the sibling guard `refusalAddressDispatches` was green on master throughout, which is
  the same evidence from the other side.
- **Fix — the assertion made form-independent.** `cli_undispatched_tool_words_test.go:62`'s
  `dashTwin(e.canonical) == dashTwin(e.word)` became

  ```go
  if e.refusalAddress() == programAddress(e.word) {
  ```

  i.e. *the address a refusal prints must not be the refused spelling itself* — a row printing
  `gopi <the word>` would correct the reader straight back into the refusal they just answered, a dead end
  (Rule 15). The property holds for every row shape in the table (a `tool` group address, a flat
  successor, and a row with no `homeProg` at all) and depends on no spelling convention.
- **Comment rewritten** to state that `canonical` is an address fragment, to name the tautology that made
  the old assertion vacuous, and to point at the guard that actually answers the shadowing question
  (`dispatched[e.word]`, kept).
- **Both guards preserved and still load-bearing**: `refusalAddressDispatches(t, e.refusalAddress())`
  (the printed address is one this binary RUNS — Rule 31) and `dispatched[e.word]` (a refused word must
  not be reachable, or the command would be unreachable).
- **A second, independent master-level drift was found and repaired**: `go test ./internal/standards/`
  was RED on `TestCommandsDocGeneratedBlockMatchesSource` — `COMMANDS.md`'s generated verb index cited
  `cmd/agent/cli_tasks.go:465` where the source now says `:468` (commit `7c32e88f4` moved lines in
  `cli_tasks.go` without regenerating). Repaired by running the gate's own prescribed generator, not by
  hand: the diff is **one line**. Without this the acceptance criterion "`go test ./internal/standards/`
  green" could not be met from this base.
- **Report** written and committed with the change.

## Not Done

- **Nothing deferred.** The code file was not modified, no property was weakened, and the refusal
  vocabulary (`undispatchedToolWords`) was not touched.
- **Honest scope note on the standards repair**: the `COMMANDS.md` line is a pre-existing defect of the
  base, not of this task's subject. It is carried here only because it gates the acceptance run; it is
  the generator's own output, byte for byte.
- **No repair was made to the process that let 8 red lines sit on master**: commit `844d1f3d2` changed a
  field's semantics and the consumer test's tautology went unnoticed because a tautology fails only when
  it fails — it passed until the field moved. A gate that pins a *semantic* property of a field rather
  than a spelling relation between two fields is the durable countermeasure, and this branch is where it
  now exists; auditing the other 3 readers of `canonical` for the same hazard was out of scope and is
  not done.
- **A documentation drift found in passing, not repaired** (Rule 36's own text is out of this task's
  scope): the rule spells the corpus checker `gopi doccheck`, but on this binary that spelling is
  **retired** — the live one is `gopi tool doccheck` (verified this run: `gopi tool doccheck` → exit 0,
  `doccheck: the devdoc corpus is consistent — every file under devdoc/ has a well-formed row`). Filed
  here rather than silently corrected, because the phrase lives in `GOPI.md`, whose edits are not this
  branch's business.

## Verification

- Build: `CGO_ENABLED=0 go build -o "$SB/gopi-vf" ./cmd/agent/` → `BUILD_OK` (`$SB` = the Rule-34 sandbox named below; the binary is built from THIS branch's source and kept OUT of the worktree, Rule 30)
- Tests: `go test ./cmd/agent/ -run 'TestUndispatchedToolWords|TestUnDispatched' -count=1 -v` →
  `--- PASS: TestUndispatchedToolWordsNameADispatchingCanonical (7.42s)`, `ok github.com/nick/gopi/cmd/agent 9.894s`
  (`TestUndispatchedToolWordsNameADispatchingCanonical` is the only test the pattern matches — verified
  with `-v`: one `--- PASS`, no `--- FAIL`; the master red was the same single test with 8 `t.Errorf` lines.)
- Tests: `make test-budget PKGS='./internal/standards/'` → `exit=0 outcome=pass`, elapsed 113s
  (before the `COMMANDS.md` regeneration: `exit=1 outcome=fail`, `--- FAIL:
  TestCommandsDocGeneratedBlockMatchesSource`, `--- FAIL: TestCommandsDocGateRedProofStaleGeneratedBlock`).
- Tests: `go test ./internal/standards/ -count=1` (re-run at close-out, on the final tree) →
  `ok github.com/nick/gopi/internal/standards 141.488s`
- Ran: `gopi tool doccheck` → exit 0, `doccheck: the devdoc corpus is consistent — every file under devdoc/
  has a well-formed row` (Rule 36: this report's own row is in `devdoc/DOC-INDEX.tsv`, committed with it)
- **Ran: (Rule 31 — each refusal address driven for real, in a private `GOPI_HOME` inside a Rule-34 sandbox, against the freshly built binary)**

  ```
  SB=$(gopi tool sandbox create --label verify-844d1f3d | tail -1)   # Rule 34, the ONE initializer
  CGO_ENABLED=0 go build -o "$SB/gopi-vf" ./cmd/agent/
  VH="$SB/home"; mkdir -p "$VH"
  for w in code_search keyword-search vision-analyze screen-capture \
      ui-state voice-input web-search web-fetch; do \
    msg=$(GOPI_HOME=$VH "$SB/gopi-vf" "$w" 2>&1 >/dev/null); rc=$?; \
    addr=$(printf '%s' "$msg" | sed -n 's/.*spelled `\([^`]*\)` on the command line.*/\1/p'); \
    rest=${addr#gopi }; GOPI_HOME=$VH "$SB/gopi-vf" $rest -h >/dev/null 2>&1; \
    echo "$w | refuse_rc=$rc | addr=[$addr] | -h_rc=$?"; done
  ```

  Run from `~/workspace/gopi-01a113e32a6b747b9c0ed7ef029e950b` (this branch's own worktree). `sandbox
  create` printed, verbatim:

  ```
  SB=/home/user/workspace/gopi-sandbox/sandbox-verify-844d1f3d-b934efad
  sandbox /home/user/workspace/gopi-sandbox/sandbox-verify-844d1f3d-b934efad created=true reused=false
    root=/home/user/workspace/gopi-sandbox source=from the built-in default
    ledger=…/sandbox-verify-844d1f3d-b934efad/.gopi/memory.db owner=pid=745662 plugins=/home/user/.gopi/plugins
  ```

  **Do not verify against the MAIN checkout's `bin/gopi`** — the binary must be built from THIS branch's
  source, which is why the build writes into the sandbox rather than into the repo.

  The address is **not typed from the table** — it is parsed out of the refusal the binary itself printed,
  which is the only form that proves the printed address (what the test asserts on) is runnable.
- Result: every refused word exits **1** and every printed address dispatches, **exit 0**:

  | refused word | refusal exit | address the refusal printed | `gopi <address> -h` |
  |---|---|---|---|
  | `code_search` | 1 | `gopi tool code-search` | 0 |
  | `keyword-search` | 1 | `gopi tool keyword_search` | 0 |
  | `vision-analyze` | 1 | `gopi tool vision_analyze` | 0 |
  | `screen-capture` | 1 | `gopi tool screen_capture` | 0 |
  | `ui-state` | 1 | `gopi ui_state` | 0 |
  | `voice-input` | 1 | `gopi tool voice_input` | 0 |
  | `web-search` | 1 | `gopi tool web search` | 0 |
  | `web-fetch` | 1 | `gopi tool web fetch` | 0 |

  The private `GOPI_HOME` minted **0 session directories** across all 16 invocations — `find "$VH"
  -mindepth 1` printed nothing and `$VH/.gopi` does not exist at all, i.e. the refusal spends no run.
  (One earlier probe form produced a doubled `gopi gopi …` argv and therefore exercised the
  **prompt path**; both home directories were measured afterwards and were **empty**, then removed.)

  The sandbox is Rule 34's own shape (`gopi tool sandbox create`, retired spelling `gopi sandbox`
  refused — `gopi tool sandbox -h` is the live one; the sandbox root came from the built-in default,
  `source=from the built-in default`), and it holds the verification binary + the private profile, so
  **nothing of the verification touches this worktree** (Rule 30). It is reclaimed by the existing
  sweep; no cleanup hook was written.
- Result (red proof of the NEW assertion — a test that cannot fail is not a test): a temporary row
  `{word: "frobnicate-self", canonical: "frobnicate-self"}` injected at the head of the table, then
  `go test ./cmd/agent/ -run TestUndispatchedToolWordsNameADispatchingCanonical` →
  `--- FAIL` with *both* guards firing on it —
  `cli_undispatched_tool_words_test.go:72: "frobnicate-self" refuses itself — the address its refusal
  prints ("gopi frobnicate-self") IS the refused spelling …` and `:81: … which this binary does not run`.
  `cli_undispatched_tool_words.go` was then restored from backup and is byte-identical to `HEAD`
  (it does not appear in `git status --porcelain`).
- Result (tree state): `git status --porcelain` after commit → prints nothing (Rule 30).
- Commit: `6f20bdd87` (the fix), `e13353b4a` (this report), `61bc4691b` (this report's evidence section, re-pinned to
  the Rule-34 sandbox command actually run and to the `-v` run's own PASS line — a report revision, not a fix)

## Delivery-point sync (Rule 30a — run LAST, after the work was committed)

The branch was aligned with master only after the fix, the report and the evidence edits were committed:
`git merge --no-edit master` → `5530c8d73` (no conflict; master had advanced by `ac35c924f fix(help): gopi -h
drops the one row that could not keep its own -h promise (gopi merger)` + a merge). Re-run after the merge:

- `go test ./cmd/agent/ -run 'TestUndispatchedToolWords|TestUnDispatched' -count=1` →
  `ok github.com/nick/gopi/cmd/agent 12.465s`
- Rule 31, re-done on the MERGED source (same Rule-34 sandbox, binary rebuilt → `BUILD_OK`): each of the 8
  refused words still exits `refuse_rc=1`, and the address its own refusal prints still runs `-h_rc=0`
  (`gopi tool code-search`, `gopi tool keyword_search`, `gopi tool vision_analyze`, `gopi tool screen_capture`,
  `gopi ui_state`, `gopi tool voice_input`, `gopi tool web search`, `gopi tool web fetch`).
- `go test ./internal/standards/ -count=1` after the merge → **1 FAIL**, and it is MASTER's own:
  `--- FAIL: TestDocClassBannerMatchesManifest … devdoc/cli-help-user-audit.md: carries the "historical" banner but
  devdoc/DOC-INDEX.tsv classes it "normative"`. Measured on a pristine master tree with no branch content at all
  (`git archive master | tar -x -C <scratch>` → same test → `FAIL … 1163 markdown docs` vs the merge's `1164`) —
  the defect rides master's own `ac35c924f`, which added the doc and its row. It is covered by the already-filed
  pending row `01a112ce-0fd7 fix(standards): 5 pre-existing red gates on master — stale COMMANDS.md census,
  DOC-INDEX evidence/banner mismatches, …`. The gates this change touches are green on the final tree:
  `go test ./internal/standards/ -run 'TestCensusGate|RepoHygiene|TestDocIndex' -count=1` → `ok … 1.161s`.
  `python3 tools/docclass/classify.py . --check` → `0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv` (exit 0):
  the corpus has no missing row; the red is a CLASS disagreement, not a coverage gap.
- `gopi tool doccheck` → exit 0 (the doc crossed the merge with its row intact, `devdoc/DOC-INDEX.tsv:1855`).

## Evidence (self-filed)

- Red on master, observed before any edit:
  `go test ./cmd/agent/ -run TestUndispatchedToolWordsNameADispatchingCanonical` →
  `--- FAIL … 8 lines at cli_undispatched_tool_words_test.go:62`, `FAIL github.com/nick/gopi/cmd/agent 20.888s`.
- Tautology claim, observed: the 8 red lines' own text — `"ui-state" … (canonical "ui_state")`,
  `"web-search" … (canonical "web_search")` — each canonical is the word's dash twin, so the comparison
  was true by construction.
- Pre-existing standards drift, observed: the `got`/`want` pair the gate printed
  (`line 35: | gopi task | argv[0] | cmd/agent/cli_tasks.go:465 |` vs `:468`) with
  `git diff --stat HEAD -- cmd/agent/cli_tasks.go COMMANDS.md` empty — i.e. reproducible without this
  branch's change.
- Generator, not hand-edit: `go run internal/standards/testdata/commands-census.go . COMMANDS.md` →
  `exit=0`, `git diff --stat -- COMMANDS.md` → `1 file changed, 1 insertion(+), 1 deletion(-)`.
- No load was generated and no device was written (Rule 27): every verification above is a test run, a
  build, or a short-lived CLI invocation.
