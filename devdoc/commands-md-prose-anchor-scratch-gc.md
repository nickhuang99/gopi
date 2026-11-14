<!-- doc-class: normative -->
# The scratch-gc prose anchors in COMMANDS.md: `:512` was never what it pointed at

**Task** `01a130ba-4ed0-728a-8e16-ec74b2a20de8` · worktree
`gopi-01a130ba4ed0728a8e16ec74b2a20de8` · baseline `d62542d`.

## What was wrong

`COMMANDS.md` carried TWO hand-written (prose) anchors to
`cmd/agent/cli_scratch_gc.go:512`, both of which passed
`TestCommandsDocAnchorsResolve` and neither of which pointed at the line it
claimed:

- §3.6 line 309 — `"sweep the suite scratch home ONCE and exit (watchdog face)"`
- §4 line 345 — `the one-shot sweep, reporting first`

The anchor gate cannot see this, and that is by construction, not by accident:
`commandsDocAnchorFindings` (`internal/standards/commands_doc_gate_test.go`) fires
on exactly three things — a line number past EOF, a reference to a BLANK line, and
the read-floor (`commandsDocMinAnchors = 40`). Its own doc comment says it:
"ANCHORS RESOLVE — every `file.go:NNN` reference points at an existing, non-blank
line". *Resolvable* is not *true*, and a line inside a usage block is never blank,
so the anchor stayed green while pointing at the wrong text.

## The evidence chain

```
$ git log --format=%h -S'cli_scratch_gc.go:512' -- COMMANDS.md
a82cce382          # fix(scratch-gc): reach the scratch dump class trees from the family census

$ git show a82cce382 -- COMMANDS.md | grep 'cli_scratch_gc.go:'
-  face)" (`cmd/agent/cli_scratch_gc.go:507`); the reclaimer rides ticks that
+  face)" (`cmd/agent/cli_scratch_gc.go:512`); the reclaimer rides ticks that
-| clean up the scratch home | ... (`cmd/agent/cli_scratch_gc.go:507`) |
+| clean up the scratch home | ... (`cmd/agent/cli_scratch_gc.go:512`) |     # the SAME commit moved it

$ git show a82cce382:cmd/agent/cli_scratch_gc.go | sed -n '512p'   # what :512 was WHEN WRITTEN
              --scratch-gc-stale-min N, this variable, or settings.json

$ sed -n '512p' cmd/agent/cli_scratch_gc.go                        # what :512 is TODAY
  gopi tool scratch-gc deps       # REPORT what is scheduled and resident on this host:
```

So the reference was stale the moment it was written — `a82cce382` bumped
`:507 → :512` against a file it had just grown by five lines above, and `:512`
was then (and is now) a line of the verb's **usage help text**, never the
declaration the prose describes.

## The lines that actually carry the quoted wording

```console
$ sed -n '363p' cmd/agent/cli_scratch_gc.go
const scratchGCUsageFingerprint = "gopi tool scratch-gc — sweep the suite scratch home ONCE and exit"
$ sed -n '394p' cmd/agent/cli_scratch_gc.go
  gopi tool scratch-gc --dry-run  # report what WOULD be removed; deletes nothing
```

`:363` is the *quoted sentence itself* — `scratchGCUsageFingerprint`, the
verbatim source of §3.6's `"sweep the suite scratch home ONCE and exit"` (which is
why that §3.6 row keeps its quotation marks). `:394` is the `--dry-run` line of the
usage block, the line that describes the §4 cell's `gopi tool scratch-gc --dry-run`
/ "reporting first".

Three candidates were measured before choosing (`sed -n` on each): `:363`, `:386`
(`var scratchGCUsage = scratchGCUsageFingerprint + ...`, the usage block's opening
line), and `:394`. `:386` was rejected — it is the *assignment* of the help text,
not any text about the sweep; `:512` — which is also inside that block — was
rejected for the same reason. The chosen pair are the two lines whose own words
match the cells they sit in.

## The change

Two prose anchors (`§3.6` and `§4`), PLUS the generated-block row the same task
family had left stale:

```diff
 COMMANDS.md
-| `gopi scratch-gc` | routed only `gopi tool scratch-gc` | `cmd/agent/cli_scratch_gc.go:572` |
+| `gopi scratch-gc` | routed only `gopi tool scratch-gc` | `cmd/agent/cli_scratch_gc.go:614` |
-  face)" (`cmd/agent/cli_scratch_gc.go:512`); the reclaimer rides ticks that
+  face)" (`cmd/agent/cli_scratch_gc.go:363`); the reclaimer rides ticks that
-| clean up the scratch home | ... (`cmd/agent/cli_scratch_gc.go:512`) |
+| clean up the scratch home | ... (`cmd/agent/cli_scratch_gc.go:394`) |
```

The generated block was **NOT hand-edited** — it was produced by the document's
one writer (`go run internal/standards/testdata/commands-census.go . COMMANDS.md`),
which rewrote exactly that one row (`:572 → :614`, `isScratchGCSubcommand`, the
predicate the generator publishes) and provably left both prose lines alone
(`diff` between the hand-edited file and the regenerated one shows row 135 only;
the regeneration is idempotent — a second run prints "already up to date" with an
unchanged md5).

## Why `:614` and not `:572`

The block publishes the verb's **routing predicate**, read from the source by
`internal/cliindex` (not a line typed by hand): `sed -n '614p'` is
`func isScratchGCSubcommand(args []string) bool {`. `:572` is the middle of
`scratchGCUsage`'s `families` help text. `:571` (`PROJECT.md Rule 15)…`) shows how the
row was derived: a human appears to have read the *previous* published anchor
(`:530`) and taken the next line matching "the scratch-gc implementation", landing
on help prose instead. That part is inference and is marked as such — the
measured facts are the two `sed -n` outputs and the generator's own render.

## Baseline red (pre-existing, not introduced here)

On the **untouched baseline** `d62542d`, with only the baseline `COMMANDS.md` in the
tree (`git checkout d62542d -- COMMANDS.md`):

```
$ go run internal/standards/testdata/commands-census.go . COMMANDS.md --check
commands-census: COMMANDS.md disagrees with the source — re-run without --check to regenerate
exit=1

$ go test ./internal/standards/ -count=1 -run 'TestCommandsDoc'
--- FAIL: TestCommandsDocGeneratedBlockMatchesSource (3.47s)
    got:  line 29: | `gopi scratch-gc` | … | `cmd/agent/cli_scratch_gc.go:572` | …
    want: line 29: | `gopi scratch-gc` | … | `cmd/agent/cli_scratch_gc.go:614` | …
--- FAIL: TestCommandsDocGateRedProofStaleGeneratedBlock (4.00s)
    commands_doc_gate_test.go:691: precondition: the committed document's block is already stale
FAIL
```

So the generated-block row was **already red before this task touched anything**
(filed separately as `#01a130ae-343e-7667-b17c-f43df8f59349`, completed but with its
removal skipped). Regenerating it here is the fix that row asked for, not a
side-effect of the anchor work — the two are in one commit only because both are
edits to the same file and the generator runs over the whole document.

## Not done (honest)

The **gate criterion gap itself is not touched here.** `commandsDocAnchorFindings`
asserting "resolvable" rather than "points at the claimed declaration" is a
system-layer change (`internal/standards`), which Rule 28 puts on peerbox; this task
was scoped to re-pointing the anchors and to producing the `sed -n` evidence. No
gate was relaxed, and no gate was edited.

Also left alone, deliberately: the same two-prose-anchor pattern in the
neighbouring §3.6/§4 rows. Several point at a handler, subcommand predicate or an
interior line of a file rather than at the declaration their prose names
(`cli_archive.go:133` is a *comment* about `tools.ArchiveReasons`;
`cli_doccheck.go:118`, `cli_proxy.go:104` and `cli_ledger_window.go:77` point at
predicates). That is the same defect class at a different address; it is a
separate dispatch, not part of this hand-over.

## Verification

- `go build ./...` — OK
- `go test ./internal/standards/ -count=1` — see `task-report.md` (before/after, both recorded)
- `go run internal/standards/testdata/commands-census.go . COMMANDS.md --check` — exit 0, "in step with the source (44 verbs)" (it was exit 1 on the untouched baseline)
- `sed -n '309p' / '345p' COMMANDS.md` — the two re-pointed anchors, printed beside `sed -n '363p' / '394p' cmd/agent/cli_scratch_gc.go`
- `git status --porcelain` — clean after the commit (Rule 30)

Raw copies of every command above and its output:
`~/.gopi/sessions/01a130ba-4ed0-728a-8e16-ec74b2a20de8/artifacts/`.
