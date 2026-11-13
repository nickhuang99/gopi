package tools

// nowStamp is the ledger's second-resolution timestamp, written as the float
// the tasks table stores. It lived in task_merge_executor_test.go until the
// merge test suite was retired; eight test files across the package build rows
// with it, so it moved here VERBATIM rather than being rewritten per file
// (Rule 43: a symbol referenced outside the mechanism it was born in is a
// general tool and is carried out before the mechanism is deleted).

import "time"

func nowStamp() float64 { return float64(time.Now().Unix()) }
