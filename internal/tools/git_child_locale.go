package tools

// git_child_locale.go — the ONE place this package decides the MESSAGE LOCALE
// of a git child whose output a parser reads.
//
// WHY THIS EXISTS (measured 2026-10-12, epic 01a1306b). git's diagnostics are
// gettext-translated, so the SAME command over the SAME object store prints
// different bytes depending on the ambient locale:
//
//	$ LANG=C git -C <repo> fsck --no-progress --unreachable --no-reflogs
//	unreachable commit 592d6bba379e1b351c285e2f34cc31bb5fbe0e8d
//	unreachable commit a1781a6d3e7c9ecfeadab40d85c3f4c90e750e48
//
//	$ LANG=zh_CN.UTF-8 LC_ALL=zh_CN.UTF-8 git -C <repo> fsck --no-progress --unreachable --no-reflogs
//	不可达 commit 592d6bba379e1b351c285e2f34cc31bb5fbe0e8d
//	不可达 commit a1781a6d3e7c9ecfeadab40d85c3f4c90e750e48
//
// The unreachable-backlog census selects its rows by the literal first field
// `unreachable` (archiveUnreachableLinePrefix), so under a translated locale it
// matched ZERO lines and rendered its clean-looking zero —
// "no unreachable tip — every commit is named by a ref". That is a Rule 15
// silent failure in PRODUCTION, not merely a test symptom. Measured with the
// shipped binary against a repo holding one orphaned tip:
//
//	$ LANG=zh_CN.UTF-8 LC_ALL=zh_CN.UTF-8 gopi task archive --unreachable --repo <repo>
//	  unreachable tips:    0
//	$ LANG=C LC_ALL=C gopi task archive --unreachable --repo <repo>
//	  unreachable tips:    1
//
// The localization is therefore not cosmetic: it is an INPUT to a parser that
// reads OUR strings out of git's output, so the language they arrive in is part
// of the contract. Two seams in this repository already apply exactly this rule
// to their own children — bashChildEnv (bash.go, GOPI_BASH_LOCALE) and
// cmd/agent's mergeGitEnv (merge_run.go, GOPI_MERGE_LOCALE, whose comment
// records the identical 2026-09-26 incident on `CONFLICT`). This file is the
// shared half of that rule (Rule 14), so a third caller cannot invent a fourth
// spelling of "byte-stable English diagnostics".

import (
	"os"
	"strings"
)

const (
	// ChildLocaleEnglish is the locale pinned onto every child whose output
	// this package PARSES. "C" keeps git's diagnostics byte-stable across
	// hosts: an unlocalized host and a zh_CN host then agree on the text a
	// classifier matches.
	ChildLocaleEnglish = "C"
	// childLocaleAssignment marks an environment entry as a KEY=VALUE pair, so
	// the name can be split off without assuming a separator position.
	childLocaleAssignment = "="
)

// ChildLocaleDuplicateKeyRule is the ONE statement of what a DUPLICATED variable
// name in a child's environment actually does — the rule the rest of this
// repository cites instead of restating (Rule 14).
//
// IT IS NOT "FIRST WINS" AND IT IS NOT "LAST WINS": it is ORDER- AND
// READER-DEPENDENT, and both one-word rules are refuted by measurement on this
// host (2026-10-16; the re-runnable script, its compiled probe and its raw
// output live in the session evidence directory —
// evidence/measure-readers.sh, evidence/measure-readers-output.txt, which
// SUPERSEDES evidence/measure-final.sh: that earlier pass reported Go's
// os.Getenv as LAST-winning, and a correct reading of the same variable REFUTED
// its own Go row — see the third bullet below. Host: Linux 5.15, glibc 2.35,
// dash 0.5.11, bash 5.1, go1.24.5, coreutils 8.32).
//
// Handing the SAME duplicated pair to the readers of a raw execve(2)
// environment, with the order swapped in each pair:
//
//	LC_ALL=C, LC_ALL=zh_CN.UTF-8  →  glibc getenv   = "C"        (the FIRST copy)
//	                                 strerror(EPIPE) = "Broken pipe"
//	LC_ALL=zh_CN.UTF-8, LC_ALL=C  →  glibc getenv   = "zh_CN.UTF-8"
//	                                 strerror(EPIPE) = "断开的管道"
//
//	glibc getenv(3) and setlocale(3)+strerror(3) — the GETTEXT catalogue path —
//	take the FIRST copy.
//
//	CAFEBEAN=first, CAFEBEAN=second →  Go os.Getenv = "first"
//	                                   dash `$CAFEBEAN` = "second"
//	CAFEBEAN=second, CAFEBEAN=first →  Go os.Getenv = "second"
//	                                   dash `$CAFEBEAN` = "first"
//
//	Go's own os.Getenv ALSO takes the FIRST copy — its environment table keeps
//	the first mention of a key and clears the later ones (syscall/env_unix.go
//	copyenv, `env[key] = i // first mention of key`); dash and bash take the LAST.
//
//	So glibc, Go and the shells do not split 2-to-1: the FIRST/LAST lines run
//	BETWEEN two libc/Go readers and the two shells, and a measurement that reads
//	only one of each is what produced the wrong "Go takes the LAST" row.
//
// SO NEITHER ORDER CAN BE RELIED ON, and that — not a winner — is the reason
// this function strips before it appends: the child is given exactly ONE entry,
// so no reader has a duplicate to resolve at all and the pin holds whatever the
// order or the reader would have been. A pin that leaves two entries is a pin
// only while the order AND the reader happen to agree, and both vary by libc,
// by shell and by whether a wrapper is in the middle.
//
// TWO FURTHER MEASURED SUBTLETIES, which is why "the shell says LAST, so LAST
// wins" is also not the rule:
//
//   - A WRAPPER COLLAPSES THE DUPLICATE BEFORE THE NEXT READER SEES IT. GNU
//     coreutils `env -i K=v1 K=v2 PROG` and `/bin/sh -c PROG` re-export ONE
//     entry (the LAST, which is the copy their own lookup took), so a
//     measurement built with `env K=v1 K=v2` measures `env` and the shell, not
//     the reader it names.
//   - Go DEDUPES `os/exec`'s Cmd.Env ITSELF, in favour of later values
//     (`dedupEnv`, "in favor of later values", $GOROOT/src/os/exec/exec.go), so
//     an os/exec caller's child never receives a duplicate either way — this
//     function's strip is redundant for THAT caller but load-bearing for every
//     caller that assembles an envp by hand, which is what makes one shared
//     implementation (Rule 14) the right shape rather than two.
const ChildLocaleDuplicateKeyRule = "a duplicated variable name in a child's environment is ORDER- and " +
	"READER-dependent, not first-wins and not last-wins: glibc getenv(3), setlocale(3)+strerror(3) (the " +
	"gettext catalogue path) and Go's os.Getenv take the FIRST copy, while dash and bash take the LAST, and a " +
	"wrapper (GNU env, a shell, Go's own os/exec Cmd.Env) collapses the pair before the reader sees it. So a pin " +
	"that leaves two entries holds only while the order and the reader happen to agree; the child is given " +
	"exactly ONE entry instead. Measured 2026-10-16 — see internal/tools/git_child_locale.go for the reading " +
	"and its host."

// ChildLocaleLocalePinnedNames are the variables the pin WRITES: the two that
// select the message catalogue outright, and that gettext itself consults before
// the narrower selectors (LC_ALL overrides LANG; ChildLocaleSelectorNames are
// stripped because a leftover one would still be an input to the catalogue
// lookup).
//
// EXPORTED (Rule 13/14). A second caller whose child's output is PARSED — the
// voice recorder in cmd/agent/cli_voice_client.go, measured 2026-10-12 — must
// strip and pin the SAME four names. Unexported here, that caller could only
// re-type them, and a re-typed list is a bare literal in disguise (Rule 14):
// the two would then drift the day a fifth selector is added, and neither
// caller would notice. The lists are therefore the ONE spelling, and the
// consumer is named in the file that owns them.
var ChildLocaleLocalePinnedNames = [...]string{"LANG", "LC_ALL"}

// ChildLocaleSelectorNames are the gettext selectors STRIPPED rather than set:
// LC_ALL outranks them, but leaving one pointing at a translation this process
// did not choose is a second, silent input to a parser that quotes English.
//
// Together the two lists are the same four bashChildEnv and mergeGitEnv strip,
// spelled once here (Rule 14) — and since 2026-10-14 mergeGitEnv CALLS the
// helper rather than spelling them a second time (ChildLocaleEnvPinning).
var ChildLocaleSelectorNames = [...]string{"LC_MESSAGES", "LANGUAGE"}

// ChildLocaleEnv returns the current environment with the message-locale
// variables REPLACED by ChildLocaleEnglish.
//
// Replaced, never appended — and the REASON is that no duplicate-order rule can
// be relied on, not that one of them would have picked the wrong copy. Hand the
// same duplicated pair to the readers of a raw execve(2) environment and they
// disagree: glibc getenv(3), setlocale(3)+strerror(3) and Go's os.Getenv take
// the FIRST copy, while dash and bash take the LAST. See
// ChildLocaleDuplicateKeyRule above for the reading, its host and its date.
//
// Giving the child exactly ONE entry is therefore not a workaround for a loader
// that reads the wrong copy: it is the only shape under which the child's locale
// is a fact WE decided, rather than an outcome the order and the reader decide
// between them. `append(os.Environ(), "LC_ALL=C")` is a pin only while both
// agree, and both vary — by libc, by shell, and by whether a wrapper (GNU env, a
// shell, Go's own os/exec Cmd.Env) collapses the pair before the child runs.
//
// IT IS NAMED FOR THE RULE, NOT FOR GIT. It began with git children (this
// file's header), and the second measured caller is NOT a git child at all: the
// voice recorder in cmd/agent (cli_voice_client.go) classifies its stderr by
// four English literals — `Broken pipe` among them — and `Broken pipe` IS a
// glibc strerror message, so gettext translates it. Measured on this host
// 2026-10-12 against the real recorder:
//
//	$ arecord -D pulse … | head -c 3200 >/dev/null        # LANG=C
//	stdout: Broken pipe
//	$ env -u LC_ALL LANG=zh_CN.UTF-8 arecord -D pulse … | head -c 3200 >/dev/null
//	stdout: 断开的管道                                      # the marker no longer matches
//
// So the recorder child takes the same pin through this same function, rather
// than a second spelling of "byte-stable English diagnostics" in cmd/agent.
func ChildLocaleEnv() []string {
	return ChildLocaleEnvPinning(ChildLocaleEnglish)
}

// ChildLocaleEnvPinning is the SAME operation as ChildLocaleEnv, with the locale
// this child is pinned TO supplied by the caller.
//
// WHY THE PARAMETER EXISTS (measured 2026-10-14, epic 01a141e6). A seam that
// documents an operator override — cmd/agent's merge lane reads
// GOPI_MERGE_LOCALE before handing a git child its environment
// (merge_run.go, Rule 2) — could not call ChildLocaleEnv: the override would
// have had to be re-applied on top of the returned slice, i.e. by re-deciding
// the strip/append policy at the call site, which is exactly how a SECOND
// spelling of "byte-stable diagnostics" appears. Measured in that lane: the
// caller built its own four-name strip/append (`"LANG="`, `"LC_ALL="`,
// `"LC_MESSAGES="`, `"LANGUAGE="` typed inline) while this file owned the same
// four names — two owners of one list (Rule 13/14), so a fifth gettext selector
// added HERE would never have reached the merge lane's git children and no test
// in cmd/agent could have seen it.
//
// The VALUE is the caller's; the POLICY is not. Which names are written, which
// are stripped, and that the inherited entries are REPLACED rather than
// appended beside (a duplicate key resolves to the FIRST entry, so an appended
// override loses) all stay here, in one place.
func ChildLocaleEnvPinning(locale string) []string {
	return ChildLocaleEnvFrom(os.Environ(), locale)
}

// ChildLocaleEnvFrom is the SAME operation applied to a base the CALLER built,
// instead of to os.Environ().
//
// WHY THE BASE IS A PARAMETER (measured 2026-10-30, epic 01a18d50). One runner
// had already assembled its child's environment before the pin was consulted:
// gitPeerRun carries a caller-supplied slice, and the ONE caller that passes a
// non-nil one is task_run_ground.go's `gitPeerRun(ctx, repo,
// withoutGitDirEnv(os.Environ()), …)` — it strips a routed GIT_DIR because a
// worktree's .git is a FILE pointing elsewhere, and inheriting GIT_DIR would
// answer about the wrong repository. (Every other caller passes nil:
// peer_repo_identity.go ×2, task_sync_git_pushback.go, task_sync_git.go,
// git_peer_fetch.go ×2.) That caller could not use ChildLocaleEnvPinning
// without DISCARDING its slice, so the pin was skipped there entirely and a
// translated locale survived into a child whose output the pushback lane
// classifies (pbUnresolvableRepoPhrase). Handing the pin the slice makes "keep
// the caller's decisions" and "add ours" one operation (Rule 14: the POLICY
// stays here — which names are written, which are stripped,
// replaced-not-appended; only the base is theirs).
func ChildLocaleEnvFrom(base []string, locale string) []string {
	env := make([]string, 0, len(base)+len(ChildLocaleLocalePinnedNames))
	for _, kv := range base {
		name, _, _ := strings.Cut(kv, childLocaleAssignment)
		if ChildLocaleEnvName(name) {
			continue
		}
		env = append(env, kv)
	}
	for _, name := range ChildLocaleLocalePinnedNames {
		env = append(env, name+childLocaleAssignment+locale)
	}
	return env
}

// ChildLocaleEnvName reports whether name selects a message catalogue.
func ChildLocaleEnvName(name string) bool {
	for _, n := range ChildLocaleLocalePinnedNames {
		if n == name {
			return true
		}
	}
	for _, n := range ChildLocaleSelectorNames {
		if n == name {
			return true
		}
	}
	return false
}
