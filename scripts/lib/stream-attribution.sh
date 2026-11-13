# shellcheck shell=bash
# stream-attribution.sh — the ONE reader of "where did the wall-clock go?",
# over a run's own `go test -json` stream (Rule 14: one definition, sourced by
# the entry point AND driven directly by the gate).
#
# WHY THIS EXISTS (measured 2026-10-15, bug row on `cmd/agent` + the 25m cap)
# --------------------------------------------------------------------------
# A run that tripped the per-package budget printed `exit=3 outcome=timeout`
# and a `slowest 15 tests` list, and NOTHING that said which package absorbed
# the wall or which arms account for it. The bug row's own honest gap was
# exactly this: "the heartbeat's last-started test names do not prove
# attribution". The numbers were already IN the stream — every finished test
# carries `Elapsed` — so the run can attribute itself; nothing new has to be
# measured and no device work is generated (Rule 27: this is a capped read of a
# file the run already wrote).
#
# THE THREE READINGS, and each is a DIFFERENT claim (Rule 15 — a number must
# name what it is):
#
#   ΣElapsed (finished)     the sum of the `Elapsed` values of the tests that
#                           FINISHED, for a package whose own run COMPLETED.
#                           Comparable with that package's wall clock.
#   ΣElapsed (LOWER BOUND)  the same sum for a package that was STILL RUNNING
#                           when the cap stopped it. Arms that never finished
#                           carry no `Elapsed` at all, so the sum is a floor on
#                           what the package cost — never printed without the
#                           word LOWER BOUND beside it (Rule 15: an unlabelled
#                           partial sum reads as the whole answer).
#   arms started but never finished
#                           the tests that emitted a `run` event and no
#                           terminal one. This is the count that READS the
#                           lower bound: it is the gap between the sum and the
#                           real cost.
#
# HOW "still running" IS DECIDED, and it is NOT "the package failed": a killed
# package emits a package-level `fail` verdict exactly like a package whose
# assertion failed (measured: run-1792019037's stream carries a `fail` for
# cmd/agent, which the cap had just killed). The discriminating evidence is go's
# OWN panic line, read PACKAGE-SCOPED: the timeout panic is emitted as `output`
# events carrying the killed package's Package value, so a package with that
# line in its own events is a package whose run the cap ended. A package that
# never reached a package-level verdict is treated the same way — no verdict is
# not proof of completion.
#
# SUBTESTS are recorded by `go test -json` too, and they would double-count their
# parent (`TestX/arm` and `TestX` both carry Elapsed). They are therefore
# EXCLUDED from every sum here, and the exclusion is stated in the block's own
# heading, printed by the caller — never hidden in this function (Rule 15).
#
# WHAT go's OWN DUMP CANNOT SAY, and why this block computes its own age.
# `running tests:` is rendered by runningList (Go 1.x testing/testing.go:2490)
# as `name (Round(highPrecisionTimeSince(start), time.Second))` — i.e. the age
# is ROUNDED TO WHOLE SECONDS and it is the age AT THE INSTANT OF THE DUMP. So
# the newest starter reads `(2s)` beside a package that burned 600s, and a
# reader concludes "it just started, so it cannot be the culprit" — which is
# what sent a whole investigation at a test that passes in 7.7s on its own
# (measured 2026-11-08, row 01a1b971). The stream carries the two exact
# timestamps that answer it (the arm's `run` event and the `running tests:`
# line), so this reader subtracts them and prints the result to the
# MILLISECOND, beside the names, in the block every run prints.
#
# The printer takes its wording as ARGUMENTS rather than hardcoding sentences:
# the constants that name each label live in the entry point (one spelling per
# label, Rule 13), so a reader greps the script for the phrase and finds both
# the producer and the gate's expectation.
gopi_stream_attribution() {
    local path="${1:?gopi_stream_attribution needs a stream path}"
    local n="${2:-15}"
    shift 2 || true
    # Every one of these is supplied by the caller (see the constant block in
    # scripts/test-budget.sh) and defaulted only so a missing argument degrades
    # to English instead of printing a half-rendered line.
    local pkg_fmt="${1:-  %s: %s = %ss over %d finished arm(s)}"
    local unfinished_fmt="${2:-  ^ %d arm(s) started and never finished: %s}"
    local arm_header="${3:-  top arms by wall-clock (seconds):}"
    local arm_fmt="${4:-  %8.2f  %s}"
    local none_line="${5:-  (no finished test carried an Elapsed in this stream)}"
    local lower_label="${6:-ΣElapsed (LOWER BOUND)}"
    local plain_label="${7:-ΣElapsed}"
    local cut_marker="${8:-panic: test timed out after}"
    local running_marker="${9:-running tests:}"
    local age_fmt="${10:-    %s: in flight %.1fs when the cap expired (its run event %s, the cap line %s)}"
    local unknown_age_fmt="${11:-    %s: in flight for an UNMEASURED time — the stream carries no 'running tests:' timestamp to measure against}"
    command -v python3 >/dev/null 2>&1 || return 0
    [ -s "$path" ] || return 0
    python3 - "$path" "$n" "$pkg_fmt" "$unfinished_fmt" "$arm_header" "$arm_fmt" \
        "$none_line" "$lower_label" "$plain_label" "$cut_marker" \
        "$running_marker" "$age_fmt" "$unknown_age_fmt" <<'PY'
import datetime, json, re, sys

(path, n, pkg_fmt, unfinished_fmt, arm_header, arm_fmt,
 none_line, lower_label, plain_label, cut_marker, running_marker,
 age_fmt, unknown_age_fmt) = (
    sys.argv[1], int(sys.argv[2]), sys.argv[3], sys.argv[4], sys.argv[5],
    sys.argv[6], sys.argv[7], sys.argv[8], sys.argv[9], sys.argv[10],
    sys.argv[11], sys.argv[12], sys.argv[13])

def parse_time(s):
    """Parse go test -json's RFC3339Nano Time. The fraction carries nanoseconds
    and fromisoformat stops at 6 digits, so it is cut to microseconds."""
    m = re.match(r"(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d)(\.\d+)?(.*)", s or "")
    if not m:
        return None
    base = datetime.datetime.fromisoformat(m.group(1) + m.group(3))
    return base + datetime.timedelta(seconds=float(m.group(2) or 0))

order, verdict = [], set()
cut = set()            # packages whose own run the wall-clock cap ended
cut_at = {}            # pkg -> Time of go's own "running tests:" line
finished = {}          # (pkg, top-level test) -> Elapsed
started = {}           # pkg -> [top-level tests that emitted `run`]
started_at = {}        # (pkg, top-level test) -> Time of its `run` event
with open(path, errors="replace") as fh:
    for line in fh:
        line = line.strip()
        if not line.startswith("{"):
            continue
        try:
            ev = json.loads(line)
        except ValueError:
            continue
        action, pkg, test = ev.get("Action"), ev.get("Package"), ev.get("Test")
        if not pkg:
            continue
        if pkg not in order:
            order.append(pkg)
        if action == "output":
            out = ev.get("Output") or ""
            # go's own panic text, carrying the Package it was emitted for.
            if cut_marker in out:
                cut.add(pkg)
            # The `running tests:` line is the CUT INSTANT — the only timestamp
            # in the stream that says WHEN the cap expired. An unfinished arm's
            # own `run` time is measured against it, which is what turns "it was
            # in flight" into "it had been in flight N s" without inheriting
            # go's whole-second rounding.
            if running_marker in out:
                cut_at.setdefault(pkg, ev.get("Time"))
            continue
        if test is None or test == "":
            if action in ("pass", "fail", "skip"):
                verdict.add(pkg)
            continue
        if "/" in test:
            continue   # a subtest: its parent already carries the cost
        if action == "run":
            started.setdefault(pkg, []).append(test)
            started_at[(pkg, test)] = ev.get("Time")
        elif action in ("pass", "fail", "skip"):
            finished[(pkg, test)] = ev.get("Elapsed", 0.0)

for pkg in order:
    arms = {t: s for (p, t), s in finished.items() if p == pkg}
    total = sum(arms.values())
    begun = started.get(pkg, [])
    unfinished = [t for t in begun if t not in arms]
    complete = pkg in verdict and pkg not in cut
    print(pkg_fmt % (pkg, plain_label if complete else lower_label,
                     "%.1f" % total, len(arms)))
    if unfinished:
        print(unfinished_fmt % (len(unfinished), ", ".join(sorted(unfinished))))
        # THE AGE of each unfinished arm, measured from its own `run` event to
        # go's `running tests:` line — the one timestamp in the stream that says
        # WHEN the cap expired. This is the number `running tests:` cannot give:
        # its `(Ns)` is an age too, but go truncates it to whole seconds and
        # prints it INSIDE the panic, so a reader comparing `(0s)` against "the
        # suite ran 600s" concludes the opposite of the truth.
        cut_t = parse_time(cut_at.get(pkg))
        for name in sorted(unfinished):
            began = parse_time(started_at.get((pkg, name)))
            if cut_t is None or began is None:
                print(unknown_age_fmt % name)
                continue
            print(age_fmt % (name, (cut_t - began).total_seconds(),
                             began.strftime("%H:%M:%S.%f")[:-3],
                             cut_t.strftime("%H:%M:%S.%f")[:-3]))
    if not arms:
        print(none_line)
        continue
    print(arm_header)
    for name, sec in sorted(arms.items(), key=lambda kv: (-kv[1], kv[0]))[:n]:
        print(arm_fmt % (sec, name))
PY
}
