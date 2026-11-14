#!/usr/bin/env bash
# Re-apply the subcommand-flag fix to the CURRENT worktree (~/work/GoPi).
# The memsync-timestamp-delta worktree (where the fix was first written)
# was merged+removed mid-session; this re-applies it here.
set -e
cd /home/user/work/GoPi
python3 - <<'EOF'
p='internal/config/flags.go'
s=open(p).read()
old='''	scalar["settings"] = true // --settings PATH
	scalar["home"] = true     // --home PATH (gopi data root; GOPI_HOME)
	scalar["prompt"] = true   // --prompt TEXT (task instruction; positionals = files)
'''
new='''	scalar["settings"] = true // --settings PATH
	scalar["home"] = true     // --home PATH (gopi data root; GOPI_HOME)
	scalar["prompt"] = true   // --prompt TEXT (task instruction; positionals = files)

	// Subcommand-only flags (M4.12 harvest / M4.13 sync): consumed by
	// isTasksSubcommand BEFORE the agent loop sees argv. Without entries
	// here FlagOverrides rejects the whole invocation as "unknown flag"
	// before the subcommand dispatch ever runs — the serve-remote E2E
	// ssh leg failure (gopi: unknown flag --serve-remote).
	scalar["peer"] = true          // task harvest/sync --peer ALIAS
	scalar["peers"] = true         // task sync --peers a,b
	scalar["since"] = true         // task harvest --since UNIX
	scalar["remote-db"] = true     // task harvest --remote-db PATH
	scalar["serve-remote"] = false // task sync --serve-remote (boolean)
'''
assert old in s, "anchor not found"
s=s.replace(old,new)

old2='''	case "compact-reference-only":
		b, err := atob(flag, value)
		if err != nil {
			return err
		}
		cfg.CompactReferenceOnly = &b
	default:'''
new2='''	case "compact-reference-only":
		b, err := atob(flag, value)
		if err != nil {
			return err
		}
		cfg.CompactReferenceOnly = &b
	// Subcommand-only flags: accepted and DISCARDED here — their values are
	// re-parsed by the subcommand's own flag set (parseTasksArgs) from the
	// raw argv; main()'s subcommand dispatch runs before the agent loop,
	// so these never reach an agent run.
	case "peer", "peers", "since", "remote-db":
		// scalar subcommand flags — value consumed and dropped
	case "serve-remote", "status":
		// boolean subcommand flags — presence is all that matters
	default:'''
assert old2 in s, "applyFlag anchor not found"
s=s.replace(old2,new2)
open(p,'w').write(s)
print("flags.go patched")
EOF
gofmt -l internal/config/flags.go || true
go build ./... && echo BUILD_OK
