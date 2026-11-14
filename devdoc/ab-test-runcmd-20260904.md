<!-- doc-class: historical -->
# A/B Test #1: run_cmd --settings fix — feature vs baseline (2026-09-04 night)

Design (nick): same task, same DS model, one node with the memory-aware feature (write_task_area + memLayout), one without.
- Experiment (peerbox, master 876090d +feature): t-1788526647 (task show run_cmd must be accurate) — DS
- Control (HP, baseline acbf958 no-feature): t-1788527319 (clone of the same task) — DS via --settings

## Results
| metric | exp (feature) | ctrl (baseline) | ratio |
|---|---|---|---|
| elapsed | 1281s | 890s | 1.44x |
| total_tokens | 11,882,898 | 2,495,808 | 4.76x |
| iterations | 153 | 66 | 2.3x |
| fix depth | root (BootstrapEnvFromArgs — settings never reached the child; main.go hoists before subcommand chain) | surface (runMethodFor echoes os.Args --settings) | — |

## Honest attribution
- Confounders dominate: two INDEPENDENT Bonds explored differently. The feature-node Bond dug to the root cause (settings flag never propagated to the run/child — a real 2nd bug) and verified deeply (153 rounds); the control Bond took the direct surface fix (66 rounds). Token delta is mostly Bond behavior + fix depth, NOT the feature.
- Task-type mismatch: run_cmd is a single-bug SHORT task. write_task_area/memLayout is designed for LONG multi-item chains (per-item commit + region trim). A short task gives the feature no scenario — this A/B cannot measure the design's value.
- Per-request fixed overhead (tool schema ~35k chars repeated) is amplified over 153 rounds.

## Conclusion / next
A/B method is right; the task must be a LONG multi-step task for the feature to have a scenario. A/B #2 will use a multi-step task. The exp node's deeper fix (BootstrapEnvFromArgs) is the better fix — merged to master (8510649 + 75bb579). Control's surface fix archived at branch ab-control-runcmd-ae39ded.
