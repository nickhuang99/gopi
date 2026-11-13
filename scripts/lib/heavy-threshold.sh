# shellcheck shell=bash
# heavy-threshold.sh — the ONE definition of the heavy-test classification
# floor and the inventory path (GOPI.md Rule 2/14: a value shared by several
# scripts lives in one small leaf file that every consumer sources; a second
# copy is drift, a bare literal is a defect).
#
# WHY THIS FILE EXISTS
# --------------------
# testdata/heavy-tests.tsv is a SNAPSHOT: one measured run (2026-09-16, 44 rows
# -> 41 call sites) chose everything at or above the floor. Nothing in that
# snapshot can notice a test that becomes long LATER — such a test silently eats
# the smoke budget, which is the exact cost the classification was built to
# remove. Two scripts close that half from opposite ends and must agree on the
# floor, hence one definition here:
#
#   scripts/test-budget.sh   reports, in every run, the tests at or above the
#                            floor that are NOT in the inventory (the drift is
#                            named by the run that paid for it);
#   scripts/heavy-derive.sh  re-derives the inventory (candidate rows) from a
#                            run's own `go test -json` stream.
#
# The recipe (commands + observed output) is devdoc/heavy-tests-rederivation.md.

# heavy_slow_threshold_s — a test at or above this many wall-clock seconds is a
# classification CANDIDATE. 5 is the measured floor the inventory was derived
# with (45 tests >=5s of 3553 in the 2026-09-16 baseline stream). It is a floor
# for NAMING, not for classifying: a test above it is not heavy by arithmetic,
# a human reads it and either classifies it (heavytest.Require) or accepts it
# with a reason (see the devdoc's "below/above the floor" section).
heavy_slow_threshold_s=5

# heavy_inventory_rel — the checked-in inventory, repo-relative. Its Go-side
# single source is heavytest.InventoryRel; the inventory gate
# (internal/standards) asserts these two spell the same path, because a renamed
# file would otherwise leave this report reading an empty "classified" set and
# reporting every heavy test as unclassified.
heavy_inventory_rel="testdata/heavy-tests.tsv"

# heavy_gate_packages — package path fragments that may NEVER be classified
# heavy (a merge must not land with its own gates skipped). Kept here so both
# scripts exclude the same set the Go gate enforces.
heavy_gate_packages="internal/standards internal/stylegate"

# heavy_derive_cmd — the ONE command the report prints so a reader can re-derive
# the inventory instead of hand-adding a row.
heavy_derive_cmd="scripts/heavy-derive.sh"

# heavy_drift_report — the report script itself, repo-relative: one definition,
# called by test-budget.sh (every full smoke run) and by hand over a stream.
heavy_drift_report="scripts/heavy-drift-report.sh"

# heavy_min_candidates_floor — non-vacuity floor for the derive script: a scan
# that finds nothing must be distinguishable from a scan that read nothing.
heavy_stream_tests_floor=100

# heavy_golist_format — the `go list` format that names every package carrying
# test files (`go test ./...` is the run the inventory is derived from). The
# derive script requires the stream to COVER this set: a test count can be
# spoiled by one package's subtests, so counting events cannot tell a whole
# suite from a narrowed run — coverage can.
heavy_golist_format='{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}'
