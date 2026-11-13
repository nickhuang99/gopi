package main

// workerLoopSourcePath is the worker loop the ruling binds (the loop itself, not
// the whole CLI): the file whose round used to spawn `gopi merge --fast`. It
// was declared in merge_tick_retired_test.go; memory_sync_tick_retired_test.go
// binds the SAME loop for its own ruling, so the constant moved here VERBATIM.
const workerLoopSourcePath = "cli_tasks_worker.go"
