<!-- doc-class: historical -->
# Clause ① of the capability-first gate: what it covers, what it measures, and where its scope stops

> **SUPERSEDED 2026-10-12** by `devdoc/help-clause1-order-relation.md` (task
> `01a12bb2-3772-72b7-bb2a-a5a3e8fecf13`, the follow-up row this document's own
> closing section asked for). The clause is now an ORDER relation over all 12
> published pages, not a position window over two constants; `hcfSectionMaxLine`
> is retired (it survives only as the red-proof fixture
> `hcfLegacyInvocationInWindow`). This file stays as the RECORD of the
> measurement that motivated the change — every figure below was true when it
> was taken, and the new document cites them rather than restating them.

Task: `01a12b9f-9736-778a-811a-52ed659b5dcd` (bug row; raised by `01a12b7e-4388`).
Status: decided and closed — the gap is REAL, the repair belongs to
`01a12b36-96c3-7e28-9121-dd28b209540d` ("help 瘦身"), and no help text was
touched here.

## 0. The question

`cmd/agent/help_capability_first_test.go` defines `hcfCapabilityFirstOffenses`,
whose clause ① says a `-h` page must present an invocation within its first
`hcfSectionMaxLine = 3` lines, on a line carrying `hcfInvocationPrefix`
(`"  gopi "`). The gate evaluates the predicate on exactly two constants —
`workerUsageHead` and `mergeUsageHead`.

12 top-level verbs publish a `-h` page. The question this row settles: is the
fact that only 2 of them are scanned a **real gap** between the principle and its
executor, or a **self-consistent design** whose scope is the two service verbs
by construction?

## 1. The verdict: a real gap — and the executor, as written, must not be widened

Two independent facts, both measured, both reproducible.

**(a) 5 of the 12 published pages violate clause ①.** Measured on the real
binary, through the census test this change adds:

```
$ go test ./cmd/agent/ -run 'UsageForm' -count=1 -v
    clause1 BREACH chat     no line carrying "  gopi " in its first 4 lines
    clause1 OK     config   first invocation at line 3 (0-based, window 0..3)
    clause1 OK     memory   first invocation at line 3 (0-based, window 0..3)
    clause1 OK     merge    first invocation at line 2 (0-based, window 0..3)
    clause1 OK     notify   first invocation at line 2 (0-based, window 0..3)
    clause1 BREACH session  no line carrying "  gopi " in its first 4 lines
    clause1 OK     task     first invocation at line 3 (0-based, window 0..3)
    clause1 BREACH tool     no line carrying "  gopi " in its first 4 lines
    clause1 BREACH ui_state no line carrying "  gopi " in its first 4 lines
    clause1 BREACH version  no line carrying "  gopi " in its first 4 lines
    clause1 OK     web      first invocation at line 3 (0-based, window 0..3)
    clause1 OK     worker   first invocation at line 2 (0-based, window 0..3)
    clause ① over the published set: satisfied=7 breached=5 of 12
```

**(b) the four pages that come closest to breaking it sit EXACTLY on the
boundary.** `config`, `memory`, `task`, `web` all put their first invocation on
0-based line 3 = `hcfSectionMaxLine` exactly. `merge`, `notify`, `worker` sit on
line 2. **One blank line added after the title of any of those four — no text
added, removed or reworded — turns the page red.** A criterion whose verdict is
decided by a blank line measures layout accidents, not the property it names.
The red proof is `TestClauseOnePositionIsOneLineWide`: the same fixture page,
satisfying clause ① on the boundary, then flips to violating it with a single
inserted blank line.

So the honest reading is: **the principle is universal, its executor covers 2
pages, and widening the executor as written would impose a brittle criterion on
8 more pages.** A "scan all 12 with the same predicate" change would be
*stricter and wrong at once*. That is why this row measures and decides, but
does not widen.

### Why the scope is what it is (not an oversight)

Commit `9e24d228fd` ("service help is capability-first, not a devdoc
(worker/merger -h)", 2026-09-14) restructured the two pages whose `-h` "was not
a help, but a devdoc". Its subject was those two service verbs. The other ten
pages keep the older `Usage:`-header convention the rest of the CLI uses. The
scope is therefore **a property of the epic**, carried into the predicate by the
constant set the gate iterates — not a claim the predicate's own comment ever
made about the whole published surface. That distinction is what the decision
comment now records at the clause, so the next reader meets it as a measurement
rather than re-litigating it.

## 2. The measured table (verbatim from the real binary)

Artifacts: `~/.gopi/sessions/01a12b9f-9736-778a-811a-52ed659b5dcd/artifacts/clause1-per-page.txt`
(per-page first-4-lines dump + verdict table) and `…/clause1-census.txt` (the
suite's own census output).

```
$ for v in chat config memory merge notify session task tool ui_state version web worker; do
    out=$(./bin/gopi $v -h 2>&1 | head -4)
    idx=$(printf '%s\n' "$out" | awk '/^  gopi / {print NR-1; exit}')
    if [ -n "$idx" ]; then printf '%-10s %-9s %s\n' "$v" OK "$idx"; else printf '%-10s %-9s %s\n' "$v" BREACH "--"; fi
  done

VERB       VERDICT   FIRST_INVOCATION_LINE
chat       BREACH    --
config     OK        3
memory     OK        3
merge      OK        2
notify     OK        2
session    BREACH    --
task       OK        3
tool       BREACH    --
ui_state   BREACH    --
version    BREACH    --
web        OK        3
worker     OK        2
```

Why each of the five breaches, from the same dump (indices are 0-based):

| verb | first lines | why clause ① misses |
|---|---|---|
| `chat` | `Usage: gopi chat …` on line 0, continuation on 1–2 | the invocation sits ON the `Usage:` line (the *header-line* form), and the prefix is `Usage: gopi `, not `  gopi ` |
| `version` | `Usage: gopi version` on line 0, prose from 1 | same header-line form |
| `session` | title, blank, prose paragraph (2 lines), `Usage:` below | prose separates title from invocations |
| `tool` | title, then a 4-line prose paragraph | same — prose before invocations |
| `ui_state` | title, then a 6-line prose paragraph | same |

So the five breaches are **two different shapes** (header-line form ×2,
prose-before-invocations ×3), not one. The census file `help_usage_form_census_test.go`
already classifies them: `header (8)`, `header-line (2)`, `headerless (2)`. The
five breaches are exactly `header-line` (both) + 3 of the 8 `header` pages.

Note this is **not** the `Usage:` header as such: 5 of the 8 header-form pages
satisfy clause ① (they put `Usage:` on line 2 with the rows beneath, so a row
lands on line 3). The header's position is what decides, which is the second
reason a position test is the wrong instrument here.

## 3. What this change lands

1. **A decision comment at clause ①** (`cmd/agent/help_capability_first_test.go`)
   recording: scope = the two service verbs; the measured 5/12; why the repair is
   not made here; the boundary fragility (four pages with zero headroom); and the
   convergent shape (re-specify the clause as an ORDER relation — first invocation
   before the first prose paragraph — and *then* widen the scope).
2. **`hcfFirstInvocationLine`** — clause ①'s judgement extracted into one
   function, so the census in the next file runs the SAME code the gate runs.
   A census that re-implemented the scan could drift from the gate it claims to
   report on.
3. **`TestClauseOnePositionIsOneLineWide`** — the red proof of the fragility
   (Rule 10.5's minimal sample: one blank line).
4. **`TestUsageFormCensusCarriesClauseOne`**
   (`cmd/agent/help_usage_form_census_test.go`) — clause ① re-derived over the
   SAME 12 published pages that file already walks, against the real binary
   (Rule 31), so the numbers above are re-exported by the suite instead of
   ageing in a document. It **logs** the per-page line and the totals and
   asserts only: every page classified; the count is non-vacuous on both sides
   (`satisfied > 0` **and** `breaching > 0`); and every page counted as breaching
   actually has clause ①'s own sentence among its offenses.

### Why the census ASSERTS non-vacuity but not "all 12 pass"

Asserting "every published page satisfies clause ①" would be a second gate over
text this package does not own, and it would go red on 5 shipped pages the
moment it landed — a defect claim about pages whose owner is about to rewrite
them. `help_usage_form_census_test.go`'s own header states the doctrine for the
form it already measures: *the FORM a page uses is measured and LOGGED, never
asserted, because pinning today's count freezes a legitimate change of shape
into a red build.* Clause ① is the same kind of fact about today's pages, so it
gets the same treatment. What makes the log trustworthy rather than decorative
is the three assertions above.

**Consequence for the follow-up:** once `01a12b36` reshapes those pages, the
`breaching == 0` assertion will fire and name itself as the place to revisit —
the census cannot silently become vacuous, and it cannot silently become a
gate either.

## 4. Not done here, and why that is correct

- **No help/usage text changed** (0 of the 52 usage constants touched): they are
  owned by `01a12b36-…-b209540d`'s step 3, and touching them in two rows collides.
- **Clause ① not widened to the published set**: see §1(b) — widening the
  position test would freeze a layout accident into 8 more pages.
- **The re-specification** (order relation instead of position) is the follow-up
  row's deliverable, parented on `01a12b36-…-b209540d` so it lands after the
  pages move.

## Verification

- `go build ./...` → OK
- `go test ./cmd/agent/ -run 'CapabilityFirst|UsageForm' -count=1 -v` → ok
  (6 tests PASS, including the census and both red proofs)
- `go test ./cmd/agent/ -run 'TestClauseOnePositionIsOneLineWide' -count=1 -v` → PASS
- Ran: the 12-verb loop above, against `./bin/gopi` built from this tree
  (`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`)
- Artifacts: `~/.gopi/sessions/01a12b9f-9736-778a-811a-52ed659b5dcd/artifacts/`
  (`clause1-per-page.txt`, `clause1-census.txt`, `clause1-boundary-green.txt`)
