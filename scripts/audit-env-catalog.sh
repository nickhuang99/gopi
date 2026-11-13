#!/usr/bin/env bash
# audit-env-catalog.sh — a READ-ONLY audit of the `gopi --help-env` catalog that
# is INCAPABLE of dispatching an agent run.
#
# WHAT IT PROVES, for every row of the catalog:
#   * a FLAGLESS row (em dash in the flag column) is refused BY NAME, and never
#     with a bare `unknown flag`;
#   * a FLAG-BEARING row's env-name-as-flag SPELLING parses: a value-taking knob
#     refuses the missing value by naming its real flag (`flag --prompt requires
#     a value`), and a BOOLEAN knob (value optional) consumes the spelling and
#     then has no command to run, so it prints usage — the named refusal IS the
#     proof of the equivalence, which is why no value is ever passed;
#   * NO row starts an agent run;
#   * the ledger gains ZERO rows — in the sandbox AND in the operator profile.
#
# WHY IT IS WRITTEN THIS WAY — the incident it exists to prevent
# (task 01a0de69-edfa-770e-b104-0d51ced38eb4; memory #6103/#6106):
#   A former probe ran `gopi --<env-as-flag> 1 version` for every row. For a
#   FLAG-BEARING row that is not a probe: `--gopi-prompt 1` IS `--prompt 1`
#   (documented equivalence, internal/config/flags.go FlagOverrides), so the
#   probe DISPATCHED a real agent run whose prompt was the literal "1" and
#   auto-registered a real ledger row (`[auto] 1`) — burning a model call — while
#   its classifier had no branch for "a run started": the row fell into a silent
#   bucket and the audit reported clean. MEASURED 2026-09-26 (this run's own
#   transcript): `--gopi-prompt 1 version` → `[gopi] loaded GOPI.md`,
#   `[gopi] session: …`, ledger rows 1 → 3 (new row 01a0de98-…, summary "1").
#
# THE THREE HYGIENE RULES THIS SCRIPT OBEYS
#   1. PROBE THE SPELLING, NEVER THE VALUE. Every probe is `--<env-as-flag>` with
#      NO value (`</dev/null`, no verb). Passing a value would make the probe a
#      real invocation of a flag-bearing row.
#   2. RUN INSIDE A SANDBOX. The script re-execs itself through `gopi tool sandbox run`
#      (Rule 34) — never the operator profile — so even a surprise dispatch writes
#      to the sandbox's own ledger, and the probes' cwd is the sandbox, never a
#      worktree (the cwd is what pins a worktree).
#   3. ASSERT ZERO LEDGER DELTA. Row ids are captured before and after; any
#      addition, and any run banner in the output, is an AUDIT FAILURE — never a
#      bucket to be counted.
#
# WHY AN UNOBSERVABLE LEDGER IS A FAILURE AND NOT A WARNING (task 01a10efc-518d,
# measured 2026-10-06). The two ledger readers once discarded the binary's stderr
# (`… task list 2>/dev/null`) and took their status from a PIPELINE they fed, so
# `sort`'s exit 0 stood in for the reader's. With a `task list` that refuses, the
# audit printed `sandbox ledger delta=none` for a reading that never happened and
# closed with `OK: no run dispatched, no bare unknown flag, zero ledger delta`,
# exit 0 — and when it did report, the sink printed
# `FAIL: the ledger could not be observed:` with an EMPTY reason tail, naming
# neither the channel nor the cause (Rule 15). "Zero delta" is the assertion this
# whole script rides, so an unmeasured ledger must never be able to produce it:
# each reader now keeps its own status and stderr, and an unobservable ledger
# fails loudly, naming the channel, the command and the reader's own reason.
#
# BUCKETS (a row landing anywhere unexpected is a failure, not a quiet outcome):
#   DASH_NAMED        flagless row, refused by name      (expected == em-dash rows)
#   FB_VALUE_NAMED    flag-bearing row, missing value named (`requires a value`)
#   FB_USAGE_PARSED   flag-bearing boolean row, spelling consumed, then usage
#   BARE_UNKNOWN      refused with a bare `unknown flag` (must be 0)
#   DISPATCHED        a run banner appeared (must be 0)
#   OTHER             anything else (must be 0)
# A bucket name may carry a `_CLASS_MISMATCH` suffix when the outcome disagrees
# with the table's own flag column — the row is then counted BOTH as its bucket
# and as a mismatch, so the disagreement can never hide inside a passing count.
#
# Rule 27: no load generation — 88 short invocations, each refused in-process.
# Rule 35: a ONE-SHOT the operator invokes; no timer, no daemon, no resident
# state. Its only artifact is the evidence transcript it is asked to write.
#
# USAGE: scripts/audit-env-catalog.sh [--bin PATH] [--evidence DIR] [--self-check]
#   --self-check  regression guard (default: OFF, so the audit itself stays a
#                 pure reading of the real catalog): drive the loop against THREE
#                 internal stub binaries and require each to fail for its own
#                 reason — (1) a flag-bearing spelling that STARTS A RUN must be
#                 classified DISPATCHED; (2) a stub that adds a ledger row in the
#                 SANDBOX profile without dispatching must fail the ledger
#                 assertion; (3) the same in the OPERATOR profile. Legs 2/3 exist
#                 because the run banner would otherwise take the credit for the
#                 ledger check. Without this guard a future edit could go back to
#                 PASSING A VALUE — the stub would then be dispatched into an
#                 ordinary bucket and nobody would notice.
# EXIT:  0 clean · 1 AUDIT FAILURE (dispatch / bare unknown flag / ledger delta /
#        bucket or class mismatch) · 2 no oracle (no binary, no table)
set -u

readonly SANDBOX_LABEL="env-catalog-audit"
readonly PROBE_TIMEOUT_SEC=20
readonly EM_DASH="—"
readonly BANNER_A="loaded GOPI.md"
readonly BANNER_B="session: "

# The regression stubs, held as data and written into the work dir, never into
# the tree (Rule 30). The first is a minimal "gopi" whose flag-bearing spelling
# DISPATCHES; the second never dispatches but ADDS a ledger row, so the ledger
# assertion is red-proved on its own (the banner cannot take the credit).
readonly STUB_SOURCE='#!/usr/bin/env bash
case "$1" in
  --help-env) echo "  GOPI_PROMPT  prompt  --prompt"; exit 0;;
  version) echo v0.1.0-stub; exit 0;;
  --gopi-prompt) echo "[gopi] loaded GOPI.md (100 bytes)"; echo "[gopi] session: /nowhere/session.jsonl"; exit 1;;
  task) exit 0;;
  *) echo "gopi: unknown flag $1"; exit 1;;
esac'
# @ADD_ON@ = the ordinal `task list` call whose answer gains a row: 3 = after the
# sandbox's before-ledger read, 4 = after the operator's.
readonly LEDGER_STUB_SOURCE='#!/usr/bin/env bash
state="${0}.state"
case "$1" in
  --help-env) echo "  GOPI_PROMPT  prompt  --prompt"; exit 0;;
  task)
    n=$(cat "$state" 2>/dev/null || echo 0); n=$((n + 1)); echo "$n" > "$state"
    echo "#aaaa1111 row one"
    [ "$n" = "@ADD_ON@" ] && echo "#bbbb2222 row added by the stub"
    exit 0;;
  --gopi-prompt) echo "gopi: flag --prompt requires a value" >&2; exit 1;;
  *) echo "gopi: unknown flag $1"; exit 1;;
esac'

# Leg 3's stub: the ledger reader is REFUSED. Its stderr carries a real reason the
# audit must QUOTE — the defect being repaired is the opposite case: an empty tail.
readonly UNREADABLE_STUB_SOURCE='#!/usr/bin/env bash
case "$1" in
  --help-env) echo "  GOPI_PROMPT  prompt  --prompt"; exit 0;;
  task) echo "gopi: task: cannot open the ledger at /nonexistent/memory.db (unable to open database file)" >&2; exit 1;;
  --gopi-prompt) echo "gopi: flag --prompt requires a value" >&2; exit 1;;
  *) echo "gopi: unknown flag $1" >&2; exit 1;;
esac'

die() { printf '%s: %s\n' "${0##*/}" "$*" >&2; exit 2; }
say() { printf '%s\n' "$*"; }

# Rule 22: a temp workspace must be provably OFF the `/` partition — the property
# is measured (device id), never spelled. scripts/lib/tmp-ban.sh is the ONE
# reader of the scratch root (GOPI_TEST_SCRATCH_HOME, else GOPI_TEST_HOME's
# workspace — the same tree the Go suite GC sweeps); gopi_scratch_mkdir asserts
# by DEVICE ID that what it hands back is off `/`, registers it for trap-time
# cleanup, and never falls back to /tmp.
TMPBAN="$(cd "$(dirname "$0")" && pwd)/lib/tmp-ban.sh"
[ -r "$TMPBAN" ] || die "the shared scratch helper is missing: $TMPBAN"
# shellcheck source=lib/tmp-ban.sh
. "$TMPBAN"

BIN=""
EVID=""
SELF_CHECK=0
while [ $# -gt 0 ]; do
  case "$1" in
    --bin) BIN="${2:-}"; shift 2;;
    --evidence) EVID="${2:-}"; shift 2;;
    --self-check) SELF_CHECK=1; shift;;
    -h|--help) sed -n '2,70p' "$0"; exit 0;;
    *) die "unknown argument: $1";;
  esac
done

SELF="$(cd "$(dirname "$0")" && pwd)/$(basename "$0")"
[ -f "$SELF" ] || die "cannot resolve my own path ($0)"

# ---- LEG 1: outside the sandbox — hand off to `gopi tool sandbox run` (Rule 34) ----
if [ "${ENV_AUDIT_SANDBOXED:-}" != "1" ]; then
  if [ -z "$BIN" ]; then
    BIN="$(command -v gopi || true)"
    [ -n "$BIN" ] || die "no gopi binary: pass --bin PATH or put gopi on PATH"
  fi
  command -v gopi >/dev/null 2>&1 || die "gopi is not on PATH: cannot reach the sandbox layer"
  if [ -z "$EVID" ]; then
    if [ -n "${GOPI_EXECUTOR_SESSION_DIR:-}" ]; then EVID="$GOPI_EXECUTOR_SESSION_DIR"
    elif [ -n "${GOPI_EXECUTOR_SESSION_ID:-}" ]; then EVID="$HOME/.gopi/sessions/$GOPI_EXECUTOR_SESSION_ID"
    else EVID="$PWD"; fi
  fi
  mkdir -p "$EVID" || die "cannot create the evidence dir $EVID"
  TASK_ID="${GOPI_TASK_ID:-${GOPI_EXECUTOR_TASK_ID:-unknown}}"
  # `gopi tool sandbox run` materialises the sandbox AND sets the child's cwd to it,
  # so the probes never stand in a worktree.
  # The TOOL-group address, not the retired flat `gopi sandbox`: the flat word is
  # refused at argv[0] since the verb-retirement epic (2026-10-05), and this is the
  # hand-off Rule 34 requires before any probe runs.
  exec gopi tool sandbox run --label "$SANDBOX_LABEL" --task-id "$TASK_ID" -- \
    env ENV_AUDIT_SANDBOXED=1 ENV_AUDIT_BIN="$BIN" ENV_AUDIT_EVIDENCE="$EVID" \
        ENV_AUDIT_SELF_CHECK="$SELF_CHECK" "$SELF"
fi

# ---- LEG 2: inside the sandbox ---------------------------------------------
SB="${GOPI_HOME:-}"
[ -n "$SB" ] || die "not sandboxed: GOPI_HOME is unset in the sandboxed leg"
EVID="${ENV_AUDIT_EVIDENCE:-}"
[ -n "$EVID" ] || die "sandboxed leg: no evidence dir"
mkdir -p "$EVID" || die "sandboxed leg: cannot write the evidence dir $EVID"
WORK="$(gopi_scratch_mkdir audit-env-catalog)" || die "cannot create a scratch work dir under $(gopi_scratch_home) — refusing to write temp files on /"
trap 'gopi_scratch_cleanup' EXIT
say "sandbox=$SB"
say "workdir=$WORK (off / by device: $(stat -c %d "$WORK") != $(stat -c %d /))"

# ---- the audit of ONE binary, against ONE evidence dir ----------------------
# prints its summary; exits 0 clean, 1 on any audit failure.
run_audit() {
  abin="$1"; aev="$2"; amsg=""
  [ -x "$abin" ] || die "binary not executable: $abin"

  # oracle: the catalog table — refuse an absent one (Rule 15b), and NAME why it
  # failed if it did: a refusal whose reason tail is empty is the defect this
  # script is being repaired for.
  "$abin" --help-env > "$aev/help-env.txt" 2>"$WORK/help-env.err"
  _hrc=$?
  if [ "$_hrc" != "0" ]; then
    _hwhy="$(head -1 "$WORK/help-env.err")"
    [ -n "$_hwhy" ] || _hwhy="(the binary wrote no stderr)"
    amsg="$amsg --help-env (\`$abin --help-env\`, audited profile) exited $_hrc; stderr: $_hwhy;"
  fi
  [ -s "$aev/help-env.txt" ] || die "--help-env produced an empty table — refusing to audit nothing"
  # row = exactly two leading spaces + an UPPERCASE name; last field = flag column
  awk '/^  [A-Z][A-Z0-9_]+[ \t]/ { if ($NF == "—" || $NF ~ /^--/) print $1 "\t" $NF }' \
    "$aev/help-env.txt" > "$aev/rows.tsv"
  rows_total=$(wc -l < "$aev/rows.tsv" | tr -d ' ')
  [ "$rows_total" -gt 0 ] || die "the table parsed to 0 rows — the row shape changed; refusing"
  rows_dash=$(awk -F'\t' -v d="$EM_DASH" '$2 == d' "$aev/rows.tsv" | wc -l | tr -d ' ')
  rows_flag=$((rows_total - rows_dash))

  # --- the two ledger readers -------------------------------------------------
  # Shape: the reader runs the binary ONCE and keeps that command's OWN exit
  # status; the CALLER writes the evidence file. An unobservable ledger therefore
  # leaves a file that exists and is EMPTY plus a NAMED reason in `amsg` — a
  # refusal whose reason tail is empty was the defect this repair closes (Rule 15).
  #
  # Why it is not a pipeline: `reader || note` followed by `reader | awk | sort`
  # looks right and is NOT — a pipeline's status is its LAST element's, so a
  # reader the binary REFUSES (a retired verb exits 1 — measured) is masked by
  # `sort`'s exit 0, the capture runs on an empty result, and `amsg` is never
  # touched. That is `2>/dev/null`'s failure one layer up. The reader's stderr is
  # kept in a file and quoted into the reason instead of being discarded.
  # `sandbox` reads the sandbox's own ledger (GOPI_HOME is the sandbox); `operator`
  # reads the operator's (both profile vars stripped). Prints the row ids.
  read_ledger() {
    _lprof="$1"; _lchan="$2"
    if [ "$_lprof" = "sandbox" ]; then "$abin" task list
    else env -u GOPI_HOME -u GOPI_SETTINGS "$abin" task list; fi \
      > "$WORK/ledger.raw" 2> "$WORK/ledger.err"
    _lrc=$?
    if [ "$_lrc" != "0" ]; then
      _lwhy="$(head -1 "$WORK/ledger.err")"
      [ -n "$_lwhy" ] || _lwhy="(the binary wrote no stderr)"
      amsg="$amsg [$_lchan] \`$abin task list\` exited $_lrc; the command's own first stderr line: $_lwhy;"
      return 0
    fi
    # A successful reader that prints NOTHING is not an empty ledger — a real
    # `task list` answer always carries a header. Silence here means the ledger
    # could not be observed, and an unmeasured ledger must never read as "no rows
    # were added" (the delta assertion rides this reading).
    if [ ! -s "$WORK/ledger.raw" ]; then
      amsg="$amsg [$_lchan] \`$abin task list\` exited 0 and printed nothing — an empty answer is indistinguishable from an unread ledger;"
      return 0
    fi
    awk '$1 ~ /^#[0-9a-f]/ {print $1}' "$WORK/ledger.raw" | sort
  }
  observe() { read_ledger "$1" "$2" > "$3"; }   # $1 profile · $2 channel · $3 evidence file
  observe sandbox  "sandbox ledger, the audit's own profile (GOPI_HOME=$SB)" \
                   "$aev/ledger-sandbox-before.txt"
  observe operator "operator ledger, read with GOPI_HOME/GOPI_SETTINGS stripped" \
                   "$aev/ledger-operator-before.txt"

  : > "$aev/buckets.tsv"
  while IFS=$'\t' read -r env flag; do
    [ -n "$env" ] || continue
    sp="--$(printf '%s' "$env" | tr 'A-Z_' 'a-z-')"
    timeout "$PROBE_TIMEOUT_SEC" "$abin" "$sp" </dev/null >"$WORK/out" 2>"$WORK/err"
    rc=$?
    first=$(head -1 "$WORK/err" | cut -c1-100)
    cls=OTHER
    if grep -qF "$BANNER_A" "$WORK/out" "$WORK/err" || grep -qF "$BANNER_B" "$WORK/out" "$WORK/err"; then
      cls=DISPATCHED
    elif grep -qE '^gopi: flag --[a-z0-9-]+ requires a value' "$WORK/err"; then
      cls=FB_VALUE_NAMED
    elif grep -qF "NO CLI flag of its own" "$WORK/err"; then
      cls=DASH_NAMED
    elif grep -qE '^gopi: unknown flag ' "$WORK/err"; then
      # one diagnostic names one specific case; the general one is the bare message
      case "$(tail -1 "$WORK/err")" in
        "gopi: unknown flag $sp") cls=BARE_UNKNOWN;;
        *) cls=OTHER;;
      esac
    elif grep -qF "gopi -h" "$WORK/out"; then
      cls=FB_USAGE_PARSED
    fi
    # disagreement with the table's own flag column is NAMED, never absorbed
    if [ "$flag" = "$EM_DASH" ] && [ "$cls" != "DASH_NAMED" ]; then cls="${cls}_CLASS_MISMATCH"; fi
    if [ "$flag" != "$EM_DASH" ] && [ "$cls" != "FB_VALUE_NAMED" ] && [ "$cls" != "FB_USAGE_PARSED" ]; then
      cls="${cls}_CLASS_MISMATCH"
    fi
    printf '%s\t%s\t%s\trc=%s\t%s\n' "$cls" "$env" "$flag" "$rc" "$first" >> "$aev/buckets.tsv"
  done < "$aev/rows.tsv"

  observe sandbox  "sandbox ledger, the audit's own profile (GOPI_HOME=$SB)" \
                   "$aev/ledger-sandbox-after.txt"
  observe operator "operator ledger, read with GOPI_HOME/GOPI_SETTINGS stripped" \
                   "$aev/ledger-operator-after.txt"
  # The delta. Same masking hazard as the readers, one step further on: the
  # obvious `led_sb=$(diff a b | grep '^>' | head -5)` cannot tell "the ledgers
  # match" from "diff never ran" — a missing evidence file makes diff exit 2, grep
  # exits 1 and prints nothing, and the summary then reads `delta=none`, i.e. a
  # CLEAN audit built on an observation that never happened. So diff's own status
  # is checked and anything but "identical"(0) / "differ"(1) is a named failure.
  ledger_delta() {
    _dd1="$1"; _dd2="$2"; _dlbl="$3"
    diff "$_dd1" "$_dd2" > "$WORK/diff.raw" 2> "$WORK/diff.err"
    _drc=$?
    case "$_drc" in
      0|1) ;;   # 0 = the two observations are identical; 1 = they differ. Both real.
      *) _dwhy="$(head -1 "$WORK/diff.err")"
         [ -n "$_dwhy" ] || _dwhy="(diff wrote no stderr)"
         amsg="$amsg [$_dlbl] the delta could not be computed: diff exited $_drc; first stderr line: $_dwhy;"
         return 0;;
    esac
    grep '^>' "$WORK/diff.raw" | head -5
  }
  led_sb="$(ledger_delta "$aev/ledger-sandbox-before.txt"  "$aev/ledger-sandbox-after.txt"  "sandbox ledger delta")"
  led_op="$(ledger_delta "$aev/ledger-operator-before.txt" "$aev/ledger-operator-after.txt" "operator ledger delta")"

  # counts are PREFIX matches, so a _CLASS_MISMATCH row is still counted here
  count() { awk -F'\t' -v c="$1" 'index($1, c) == 1' "$aev/buckets.tsv" | wc -l | tr -d ' '; }
  c_dash=$(count DASH_NAMED); c_fbv=$(count FB_VALUE_NAMED); c_fbu=$(count FB_USAGE_PARSED)
  c_bare=$(count BARE_UNKNOWN); c_disp=$(count DISPATCHED); c_other=$(count OTHER)
  c_mis=$(awk -F'\t' 'index($1, "_CLASS_MISMATCH") > 0' "$aev/buckets.tsv" | wc -l | tr -d ' ')
  c_flag=$((c_fbv + c_fbu))

  {
    say "rows=$rows_total em-dash=$rows_dash flag-bearing=$rows_flag"
    say "em-dash rows:      refused-by-name=$c_dash  (expected $rows_dash)  BARE-unknown-flag=$c_bare"
    say "flag-bearing rows: no-value-named-value=$c_fbv + boolean-usage=$c_fbu = $c_flag  (expected $rows_flag)"
    say "dispatched runs=$c_disp  other=$c_other  class-mismatch=$c_mis"
    say "sandbox ledger delta=${led_sb:-none}"
    say "operator ledger delta=${led_op:-none}"
  } | tee "$aev/summary.txt"

  status=0
  if [ "$c_disp" != "0" ]; then say "FAIL: $c_disp row(s) DISPATCHED a run:"; awk -F'\t' 'index($1,"DISPATCHED")==1{print "  "$1" "$2" "$3}' "$aev/buckets.tsv"; status=1; fi
  if [ "$c_bare" != "0" ]; then say "FAIL: $c_bare row(s) refused with a bare unknown flag:"; awk -F'\t' 'index($1,"BARE_UNKNOWN")==1{print "  "$1" "$2" "$3}' "$aev/buckets.tsv"; status=1; fi
  if [ "$c_other" != "0" ]; then say "FAIL: $c_other row(s) landed in OTHER:"; awk -F'\t' 'index($1,"OTHER")==1{print "  "$1" "$2" "$3" "$5}' "$aev/buckets.tsv"; status=1; fi
  if [ "$c_mis" != "0" ]; then say "FAIL: $c_mis row(s) disagree with the table's flag column:"; awk -F'\t' 'index($1,"_CLASS_MISMATCH")>0{print "  "$1" "$2" "$3}' "$aev/buckets.tsv"; status=1; fi
  if [ "$c_dash" != "$rows_dash" ]; then say "FAIL: refused-by-name=$c_dash but the table has $rows_dash em-dash rows"; status=1; fi
  if [ "$c_flag" != "$rows_flag" ]; then say "FAIL: flag-bearing outcomes=$c_flag but the table has $rows_flag flag-bearing rows"; status=1; fi
  if [ -n "$led_sb" ]; then say "FAIL: the SANDBOX ledger gained rows:"; say "$led_sb"; status=1; fi
  if [ -n "$led_op" ]; then say "FAIL: the OPERATOR ledger gained rows:"; say "$led_op"; status=1; fi
  if [ -n "$amsg" ]; then say "FAIL: the ledger could not be observed — the channel, the command and the reason, each named:"; say "$amsg"; status=1; fi
  if [ "$status" = "0" ]; then say "OK: no run dispatched, no bare unknown flag, zero ledger delta"
  else say "AUDIT FAILURE — see buckets.tsv"; fi
  say "--- one refused-by-name row (verbatim) ---"
  awk -F'\t' 'index($1,"DASH_NAMED")==1{print $2"\t"$5; exit}' "$aev/buckets.tsv"
  say "--- one flag-bearing row (verbatim) ---"
  awk -F'\t' 'index($1,"FB_")==1{print $1"\t"$2"\t"$5; exit}' "$aev/buckets.tsv"
  return "$status"
}

BIN="${ENV_AUDIT_BIN:-}"
if [ "${ENV_AUDIT_SELF_CHECK:-0}" = "1" ]; then
  guard_fail=""
  # leg 1: a probe that DISPATCHES must be classified DISPATCHED and fail.
  printf '%s\n' "$STUB_SOURCE" > "$WORK/stub-gopi"
  chmod +x "$WORK/stub-gopi"
  mkdir -p "$WORK/guard"
  grc=0; run_audit "$WORK/stub-gopi" "$WORK/guard" || grc=$?
  gd=$(awk -F'\t' 'index($1,"DISPATCHED")==1' "$WORK/guard/buckets.tsv" | wc -l | tr -d ' ')
  say "self-check: dispatch-stub rows classified DISPATCHED=$gd  audit exit=$grc"
  if [ "$gd" = "0" ] || [ "$grc" != "1" ]; then
    say "GUARD FAIL: a probe that STARTS A RUN was not failed by the audit"
    say "  (this is the regression the guard exists for: a value-passing probe)"
    guard_fail=1
  fi
  # leg 2: a probe that dispatches NOTHING but ADDS a ledger row must still fail.
  # Without this leg the banner would take the credit for the ledger assertion:
  # a value-passing probe could hide behind a silent ledger write.
  for add_on in 3 4; do
    lstub="$WORK/ledger-stub-gopi-$add_on"   # per-leg path: the stub's state file rides it
    sed "s/@ADD_ON@/$add_on/" <<EOF > "$lstub"
$LEDGER_STUB_SOURCE
EOF
    chmod +x "$lstub"
    ldir="$WORK/guard-$add_on"; mkdir -p "$ldir"
    lrc=0; run_audit "$lstub" "$ldir" || lrc=$?
    ldisp=$(awk -F'\t' 'index($1,"DISPATCHED")==1' "$ldir/buckets.tsv" | wc -l | tr -d ' ')
    where="sandbox"; [ "$add_on" = "4" ] && where="operator"
    say "self-check: ledger-stub (+1 $where row, no dispatch) DISPATCHED=$ldisp  audit exit=$lrc"
    if [ "$lrc" != "1" ] || [ "$ldisp" != "0" ]; then
      say "GUARD FAIL: a ledger row added in the $where profile was not failed by the audit"
      say "  (the zero-ledger-delta assertion is the guard: it must fire on its OWN evidence)"
      guard_fail=1
    fi
  done
  # leg 3: a REFUSED ledger reader must fail the audit with a NAMED reason on a
  # named channel, never with an empty tail. This is the regression guard for the
  # defect being repaired: `2>/dev/null` + a pipeline made the reader's refusal
  # invisible and the "reason" empty, so the audit reported an unobservable ledger
  # and said nothing about why.
  ustub="$WORK/unreadable-stub-gopi"
  printf '%s\n' "$UNREADABLE_STUB_SOURCE" > "$ustub"
  chmod +x "$ustub"
  udir="$WORK/guard-unreadable"; mkdir -p "$udir"
  urc=0; run_audit "$ustub" "$udir" > "$udir/audit.out" 2>&1 || urc=$?
  # The refusal is two lines (a header, then the named reasons) — assert on each
  # property separately, never on a single line carrying both.
  uloud="$(grep -F 'the ledger could not be observed' "$udir/audit.out" || true)"
  uchannamed="$(grep -F 'sandbox ledger, the audit' "$udir/audit.out" || true)"
  uquoted="$(grep -F 'cannot open the ledger at /nonexistent/memory.db' "$udir/audit.out" || true)"
  unamed=0
  [ -n "$uloud" ] && unamed=$((unamed + 1))
  [ -n "$uchannamed" ] && unamed=$((unamed + 1))
  [ -n "$uquoted" ] && unamed=$((unamed + 1))
  say "self-check: unreadable-ledger stub — audit exit=$urc  loud-refusal=$([ -n "$uloud" ] && echo yes || echo no)  channel-named=$([ -n "$uchannamed" ] && echo yes || echo no)  reader-reason-quoted=$([ -n "$uquoted" ] && echo yes || echo no)"
  if [ "$urc" != "1" ] || [ "$unamed" -lt 3 ]; then
    say "GUARD FAIL: a refused ledger reader did not produce a loud, NAMED refusal"
    say "  (expected: audit exit 1 + a refusal naming the channel AND quoting the reader's stderr)"
    say "  observed: $(printf '%s' "$uloud" | head -1)"
    guard_fail=1
  fi
  if [ -n "$guard_fail" ]; then exit 1; fi
  say "GUARD OK: a probe that starts a run is classified DISPATCHED and fails the audit;"
  say "  a probe that adds a ledger row (sandbox or operator) fails without dispatching;"
  say "  a refused ledger reader fails loudly, naming the channel and quoting the reader's own reason"
  exit 0
fi

[ -n "$BIN" ] || die "sandboxed leg: no --bin given"
run_audit "$BIN" "$EVID"
