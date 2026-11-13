#!/usr/bin/env bash
# flake-catch.sh — the ORDER/DETERMINISM flake catch mechanism: re-run a suspect
# scope N times with `go test -count=N` and NAME every test whose verdict MOVES
# across repetitions. This is a NAMED OPT-IN (Makefile target `flake-catch`),
# deliberately OFF the smoke/merge path — `make test` stays smoke-only.
#
# WHY REPETITION (and why nothing else)
# -------------------------------------
# A flake's signature is a verdict that moves while the CODE and the HOST stay
# the same: map-iteration order, a shared fixture root, a pid slot, a
# wall-clock seeding race. Duration cannot see it (that is the heavy class,
# scripts/lib/heavy-threshold.sh), and load cannot see it either: generating
# device load or pressure to make a flake appear is FORBIDDEN absolutely by
# GOPI.md Rule 27 — it manufactures a broken host and then measures that. The
# instrument for this class is the test itself, run again: `go test -count=N`.
# Same code, no load, and the verdict is the measurement.
#
# WHAT IT SAYS
#   FLAKY   <pkg> <test>  pass=N fail=M  (rate)  -> the verdict moved: a DEFECT
#   FAILING <pkg> <test>  failed all M times    -> not a flake: a plain defect
# A test that passes every repetition is NOT reported stable-and-fine as a
# finding — a clean run says "no verdict moved", which is the absence of a
# finding at N repetitions, never a proof of determinism (a 1-in-20 flake needs
# `-reps 20`; the report prints the N it used so the claim is never wider than
# the run). That asymmetry is deliberate: this script's job is to NAME the
# moving verdict, not to bless the quiet ones.
#
# REGISTRY: testdata/flakes.tsv records the already-root-caused rows (one per
# known flake: test, package, deterministic root cause, owning ledger row, date).
# The loop is: a registry row names a test -> `make flake-catch PKGS=<its
# package>` re-runs it -> a verdict that still moves is a defect that is still
# live. The registry is NOT a suppression list, and this script never writes to
# it: catching a NEW flake means a human pins its deterministic cause and adds a
# row (the standards gate refuses a row without one).
#
# USAGE
#   make flake-catch                              # default scope, 10 reps
#   make flake-catch PKGS='./internal/tools/'     # a suspect package
#   make flake-catch PKGS='-reps 20 ./cmd/agent/' # raise the repetition count
#   scripts/flake-catch.sh -reps 20 ./internal/tools/
#
#   -reps N        repetitions (default $GOPI_FLAKE_REPS, else the shared
#                  default; the band is 2..100 — below it a run proves nothing,
#                  above it the mechanism becomes a load generator, which Rule 27
#                  forbids)
#   -timeout DUR   go test -timeout (default 300s)
#   -json FILE     keep the raw stream here (default: a scratch-queue file)
#   any other flag is FORWARDED to go test, so a run can be aimed at the one
#                  test a registry row names:  PKGS='-run TestTheFlakyOne ./pkg/'
#   -replay CASE   emit synthetic verdicts instead of running go test (CASE is
#                  one of the shared flake_replay_cases) — how the report's
#                  verdict logic is checked deterministically; never a real run
#   -h             this help
#
# Exit codes: 0 = report printed, no verdict moved (the tests RAN); 1 = a FLAKY
# test was named (the catch fired); 3 = a test failed EVERY repetition (a plain
# defect, not a flake); 4 = the scope measured NOTHING (empty, failed to build,
# all-SKIPPED, or filtered to nothing — never read as a clean green); 2 = usage.
#
# A skipped test is NOT a measured verdict: `-count=N` with a heaviness switch
# (GOPI_HEAVY_TESTS unset) skips exactly the tests a caller aimed at, and the
# first version of this script reported that as "no verdict moved" (exit 0).

set -uo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=lib/tmp-ban.sh
. "$root/scripts/lib/tmp-ban.sh"
# shellcheck source=lib/flake-threshold.sh
. "$root/scripts/lib/flake-threshold.sh"

reps="" # resolved after the flags: -reps > $flake_reps_env > the shared default
timeout_arg="${flake_timeout_default}"
stream=""
scope=""
# Flags the mechanism does not own are FORWARDED to go test, in order: the
# registry names a TEST, so the natural loop is to aim a catch run at that one
# test (`-run TestTheFlakyOne`) instead of repeating a whole package N times.
# Refusing them would push the caller into the wider, slower scope; swallowing
# them would silently ignore the narrowing and repeat far more than asked.
passthrough=()
replay="" # non-empty = emit synthetic verdicts instead of running go test

usage() { sed -n '2,/^$/p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; }

# flake_flag_takes_value reports whether a forwarded flag needs its next token —
# membership in the shared list, never a guess about the token's shape.
flake_flag_takes_value() {
    local flag="$1"
    for known in $flake_passthrough_value_flags; do
        [ "$flag" = "$known" ] && return 0
    done
    return 1
}

# flake_replay_emit CASE — write a synthetic `go test -json` stream for CASE
# (one of flake_replay_cases). The event shape is the minimum the report reads:
# package start, per-repetition test verdicts, package terminal.
flake_replay_emit() {
    local case="$1" i
    case " $flake_replay_cases " in
        *" $case "*) ;;
        *) echo "flake-catch: unknown replay case '$case' (want: $flake_replay_cases)" >&2; return 2 ;;
    esac
    printf '{"Action":"start","Package":"%s"}\n' "$flake_replay_pkg"
    if [ "$case" = "nothing" ]; then
        # A scope whose only test SKIPS: no verdict was measured at all. The
        # mechanism must say so (exit 4) instead of reporting a clean green.
        printf '{"Action":"run","Package":"%s","Test":"TestReplaySkipped"}\n' "$flake_replay_pkg"
        printf '{"Action":"skip","Package":"%s","Test":"TestReplaySkipped","Elapsed":0.01}\n' "$flake_replay_pkg"
    else
        for pair in $flake_replay_verdicts; do
            local name="${pair%%:*}" kind="${pair##*:}"
            printf '{"Action":"run","Package":"%s","Test":"%s"}\n' "$flake_replay_pkg" "$name"
            i=1
            while [ "$i" -le "$reps" ]; do
                case "$kind" in
                    pass) flake_replay_event pass "$name" ;;
                    fail) flake_replay_event fail "$name" ;;
                    moved)
                        # floor(reps/2) passes, the rest fail: a verdict that MOVES.
                        if [ "$i" -le $((reps / 2)) ]; then
                            flake_replay_event pass "$name"
                        else
                            flake_replay_event fail "$name"
                        fi ;;
                    sub)
                        # The PARENT always passes; only its SUBTEST moves. A
                        # subtest must never be NAMED (its scope is its parent,
                        # and a registry row names a top-level test).
                        flake_replay_event pass "$name"
                        local sub="$name/Subtest"
                        printf '{"Action":"run","Package":"%s","Test":"%s"}\n' "$flake_replay_pkg" "$sub"
                        if [ "$i" -le $((reps / 2)) ]; then
                            flake_replay_event pass "$sub"
                        else
                            flake_replay_event fail "$sub"
                        fi ;;
                esac
                i=$((i + 1))
            done
        done
    fi
    printf '{"Action":"fail","Package":"%s","Elapsed":1.0}\n' "$flake_replay_pkg"
}

# flake_replay_event ACTION TEST — one test verdict event.
flake_replay_event() {
    printf '{"Action":"%s","Package":"%s","Test":"%s","Elapsed":0.01}\n' "$1" "$flake_replay_pkg" "$2"
}

while [ "$#" -gt 0 ]; do
    case "$1" in
        -reps) [ "$#" -ge 2 ] || { echo "flake-catch: -reps needs a value" >&2; exit 2; }; reps="$2"; shift 2 ;;
        -timeout) [ "$#" -ge 2 ] || { echo "flake-catch: -timeout needs a value" >&2; exit 2; }; timeout_arg="$2"; shift 2 ;;
        -json) [ "$#" -ge 2 ] || { echo "flake-catch: -json needs a path" >&2; exit 2; }; stream="$2"; shift 2 ;;
        -replay) [ "$#" -ge 2 ] || { echo "flake-catch: -replay needs a case ($flake_replay_cases)" >&2; exit 2; }; replay="$2"; shift 2 ;;
        -h|--help) usage; exit 0 ;;
        -*)
            # A forwarded flag that takes a VALUE keeps its value, or the value
            # would be read as the scope (see flake_passthrough_value_flags).
            if flake_flag_takes_value "$1"; then
                [ "$#" -ge 2 ] || { echo "flake-catch: $1 needs a value" >&2; exit 2; }
                passthrough+=("$1" "$2"); shift 2
            else
                passthrough+=("$1"); shift
            fi
            ;;
        *) scope="$1"; shift ;;
    esac
done

# The repetition count: an explicit -reps wins, then the opt-in env var, then the
# shared default. The env var is reached through its ONE name, ${flake_reps_env}
# (shared with internal/flaketest and asserted by the standards gate) — never a
# bare "GOPI_FLAKE_REPS" literal (GOPI.md Rule 13).
if [ -z "$reps" ]; then
    reps="${!flake_reps_env:-$flake_default_reps}"
fi
# A value outside the band is REFUSED loudly (Rule 15: a knob that quietly does
# something else is worse than a knob that says no) — silently clamping it would
# let a caller believe it ran 100 repetitions when it ran 2.
case "$reps" in
    ''|*[!0-9]*) echo "flake-catch: reps must be an integer, got '${reps}'" >&2; exit 2 ;;
esac
if [ "$reps" -lt "$flake_min_reps" ] || [ "$reps" -gt "$flake_max_reps" ]; then
    echo "flake-catch: reps=${reps} is outside the band ${flake_min_reps}..${flake_max_reps}." >&2
    echo "flake-catch: below the floor a single pass proves nothing (this class's whole signature);" >&2
    echo "flake-catch: above the ceiling the mechanism stops being a mechanism and becomes a load" >&2
    echo "flake-catch: generator, which GOPI.md Rule 27 forbids absolutely." >&2
    exit 2
fi

[ -n "$scope" ] || scope="${flake_scope_default}"

# The raw stream is a large throwaway artifact: it belongs in the scratch queue
# (off the `/` partition, age-GC'd), never an ambient temp path (GOPI.md Rule 22).
if [ -z "$stream" ]; then
    stream_dir="$(gopi_scratch_mkdir "$flake_stream_sub")" || exit 2
    stream="$stream_dir/run.json"
else
    case "$stream" in /*) ;; *) stream="$PWD/$stream" ;; esac
fi

command -v python3 >/dev/null 2>&1 || {
    echo "flake-catch: python3 is required (stdlib json, reads the go test -json stream)" >&2
    exit 2
}

echo "flake-catch (order/determinism flake catch — repetition, never device load, GOPI.md Rule 27)"
echo "  scope:        $scope"
echo "  repetitions:  $reps   (go test -count=$reps)"
echo "  stream:       $stream"
echo

# shellcheck disable=SC2086
if [ -n "$replay" ]; then
    # A replay is a synthetic stream, so it can never be confused with a measured
    # run: it is NAMED in the stream (flake_replay_pkg) and it runs no test.
    flake_replay_emit "$replay" >"$stream" || exit 2
    go_rc=0
else
    # Flags first, packages last: `go test [flags] [packages]` is the canonical
    # order (measured: a value-taking flag placed AFTER the package list fails
    # with "flag needs an argument").
    go test -count="$reps" -json -timeout "$timeout_arg" "${passthrough[@]}" $scope >"$stream" 2>&1
    go_rc=$?
fi

# A named test in the scope keeps failing: go test exits non-zero AND says so in
# the stream. The stream is the report's input, so a non-zero exit is not fatal
# here — but a run that produced no readable stream is.
if [ ! -s "$stream" ]; then
    echo "flake-catch: go test wrote no stream to $stream (rc=$go_rc) — the scope or the" >&2
    echo "flake-catch: command is wrong; nothing was measured." >&2
    exit 2
fi

python3 - "$stream" "$flake_verdict_marker" "$reps" <<'PY'
import json, sys

stream, flaky_marker, requested = sys.argv[1], sys.argv[2], int(sys.argv[3])

# Verdict tally per TOP-LEVEL test. A subtest's verdict is reported to its parent
# by `go test -json`, so a name of the form parent/sub is skipped: naming a
# subtest would name the wrong owner, and the registry's rows are test functions.
per = {}
subtests = set()
for line in open(stream, errors="replace"):
    line = line.strip()
    if not line.startswith("{"):
        continue
    try:
        ev = json.loads(line)
    except ValueError:
        continue
    test = ev.get("Test")
    action = ev.get("Action")
    if not test or action not in ("pass", "fail", "skip"):
        continue
    # A SUBTEST is named parent/sub by `go test -json`, and it is NOT a name this
    # mechanism may act on: its re-run scope is its parent (`-run Parent`), and a
    # registry row names a top-level test function. Counting it would (a) name the
    # wrong owner and (b) inflate the repetition count the report prints. Measured
    # defect this replaces: a stream whose only moved verdict was a SUBTEST was
    # reported as `FLAKY ... TestX/Subtest` — the parent had not moved at all.
    if "/" in test:
        subtests.add((ev.get("Package", "?"), test))
        continue
    key = (ev.get("Package", "?"), test)
    per.setdefault(key, {"pass": 0, "fail": 0, "skip": 0})[action] += 1

measured = any(c["pass"] or c["fail"] for c in per.values())
if not measured:
    # Nothing was MEASURED: an empty scope, a compile failure, an all-skipped
    # scope (a heaviness classification silently skipping what the caller asked
    # for) or a filter that matched nothing. It is NOT a clean green — the whole
    # point of naming the class is that "no verdict moved" must mean the tests RAN.
    print("flake-catch: the run measured NO verdict — nothing was measured. Whether that is an"
          " empty scope, a compile failure, an all-skipped scope (a heaviness classification"
          " skipping what was asked for) or a filter that matched nothing, it is NOT a clean"
          " green (a mechanism that measured nothing must never read as 'no flake').")
    for (pkg, test), c in sorted(per.items()):
        print("  SKIP   %-14s %-52s skip=%d" % (pkg.rsplit("/", 1)[-1], test, c["skip"]))
    sys.exit(4)

observed = max(sum(c.values()) for c in per.values())
flaky, always, skipped = [], [], []
for (pkg, test), c in sorted(per.items()):
    total = c["pass"] + c["fail"]
    if c["fail"] and c["pass"]:
        flaky.append((pkg, test, c))
    elif c["fail"] and not c["pass"]:
        always.append((pkg, test, c))
    if total == 0:
        skipped.append((pkg, test, c))

print("repetitions requested: %d   observed per test (max): %d" % (requested, observed))
if observed < requested:
    print("NOTE: fewer repetitions were observed than requested — the stream may be truncated or")
    print("NOTE: the scope may have been narrowed by a -run filter passed through PKGS.")
print()

if flaky:
    print("FLAKY — the verdict MOVED across repetitions of the same code on the same host.")
    print("       This is a DEFECT (not a heaviness class): pin its deterministic root cause and")
    print("       add a %s row (test/package/root_cause/owner/date)." % "testdata/flakes.tsv")
    print("       owner is the FULL ledger id (36 chars) — an 8-hex chip is ambiguous")
    for pkg, test, c in flaky:
        total = c["pass"] + c["fail"]
        print("  %-6s %-14s %-52s pass=%d fail=%d  rate=%.0f%%" % (
            flaky_marker, pkg.rsplit("/", 1)[-1], test, c["pass"], c["fail"],
            100.0 * c["fail"] / total))
else:
    print("no verdict moved in this scope at %d repetition(s)." % requested)
    print("That is the ABSENCE of a finding at this N, not a proof of determinism: a 1-in-20 flake")
    print("needs `-reps 20`. The instrument is repetition (GOPI.md Rule 27) — never device load.")
print()

if always:
    print("FAILING EVERY REPETITION — not a flake: the test simply fails (%d)." % len(always))
    for pkg, test, c in always:
        print("  %-6s %-14s %-52s fail=%d" % ("FAILED", pkg.rsplit("/", 1)[-1], test, c["fail"]))
    print()

if skipped:
    print("SKIPPED — no verdict to measure (%d). A skip is not a pass: if the scope is meant to" % len(skipped))
    print("run, check that the classification switch (GOPI_HEAVY_TESTS) is not silently skipping it.")
    for pkg, test, c in skipped:
        print("  %-6s %-14s %-52s skip=%d" % ("SKIP", pkg.rsplit("/", 1)[-1], test, c["skip"]))
    print()

seen = len(per)
print("SUMMARY: %d repetition(s), %d top-level test(s) seen, %d flaky, %d failing, %d skipped"
      % (requested, seen, len(flaky), len(always), len(skipped)))
if subtests:
    # Say what was left out, so a reader never wonders why a moved subtest is absent.
    print("NOTE: %d subtest verdict(s) were ignored — a subtest's scope is its parent test, and a"
          " registry row names a top-level test." % len(subtests))

if flaky:
    sys.exit(1)
if always:
    sys.exit(3)
sys.exit(0)
PY
report_rc=$?

echo
echo "raw stream: $stream"
exit "$report_rc"
