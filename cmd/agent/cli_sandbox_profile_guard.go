package main

// cli_sandbox_profile_guard.go — the CLI half of the near-miss GOPI_HOME guard.
//
// THE DEFECT (measured 2026-09-26, end-user audit): a run launched with
// `GOPI_HOME=<sandbox>/.gopi` — the path `gopi tool sandbox -h` itself prints as that
// sandbox's profile — resolved, per the documented arithmetic
// (`<GOPI_HOME>/<profile leaf>`, config.ProfileDirIn), to
// `<sandbox>/.gopi/.gopi`: a SECOND, nested profile with its own memory.db and
// sessions/, no .sandbox-owner.json, and which NO reclaimer models —
// SweepSandboxes sweeps a sandbox ROOT's children, so a profile nested inside a
// profile is invisible to it. Meanwhile the sandbox's own ledger and sessions
// stayed empty, and the operator saw no error at all: for `gopi session log --last`,
// `gopi memory store` or a worker round, a nested profile looks exactly like an
// empty one.
//
// WHY THE GUARD IS HERE AND NOT ONLY IN internal/config: this is the earliest
// point in the process where the value can be judged with NOTHING materialised —
// so the refusal provably creates no directory (the acceptance measure is
// `test -d <sandbox>/.gopi/.gopi` before/after), and it covers the group verbs
// (`worker`, `merger`, `task run`) that return from main() at the subcommand
// chain and never reach the agent-path config block. internal/config.GopiDir
// additionally REFUSES the same value for direct library callers (Rule 14: one
// predicate, two enforcement points), which is why a caller that only uses the
// library also cannot slip into the nested profile silently.
//
// The wording of the diagnosis lives in config.GopiHomeMisuseMessage — one
// function, so this refusal and the library sentinel cannot describe the same
// problem differently.

import (
	"fmt"
	"os"
	"strings"

	"github.com/nickhuang99/gopi/internal/config"
)

// nestedProfileExitCode is the exit status for the refusal. It is the USAGE
// status, not a run failure: the operator typed a value that cannot mean what it
// appears to mean, and no run happened.
const nestedProfileExitCode = 2

// refuseNestedSandboxProfile refuses a GOPI_HOME that names a PROFILE rather
// than the HOME that profile lives in, naming the value, the arithmetic that
// makes it wrong, and the exact correction (`GOPI_HOME=<parent>`).
//
// TWO SPELLINGS, ONE PREDICATE (the value's LEAF), and both are measured on
// this host. The SANDBOX arm (2026-09-26 audit) is the one described above. The
// ORDINARY-HOME arm is the one that actually put rows in a second ledger
// (2026-10-13): `GOPI_HOME=/home/user/.gopi`, no sandbox anywhere in the path,
// resolved to a nested profile holding 2 rows while the real ledger held 1376 —
// and `gopi task submit` printed a normal `Task queued: <id>` with exit 0. The
// first version of this guard asked about the value's PARENT (a sibling sandbox
// marker), which is why that arm walked through it.
//
// The operator's deliberate opt-out (config.SandboxNestedProfileOptOutEnv) is
// honoured — but LOUDLY (Rule 15/25: a degradation is announced, never silent),
// because a nested profile is still a profile whose readers all resolve
// elsewhere, and the next operator to wonder where the sessions went deserves to
// find the sentence in the run's own output.
func refuseNestedSandboxProfile() {
	home := strings.TrimSpace(os.Getenv(config.GopiDirEnv))
	msg := config.GopiHomeMisuseMessage(home)
	if msg == "" {
		return
	}
	if config.NestedProfileOptOut() {
		fmt.Fprintf(os.Stderr, "gopi: %s=%s is set, allowing it: %s\n",
			config.SandboxNestedProfileOptOutEnv, "1", msg)
		return
	}
	fmt.Fprintf(os.Stderr, "gopi: %s\n%s\n", config.GopiHomeMisuseErrorPrefix, msg)
	os.Exit(nestedProfileExitCode)
}
