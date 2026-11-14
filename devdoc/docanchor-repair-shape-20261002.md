<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# Devdoc: master-red `TestDocAnchorsResolveInNormativeDocs` — the repair shape, measured

**Task:** 01a101eb-70e6-730e-9c29-cdbb725be861 (branch `task/01a101eb70e6730e9c29cdbb725be861`)
**Tree measured:** master tip `ec8f9b97` (`docs(g5): re-point COMMANDS.md and re-derive
the chat/web flag census for --film-terminal`), this run's worktree.
**Verdict of the epic:** DOABLE — two findings, both repairable inside one session.

## 1. What was red, re-measured here (not carried over from the briefing)

The briefing's long drift list (measured at `a98e62e0`) is **stale**: three commits
landed between it and this tree — `d0184dce` (54 anchors re-keyed by symbol + 15
permits deleted), `0b2ab3be` (the remainder recorded as permits), `53635229` (three
pre-existing gate reds). Re-running the gate on `ec8f9b97` leaves exactly two
findings, both reproduced here:

```
$ CGO_ENABLED=0 go test ./internal/agent/ -run 'TestDocAnchor' -count=1     # BEFORE
--- FAIL: TestDocAnchorsResolveInNormativeDocs (1.18s)
    docanchor_test.go:203: devdoc/chat-status-row-partition.md: anchor cmd/agent/cli_chat.go:535
        points at a BLANK line of .../cmd/agent/cli_chat.go — the anchor drifted off its symbol
    docanchor_test.go:223: stale baseline entry
        "devdoc/requirements/04-sync-and-nodes.md|blank|(bare) internal/config/config.go:612" ... delete the entry
    docanchor_test.go:227: doc anchors verified: 2849 across 620 normative docs (2361 explicit + 488 bare
        continuations; 568 bare out of row/table/heading scope; 232 pre-existing drift entries permitted,
        1 new, 1 stale permits)
FAIL	github.com/nick/gopi/internal/agent	1.657s
```

## 2. The decision: (b) for the live doc, (a) for the landed records — (c) was NOT added

The three candidate shapes are not alternatives covering one case; each answers a
different question, and the corpus already separates them:

- **(b) re-key the live doc by SYMBOL** — the rule for a doc that makes a
  **standing claim about current master**.
  `devdoc/chat-status-row-partition.md` is exactly that: its manifest row reads
  `normative: standing claim about master that names code that resolves (anchor);
  standing 2 / record 0` (`devdoc/DOC-INDEX.tsv:236`), and it carries no dated
  header — it describes the row's partition in the present tense. Its `model` row
  cited `cmd/agent/cli_chat.go:535` with the symbol `newChatStatusSource(ag.Model, …)`
  named in the same table row. `:535` is blank today; the symbol's definition is
  `func newChatStatusSource` at `cmd/agent/chat_stream.go:1220` (code_search) and its
  call site in the chat REPL is `cmd/agent/cli_chat.go:2302`
  (`status := newChatStatusSource(ag.Model, sess.toolsEnabled)`). The anchor was
  re-pointed at **2302 by symbol**, never by an arithmetic shift.
- **(a) allow-list the landed records** — that is what the permit list already is,
  and it is a **living** artifact, not a frozen one: `docAnchorBaselineRel` entries
  are keyed `doc|kind|detail`, logged on every run (`BASELINE DRIFT (…)`), and the
  gate fails a permit that matched nothing (the ratchet-down Rule 13 describes).
  Landed records — docs whose header pins base-master/date, here
  `devdoc/requirements/04-sync-and-nodes.md` whose front matter reads
  `**Measured:** 2026-09-17` — must **not** be re-keyed: re-keying a dated record
  makes it describe a tree it never measured. **Zero new permits were added by this
  change**; the list went 232 → 231.
- **(c) exempt historical docs by class — NOT added, because it already exists and
  does not apply.** The gate's scope *is* the manifest class: `620 normative
  markdown docs gated, 1097 historical markdown docs deliberately not gated`
  (`docanchor_test.go:617`, `TestDocAnchorScopeComesFromTheManifest`). Measured on
  the permit list itself: **all 73 docs named by a permit row are `normative`, zero
  are `historical`** (`cut -f1 internal/agent/testdata/docanchor-baseline.tsv |
  sort -u`, classified through `devdoc/DOC-INDEX.tsv`). So class-based exemption
  would have exempted **nothing** the two findings touch, while requiring a
  re-classification of docs the manifest calls standing claims (04's own row:
  `a landed ruling: cited by briefings/brief-bug-ledger-divergence.md:112`) — i.e.
  a *rewrite of the gate's scope* to fix an anchor, which is Rule 27's
  delete/skip-the-test in softer clothing. No new mechanism was needed, so none
  was added: `internal/agent/docanchor_test.go` is **byte-identical** in this
  change.

## 3. The stale permit: why deleting it was the repair, not a bypass

A stale permit means "the key no longer matches anything", and here the cause is
measurable rather than a guess. `53635229` added the row because
`(bare) internal/config/config.go:612` was a BLANK line then; master then grew
`internal/config/config.go` above it:

```
$ git show 0b2ab3be:internal/config/config.go | wc -l   # 1576 lines (row added here)
$ git show 0b2ab3be:internal/config/config.go | sed -n 612p
	NotifyAsyncEnv   = "GOPI_TASK_NOTIFY_ASYNC"          # the address shifted again after this row landed
$ git show ec8f9b97:internal/config/config.go | wc -l   # 1600 lines
$ sed -n 612p internal/config/config.go
	// (GOPI_TASK_NOTIFY_EVENTS); "all" = every status plus every synthetic
```

At the tip `:612` is non-blank, so the `blank` key fires for nobody and the permit
is stale **by the gate's own definition** (the row was added by `53635229`, where
`internal/config/config.go` was 1585 lines and `:612` was blank; the drift
re-opened when `7e5678f4` grew the file to 1600) — the doc's anchor there is still a
drifted citation (it is the `internal/config/config.go:311`/`:612 merges all three`
half of the O2 finding, §5), but a permit is a permission for a *firing* finding,
not a bookmark. It was deleted; the row count only shrank.

## 4. Before / after, one command

```
$ CGO_ENABLED=0 go test ./internal/agent/ -run 'TestDocAnchor' -count=1 -v       # AFTER
--- PASS: TestDocAnchorsResolveInNormativeDocs (3.70s)
    docanchor_test.go:227: doc anchors verified: 2849 across 620 normative docs (2361 explicit + 488 bare
        continuations; 568 bare out of row/table/heading scope; 231 pre-existing drift entries permitted,
        0 new, 0 stale permits)
--- PASS: TestDocAnchorBaselineHasNoDuplicateKeys (0.00s)
    docanchor_test.go:408: internal/agent/testdata/docanchor-baseline.tsv: 231 permit rows, no duplicate key, no duplicate line
--- PASS: TestDocAnchorBaselineDuplicateGuardCatchesDuplicates (0.00s)
--- PASS: TestDocAnchorBareLineRules (0.00s)
--- PASS: TestDocAnchorScopeComesFromTheManifest (0.03s)
ok  	github.com/nick/gopi/internal/agent	5.233s
```

Permit rows: **232 → 231** (one deleted, none added). Explicit + bare anchors
resolved: **2849 → 2849** (the re-key moved an anchor, it removed none).

## 5. Not Done (honest)

- The O2 citation in `devdoc/requirements/04-sync-and-nodes.md:796`
  (`internal/config/config.go:311`/`:612 merges all three`) is **still substantively
  wrong**: `:612` is now a `GOPI_TASK_NOTIFY_EVENTS` comment and the merge code the
  finding measured is `ResolveSyncPeers` (`internal/config/config.go:763`). That
  doc is a **dated record** (`**Measured:** 2026-09-17`), so re-keying it is the
  decision its own permit reason already records — *"the band is re-keyed when the
  finding is re-measured, not when an unrelated edit moves a line"*. It is left
  re-measurable, not silently "fixed": the exact path and line are named here so the
  re-measurement task needs no archaeology.
- The gate does not assert a line's CONTENT (by design, stated in
  `docanchor_test.go`): a re-keyed anchor is only as good as the symbol named
  beside it in the same row. Both rows repaired/reviewed here name their symbol.

## 6. Re-verified after aligning with master (Rule 30a: the sync runs LAST and proves nothing on its own)

Master advanced during this run (`ec8f9b97` → `c338b3a8`, three commits: a
`--source doc` verb-index re-derivation, `chat-provenance.md` plus its two test
files, and a master-merge). The delivery-point sync was run at the end and the
gate was then re-measured ON the merged tree, because an alignment is a statement
about a merge, not about the work:

```
$ git merge master --no-edit        # in the worktree
$ git status --porcelain            # prints nothing
$ CGO_ENABLED=0 go test ./internal/agent/ -run 'TestDocAnchor' -count=1
ok  	github.com/nick/gopi/internal/agent	2.443s
$ CGO_ENABLED=0 go test ./internal/agent/ -run 'TestDocAnchorsResolveInNormativeDocs' -count=1 -v | grep 'anchors verified'
    docanchor_test.go:227: doc anchors verified: 2849 across 620 normative docs (2361 explicit + 488 bare continuations; 568 bare out of row/table/heading scope; 231 pre-existing drift entries permitted, 0 new, 0 stale permits)
$ git diff master -- internal/agent/docanchor_test.go | wc -l   # 0 — the gate is untouched
$ echo master=$(git show master:internal/agent/testdata/docanchor-baseline.tsv | grep -vc '^#') branch=$(grep -vc '^#' internal/agent/testdata/docanchor-baseline.tsv)
master=232 branch=231
```

The merge touched none of the three repair sites
(`git diff --stat master -- internal/agent/testdata/docanchor-baseline.tsv devdoc/chat-status-row-partition.md`
→ 1 file changed, 1 deletion for the permit list and the doc row), so the numbers
stand at the integration point: **0 new, 0 stale, 231 permits, the branch one row
below master's 232.**

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK (exit 0)
- Tests: `make test-budget PKGS='./internal/agent ./internal/docclass'` →
  `exit=0 outcome=pass` (stamp 2026-10-03T21:36:49+08:00, elapsed 46s)
- Ran: `CGO_ENABLED=0 go test ./internal/agent/ -run 'TestDocAnchor' -count=1 -v`
  (verbatim, in the worktree) — and, for the manifest rows the re-key and this
  report own, `python3 tools/docclass/classify.py . --only
  devdoc/chat-status-row-partition.md`, `python3 tools/docclass/classify.py .
  --only devdoc/docanchor-repair-shape-20261002.md`, then `python3
  tools/docclass/classify.py . --check` and `python3 tools/docclass/banner.py .
  --check`.
- Result: gate exit **0**, summary `… 231 pre-existing drift entries permitted,
  0 new, 0 stale permits`; `go build` exit 0; budget run `outcome=pass`;
  `classify.py --check` → `0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv`
  (exit 0); `banner.py --check` → `0 markdown docs whose banner disagrees with the
  manifest`; `gopi doccheck` → "the devdoc corpus is consistent — every file under
  devdoc/ has a well-formed row". The manifest diff is exactly two lines: row 236,
  whose evidence cell now reads
  `anchor cmd/agent/cli_chat.go:2302 at status := newChatStatusSource(ag.Model, sess.toolsEnabled)`,
  and this report's own row (`historical (3 standing / 2 record)`).
  (`classify.py --only` rewrites the whole manifest by construction — it is the
  sanctioned writer — so the diff was verified to carry no other line.)

## Evidence (self-filed)

- Before-run red: `/home/user/.gopi/sessions/01a101eb-70e6-730e-9c29-cdbb725be861/gate-run.txt`
  — `FAIL github.com/nick/gopi/internal/agent  1.657s`, summary `232 permitted, 1 new, 1 stale`.
- After-run green: `…/gate-run-v.txt` — `ok github.com/nick/gopi/internal/agent 5.233s`, summary `231 permitted, 0 new, 0 stale`.
- Permit list row count: `grep -vc '^#' internal/agent/testdata/docanchor-baseline.tsv` → 232 (before) / 231 (after).
- Class census of the permit list's docs: 73 distinct docs, **73 normative / 0 historical** (join against `devdoc/DOC-INDEX.tsv`).
- Gate scope: `docanchor_test.go:617` → `620 normative … 1097 historical … deliberately not gated`.
- Corpus consistency after the manifest update: `python3 tools/docclass/classify.py . --check` → `0 gap(s)`; `python3 tools/docclass/banner.py . --check` → `0 markdown docs whose banner disagrees with the manifest`.
- `internal/agent/docanchor_test.go` unchanged: `git status --porcelain` lists only the doc, the permit list, the manifest and this report.
