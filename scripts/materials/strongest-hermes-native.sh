#!/bin/bash
# Operator-authored, LLM-free evidence for hermes' OWN native surfaces (M1/M2/M3).
# Only hermes' own CLI is invoked; no wrapper logic beyond cd/timeout/redirect.
H=/home/user/work/hermes-agent/.venv/bin/hermes
R=/home/user/.gopi/sessions/01a0eec1-e2a4-7366-bcf3-d2c2e183fe18/strongest/raw
B=probe-01a0eec1c
k(){ timeout 45 $H kanban --board $B "$@" 2>&1; }
echo "=== hermes NATIVE-SURFACE DIRECT EVIDENCE (operator-run, no LLM) ==="
echo "--- device load: none; Rule 27 respected (no synthetic pressure) ---"
echo
echo "### M3: native introspection surface (own CLI) ###"
timeout 60 $H --help 2>&1 | tr ',' '\n' | grep -xE "(sessions|insights|logs|checkpoints|dump|backup)" | sort -u | tr '\n' ' '; echo
echo "--- hermes sessions list (last 5) ---"; timeout 60 $H sessions list 2>&1 | head -8
echo "--- hermes sessions stats ---"; timeout 60 $H sessions stats 2>&1 | head -14
echo "--- hermes insights --days 1 (token/cost) ---"; timeout 90 $H insights --days 1 2>&1 | head -20
echo
echo "### M1: ledger rows + event/audit trail ###"
timeout 60 $H kanban boards create $B 2>&1 | head -3
P=$(k create "M1-ledger-probe-hermes" | grep -oE 't_[0-9a-f]+'); echo "P=$P"
echo "--- list ---"; k list | head -4
echo "--- show (events) ---"; k show "$P" | head -14
echo
echo "### M2: parent gate, strict sequence (--parent flag) ###"
C=$(k create "M2-child-hermes" --parent "$P" | grep -oE 't_[0-9a-f]+'); echo "C=$C (--parent $P)"
echo "--- show child BEFORE parent completion (status + parents) ---"; k show "$C" | head -12
echo "--- claim child (expect refusal) ---"; k claim "$C" | tail -3
echo "--- dispatcher pass (product's own scheduler) ---"; timeout 90 $H kanban --board $B dispatch 2>&1 | tail -4
echo "--- child status after dispatch ---"; k list | head -5
echo "--- complete parent ---"; k complete "$P" | tail -2
echo "--- show child AFTER parent completion ---"; k show "$C" | head -12
echo "--- claim child (expect claim) ---"; k claim "$C" | tail -4
echo "--- parent audit trail (all events) ---"; k show "$P" | sed -n '/Events/,$p' | head -8
echo "--- cleanup ---"; timeout 60 $H kanban boards rm $B 2>&1 | tail -2
echo "DONE hermes-native"
