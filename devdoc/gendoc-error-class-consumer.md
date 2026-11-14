<!-- doc-class: historical -->
# The gendoc ERROR class gets a consumer — and it is a QUESTION, not a refusal (task 01a13577-57e9)

## The epic, and the two ways it could close

`internal/gendoc` splits its findings into two channels. `Answer.Findings` carries
what was there before the split; `Answer.Errors` carries the ERROR-level class —
an anchor published **inside a generated region** that RESOLVES but lands on a
comment or a blank line, or that names a test file. The class was added
deliberately OUT of `Findings` (the code says why, and the reason is measured:
with the ERROR in `Findings` the anchor lane refused EARLIER and the gate the
document obliged never ran — `TestBranchCheckAnchorArmRefusesADroppedRegeneration`).

The consequence, confirmed by recon in this run: **`Answer.Errors` had no
consumer anywhere.** Three call sites read only `ans.Findings`
(`task_finish_premerge_smoke.go:350`, `mergeset_gendoc.go:173`,
`mergeset_gendoc_base.go:148`), and `commands-census.go` never touches the field.
A class a committing path deliberately does not read is a claim nobody checks.

Two ways out: **(A)** wire it into a committing path so it refuses, or **(B)**
rule it a diagnostic surface, write the conclusion down, and add a consumer that
forces it to be read.

## Verdict: B

**A is not available, and not for a taste reason.** The ERROR fires on the lane
fixture itself: `branch_check_anchor_test.go` moves an anchor source so the
region anchor lands on a comment line, and the ERROR is raised by that same walk.
Wiring the class into the branch-check arm therefore changes the refusal an
operator reads on the anchor lane — the exact side effect the split was made to
avoid, and the one the constraint "existing lane tests stay green" forbids.
Wiring it into `task_finish_premerge`'s arm has the same shape for the same
reason.

**B is also the better answer on its own merits.** The class asks a question
about a tree that is orthogonal to every question the committing paths ask:

| question | owner | when it is asked |
|---|---|---|
| is the document in step with its source? | the generated block's own gate | on a change that touches a published anchor source |
| is the devdoc corpus consistent? | `gopi tool doccheck` (gaps / drift / provenance) | when an operator or a close-out asks |
| **does the span a region already publishes LAND where it claims to?** | **this arm** | **when an operator asks** |

The third one is the fact a reference check structurally cannot see: a stale
anchor RESOLVES (line 125 exists), so only a declaration test catches it. It is
worth being able to ASK about a tree — including the integration branch, after a
merge, where no branch's change is in hand to trigger a committing path.

## What landed

- **`gopi tool doccheck --anchors`** — a READ-ONLY fourth arm on the verb that
  already owns the devdoc-corpus noun. It prints one line per ERROR-level finding
  (carrying `gendoc.ErrorFindingPrefix`, verbatim — the class is defined by that
  text and the consumer must not re-word it), names the class in its closing
  line, and exits non-zero on any finding.
- **A mode, not a new verb.** A new top-level word has to reach nine registration
  sites and obliges a regeneration of `COMMANDS.md`, whose generated block
  publishes each verb's LINE NUMBER — so a new word would also have moved the
  frozen anchors the same document carries. The fourth question is about the SAME
  tree and the SAME corpus the other three questions are about, so it is an arm
  of the verb that owns that noun.
- **The consumer lives in `cli_doccheck_anchors.go`**, its own file, on purpose:
  adding `internal/gendoc` to `cli_doccheck.go`'s import block moved the frozen
  anchor `cmd/agent/cli_doccheck.go:161` (`devdoc/mergeset-published-anchor-union.md`)
  down one line. That drift was OBSERVED, not predicted — the first run of the new
  arm reported it against this very tree (see the evidence below). The consumer
  moved to a file of its own and the anchor is back at 161.
- **Two abstentions, stated rather than passed off as clean answers** (Rule 15):
  a tree that publishes no generated document (`not asked … (N candidate(s), M
  edge(s)): <reason>`), and a walk that could not read the tree
  (`NOT MEASURED — …`). Neither fails the run: the first is not a defect, the
  second is not this tree's defect.

## Evidence (measured, this run)

The first invocation of the new arm — against the tree I was editing — caught a
REAL drift, created by my own edit:

```
$ ./bin/gopi tool doccheck --anchors --repo .
doccheck: anchor DECLARATION (a span a generated region publishes that resolves to no declaration):
doccheck: ERROR: COMMANDS.md: the anchor cmd/agent/cli_doccheck.go:345 published inside its generated region points at a comment line, not a declaration — …
doccheck: this run exits 1 on the published-anchor DECLARATION invariant (1 finding(s))
EXIT=1
```

That is the class working, on a real tree, on a real drift — not a fixture. After
moving the consumer to its own file and re-running the census generator:

```
$ go run internal/standards/testdata/commands-census.go . COMMANDS.md
commands-census: wrote COMMANDS.md (44 verbs)
$ ./bin/gopi tool doccheck --anchors --repo .
doccheck: anchor DECLARATION (…):
doccheck: none — every `file.go:NNN` span the generated regions publish resolves to a declaration in a production source
EXIT=0
$ grep -n "// modes, what each writes" cmd/agent/cli_doccheck.go
161:// modes, what each writes, and the exit convention a caller gates on.
```

## Not done (honest)

- **No committing path consumes the class.** That is the verdict, not an
  omission — see above. The arm is a question an operator asks.
- **`gopi task finish-cleanup`'s pre-push leg is unchanged.** Wiring it there is
  available as a *separate* task once the arm has been in use; it is deliberately
  not in this change, because the lane's refusal text is the thing the split
  protects.
- The `dcatDocBody` fixture builds its document with `cliindex.TableBegin/End`
  rather than a literal marker — a literal would be a second spelling of the
  marker (Rule 13).
