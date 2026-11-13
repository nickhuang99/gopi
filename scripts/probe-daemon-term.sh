#!/usr/bin/env bash
# One-off probe: with a daemon sitting in its DRAINED wait (1h idle interval —
# the shape the acceptance rig uses), does a SIGTERM stop it? Measured, because
# the rig's cleanup observed two such daemons surviving `--stop --wait 20s`.
set -u
BIN="$1"
# shellcheck source=scripts/lib/tmp-ban.sh
. "$(dirname "$0")/lib/tmp-ban.sh"
S="$(gopi_scratch_mkdir gopi-term3)"
strip=(-u GOPI_EXECUTOR_SESSION_ID -u HERMES_SESSION_ID -u GOPI_AGENT_SESSION -u GOPI_HUMAN_TERMINAL -u GOPI_ALLOW_AGENT_SPAWN -u GOPI_AGENT_SPAWN_GUARD_OFF)
mkdir -p "$S/p/.gopi"
printf '{"repos":{}}' > "$S/p/.gopi/settings.json"
out="$(env "${strip[@]}" GOPI_HOME="$S/p" GOPI_WORKER_IDLE_SEC=3600 GOPI_WORKER_IDLE_EXITS=0 "$BIN" worker --daemon 2>&1)"
pid="$(printf '%s' "$out" | sed -n 's/.*daemon started: pid \([0-9]*\),.*/\1/p' | tail -1)"
echo "started pid=$pid"
echo "waiting 8s so the daemon is INSIDE its drained wait (1h interval)…"
sleep 8
grep -c "" "$S/p/.gopi/logs/worker.log" >/dev/null 2>&1 && tail -1 "$S/p/.gopi/logs/worker.log"
echo "--- SIGTERM ---"
kill -TERM "$pid"
for i in $(seq 1 20); do
	sleep 1
	if ! kill -0 "$pid" 2>/dev/null; then echo "RESULT: SIGTERM stopped it after ${i}s"; break; fi
	[ "$i" = "20" ] && echo "RESULT: STILL ALIVE 20s after SIGTERM (bare TERM cannot stop a draining daemon)"
done
echo "--- worker --stop --wait 20s ---"
env "${strip[@]}" GOPI_HOME="$S/p" "$BIN" worker --stop --wait 20s 2>&1 | grep -E "STILL running|stopped|no worker daemon" | head -3
for i in $(seq 1 5); do
	sleep 1
	kill -0 "$pid" 2>/dev/null || { echo "RESULT: --stop removed it within ${i}s"; break; }
done
if kill -0 "$pid" 2>/dev/null; then
	echo "RESULT: STILL ALIVE after --stop --wait 20s"
	echo "--- escalation: SIGUSR1 ---"
	kill -USR1 "$pid"
	for i in $(seq 1 10); do sleep 1; kill -0 "$pid" 2>/dev/null || { echo "SIGUSR1 stopped it after ${i}s"; break; }; done
fi
echo "--- worker log tail ---"
tail -5 "$S/p/.gopi/logs/worker.log" 2>/dev/null
kill -9 "$pid" 2>/dev/null
sleep 1
kill -0 "$pid" 2>/dev/null && echo "STILL ALIVE after SIGKILL (impossible)" || echo "SIGKILL took it — the escalation of last resort works"
rm -rf "$S"
