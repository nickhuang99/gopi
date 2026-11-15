#!/usr/bin/env bash
# 99-summary-gate.sh — the acceptance instrument for devdoc/requirements/99-summary.md.
#
#   bash devdoc/requirements/99-summary-gate.sh [doc] [--self-test]
#
# Exit 0 = all green; any FAIL prints the offending item and exits 1. Run from the
# repository root (paths inside the document are repo-root-relative).
#
# Six checks. Each carries the reason it exists. Every check is written so it CAN
# go red — `--self-test` mutates a copy and asserts each expected FAIL appears,
# because a green that measured nothing is this series' own recorded worst defect
# (devdoc/requirements/09-chat-ui-verification-report.md:99-118: a grep over a
# directory that does not exist, reporting "no hits").

set -uo pipefail

# Rule 22: the self-test's mutated copy is scratch, so it comes from
# scripts/lib/tmp-ban.sh — the ONE reader of the scratch root, asserted off `/`
# by DEVICE ID and registered for trap-time cleanup. /tmp shares `/`'s device
# here, so an ambient mktemp -d would land it on the boot device.
. "$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/scripts/lib/tmp-ban.sh"
trap 'gopi_scratch_cleanup' EXIT

DOC=${1:-devdoc/requirements/99-summary.md}
SELF=${2:-}
DIR=$(dirname "$DOC")
GATE=$(basename "$0")
fails=0

say()  { printf '%s\n' "$*"; }
fail() { say "FAIL  $*"; fails=$((fails + 1)); }
pass() { say "ok    $*"; }

# section <literal heading prefix> — body of that section (until the next "## ").
section() {
  awk -v h="$1" 'index($0, h)==1 {p=1; next} p && /^## / {exit} p' "$DOC"
}

# idents — the code-identifier predicate, applied to stdin. A backticked token that
# contains an underscore, an internal capital, or a source-file suffix. Citations to
# this series' own documents and the constraint labels C1..C6 are NOT identifiers:
# they are the evidence apparatus, and a purpose paragraph without them could not be
# checked at all.
idents() {
  grep -oE '`[^`]+`' | sed 's/`//g' \
    | grep -E '_|[a-z][A-Z]|\.(go|sh|json)$' | grep -vE '\.md|^C[0-9]'
}

# ---------------------------------------------------------------------------
# 1. Citation COVERAGE — every module document in this directory is cited by path.
#    Reason: the epic's acceptance. An uncited module document is a document this
#    summary never read, which would make "the closing document of 15" false.
covered=0; missing=""
for f in "$DIR"/*.md; do
  b=$(basename "$f")
  [ "$b" = "$(basename "$DOC")" ] && continue
  if grep -q "requirements/$b" "$DOC"; then covered=$((covered + 1)); else missing+=" $b"; fi
done
if [ -n "$missing" ]; then fail "module docs not cited by path:$missing"
elif [ "$covered" -lt 18 ]; then fail "citation coverage: only $covered docs cited (expected >= 18)"
else pass "citation coverage: $covered module docs cited by path"; fi

# 1b. Citation RESOLUTION — every cited path:line exists and is inside the file.
#     Reason: a citation that does not resolve is how a summary rots; the series'
#     citation standard makes it a defect class (00-method-and-standards.md:571-601).
badc=0; okc=0
while read -r c; do
  [ -z "$c" ] && continue
  p=${c%%:*}; rng=${c#*:}; hi=${rng##*-}
  if [ ! -f "$p" ]; then say "      bad file: $c"; badc=$((badc + 1)); continue; fi
  n=$(wc -l < "$p")
  if [ "$hi" -gt "$n" ]; then say "      out of range: $c (file has $n lines)"; badc=$((badc + 1))
  else okc=$((okc + 1)); fi
done < <(grep -oE '[A-Za-z0-9_./-]+\.(go|md|sh|json):[0-9]+(-[0-9]+)?' "$DOC" | sort -u)
[ "$badc" -eq 0 ] && pass "citation resolution: $okc distinct citations resolve" \
                  || fail "citation resolution: $badc of $((okc + badc)) do not resolve"

# ---------------------------------------------------------------------------
# 2. The "what gopi is for" section carries NO code identifiers.
#    Reason: the epic's constraint — a product owner must be able to read it. A
#    purpose paragraph naming internal symbols is a code description in a purpose's
#    clothes (the method's §2.1 failure mode, 00-method-and-standards.md:107-120).
neg=$(section '## 1. What gopi is for' | idents)
words=$(section '## 1. What gopi is for' | wc -w)
if [ "$words" -lt 200 ]; then
  fail "product-language section is only $words words — nothing to check"
elif [ -n "$neg" ]; then
  fail "product-language section carries code identifiers:"; say "$neg" | sed 's/^/      /'
else
  # Non-vacuity: the SAME predicate must find identifiers elsewhere in the summary,
  # or the green above is an empty-set artifact.
  probe=$(section '## 3. The biggest gaps, ranked' | idents | wc -l)
  if [ "$probe" -gt 0 ]; then pass "product-language section: no code identifiers ($words words; predicate finds $probe elsewhere)"
  else fail "identifier predicate matched nothing anywhere — it is vacuous"; fi
fi

# ---------------------------------------------------------------------------
# 3. At least 3 RANKED gaps, each carrying a module-doc citation.
#    Reason: the epic's acceptance ("the biggest gaps, ranked, each traced to a
#    cited module doc's evidence"). A gap with no citation is an opinion.
gaps=$(grep -c '^### GAP-[0-9]' "$DOC")
if [ "$gaps" -lt 3 ]; then
  fail "ranked gaps: $gaps found (need >= 3)"
else
  bad=0
  for n in $(grep -oE '^### GAP-[0-9]+' "$DOC" | grep -oE '[0-9]+'); do
    body=$(awk -v h="### GAP-$n" 'index($0,h)==1 {p=1; next} p && /^### / {exit} p' "$DOC")
    # herestring, not a pipe: `grep -q` exits on the first match and SIGPIPEs the
    # writer, which under `set -o pipefail` reports a FALSE failure on the longest
    # bodies — a verdict that depended on output size. (Found here by running it.)
    grep -q 'devdoc/requirements/' <<<"$body" || { fail "GAP-$n has no module-doc citation"; bad=1; }
  done
  z=$(grep -ci 'ranked by' "$DOC")
  [ "$z" -ge 1 ] || { fail "the ranking is not stated as a ranking"; bad=1; }
  [ "$bad" -eq 0 ] && pass "ranked gaps: $gaps, each with a module-doc citation"
fi

# ---------------------------------------------------------------------------
# 4. The disagreement section names concrete instances WITH citations, and names
#    the single sharpest one as one sentence.
#    Reason: the epic's acceptance. "He believes X, the code does Y" is only
#    checkable if both halves are cited; singling out one is a judgement the epic
#    asks for explicitly.
dis=$(section '## 4. Where the operator' | grep -cE '^[0-9]+\. \*\*[^*]')
cited=$(section '## 4. Where the operator' | grep -c 'devdoc/requirements/')
if [ "$dis" -lt 3 ]; then
  fail "disagreements: $dis instances (need >= 3)"
elif [ "$cited" -lt 3 ]; then
  fail "disagreements: only $cited lines carry module citations"
elif ! grep -q 'single sharpest disagreement' "$DOC"; then
  fail "disagreements: the sharpest-one sentence is not named"
else
  pass "disagreements: $dis instances, $cited cited lines, sharpest one named"
fi

# ---------------------------------------------------------------------------
# 5. The consolidated could-not-determine list and the next-phase test
#    recommendation are both present, and the pins are named items.
#    Reason: the epic's acceptance; these two sections are the deliverable's point.
for want in '## 5. What this series could not determine' \
            '## 6. What the test-writing phase should pin first' \
            'No test is written by this document'; do
  grep -qF "$want" "$DOC" && pass "present: $want" || fail "missing: $want"
done
pins=$(section '## 6. What the test-writing phase should pin first' | grep -cE '^[0-9]+\. \*\*[^*]')
[ "$pins" -ge 5 ] && pass "test recommendation: $pins named pins" \
                  || fail "test recommendation: only $pins pins (need >= 5)"

# ---------------------------------------------------------------------------
# 6. The operator's six constraints appear AS CONSTRAINTS (the epic's own list).
#    Reason: the epic requires the positive vision to appear as constraints, not as
#    background. Missing one means a whole dimension of the whole-project answer is
#    absent, which no other check would notice.
miss=""
for c in C1 C2 C3 C4 C5 C6; do
  grep -qE "\*\*$c\*\*|\| \*\*$c\*\*" "$DOC" || miss+=" $c"
done
[ -z "$miss" ] && pass "operator vision present as constraints: C1…C6" \
               || fail "constraints absent from the summary:$miss"

# ---------------------------------------------------------------------------
if [ "$SELF" = "--self-test" ]; then
  say ""; say "--- self-test: each check must be able to fail (copy only) ---"
  # The mutated copy lives OUTSIDE the audited directory: the self-test must not
  # leave litter in the tree it checks (Rule 30), nor be scanned as a module doc.
  sd="$(gopi_scratch_mkdir summary-gate-selftest)" || exit 1
  t="$sd/m.md"; cp "$DOC" "$t"
  sed -i 's/devdoc\/requirements\/test-infrastructure\.md/the test doc/g' "$t"   # 1 coverage
  sed -i 's/^## 1\. What gopi is for/## 1. What gopi is for\n\n`task_finish_cleanup` and `newClient`./' "$t"  # 2 identifiers
  sed -i 's/^### GAP-[3456] —/### NOTE- —/' "$t"                                  # 3 gaps < 3
  sed -i 's/single sharpest disagreement/the disagreement/' "$t"                 # 4 sharpest
  sed -i 's/^## 6\. What the test-writing phase should pin first/## 6. Pins/' "$t"   # 5
  sed -i 's/| \*\*C1\*\*/| C1 /' "$t"                                            # 6 constraint
  printf '\n%s\n' '(`devdoc/requirements/05-context-and-memory.md:99999`)' >> "$t"   # 1b range
  out=$(bash "$0" "$t" 2>&1); rm -rf "$sd"
  # only FAIL lines count: matching the whole output would let the GREEN lines of
  # the very same run satisfy an expectation (the "ranked gaps" string appears in
  # both), which is a vacuous self-test — the defect this whole file is about.
  reds=$(grep '^FAIL' <<<"$out")
  expect=( "citation coverage" "do not resolve" "code identifiers" "ranked gaps" "sharpest" "missing: ## 6" "constraints absent" )
  bad=0
  for e in "${expect[@]}"; do
    if grep -qF "$e" <<<"$reds"; then say "      red as expected: $e"
    else say "      NOT RED (vacuity!): $e"; bad=1; fi
  done
  [ "$bad" -eq 0 ] && say "self-test: all ${#expect[@]} checks went red" \
                   || { say "self-test: a check could NOT fail"; fails=$((fails + 1)); }
fi

say ""
if [ "$fails" -eq 0 ]; then say "GATE GREEN — $DOC"; exit 0; fi
say "GATE RED — $fails failing check(s)"; exit 1
