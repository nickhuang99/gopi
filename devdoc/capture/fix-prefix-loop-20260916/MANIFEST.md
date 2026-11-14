<!-- doc-class: historical -->
# Capture manifest — superseded draft from /tmp/fix-prefix-loop

Source: git worktree /tmp/fix-prefix-loop (detached HEAD 830ac32b, 55M)
  device id measured 2051 == device id of `/` (2051)  ->  Rule 22 violation
Incident: task 01a0a92d burned by GOPI_TASK_CHILD_FAIL_TEST=1 (resubmitted 01a0a941)

Why captured rather than landed: the subject (the durable rebuild-pending mark /
Rule-12 close-loop drain) ALREADY LANDED on master at 569a5b0e
("fix(merger): the Rule-12 close loop survives a dead pass — drain a durable
rebuild mark", merged 0dac2ae7). Master's copies are strictly NEWER than the
draft (master adds TestPassOnceDrainsMarksAtPassStart and refreshed measured
evidence). The 3 files are therefore a superseded draft, kept for the record.

Per-file sha256 (as captured, byte-identical to the removed worktree copies):
  e79a9a55107bb5a870811a68a9a0c558cc1d8584eaaa8587c9d204fa632c88a7  cmd/agent/cli_tasks_rebuild_gate_test.go    (118 lines; DIFFERS from master by 6 lines — master adds a start-of-pass drain assertion)
  7c37aa375d0a6bfbb2909056096dd3167011a6338980f3f0528966e8002491a5  cmd/agent/cli_tasks_rebuild_pending_test.go  (79 lines; DIFFERS from master by 69 lines — master renamed writeFileForTest->writeFileRaw and added TestPassOnceDrainsMarksAtPassStart)
  47a691ad9ba956a1db2ab528be5a3a5da05a586652fdf886ab9c62cc016baf0b  internal/tools/rebuild_pending_test.go       (149 lines; IDENTICAL to master)

Recovery:
  git show archive/fix-prefix-loop-20260916:cmd/agent/cli_tasks_rebuild_pending_test.go
  git show archive/fix-prefix-loop-20260916:devdoc/capture/fix-prefix-loop-20260916/MANIFEST.md
