<!-- doc-class: historical -->
# The citation index's sixth arm — a mention inside a literal VALUE is data

Task `01a13ba6-dcea-7837-a06a-bd6294be45b7`, base `b8f0ad2b7`, measured 2026-10-14.

## The defect

`tools/docclass/classify.py` builds the authority corpus that decides every
document's truth class (`normative` = a standing claim about master, `historical`
= a past run's record). Its index asked "does some line of some non-devdoc file
contain this filename", then subtracted five LINE-shaped refusals
(`refused_mention`: fenced, checker output, corpus bookkeeping, command operand,
and a quoted bare path alone on its line — arm 5, landed by `01a13b71`).

Arm 5 closed the case where a fixture line is *nothing but* a quoted path. It
could not see the bigger half of the same defect: a mention inside a string
literal that carries a sibling field —

```go
"devdoc/chat-table-design.md": "design diagram: verbs on an arrow into the engine",
```

— which is a cell of `internal/testutil/worker_ownership_gate_test.go`'s own
evidence map. Every such line still counted as the corpus's authority, so the
class of 35 documents rested on another test's data: editing a fixture
re-classified documents, and `banner.py` (a FOLLOWER of the class) then added or
deleted their banners.

## The arm

**A mention that stands inside a string literal of a SOURCE file
(`.go`/`.js`/`.py`/`.ts`) whose literal is nothing but the document's path is
DATA, not a statement.** The unit is the mention's own environment, not the line:
`literal_spans` (the same small machine `strip_strings_and_comments` walks,
spelled separately because this one answers a POSITION question rather than
blanking code for a declaration scan) says CODE / LIT / COM per byte, and
`refused_literal_mention` refuses a LIT position whose literal matches
`VALUE_LITERAL_RE` — a path token and nothing else.

**The boundary is whitespace inside the literal**, and that is the whole
measurement:

| shape | verdict | why |
|---|---|---|
| `"devdoc/x.md"` | refused | a value in a table the file holds |
| `"devdoc/x.md": "evidence: …"` | refused | the same value with a sibling field — arm 5 could not reach it |
| `mustWrite(t, "devdoc/x.md")` | refused | an argument, not a sentence |
| `` `… the full CONFIG.md §2c row …` `` | kept | a sentence a reader is given: the CLI help pages (`cmd/agent/cli_*.go`'s `const usage = …`, 141 literal mentions) |
| `"see devdoc/x.md for the measurement"` | kept | same judgement, one shape over |
| `// devdoc/x.md is the design record.` | kept | a comment is a statement (never touched) |
| markdown prose, `.tsv` cell, `.sh` line, fenced block | kept/unchanged | the arm is INERT outside source files; the fence arm still decides fences |

**Precedence.** The line-shaped arms run first (`refused_mention`), and the sixth
arm is asked only for a mention they accepted, so one mention gets exactly one
verdict and a fenced mention is never re-judged. The fence arm still wins.

## Measured (read-only, before the arm was written)

Census: `tools/docclass/arm6_census.py` (generator, read-only; `ARM6_BEFORE_REV`
names the revision the BEFORE column is read from), full output under
`~/.gopi/sessions/01a13ba6-dcea-7837-a06a-bd6294be45b7/artifacts/`.

* accepted mentions in the tree: **3907** — CODE 1481 / COM 1464 / FENCE 2 /
  literal 960 (819 in `*_test.go`, 141 elsewhere).
* the arm refuses **594 mentions**, and moves **35 documents' classes** —
  every one of them `normative → historical`; 34 of the 35 cite an evidence map
  in `internal/testutil/worker_ownership_gate_test.go`, one in
  `internal/tools/chat_write_guard_test.go`.
* the alternatives, rejected: "any mention inside any literal of a source file"
  (whitespace boundary dropped) = **58** documents and reaches the help pages;
  "no doc name outside a literal" = 61 (rejected by `01a13b71`); "any `*_test.go`
  file" = 147. This arm is a FLOOR of arm 5, not a widening of it.
* a doc whose OTHER citations survive does not move — measured on
  `devdoc/chat-table-design.md`, whose row stays `normative` through
  `path internal/agent/chatregister.go` even though the fixture cell that named
  it is now refused. That is the property that keeps the arm from being a
  re-classification spree.

## The census is what justifies the re-derivation

Rule 36 forbids hand-editing `devdoc/DOC-INDEX.tsv`, so the 35 flipped rows were
re-derived through the shipped generator (`classify.py . --only <path>` × 35, one
call). The census artifact lists, per document: the BEFORE class (read from
revision `b8f0ad2b7`), the AFTER class, the direction, and the authority line
that caused it. Five further rows were re-derived in the same commit because the
re-derivation's own citation source moved
(`hook-investigation-report.md`, `signalrec-session-attribution.md`,
`task-dispatcher-p2.md`, `tmpdir-param-not-banner.md`,
`worktree-live-cwd-guard-20260910.md`) — all six are named, none is silent.

Two classes of dead end, recorded because both cost measurement time:

* a census that classifies without first calling `build_code_index()` reports
  every doc held up by a `path`/`sym` anchor as if the arm had moved it. The
  census must run the same index the generator runs, or its AFTER column is a
  different question.
* the token loop is a `finditer` over the file, so a mention can appear in a
  LATER line than the first one that names the doc; the census prints every
  authority line the arm refuses, not just the first.

## Defect 2 — `--check` compares stored row ↔ stored file

**Decision: no arm. The re-derivation census is the only honest instrument, and
`--check` gains nothing but a second, weaker question.**

`classify.py --check` asks "is the manifest consistent with the tree on disk",
comparing each STORED row against the class the generator renders TODAY for that
path — so it CAN see the row↔generator disagreement (a row whose class the
classifier would render differently is reported under `DOCCLASS STALE`, and it
did report 5 such rows this run). It cannot see it when the row is compared
against a manifest it just rewrote, and it cannot decide whether a flipped row
SHOULD flip — that is the reviewed-census half this document is.

What `--check` deliberately does NOT do is fail on drift: the manifest is not
regenerated on every commit (measured at `120ddaf`: `0 gap(s)` beside 721 rows
whose stored text is not what the classifier renders), so a drifting row is the
expected state of a green tree, and making it fatal would turn every unrelated
commit into a re-classification. The honest instruments stay: `--check` NAMES the
drift, `arm6_census.py` (or `--only PATH`) derives the decision, and a human
reviews it. A "the generator would classify this differently" arm would re-ask
the question `DOCCLASS STALE` already answers, one row at a time, and would have
called this run's 35 justified flips a defect.
