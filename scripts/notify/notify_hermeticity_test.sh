#!/usr/bin/env bash
# notify_hermeticity_test.sh — A RIG'S FIXTURE PAYLOAD NEVER REACHES AN
# INHERITED HOME'S ANCHOR.
#
# THE DEFECT THIS MAKES NON-RECURRING (measured 2026-11-03). notify.sh refuses a
# GOPI_HOME that names a CODE CHECKOUT and then ANCHORS the payload at
# `$HOME/.gopi` — the fallback is deliberate and documented in the refusal's own
# text, and it is pinned from the other side by notify_slot_contract_test.sh's
# case 6 (refusing to anchor would LOSE real events from production worktree
# children, which is the failure the fallback exists to prevent). So the sink is
# right and it must stay right: the danger is on the TEST side. A case that hands
# the sink a checkout-shaped GOPI_HOME and leaves HOME INHERITED appends the
# rig's fixture payload to the OPERATOR's live evidence file. That is what
# notify_peer_health_test.sh's case 7 did on every full-suite run; ~/.gopi/notify/
# task-events.jsonl carried 12 rows reading
#   {"event":"completed","task_id":"t-1","status":"completed","summary":"local"}
# before the fix (counted, not estimated — see the report). An append-only
# evidence file is not edited by a test fix, so those rows stay and are NAMED.
#
# WHAT THIS FILE CHECKS, in the two directions the defect has:
#   1. STRUCTURAL — every sink invocation in scripts/notify/*_test.sh that
#      carries GOPI_HOME must also carry an EXPLICIT HOME. A rig that names HOME
#      cannot reach a home it did not create, whatever the sink decides to do
#      with a refused value. This half fails on the SOURCE, so it catches the
#      defect before a single payload is written.
#   2. BEHAVIOURAL — every suite in scripts/notify is run with HOME pointed at a
#      FRESH EMPTY directory, and afterwards that directory must contain no
#      fixture payload. This half catches a rig that reached an inherited home
#      by some route the scan does not parse (a helper, a sourced file, an
#      option-style assignment).
#
# WHY BOTH. The scan alone is a text gate and can be routed around; the
# behaviour alone only sees the fixtures that exist today. The pair is cheap
# (the suite runs in seconds) and it names the file when it fires.
#
# Scratch lives OFF the `/` partition (GOPI.md Rule 22) through the shared helper.
set -u

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
NOTIFY_DIR="$ROOT/scripts/notify"
# shellcheck source=../lib/tmp-ban.sh
. "$ROOT/scripts/lib/tmp-ban.sh" || { echo "tmp-ban: scripts/lib/tmp-ban.sh missing" >&2; exit 2; }
RIG="$(gopi_scratch_mkdir notify-hermeticity)" || { echo "FATAL: scratch root unavailable" >&2; exit 2; }
trap gopi_scratch_cleanup EXIT INT TERM

FAILS=0
pass() { printf '  ok   %s\n' "$1"; }
fail() { printf '  FAIL %s\n' "$1" >&2; FAILS=$((FAILS + 1)); }

# THE MARKER A LEAKED FIXTURE LEAVES. Spelled once: the rigs use the same task id
# for their local payload, and this constant is what makes "no fixture row" a
# measurable statement rather than a hope. It is deliberately NOT read out of the
# rigs — a check that asked its subjects what to look for could be un-measured by
# editing them.
readonly FIXTURE_MARKER='"task_id":"t-1"'

echo "notify sink hermeticity (rig: $RIG)"

# ---- 1. STRUCTURAL: every GOPI_HOME call site pins HOME ---------------------
# Each test file is read as text and split into "sink invocations" by their
# anchor token — the sink path — because that is the seam every one of them goes
# through (`bash "$SINK"`, `bash "$sink"`, `sh -c "$cmd"` over an installed sink).
# The window is generous on purpose (the env prefix of a call spans several
# continuation lines) and BOUNDED, so a missing HOME is reported at the call site
# rather than blamed on the whole file.
#
# A file with NO sink invocation at all is not a pass: it means the scan looked
# at something that is not a rig, and notify_deploy_test.sh's case 10 is the one
# place a sink runs through `sh -c`, so the anchor list below names it too.
# scan_file FILE — prints one line per violation on stdout and returns.
# It REPORTS; the caller decides. (The planted control below runs it too, so a
# scan that called fail() itself would turn its own control into a red run.)
scan_file() {
	local f="$1" line start end window
	base="$(basename "$f")"
	# The line numbers of every sink invocation in this file.
	while IFS= read -r line; do
		[ -n "$line" ] || continue
		start=$((line - 8))
		[ "$start" -ge 1 ] || start=1
		end=$((line + 2))
		window="$(sed -n "${start},${end}p" "$f")"
		if ! printf '%s' "$window" | grep -q 'GOPI_HOME'; then
			continue
		fi
		# AN EXPLICIT HOME means an assignment of HOME — never GOPI_HOME, hence
		# the boundary: start-of-line-or-space, then HOME=. Both spellings a rig
		# uses (`HOME="$x"` as an env prefix, `env HOME="$x" …`) match.
		if printf '%s' "$window" | grep -qE '(^|[[:space:]])HOME='; then
			continue
		fi
		printf '%s:%s: no explicit HOME beside a GOPI_HOME — a checkout-shaped value would anchor the fixture payload in the INHERITED home ($HOME/.gopi/notify/task-events.jsonl)\n' \
			"$(basename "$f")" "$line"
	done < <(grep -n 'bash "\$SINK"\|bash "\$sink"\|sh -c "\$cmd' "$f" | cut -d: -f1)
	if ! grep -q 'bash "\$SINK"\|bash "\$sink"\|sh -c "\$cmd' "$f"; then
		printf '%s: the scan found no sink invocation — this file is not a rig, so it measures nothing\n' "$(basename "$f")"
	fi
	return 0
}

SCANNED=0
CALLSITES=0
for f in "$NOTIFY_DIR"/*_test.sh; do
	[ "$(basename "$f")" = "notify_hermeticity_test.sh" ] && continue
	SCANNED=$((SCANNED + 1))
	scan_out="$(scan_file "$f")"
	n_hits="$(grep -c 'bash "\$SINK"\|bash "\$sink"\|sh -c "\$cmd' "$f")"
	if [ -n "$scan_out" ]; then
		fail "$scan_out"
	else
		pass "$(basename "$f"): all $n_hits sink invocation(s) pin HOME"
	fi
done
if [ "$SCANNED" -lt 5 ]; then
	fail "the scan walked only $SCANNED rig(s) — a vacuum is not a clean result"
fi
CALLSITES="$(grep -c 'bash "\$SINK"\|bash "\$sink"\|sh -c "\$cmd' "$NOTIFY_DIR"/*_test.sh 2>/dev/null |
	awk -F: '{s += $2} END {print s + 0}')"
printf '  --   scanned %d rig(s), %s sink invocation(s)\n' "$SCANNED" "$CALLSITES"

# ---- 2. BEHAVIOURAL: run every rig with HOME in a FRESH EMPTY directory -----
# The ambient HOME is REPLACED, not merely shadowed: a rig that inherits it cannot
# write into it, so anything that appears here was produced by the suite under
# test. A rig that DELIBERATELY writes into $HOME would be caught here; none does
# — they all name their own homes — and that is exactly the property being
# measured.
ISO_HOME="$RIG/isolated-home"
CHILD_SCRATCH="$RIG/child-scratch"
mkdir -p "$ISO_HOME" "$CHILD_SCRATCH"
iso_anchor="$ISO_HOME/.gopi/notify/task-events.jsonl"

# THE VERDICT OF THIS HALF IS THE LEAK, NOT EACH RIG'S OWN SUBJECT. A rig that is
# red for its OWN reason (a pre-existing failure unrelated to hermeticity) has
# still run its sink calls, and the leak would be visible either way; binding
# this guard to every sibling's status would make it go red for a cause it does
# not own — the shape of a check nobody trusts. Each status is REPORTED so a
# reader sees it, and the vacuum floor below refuses a run in which nothing ran.
RAN=0
WENT_GREEN=0
WENT_RED=""
for f in "$NOTIFY_DIR"/*_test.sh; do
	[ "$(basename "$f")" = "notify_hermeticity_test.sh" ] && continue
	RAN=$((RAN + 1))
	name="$(basename "$f")"
	# GOPI_TEST_SCRATCH_HOME is PINNED, and it is load-bearing: tmp-ban.sh derives
	# the rig's scratch root from HOME when this is unset, so an isolated HOME
	# alone would put every child's OWN scratch tree inside the directory this
	# guard is measuring — the guard would then report its subjects' scratch as a
	# leak (measured: 20+ `queue-*/p*/.gopi/notify/task-events.jsonl` paths, all
	# of them the rigs' own `$RIG` trees). One level of indirection, one pin.
	if HOME="$ISO_HOME" GOPI_TEST_SCRATCH_HOME="$CHILD_SCRATCH" bash "$f" >"$RIG/$name.out" 2>&1; then
		WENT_GREEN=$((WENT_GREEN + 1))
		pass "$name: green under an isolated HOME"
	else
		WENT_RED="$WENT_RED $name"
		printf '  --   %s: exited non-zero under an isolated HOME — reported, NOT judged here (output: %s)\n' \
			"$name" "$RIG/$name.out"
		sed -n '1,6p' "$RIG/$name.out" | sed 's/^/        /' >&2
	fi
done
if [ "$RAN" -lt 5 ]; then
	fail "only $RAN rig(s) were run — a vacuum is not a clean result"
fi
if [ "$WENT_GREEN" -lt 4 ]; then
	fail "only $WENT_GREEN rig(s) ran green under an isolated HOME (red:$WENT_RED) — a run in which almost nothing executed cannot prove hermeticity"
fi
if [ -n "$WENT_RED" ]; then
	printf '  --   rig(s) red for their own subject (not this guard):%s\n' "$WENT_RED"
fi

# THE VERDICT ON THE INHERITED HOME, read as a fact about the filesystem.
if [ -f "$iso_anchor" ]; then
	leaked="$(grep -c "$FIXTURE_MARKER" "$iso_anchor" 2>/dev/null || true)"
	fail "the isolated home received $leaked fixture row(s) at $iso_anchor — a rig reached a home it did not create"
	grep -n "$FIXTURE_MARKER" "$iso_anchor" >&2 || true
else
	pass "no anchor was created in the isolated home (nothing reached an inherited \$HOME)"
fi
# AND THE FIXTURE PAYLOAD ITSELF, WHEREVER IT COULD HAVE LANDED. This is the
# property in the epic's own words — "fixture payloads must never land in an
# inherited HOME's anchor" — stated as a search of EVERY file rather than of one
# path, because a leak that renamed its destination would still be a leak.
markers="$(grep -rlF "$FIXTURE_MARKER" "$ISO_HOME" 2>/dev/null | head -20)"
if [ -n "$markers" ]; then
	fail "a rig's fixture payload ($FIXTURE_MARKER) was written under the isolated home:
$markers"
else
	pass "no rig fixture payload anywhere under the isolated home"
fi
# WHAT ELSE IS THERE is REPORTED, not judged: a rig that shells out to `go run`
# for a payload generator leaves $HOME/.cache/go-build behind, which is tooling
# cache and not an event. Naming the count keeps a reader from mistaking an
# empty listing for a run that did nothing.
stray="$(find "$ISO_HOME" -type f 2>/dev/null | wc -l)"
printf '  --   files under the isolated home (reported, not judged): %s\n' "$stray"

# ---- 3. THE CONTROL: the scan and the runner are not vacuous ---------------
# A checker that reports green whatever it reads is the failure this whole file
# exists to catch (Rule 41a: read the table). So a PLANTED violation is measured
# here, through the same predicates the real scan uses: a scratch rig that pins
# GOPI_HOME and no HOME must be reported, and the verdict must not be green.
PLANT="$RIG/planted_test.sh"
cat >"$PLANT" <<'PLANTED'
#!/usr/bin/env bash
SINK=/nonexistent/notify.sh
GOPI_HOME=/nonexistent/checkout-shaped bash "$SINK" </dev/null
PLANTED
planted_out="$(scan_file "$PLANT" 2>&1)"
if printf '%s' "$planted_out" | grep -q 'no explicit HOME'; then
	pass "control: a planted GOPI_HOME-without-HOME call site is REPORTED"
else
	fail "control: the scan accepted a planted violation — it is vacuous:
$planted_out"
fi

if [ "$FAILS" -ne 0 ]; then
	printf '\nnotify_hermeticity_test: %d assertion(s) FAILED\n' "$FAILS" >&2
	exit 1
fi
printf '\nnotify_hermeticity_test: all assertions passed\n'
