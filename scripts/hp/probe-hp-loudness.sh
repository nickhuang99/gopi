#!/bin/bash
# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md
# HP loudness probe for the rewritten starters (run ON HP). Uses only stubs and
# a sandbox; touches no worker. The launcher is temporarily hidden for probe 1
# and restored immediately.
set -u
SB="$HOME/.gopi-ship/probe"
rm -rf "$SB"; mkdir -p "$SB/repo/bin"
printf '#!/bin/bash\necho "[pending] stub"\n' > "$SB/repo/bin/gopi"
chmod +x "$SB/repo/bin/gopi"

cat > "$SB/launcher-refuse.sh" <<'EOS'
#!/bin/bash
if [ "${1:-}" = "--count" ]; then
  echo "live gopi workers: 0 (pids: none)"
  echo "other gopi processes: 0 (not workers)"
  exit 0
fi
echo "agent-session guard: refusing to start a worker" >&2
exit 3
EOS
chmod +x "$SB/launcher-refuse.sh"

cat > "$SB/launcher-ok.sh" <<'EOS'
#!/bin/bash
if [ "${1:-}" = "--count" ]; then
  echo "live gopi workers: 0 (pids: none)"
  echo "other gopi processes: 0 (not workers)"
  exit 0
fi
echo "launcher: starting $1 worker(s)"
exit 0
EOS
chmod +x "$SB/launcher-ok.sh"

echo "=== PROBE 1: launcher RENAMED (missing) — acceptance construction ==="
mv "$HOME/bin/gopi-worker-up.sh" "$HOME/bin/gopi-worker-up.sh.hidden"
for f in hp-worker-autostart.sh hp-worker-nightsafe.sh; do
  out=$(GOPI_NIGHT_START_HOUR=0 GOPI_NIGHT_END_HOUR=24 "$HOME/bin/$f" 2>&1); rc=$?
  echo "[$f] rc=$rc"
  echo "$out" | sed 's/^/    /'
  echo "    log tail: $(tail -1 "$HOME/.gopi/logs/worker-autostart.log" 2>/dev/null | cut -c1-200)"
done
mv "$HOME/bin/gopi-worker-up.sh.hidden" "$HOME/bin/gopi-worker-up.sh"

echo "=== PROBE 2: refusing stub launcher (agent-session guard exit 3) ==="
for f in hp-worker-autostart.sh hp-worker-nightsafe.sh; do
  out=$(GOPI_WORKER_LAUNCHER="$SB/launcher-refuse.sh" GOPI_WORKER_REPO="$SB/repo" \
        GOPI_NIGHT_START_HOUR=0 GOPI_NIGHT_END_HOUR=24 "$HOME/bin/$f" 2>&1); rc=$?
  echo "[$f] rc=$rc"
  echo "$out" | sed 's/^/    /'
done

echo "=== PROBE 3: accepting stub (exit 0) — the success path stays non-noisy ==="
for f in hp-worker-autostart.sh hp-worker-nightsafe.sh; do
  out=$(GOPI_WORKER_LAUNCHER="$SB/launcher-ok.sh" GOPI_WORKER_REPO="$SB/repo" \
        GOPI_NIGHT_START_HOUR=0 GOPI_NIGHT_END_HOUR=24 "$HOME/bin/$f" 2>&1); rc=$?
  echo "[$f] rc=$rc failed_lines=$(printf '%s' "$out" | grep -c FAILED)"
  echo "$out" | sed 's/^/    /'
done

echo "=== PROBE 4: hp-night-legacy-audit.sh rounds stop loudly on a refusal ==="
cat > "$SB/gopi-refuse" <<'EOS'
#!/bin/bash
echo "agent-session guard: refusing to start a worker" >&2
exit 3
EOS
chmod +x "$SB/gopi-refuse"
out=$(GOPI_WORKER_BIN="$SB/gopi-refuse" bash "$HOME/.gopi/hp-night-legacy-audit.sh" 2>&1); rc=$?
echo "[hp-night-legacy-audit.sh] rc=$rc"
echo "$out" | sed 's/^/    /'
echo "    rounds started: $(printf '%s' "$out" | grep -c 'round .*: 起 worker')  (must be 1 — the loop stops at the refusal)"

echo "=== launcher restored ==="; ls -la "$HOME/bin/gopi-worker-up.sh"
