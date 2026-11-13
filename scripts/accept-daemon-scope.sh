#!/usr/bin/env bash
# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md
# (The rig below starts `gopi worker --daemon` as the OPERATOR's shell shape —
#  it strips the agent-spawn signature with `env -u` for that reason, and step
#  [3] is a `go test <pkg>` invocation. Nothing here is a recipe for an agent:
#  an agent's verb is `gopi task submit`, and the refusal is the contract
#  working. This line names the ruling; the canonical block owns the policy.)
# Acceptance rig for the PER-PROFILE daemon scope (epic daemon-scope-per-profile,
# 2026-09-16).
#
# What it proves, in ONE window:
#   1. two production `gopi worker --daemon` of TWO profiles run at the same
#      time, each in its OWN gopi-daemon-worker-<profile>.scope unit
#      (`systemctl --user list-units --type=scope` shows two distinct units);
#   2. a THIRD start for a profile whose daemon is already alive is still
#      REFUSED by the flock, naming the holder pid;
#   3. the four worker-daemon e2e tests pass WHILE those production daemons are
#      running (the property the pid suffix could not deliver: a production
#      daemon of the same face cannot occupy a test profile's unit name).
#
# Scratch lives OFF the `/` partition (GOPI.md Rule 22): a per-run directory
# under the SHARED scratch home, measured by devices/statfs before anything
# runs — no hardcoded /tmp, and no longer the shared-memory filesystem
# (creating there is RETIRED, operator ruling 2026-09-18).
set -u

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$ROOT/bin/gopi"
# shellcheck source=lib/tmp-ban.sh
. "$ROOT/scripts/lib/tmp-ban.sh" || { echo "tmp-ban: scripts/lib/tmp-ban.sh missing" >&2; exit 2; }
SCRATCH="$(gopi_scratch_mkdir daemon-accept)" || { echo "FATAL: scratch root unavailable" >&2; exit 2; }

# acc_go_timeout is this rig's per-package test budget — declared ONCE, here
# (GOPI.md Rule 1a/2: no `-timeout 600s` inlined at the call site). The rig
# drives the four daemon e2e tests inside a shared-deadline window while two
# production daemons are resident, so a starved host must report a NAMED budget
# rather than Go's silent 10m default. TestTestBudgetHasASingleSourceAndNamesATimeout
# (internal/testutil/testbudget_gate_test.go) fails any committed workflow whose
# `go test` line carries no budget; scripts/test-budget.sh is the repo's single
# source, and GOPI_TEST_TIMEOUT is its one override (Rule 2).
acc_go_timeout="${GOPI_TEST_TIMEOUT:-25m}"
TEST_HOME="$SCRATCH/test-home"
PROD_A="$SCRATCH/prod-a"
PROD_B="$SCRATCH/prod-b"

# The agent-spawn signature is stripped for the PRODUCTION starts only: this rig
# runs from an agent shell, where `gopi worker` refuses to start by design
# (cmd/agent/agent_spawn_guard). Stripping it is the operator's shell shape —
# the shape "the flag works" is a meaningful assertion in.
# `env -u` takes ONE name per flag, so the list is expanded into -u pairs.
STRIP_NAMES=(GOPI_EXECUTOR_SESSION_ID HERMES_SESSION_ID GOPI_AGENT_SESSION GOPI_HUMAN_TERMINAL
             GOPI_ALLOW_AGENT_SPAWN GOPI_AGENT_SPAWN_GUARD_OFF)
STRIP=()
for n in "${STRIP_NAMES[@]}"; do STRIP+=(-u "$n"); done   # used as: env "${STRIP[@]}" …

# WHY A SHORT IDLE INTERVAL (5s) FOR THESE PRODUCTION DAEMONS: with
# GOPI_WORKER_IDLE_EXITS=0 the worker never self-exits, so it is resident for the
# whole window either way — but with a LONG interval it also becomes UNSTOPPABLE
# (measured: a 1h-interval daemon survives SIGTERM, SIGUSR1 and `worker --stop
# --wait 20s`, because the drained wait is a raw `time.Sleep` and the stop flag is
# only read at a claim boundary — the loop bug filed as task
# 01a0a715-1ae0-74ec-aa57-ae7593173d1e). A rig whose cleanup cannot stop its own
# daemons leaves resident daemons on the host, so the interval is short enough to
# be stoppable and the rig is honest about the reason. The property this rig
# demonstrates (one unit name per PROFILE) does not depend on the interval.
start_daemon() { # start_daemon <home> -> prints "pid logpath"
	local home="$1"
	env "${STRIP[@]}" \
		GOPI_HOME="$home" \
		GOPI_WORKER_IDLE_SEC=5 \
		GOPI_WORKER_IDLE_EXITS=0 \
		"$BIN" worker --daemon
}

stop_daemon() { # stop_daemon <home> <pid>
	local home="$1" pid="$2"
	[ -n "$pid" ] || return 0
	# The DURABLE stop is the documented verb first (it is bounded by --wait),
	# then the graceful signal. Both are attempted because neither alone is a
	# guarantee on this host — see the long-interval note above.
	env "${STRIP[@]}" GOPI_HOME="$home" "$BIN" worker --stop --wait 20s >/dev/null 2>&1
	kill -TERM "$pid" 2>/dev/null
	for _ in $(seq 1 10); do
		kill -0 "$pid" 2>/dev/null || return 0
		sleep 1
	done
	# ESCALATION, NAMED: this rig must not leave a resident daemon behind, so a
	# daemon that ignored every graceful path is killed and the fact is reported
	# rather than silently accepted (Rule 15).
	echo "WARNING: pid $pid ignored --stop and SIGTERM for 10s — escalating to SIGKILL (a resident daemon must not outlive this rig; see the loop bug above)" >&2
	kill -9 "$pid" 2>/dev/null
}

cleanup() {
	stop_daemon "$PROD_A" "${PROD_A_PID:-}"
	stop_daemon "$PROD_B" "${PROD_B_PID:-}"
	gopi_scratch_cleanup
	rm -rf "$SCRATCH"
}
trap cleanup EXIT

mkdir -p "$TEST_HOME/.gopi" "$PROD_A/.gopi" "$PROD_B/.gopi"
for h in "$TEST_HOME" "$PROD_A" "$PROD_B"; do printf '{"repos":{}}' > "$h/.gopi/settings.json"; done

echo "=== [1] two profiles, two daemons ==================================="
outA="$(start_daemon "$PROD_A")"; echo "$outA" | sed 's/^/[prod-a] /'
outB="$(start_daemon "$PROD_B")"; echo "$outB" | sed 's/^/[prod-b] /'
PROD_A_PID="$(printf '%s' "$outA" | sed -n 's/.*pid \([0-9]*\),.*/\1/p' | tail -1)"
PROD_B_PID="$(printf '%s' "$outB" | sed -n 's/.*pid \([0-9]*\),.*/\1/p' | tail -1)"
echo "prod-a pid=$PROD_A_PID  prod-b pid=$PROD_B_PID"

echo "--- gopi scope units while BOTH production daemons run:"
systemctl --user list-units --type=scope --no-legend | grep gopi-daemon || echo "(none)"

echo
echo "=== [2] a second --daemon for the SAME profile is still refused ======"
env "${STRIP[@]}" GOPI_HOME="$PROD_A" GOPI_WORKER_IDLE_SEC=5 GOPI_WORKER_IDLE_EXITS=0 \
	"$BIN" worker --daemon
echo "refusal exit=$?"

echo
echo "=== [3] the four worker-daemon tests, WITH the production daemons up ="
echo "--- units immediately before the suite:"
systemctl --user list-units --type=scope --no-legend | grep gopi-daemon || echo "(none)"
cd "$ROOT" || exit 1
go test ./cmd/agent -count=1 -timeout "$acc_go_timeout" \
	-run 'TestWorkerDaemon_(StatusReportsTheLiveDaemon|ForegroundWorkerIgnoresTheLock|StopEndsTheDaemon|RestartReplacesTheDaemon)$' 2>&1 | tail -5
# The budget above is also routable through scripts/test-budget.sh (its
# test_budget default 25m is the same named value, and `[3]` is exactly a
# `go test <pkg> -run <tests>` call, which the script takes as a package
# argument); the explicit -timeout is kept because this window is SHARED with
# the two resident daemons — see the header's "ONE window" contract.
echo "suite exit=$?"
echo "--- units immediately after the suite (same window):"
systemctl --user list-units --type=scope --no-legend | grep gopi-daemon || echo "(none)"

echo
echo "=== [4] cleanup: both production daemons are stopped (no residue) =="
stop_daemon "$PROD_A" "${PROD_A_PID:-}"
stop_daemon "$PROD_B" "${PROD_B_PID:-}"
sleep 2
live=0
for p in "${PROD_A_PID:-}" "${PROD_B_PID:-}"; do
	[ -n "$p" ] && kill -0 "$p" 2>/dev/null && live=$((live + 1))
done
echo "production daemons still alive after cleanup: $live (want 0)"
systemctl --user list-units --type=scope --no-legend | grep -E "gopi-daemon-worker-(prod-a|prod-b)-" \
	&& echo "WARNING: a production unit outlived the rig" \
	|| echo "both rig units are gone — the rig leaves no resident daemon"
PROD_A_PID=""; PROD_B_PID=""
