#!/usr/bin/env bash
# g7-verb-census.sh — the RE-DERIVATION command behind devdoc/gopi-verb-census.md
# (G7 of the help-surface hardening; the recipe is the G5 one, memory #4840).
#
# WHY A SCRIPT AND NOT A HAND-WRITTEN TABLE. The deliverable is a census of every
# TOP-LEVEL `gopi <verb>` this binary dispatches, and of how each one is
# exercised. A table typed by hand describes the dispatch as it was on the day
# somebody read it; this one is derived every time it runs, from four sources:
#
#   * DISPATCHED — the isXxxSubcommand predicates the dispatch chain in
#     cmd/agent/main.go actually calls, each resolved to the verb word its own
#     body compares args[0] against (the constant from cmd/agent/cli_verbs.go,
#     cmd/agent/cli_act.go or internal/toolnames). No hand-typed verb list.
#   * ADVERTISED — the command index the built binary prints for `gopi -h`: its
#     own text, not a reading of internal/config/help.go.
#   * TEST-DRIVE — how many test sites hand the verb to a real argv, in any of
#     the three shapes this repo's tests use:
#       (1) a composite literal with the verb FIRST, `[]string{"worker", …}` /
#           `[]{CliVerbMerger, stopFlag}` / `[][]string{{CliVerbMerger, …}}`;
#       (2) a call to one of the verb's OWN run functions (`runTermOnce(…)`,
#           `runUIStateSubcommand(…)`) — the newline is the only difference;
#       (3) a line naming a discovered RUNNER helper (`runGopiIn(t, ……, "worker")`)
#           and the verb — the argv reaches the binary, the slice is implied.
#     Help-query lines are dropped in every shape: `-h` is not a run (Rule 31).
#   * USAGE-LOG — `gopi <verb>` inside a command a recorded session ACTUALLY ran
#     (the `"command"` field of a session.jsonl bash tool call).
#   * DOC-RECIPE — `gopi <verb>` inside a fenced code block of devdoc/, which is
#     where the commands a reader copies and runs live (prose that merely names a
#     verb is not usage).
#
# cmd/agent/cli_verb_census_test.go re-derives DISPATCHED, ADVERTISED and
# TEST-DRIVE in Go and gates the checked-in table against them in BOTH directions,
# with a non-vacuity floor — a verb added to the dispatch without a census row (or
# a row claiming a verb the dispatch no longer has) fails the build instead of
# leaving the table stale.
#
# USAGE
#   scripts/g7-verb-census.sh [path-to-gopi]
# Default binary: ./bin/gopi
#   (build it first: CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/ )
# The USAGE-LOG column reads ${GOPI_HOME:-$HOME/.gopi}/sessions — set GOPI_HOME to
# census another profile's corpus (with none present the column is all zeros,
# which is a fact about the host, not a defect of the census).
#
# OUTPUT (TSV, tab-separated, sorted by verb):
#   verb <TAB> dispatched <TAB> advertised <TAB> test_drive <TAB> usage_log <TAB> doc_recipe <TAB> class
# class:
#   a:test         a test hands the verb a real (non-help) argv
#   b:usage-log    no test does, but a recorded run did
#   b:doc-recipe   no test and no logged run, but a devdoc recipe uses it
#   b:help-only    only `<verb> -h` has ever reached it (help is not a run)
#   c:neither      none of the above — the census's product
set -euo pipefail

# Rule 22: the scratch directory comes from scripts/lib/tmp-ban.sh — the ONE
# reader of the scratch root (GOPI_TEST_SCRATCH_HOME, else GOPI_TEST_HOME's
# workspace, the tree the Go suite GC sweeps). The helper asserts by DEVICE ID
# that it is off `/` and registers it for trap-time cleanup; /tmp shares `/`'s
# device here, so it is never a candidate and there is no fallback to it.
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/tmp-ban.sh"

BIN="${1:-./bin/gopi}"
if [ ! -x "$BIN" ]; then
    echo "g7-verb-census.sh: $BIN is not executable — build it with" >&2
    echo "  CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/" >&2
    exit 1
fi

TMP="$(gopi_scratch_mkdir g7-verb-census)" || exit 1
trap 'gopi_scratch_cleanup' EXIT

# --- 0. the constant → verb-word map (definition sites, read not typed) ------
{
    grep -hoE '^[[:space:]]*(const[[:space:]]+)?(CliVerb[A-Za-z0-9]+|Tool[A-Za-z0-9]+)[[:space:]]*=[[:space:]]*"[a-z_][a-z0-9_-]*"' \
        cmd/agent/*.go internal/toolnames/*.go internal/tools/*.go 2>/dev/null || true
} | sed -E 's/^[[:space:]]*const[[:space:]]+/ /' | sort -u >"$TMP/consts.raw"
# Alias lines (`CliVerbMemory = tools.CliVerbMemoryGroup`) are a SECOND pass: a
# single pass would depend on the line order of the files.
{
    grep -hoE '^[[:space:]]*(const[[:space:]]+)?(CliVerb[A-Za-z0-9]+)[[:space:]]*=[[:space:]]*[A-Za-z]+\.[A-Za-z0-9]+' \
        cmd/agent/*.go 2>/dev/null || true
} | sed -E 's/^[[:space:]]*const[[:space:]]+/ /' | sort -u >"$TMP/aliases.raw"
declare -A WORD_OF=()
while read -r line; do
    name=$(printf '%s' "$line" | sed -E 's/^[[:space:]]*([A-Za-z0-9]+).*/\1/')
    if printf '%s' "$line" | grep -qE '"[a-z_][a-z0-9_-]*"'; then
        WORD_OF[$name]=$(printf '%s' "$line" | grep -oE '"[a-z_][a-z0-9_-]*"' | tr -d '"')
    fi
done <"$TMP/consts.raw"
while read -r line; do
    name=$(printf '%s' "$line" | sed -E 's/^[[:space:]]*([A-Za-z0-9]+).*/\1/')
    alias=$(printf '%s' "$line" | grep -oE '[A-Za-z]+\.[A-Za-z0-9]+$' | sed 's/^[A-Za-z]*\.//')
    if [ -n "${WORD_OF[$alias]:-}" ]; then
        WORD_OF[$name]="${WORD_OF[$alias]}"
    fi
done <"$TMP/aliases.raw"

# A leaf constant package neither window above names (`internal/cliverbs`): the
# word is spelled ONCE there and cmd/agent aliases it (`CliVerbMerge =
# cliverbs.Merge`) — Rule 14 is why the package exists, and Rule 13 is why
# cmd/agent holds no second literal to find. So the word is READ from the package
# the alias names, with nothing hard-coded: the qualifier resolves through the
# IMPORTING FILE's own import block, the directory through go.mod's module line.
# (Measured 2026-09-26: without this pass the generator refused the WHOLE census
# with `constant CliVerbMerge (from isMergeSubcommand) resolves to no verb word`
# on a tree where the merge word is defined exactly once, in internal/cliverbs —
# the reader calling a Rule-14-correct tree a defect.)
MODULE=$(awk '/^module /{print $2; exit}' go.mod)
{
    grep -hoE '^[[:space:]]*(const[[:space:]]+)?CliVerb[A-Za-z0-9]+[[:space:]]*=[[:space:]]*[a-z][A-Za-z0-9]*\.[A-Za-z0-9]+' \
        cmd/agent/*.go 2>/dev/null || true
} | sed -E 's/^[[:space:]]*const[[:space:]]+/ /' | sort -u >"$TMP/leafaliases.raw"
while read -r line; do
    name=$(printf '%s' "$line" | sed -E 's/^[[:space:]]*([A-Za-z0-9]+).*/\1/')
    if [ -n "${WORD_OF[$name]:-}" ]; then
        continue # a literal in the windows above already answered it
    fi
    sel=$(printf '%s' "$line" | grep -oE '[a-z][A-Za-z0-9]*\.[A-Za-z0-9]+$' || true)
    if [ -z "$sel" ]; then
        continue
    fi
    qual=${sel%%.*}
    leaf=${sel##*.}
    file=$(grep -rlE "^[[:space:]]*${name}[[:space:]]*=[[:space:]]*${qual}\." cmd/agent/*.go | head -1 || true)
    if [ -z "$file" ]; then
        continue
    fi
    ipath=$(grep -oE "\"[^\"]*/${qual}\"" "$file" | head -1 | tr -d '"' || true)
    if [ -z "$ipath" ]; then
        continue
    fi
    dir="${ipath#"$MODULE"/}"
    if [ "$dir" = "$ipath" ]; then
        dir="$ipath"
    fi
    word=$({ grep -hoE "\b${leaf}[[:space:]]*=[[:space:]]*\"[a-z_][a-z0-9_-]*\"" "$dir"/*.go 2>/dev/null || true; } |
        grep -oE '"[a-z_][a-z0-9_-]*"' | head -1 | tr -d '"' || true)
    if [ -n "$word" ]; then
        WORD_OF[$name]="$word"
    fi
done <"$TMP/leafaliases.raw"
if [ "${#WORD_OF[@]}" -lt 20 ]; then
    echo "g7-verb-census.sh: resolved only ${#WORD_OF[@]} verb constants — cli_verbs.go/toolnames changed shape" >&2
    exit 1
fi

# func_const <file> <func> → the verb constant the function body names, or "".
# The body is a LINE RANGE (func head → the first column-0 brace), never an awk
# range expression: the range form is brittle against comment shapes.
func_const() {
    local file="$1" fn="$2" s e
    s=$(grep -nE "^func ${fn}\(args \[\]string\) bool" "$file" | head -1 | cut -d: -f1 || true)
    if [ -z "$s" ]; then
        printf ''
        return 0
    fi
    e=$(awk -v s="$s" 'NR>s && /^}/ {print NR; exit}' "$file" || true)
    if [ -z "$e" ]; then
        e=$((s + 40))
    fi
    sed -n "${s},${e}p" "$file" |
        { grep -oE 'CliVerb[A-Za-z0-9]+|toolnames\.Tool[A-Za-z0-9]+' || true; } |
        sed 's/^toolnames\.//' | head -1 || true
}

# --- 1. the DISPATCHED set: main.go's chain + each predicate body ------------
# The chain is read to its CLOSING BRACE, not `grep -m1`'s first line: main.go's
# chain is wrapped across several lines, and `grep -m1` silently derived every
# predicate below the first line as dispatched=0 — the generator disagreeing with
# the Go gate (censusChainNames reads the WHOLE file) about the same tree.
# Found 2026-10-03, when the `session` predicate landed on the chain's third line
# and the regenerated table omitted it while the gate demanded its row.
CHAIN=$(awk '/if isConfigSubcommand\(dispatchArgs\)/{f=1} f{print} f&&/\{$/{exit}' cmd/agent/main.go || true)
mapfile -t PREDS < <(printf '%s' "$CHAIN" |
    grep -oE 'is[A-Za-z0-9]+Subcommand\(dispatchArgs\)' |
    sed 's/(dispatchArgs)//' | sort -u)
# The floor is the chain's OWN size, and it moved with it: the verb-retirement
# epic (2026-10-05) removed ~30 predicates from main.go, so a floor of 30 would
# now refuse every run of a healthy tree. The floor's job is unchanged — a
# parse that found nothing (or stopped at the first line) must be
# distinguishable from a small chain — so it sits a few predicates below the
# chain's measured size rather than at the old number. Measured 2026-10-05: 18.
CENSUS_MIN_PREDICATES=12
if [ "${#PREDS[@]}" -lt "$CENSUS_MIN_PREDICATES" ]; then
    echo "g7-verb-census.sh: parsed only ${#PREDS[@]} dispatch predicates from main.go (floor ${CENSUS_MIN_PREDICATES}) — the chain changed shape" >&2
    exit 1
fi

# A REFUSING predicate dispatches no verb: it stops a spelling this CLI
# deliberately does not dispatch (`gopi code_search`) from being spent as a
# prompt, prints a pointed refusal and exits 1. There is therefore no verb word
# for the census to carry and no help page to fingerprint, so it is exempted by
# NAME — the same name the Go gate exempts
# (cmd/agent/cli_verb_census_test.go, constant undispatchedToolWordsSubcommand).
# The exemption is tied to the predicate's existence: a stale name here is
# refused rather than silently skipped.
REFUSING_PREDICATES=(UndispatchedToolWordSubcommand)
for rp in "${REFUSING_PREDICATES[@]}"; do
    if ! grep -qE "^func is${rp}\(args \[\]string\) bool \{" cmd/agent/*.go; then
        echo "g7-verb-census.sh: exempted predicate ${rp} does not exist — the census exempts a refusal that is gone" >&2
        exit 1
    fi
done

declare -A DISPATCHED=()
declare -A CONST_OF=()
declare -A RUNFNS=()
for pred in "${PREDS[@]}"; do
    pred="${pred#is}" # the chain spells the call isXxxSubcommand; the func name drops the leading "is"
    if printf '%s\n' "${REFUSING_PREDICATES[@]}" | grep -qx "$pred"; then
        continue # a refusing predicate names no verb — exempted above, never a row
    fi
    predfn="is${pred}Subcommand"
    file=""
    helper=""
    # The constant the predicate compares argv[0] against. The NAME-derived form
    # (isArchiveSubcommand → CliVerbArchive) is the contract Rule 1 imposes on
    # these predicates; where the name does not match the constant
    # (isTermSubcommand → CliVerbCLI, isWebSearchSubcommand → ToolWebSearch) or the
    # body delegates to a helper (isBrewSubcommand → brewSubcommand), the constant
    # is READ from the body — never typed here.
    const=""
    if [ -n "${WORD_OF[CliVerb${pred}]:-}" ]; then
        const="CliVerb${pred}"
        file=$(grep -rlE "^func is${pred}\(args \[\]string\) bool" cmd/agent/*.go | head -1 || true)
    else
        file=$(grep -rlE "^func is${pred}\(args \[\]string\) bool" cmd/agent/*.go | head -1 || true)
        if [ -z "$file" ]; then
            echo "g7-verb-census.sh: no predicate file for ${predfn}" >&2
            exit 1
        fi
        const=$(func_const "$file" "is${pred}")
        if [ -z "$const" ]; then
            # One helper hop: a predicate that delegates its decision carries the
            # constant in the helper (isBrewSubcommand → brewSubcommand).
            helper=$(sed -n "$(grep -nE "^func is${pred}\(args \[\]string\) bool" "$file" | head -1 | cut -d: -f1),+14p" "$file" |
                grep -oE '[a-z][A-Za-z0-9]*Subcommand\(args\)' | sed 's/(args)//' | head -1 || true)
            if [ -n "$helper" ]; then
                const=$(func_const "$file" "$helper")
            fi
        fi
    fi
    if [ -z "$const" ]; then
        echo "g7-verb-census.sh: ${predfn} names no resolvable verb constant — predicate shape changed" >&2
        exit 1
    fi
    word="${WORD_OF[$const]:-}"
    if [ -z "$word" ]; then
        echo "g7-verb-census.sh: constant ${const} (from ${predfn}) resolves to no verb word" >&2
        exit 1
    fi
    DISPATCHED[$word]=1
    CONST_OF[$word]="$const"
    # The run functions this predicate dispatches to (`runTermOnce`,
    # `runUIStateSubcommand`, …): a test that CALLS one drives the verb for real
    # without ever spelling an argv — the `cli`/`ui_state` shape.
    if [ -n "$file" ]; then
        for fn in "is${pred}" ${helper:-}; do
            s=$(grep -nE "^func ${fn}\(args \[\]string\) bool" "$file" | head -1 | cut -d: -f1 || true)
            if [ -z "$s" ]; then
                continue
            fi
            e=$(awk -v s="$s" 'NR>s && /^}/ {print NR; exit}' "$file" || true)
            if [ -z "$e" ]; then
                e=$((s + 40))
            fi
            sed -n "${s},${e}p" "$file" | { grep -oE 'run[A-Za-z0-9]+' || true; }
        done | sort -u | while read -r rf; do
            printf '%s\t%s\n' "$word" "$rf" >>"$TMP/runfns.tsv"
        done
    fi
done
sort -u "$TMP/runfns.tsv" -o "$TMP/runfns.tsv" 2>/dev/null || true

# --- 2. the ADVERTISED set: the built binary's own command index -------------
mapfile -t ADV < <("$BIN" -h 2>&1 |
    grep -E '^ {2}gopi [a-z][a-z0-9_-]*( |$)' |
    sed -E 's/^ {2}gopi ([a-z][a-z0-9_-]*).*/\1/' | sort -u)
if [ "${#ADV[@]}" -lt 8 ]; then
    echo "g7-verb-census.sh: the binary's own -h advertised only ${#ADV[@]} verbs — insufficient" >&2
    exit 1
fi
declare -A ADVERTISED=()
for v in "${ADV[@]}"; do ADVERTISED[$v]=1; done

# --- 3. the union, sorted ----------------------------------------------------
mapfile -t VERBS < <({ printf '%s\n' "${!DISPATCHED[@]}"; printf '%s\n' "${!ADVERTISED[@]}"; } | sort -u)
# The union floor moved with the chain (verb-retirement, 2026-10-05): the
# chain lost ~30 predicates, so the union of dispatched|advertised is now a
# dozen words. The floor still does its job — a parse that derived nothing (or
# stopped at the first line) must be distinguishable from a small surface.
if [ "${#VERBS[@]}" -lt 8 ]; then
    echo "g7-verb-census.sh: derived only ${#VERBS[@]} verbs — refusing to emit a vacuous census" >&2
    exit 1
fi
printf '%s\n' "${VERBS[@]}" >"$TMP/verbs.txt"

# --- 4. the observation corpora ---------------------------------------------
# The census's OWN test file is excluded from every corpus below, exactly as the
# census's own doc is: its lines are a claim about usage, not usage. (Measured:
# with it included, the gate's own argv patterns counted as test drives and the
# table drifted by one on every regeneration.)
CENSUS_SELF_TEST="cli_verb_census_test.go"

# 4-pre. The RUNNER helpers this repo's tests hand an argv to — discovered from
# their variadic signature, not typed. A verb spelled on such a call's own line is
# an argv element by construction.
{
    grep -rhoE 'func ([A-Za-z0-9]+)\(t \*testing\.T[^)]*\.\.\.string\)' --include='*_test.go' --exclude="$CENSUS_SELF_TEST" cmd testsuite 2>/dev/null || true
    grep -rhoE 'func \([a-z] \*[A-Za-z0-9]+\) ([A-Za-z0-9]+)\(t \*testing\.T[^)]*\.\.\.string\)' --include='*_test.go' --exclude="$CENSUS_SELF_TEST" cmd testsuite 2>/dev/null || true
} | sed -E 's/^func (\([a-z] \*[A-Za-z0-9]+\) )?//;s/\(t \*testing\.T.*//' | sort -u |
    grep -vE '^run$' >"$TMP/runners.txt" || true
RUNNER_RE=$(paste -sd'|' "$TMP/runners.txt" || true)
if [ -z "$RUNNER_RE" ]; then
    echo "g7-verb-census.sh: no variadic test runner discovered — the runner shape changed" >&2
    exit 1
fi

# 4a. COMPOSITE-LITERAL argv: the verb is the FIRST element of the argv the test
# hands over — `[]string{"worker", …}` or a table row `[][]string{{CliVerbMerger,
# stopFlag}, …}`. An occurrence anywhere else in a literal is NOT an argv: the
# completion lists and dispatcherOfToken maps spell the constants in map values
# and name lists, which is metadata about the verb, not a run of it.
: >"$TMP/drive_lit.txt"
: >"$TMP/drive_fn.txt"
: >"$TMP/drive_runner.txt"
for w in "${VERBS[@]}"; do
    const="${CONST_OF[$w]:-}"
    alt="\"${w}\""
    if [ -n "$const" ]; then
        alt="\"${w}\"|${const}"
    fi
    # The shape: an argv literal — `{` or `{{` (a []string element inside a
    # [][]string table) immediately followed by the verb, with a comma after it
    # (a one-word literal `{CliVerbX}` is a map VALUE in the completion-free
    # metadata tables, not an argv — requiring the comma is what separates them).
    # A package qualifier is tolerated: `{toolnames.ToolScreenCapture, …}`.
    pat="\\{\\{?[[:space:]]*([a-z][A-Za-z0-9]*\\.)?(${alt})[[:space:]]*,"
    n=$(grep -rhE "$pat" --include='*_test.go' --exclude="$CENSUS_SELF_TEST" cmd testsuite 2>/dev/null |
        { grep -v -- '-h"' || true; } | { grep -v -- '"--help' || true; } | grep -c . || true)
    if [ "$n" -gt 0 ]; then
        printf '%s %s\n' "$n" "$w" >>"$TMP/drive_lit.txt"
    fi
done

# 4b. RUN-FUNCTION calls: a test calling the verb's own run function.
if [ -s "$TMP/runfns.tsv" ]; then
    while IFS=$'\t' read -r w rf; do
        if [ -z "${rf:-}" ]; then
            continue
        fi
        n=$(grep -rlE "\b${rf}\(" --include='*_test.go' cmd testsuite 2>/dev/null | grep -c . || true)
        if [ "$n" -gt 0 ]; then
            printf '%s %s\n' "$n" "$w" >>"$TMP/drive_fn.txt"
        fi
    done <"$TMP/runfns.tsv"
fi

# 4c. RUNNER lines: the verb spelled on a line that calls a discovered runner.
# This is the floor for the `cli`/`web_fetch` shape (a bare argument to a variadic
# runner, no slice literal anywhere).
while read -r w; do
    n=$(grep -rhnE "(${RUNNER_RE})\(" --include='*_test.go' --exclude="$CENSUS_SELF_TEST" cmd testsuite 2>/dev/null |
        grep -cE "\"${w}\"" || true)
    if [ "$n" -gt 0 ]; then
        printf '%s %s\n' "$n" "$w" >>"$TMP/drive_runner.txt"
    fi
done <"$TMP/verbs.txt"

awk '{c[$2]+=$1} END {for (k in c) print c[k], k}' \
    "$TMP/drive_lit.txt" "$TMP/drive_fn.txt" 2>/dev/null |
    sort -k2 >"$TMP/drive_count.txt" || true
awk '{c[$2]+=$1} END {for (k in c) print c[k], k}' \
    "$TMP/drive_runner.txt" 2>/dev/null |
    sort -k2 >"$TMP/runner_count.txt" || true

# 4d. USAGE-LOG: a command a session ACTUALLY ran — the `"command"` field of a
# recorded bash tool call. `gopi <verb> --help` is NOT a run (Rule 31), so an
# occurrence carrying a help flag is dropped before counting: a verb whose only
# appearance in the whole corpus is a help query has been queried, not used, and
# it must not buy this class.
#
# THE FILE LIST IS HANDED TO rg, NEVER THE ROOT — measured 2026-10-01 at base
# commit 81e827f5 (Rule 38: the numbers below are `find "$SESSIONS" -type d |
# wc -l` and `ls -1 "$SESSIONS" | wc -l`, the file counts are `find … -name
# session.jsonl | wc -l` against `rg --files --glob session.jsonl "$SESSIONS" |
# wc -l`, and the arm timings are `/usr/bin/time` on this host). Re-running those
# five commands a day later already gives 162788 / 80179 — the corpus only grows,
# so the COUNT is not the claim; the SHAPE below is, and it does not change.
#
#   Handing the ROOT to rg (`rg … --glob session.jsonl "$SESSIONS"`) made rg WALK
#   the whole tree: getdents64 = 320841 calls and openat = 163604 calls, because
#   `--glob` filters AFTER the walk — every directory is enumerated once, including
#   the enormous majority that holds no session.jsonl at all (162787 directories in
#   all, 79771 of them immediate children, against 3184 session.jsonl files). Why
#   it is a defect is not the count but the SHAPE: one getdents per directory
#   bypasses the directory entry cache, so the stage is fast when the disk is idle
#   (4.03s, 2.31s, 3.86s measured) and minutes when it is busy (16.65s measured in
#   one contended arm). Handing it the MANIFEST instead — which is the whole
#   change — leaves 320847 getdents64 (the find half; the literal count above is
#   rg's own) and reads only the 3184 files that can match, and the same three
#   `"command"` occurrences come out (39143 lines both ways).
#
# THE ESCAPE HATCHES ARE THE POINT OF THE MIRRORING BELOW. `-o`/`--type f` is what
# keeps the manifest a set of REGULAR FILES: a dangling name would make the xargs
# batch exit non-zero and take the stage's output with it, where rg's own traversal
# would simply have read nothing. `-name '.*' -type d -prune` mirrors the ignore
# set rg applies (rg 13.0.0 skips hidden paths by default): of the 3430 files a
# plain `find -name session.jsonl` finds, 246 sit under a hidden directory of the
# corpus (nested sandbox/e2e profiles, e.g.
# `<id>/e2e/home/.gopi/sessions/<id>/session.jsonl`) and rg never read them, so
# feeding it all 3430 would CHANGE this column rather than preserve it — measured:
# the pruned manifest is 3184 files and differs from `rg --files --glob
# session.jsonl` by 0 lines. (`rg --files` is the oracle for that equivalence, not
# a tool this script may call: the point of the change is to stop relying on rg's
# own traversal — and `-uu` would read the hidden 246 and diverge.)
SESSIONS="${GOPI_HOME:-$HOME/.gopi}/sessions"
: >"$TMP/usage_count.txt"
if [ -d "$SESSIONS" ]; then
    find "$SESSIONS" -name '.*' -type d -prune -o -type f -name session.jsonl -print0 >"$TMP/usage_files.nul" 2>/dev/null || true
    if [ -s "$TMP/usage_files.nul" ]; then
        { xargs -0 -r rg -o -N --no-filename '"command": *"[^"]{0,400}' <"$TMP/usage_files.nul" 2>/dev/null || true; } |
            grep -oE 'gopi [a-z][a-z0-9_-]*( |\\)?(--help| -h)?[^"]{0,80}' |
            awk '!/--help| -h/' | awk '{print $2}' | sort | uniq -c | sort -k2 >"$TMP/usage_count.txt" || true
    fi
fi

# 4e. DOC-RECIPE: `gopi <verb>` inside a fenced block of devdoc/ — the recipes a
# reader copies and runs. This census's own doc is excluded: its rows are a claim
# about usage, not usage.
# A recipe is a command line: the verb followed by a space or the end of the
# line. `... gopi capture` + a backtick` inside a sentence is a MENTION of the
# verb, not a recipe somebody runs, and it is dropped here for that reason.
ls devdoc/*.md 2>/dev/null | grep -v "gopi-verb-census.md" >"$TMP/docfiles.txt" || true
if [ -s "$TMP/docfiles.txt" ]; then
    xargs -r awk '/^[[:space:]]*```/ {f = !f; next} f {print}' <"$TMP/docfiles.txt" 2>/dev/null |
        { grep -oE '\.?/?gopi [a-z][a-z0-9_-]*([[:space:]]|$)' || true; } |
        sed -E 's|^\.?/?gopi ||' | awk '{print $1}' |
        sort | uniq -c | sort -k2 >"$TMP/doc_count.txt" || true
fi

# 4f. HELP-ONLY anchor: a verb the contract suite shakes down with `<verb> -h`.
# Help is not a run, so this never makes a verb class a:test — it only separates
# "the dispatch itself has been exercised" from "nothing has ever run it".
# The pattern takes the FIRST element of EITHER argv shape: a bare verb
# (`[]string{"version", "-h"}`) and — since the retirement — a GROUP address,
# where the group word comes first and the verb second
# (`[]string{CliVerbTool, CliVerbCapture, "-h"}`). Only the bare shape existed
# before, which is why the group shape has to be stated: the old pattern
# demanded "-h" immediately after the first element and silently derived
# help_only=0 for every tool the group serves.
grep -nE '\[\]string\{[^}]*, *"(-h|--help)"' cmd/agent/cli_contracts_test.go 2>/dev/null |
    grep -oE '\[\]string\{[^}]*' | sed 's/^\[\]string{//' | tr -d '"' |
    tr ',' '\n' | sed 's/^ *//;s/ *$//' |
    sort -u >"$TMP/anchor.txt" || true

count_in() { # count_in <file> <verb> → the count, 0 when absent
    local n
    n=$(awk -v v="$2" '$2==v {print $1}' "$1" | head -1 || true)
    printf '%s' "${n:-0}"
}

# --- 5. emit one row per verb ------------------------------------------------
printf 'verb\tdispatched\tadvertised\ttest_drive\trunner_line\tusage_log\tdoc_recipe\thelp_only\tclass\n'
a_class=0
c_class=0
h_class=0
r_class=0
for w in "${VERBS[@]}"; do
    dispatch=${DISPATCHED[$w]:-0}
    ad=${ADVERTISED[$w]:-0}
    drive=$(count_in "$TMP/drive_count.txt" "$w")
    rline=$(count_in "$TMP/runner_count.txt" "$w")
    ulog=$(count_in "$TMP/usage_count.txt" "$w")
    doc=$(count_in "$TMP/doc_count.txt" "$w")
    if grep -qx "$w" "$TMP/anchor.txt"; then
        honly=1
    else
        honly=0
    fi

    if [ "$drive" -gt 0 ]; then
        class="a:test"
        a_class=$((a_class + 1))
    elif [ "$rline" -gt 0 ]; then
        # A runner LINE mentions both the helper and the verb, which is weaker
        # evidence than an argv literal: it is a class of its own, never a:test.
        class="b:runner-line"
        r_class=$((r_class + 1))
    elif [ "$ulog" -gt 0 ]; then
        class="b:usage-log"
    elif [ "$doc" -gt 0 ]; then
        class="b:doc-recipe"
    elif [ "$honly" -eq 1 ]; then
        class="b:help-only"
        h_class=$((h_class + 1))
    else
        class="c:neither"
        c_class=$((c_class + 1))
    fi
    printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$w" "$dispatch" "$ad" "$drive" "$rline" "$ulog" "$doc" "$honly" "$class"
done
echo "g7-verb-census.sh: ${#VERBS[@]} verbs, ${a_class} a:test, ${r_class} b:runner-line, ${h_class} b:help-only, ${c_class} c:neither" >&2
