<!-- doc-class: historical -->
# `gopi task merge`: the page and the parser now agree — and the refusal explains itself

Task `01a0ecea-2e9e-76d7-aca0-a8ac34efc6d0`. Class: help-vs-behaviour defect (the
page advertised flags the parser refuses), fixed on the page and at the refusal.

## The defect, measured

`gopi task merge` and `gopi merge` are ONE engine (`merge_run.go`) behind TWO
parsers: the canonical verb's `parseMergeArgs` reads nine flags, while the family
parser's table `taskFlagMember` (`cmd/agent/cli_tasks.go`) admits four for this
verb — `--all`, `--repo`, `--submit`, `--db`. The atom's `-h` page nevertheless
printed the canonical flag block under the caption

```
THE MERGE FACE'S FLAGS (identical at 'gopi merge ...'):
```

so an operator reading the page typed `gopi task merge --status` and got
`unknown flag --status` plus the ~20 KB family usage blob on stderr — a refusal
that did not contain the answer ("that flag is `gopi merge`'s; the door is
there"). Both halves were wrong: the page over-advertised, and the refusal did
not explain.

## The fix — three surfaces, one list

1. **The page tells the truth** (`cli_tasks_merger_help.go`).
   - `taskMergeUsageHead` opens by saying the two spellings share the engine but
     NOT the flag set, and that the canonical flags are refused here by name.
   - The atom's OWN flag rows are now rendered by `mergeAtomFlagLines()` from the
     four `mergeAtom*FlagName` constants (`cli_tasks.go`) — every row goes through
     `taskFlagMember(taskverbs.Merge, …)` before it prints, so the page can only
     advertise what the parser admits.
   - The canonical block is labelled `THE CANONICAL FACE'S FLAGS — its OWN page
     is where they are documented:` with the sub-caption
     `read by 'gopi merge'; REFUSED by 'gopi task merge':`, and every row is
     spelled `gopi merge --status` (canonical prefix), never bare — the row says
     whose flag it is wherever it is rendered.
   - `THE DOORS FOR THE TWO REPORT-ONLY ARMS:` names `gopi merge --status` and
     `gopi merge --fast` as the spellings that carry those two arms, with a short
     description each and a pointer to `gopi merge -h` for the full contract.
   - One table (`mergeFaceFlagRows()`) feeds the generated canonical block and the
     door lines, so no two renderings can describe one flag differently.

2. **The refusal explains itself** (`cli_tasks.go`). After `parseTasksArgs`
   fails, if `verb == merge` and argv carries a canonical-only spelling, the atom
   prints ONE line instead of the family blob:

   ```
   gopi task merge: --status is gopi merge's flag, not this atom's — the atom reads
   only --all/--repo/--submit/--db. See 'gopi merge -h' for what it does, or
   'gopi task merge -h' for what this verb admits.
   ```

   (One line, no wrap: it is what an operator greps out of a post-receive hook's
   log.) The pointer fires ONLY for a spelling the canonical table really owns —
   `--bogus` keeps the ordinary refusal — and the atom's admitted set is rendered
   from the same predicate, never typed.

3. **Gates pin both directions** — the atoms' half lives in
   `cmd/agent/cli_tasks_merger_help_atoms_test.go` and the canonical face's refusal
   half in `cmd/agent/cli_tasks_merger_help_surface_test.go` (the split is epic
   merge-removal item 3's repair, 2026-10-31: the two populations outlive each
   other differently, so a whole-file deletion may not take the survivors with it):
   `TestTaskMergeFlagSetsAreAttributedTheWayTheParsersBehave` (advertised-here ⇔
   `taskFlagMember`; every canonical + family flag either admitted here or clearly
   attributed to the engine), `TestTaskMergeAdmissionTableNamesNoBareFlagLiteral`
   (no bare flag literal in the admission path), `TestTaskMergeFlagAttributionGateIsNotBlind`
   (red-proof) — all three in the atoms file — plus, in the surface file,
   `TestTaskMergeCanonicalFlagPointerExplainsItself` (flag named + the engine's
   contract + this atom's page) and `TestTaskMergeCanonicalFlagRefusalOnTheRealBinary`
   (drives the built binary with the three argv shapes).

## Two traps worth remembering

- **A row's form carries its ARGUMENT PLACEHOLDER.** `mergeFaceFlagRows()` spells
  `--every D`, so comparing the row's form against an operator's `--every` matched
  NOTHING: the first version of the pointer served `--status` but let `--every 5m`
  fall through to the family blob. `mergeFaceFlagName()` cuts at the first space.
  Regression test: the real-binary gate iterates the table and drives every row.
- **`-h`/`--help` are not "refused canonical flags".** The family usage scan
  answers `gopi task merge -h` with this atom's own page and exit 0, so
  `mergeCanonicalOnlyFlag` exempts the help pair at the predicate (one stated
  exception), and it exempts it because `mergeFaceFlagRows()` carries the row for
  the canonical page too.

## Not done (honest)

- The alternative resolution — making `task merge` a true alias of `gopi merge` by
  teaching the canonical parser `--repo/--submit/--all/--db` — is NOT attempted.
  That is merger/system-layer work (Rule 28, peerbox's half) and exceeds this
  session's budget; the chosen resolution makes the page honest instead.
- The refusal still exits after the FIRST canonical-only flag it finds (one
  sentence, one flag). Naming several would need a joined sentence; not required
  by the acceptance and not done.

## Verification

See `~/.gopi/sessions/01a0ecea-2e9e-76d7-aca0-a8ac34efc6d0/task-report.md`
(verbatim invocations + observed output).
