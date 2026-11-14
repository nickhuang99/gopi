<!-- doc-class: historical -->
# The removal-audit census's devdoc carrier set — the instruction surface, not the corpus

> Standing claim about master: this document describes what
> `internal/standards/removal_audit_shell_gate.go` reads after this change, and
> names the constants that decide it.

## 1. The defect this closes

The census's text half reads the trees a HAND-RUN or a PROSE instruction reaches
the shell from: `scripts/`, `GOPI.md`, the production Go trees, and (since
2026-10-16) the LLM instruction files under `cmd/agent/`. `devdoc/` was outside
it, and that carve-out was stated in prose as "history must stay able to quote
the pre-fix command" — which is true of a RECORD and was silently false of the
merger's own rule set:

- `cmd/agent/merge_prompt.md` cites `devdoc/merger-takeover-branch-lifecycle.md`
  **by section** (`§3.6.1`, cited at merge_prompt.md:39, 191, 230, 322, 492, 515,
  536, 592, 595, 936 …) as the authority its own disposition table implements.
  A child that reads `git branch -d <branch>` in the document its prompt names
  by section runs it — the 2026-10-16 defect one level of indirection deeper.
- The same table shipped the four raw action cells verbatim, and two more sites
  in the same file (`5. ONLY NOW: git branch -D <branch>`, and the
  `mtpRequiredBehaviours` line that pinned it).
- `devdoc/rules-reference.md` — the Tier B half of `GOPI.md`, reached from it by
  `grep -n "## Rule 27" devdoc/rules-reference.md` — carried the Commit Path
  step 6 as `` Remove the worktree + `git branch -d` ``, i.e. the SAME sentence
  this funnel had already removed from `GOPI.md`, living one file over.

## 2. What the corpus now reads — a NAMED set, and why not a class predicate

`removalShellDevdocCarriers` (one `var`, one definition):

| carrier | why it is IN |
|---|---|
| `devdoc/merger-takeover-branch-lifecycle.md` | cited by SECTION from `cmd/agent/merge_prompt.md`; its rows ARE the merger's instructions |
| `devdoc/merge-backlog-terminal-guard.md` | the guard's own standing sentence; cited at `internal/agent/testdata/docanchor-baseline.tsv:64` |
| `devdoc/branch-lifecycle-ruling.md` | cited by name in `merge_prompt.md:742` as standing authority |
| `devdoc/rules-reference.md` | DOC-INDEX-`normative` home of the Commit Path list `GOPI.md` Rule 5 splits out |

The obvious widening — "every devdoc whose `DOC-INDEX.tsv` class is
`normative`" — was measured and rejected:

1. it admits **716** files (2617 devdocs; 1884 `historical`, 716 `normative`, 12
   `roadmap`, 1 `index-meta`), including `devdoc/removal-audit-shell-census.md`
   (whose subject IS the raw verb) and a 900-line orphan-disposition log;
2. it is **vacuous as a filter**: `DOC-INDEX.tsv` classifies a doc from a
   reference the gate would have to re-implement (Rule 14), and a filter that
   reads an empty file set still reports green.

A named list cannot widen by accident, and each member is IN for a stated reason.
Three checks keep it from being vacuous: the floor
(`removalShellMinDevdocCarriers`), the **sentinel by name**
(`removalShellDevdocSentinel`), and a red proof that splices the sentinel itself.

## 3. What stays OUT, measured rather than asserted

```
$ git grep -nE 'git branch -[dD] ' -- 'devdoc/*.md' 'devdoc/**/*.md' | wc -l
65
$ ... | cut -d: -f1 | sort -u | wc -l
27
```

28 devdocs, 69 hits; by `DOC-INDEX.tsv` class: **20 historical / 8 normative**.
(This change's own devdoc is among them — it quotes the verb it is about — which
is worth stating rather than netting out.) Not one of the hits is a disposition a
reader is told to take — every one QUOTES (a run's transcript, git's refusal
text, a `$ ` prompt inside a record, the census of the defect). The four files
whose lines DID read as a disposition (a table cell or an ordered step a child
follows) are exactly the four carriers: after the funnel each reads **0** for
`git branch -[dD] `, and the proof drives one of them red on the real file.

One more out-of-corpus instruction surface, stated so it is not mistaken for an
oversight: `GOPI.md`'s Tier A text at Rule 35 already names the retired HP
autostart script, whose `git branch -D` line lives in
`scripts/hp/hp-worker-autostart.sh`. That file is **deleted** from this tree, and
the command is fenced as a quotation — the gate reads the file only if it is ever
re-shipped, which is the desired behaviour, not a gap.

## 4. A narrowing the widening required — `branch` is not a flag word

With the corpus above, the predicate had to stop reading a **manpage citation**
as a site. `git branch -d` (no argument) is how a doc or a test names the VERB,
and `devdoc/branch-lifecycle-ruling.md` and `devdoc/merge-command.md` both do it
in prose that must not change (see §5). So `removalShellBranchDeleteFlags` gained
`removalShellVerbBranch`: the word `branch` inside the operand list now counts as
the deletion, exactly as the flag does.

What that costs, and why it is not a hole opening:

- the deliberate phrase `-d $BRANCH` (an UPPERCASE shell variable, no braces) is
  **still a site** — it is the shape this half was written from;
- a lowercase `${branch}` was, and is, unresolvable by `removalShellVars` and so
  was never reported (the header already states that honest gap);
- what the exclusion buys is the same rule that already keeps a `#` comment and
  a doc's `git push --force` (a MOVE, not a removal) out of the report: a doc can
  NAME the verb it discusses without the gate reporting the sentence as the act.

## 5. The funnel — a measurement is not rewritten, a prescription is

Funnelled by this change (the ONE writer replaces the raw verb, the measurement
each line carries is kept):

- `devdoc/rules-reference.md` Commit Path step 6, and Rule 9 step 2;
- `devdoc/merger-takeover-branch-lifecycle.md` §3.6.1's four action cells, step 5
  of the rework hand-off, the (c) invariant sentence, and the two acceptance-grep
  transcript lines that quoted the old rows;
- `devdoc/branch-lifecycle-ruling.md`'s (c) delete stage;
- `devdoc/merge-backlog-terminal-guard.md`'s sentence about what the close-out
  runs before writing `completed`.

Kept, deliberately — the doc's MEASUREMENT is its value and the sentence must
continue to print it:

- `devdoc/voice-input-rig-orphan-disposition-01a113e4.md` "Ran: `git branch -D …`
  → **exit=1**" — the refusal IS the finding;
- `devdoc/shared-integration-ref-guard.md` "`git update-ref -d refs/heads/<b>` |
  `git branch -D <b>` | 1 | `Cannot delete branch … checked out at`" — a
  measurement table of what git does;
- `devdoc/removal-audit-shell-census*.md` — the census OF this defect;
- `devdoc/closeout-deletes-undelivered-branch-debug.md`,
  `devdoc/merger-config-sweep-2026-09-04.md`,
  `internal/standards/removal_audit_gate_test.go:1288`,
  `scripts/safe-merge.sh` — each hands a throwaway/fixture repo a real deletion,
  which is what the arm does.

## 6. Verification

```bash
go build ./...                                             # rc=0
go test ./internal/standards/ -count=1 -run 'RemovalAudit' # 13/13 PASS
make test-budget PKGS='./internal/standards/'
```

Red proof, on the REAL file, two arms — one per failure mode:

- **on-disk splice** (`devdoc/merger-takeover-branch-lifecycle.md`): the routed
  disposition row at `:74` is replaced on disk by the raw row assembled from the
  gate's own vocabulary constants, and `removalShellFind` reports it with a
  file:line; the parked bytes are written back and the file stops reporting. The
  reading there is a byte comparison in Go (`bytes.Equal`), NOT a two-digest
  identity pair — that pairing with no revision named is exactly what
  `TestGateRedProofRestoreRedProof` reports, and the first draft of this arm did
  it and was caught red by `TestStandardGates` at its own line.
- **HEAD-anchored restore** (`devdoc/merge-backlog-terminal-guard.md`):
  `readHEAD()` = `git hash-object <path>` == `git rev-parse HEAD:<path>`, read
  green on HEAD's own bytes, RED after a write that splices the raw row, then
  green after the restore — a later write moves the reading, which is the
  property an index-relative diff lacks. The arm writes HEAD's bytes onto disk
  first (this epic's other edits are uncommitted, so "on disk" and "at HEAD" are
  different content until the branch's commit lands) and puts the working bytes
  back afterwards.

Two hits the gate's own machinery produced on the first draft, both fixed and
both worth recording:

1. `internal/standards/removal_audit_shell_gate.go` was reported as a SITE at its
   own line — the raw row was typed as a Go string literal, and the production Go
   tree is a carrier. The splice string is now assembled from the vocabulary
   constants (`removalShellBranchDeleteFlagShort`, `removalShellVerbBranch`,
   `archiveMintGitToken`), so the file teaches no literal site and the splice
   cannot drift from what the predicate detects.
2. The first restore criterion was an unanchored digest pair, reported by
   `TestGateRedProofRestoreRedProof` with the exact sentence this document's own
   §-1 lesson carries.

`git status --porcelain` is empty at hand-over (Rule 30).
