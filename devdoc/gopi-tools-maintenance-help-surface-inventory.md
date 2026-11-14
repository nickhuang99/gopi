<!-- doc-class: normative -->
# Inventory — G6 help surface (tools-from-the-shell, maintenance, top-level)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

GENERATED — do not hand-edit. This is the surface the G6 help pages
ADVERTISE, with whether a BASIC test drives it, derived from the binary's own
`-h` output. Re-running the two commands below reproduces every row:

```bash
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
scripts/g6-tools-maintenance-help-surface.sh ./bin/gopi
```

`cmd/agent/cli_tools_maintenance_help_surface_basic_test.go` performs the SAME
derivation in-process — from the usage CONSTANTS the help printer itself reads —
and fails when this file and the help disagree, in EITHER direction. A form
listed here that no page prints any more goes red; so does an advertised form
that is neither typed by a test nor declared blocked with a reason.

A row is the help's own line, VERBATIM (prose cut). The `tested?` column
names the test that types it or the blocker that makes a mock meaningless — a
form whose happy path needs a credential, device, peer or Chromium render is
asserted UP TO ITS FIRST LOCAL REFUSAL, and a form with no local state at all is
listed as blocked rather than faked.

| # | verb | form | tested? |
|---|------|------|---------|
| 1 | `vision_analyze` | `gopi tool vision_analyze <image_path> [--prompt "text"]` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 2 | `vision_analyze` | `gopi tool vision_analyze screenshot.png` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 3 | `vision_analyze` | `gopi tool vision_analyze chart.png --prompt "describe the chart data"` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 4 | `vision_analyze` | `gopi tool vision_analyze photo.png --prompt "识别图片中的所有中文文字"` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 5 | `text2img` | `gopi tool text2img <input.txt> [output.png] [flags]` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 6 | `text2img` | `gopi tool text2img notes.txt` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 7 | `text2img` | `gopi tool text2img report.txt out.png --size 28 --cols 60` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 8 | `text2img` | `gopi tool text2img report.txt --out out.png --size 28` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 9 | `text2img` | `gopi tool text2img paper.txt --lines 60 --lh 36` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 10 | `screen_capture` | `gopi tool screen_capture --out before.png` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 11 | `screen_capture` | `gopi tool screen_capture --out after.png` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 12 | `screen_capture` | `gopi tool screen_capture [flags]` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 13 | `act` | `gopi tool screen_capture --out before.png` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 14 | `act` | `gopi tool act move 400 300` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 15 | `act` | `gopi tool act click 1` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 16 | `act` | `gopi tool screen_capture --out after.png` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 17 | `act` | `gopi tool act info` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 18 | `act` | `gopi tool act move X Y` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 19 | `act` | `gopi tool act click [--button N]` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 20 | `act` | `gopi tool act type [--text STR]` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 21 | `act` | `gopi tool act key [--key NAME]` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 22 | `act` | `gopi tool voice_input --client --self` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 23 | `act` | `gopi tool act type --text 'say hello\n'` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 24 | `act` | `gopi tool act type --text 'say hello' --enter` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 25 | `act` | `gopi tool act key --key return` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 26 | `act` | `gopi tool act type --text 'ls -l\n' --self` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 27 | `act` | `gopi tool act type --text 'x' --self --dry-run` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 28 | `act` | `gopi tool act key --key return --self` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 29 | `voice_input` | `gopi tool voice_input` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 30 | `voice_input` | `gopi tool voice_input <audio-file> [--chunk N]` | no — transcribes only with the speech model available |
| 31 | `voice_input` | `gopi tool voice_input --server` | no — starts the transcription HTTP server and loads the speech-engine model |
| 32 | `voice_input` | `gopi tool voice_input --server --listen :7788` | no — opens a listener and downloads the speech model |
| 33 | `voice_input` | `gopi tool voice_input --server --stdio` | no — needs the speech engine model on disk |
| 34 | `voice_input` | `gopi tool voice_input --client [--device ID] [--seconds N]` | no — needs a real capture device (pulse/alsa) |
| 35 | `voice_input` | `gopi tool voice_input --list-devices` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 36 | `voice_input` | `gopi tool voice_input --client --list-devices` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 37 | `voice_input` | `gopi tool voice_input --client --seconds N --stdio` | no — needs a real capture device (pulse/alsa) |
| 38 | `voice_input` | `gopi tool voice_input --client --seconds 5 --stdio` | no — needs a real capture device (pulse/alsa) |
| 39 | `voice_input` | `gopi tool voice_input --client --audio-source FILE [--seconds N]` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 40 | `voice_input` | `gopi tool act key --key return --self` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 41 | `voice_input` | `gopi tool voice_input --seconds 5` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 42 | `voice_input` | `gopi tool voice_input talk.wav` | no — transcribes only with the speech model available |
| 43 | `voice_input` | `gopi tool voice_input long-interview.wav --chunk 15` | no — transcribes only with the speech model available |
| 44 | `voice_input` | `gopi tool voice_input --client --seconds 8` | no — needs a real capture device (pulse/alsa) |
| 45 | `voice_input` | `gopi tool voice_input --client --seconds 8 --out nick.wav` | no — needs a real capture device (pulse/alsa) |
| 46 | `voice_input` | `gopi tool voice_input --client --self --seconds 8` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 47 | `voice_input` | `gopi tool voice_input --client --self --seconds 8 --display :57` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 48 | `voice_input` | `gopi tool voice_input --client --server-url http://192.0.2.6:7788 --seconds 8` | no — needs a peer voice server on the LAN |
| 49 | `voice_input` | `gopi tool voice_input clip.ogg` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 50 | `voice_input` | `gopi tool voice_input clip.pcm` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 51 | `voice_input` | `gopi tool voice_input --server --stdio < clip.pcm` | no — needs the speech engine model on disk |
| 52 | `shot` | `gopi tool shot --self && gopi tool vision_analyze <out.png>` | no — a chained shell example, not a form of one verb; needs a Chromium render (see the --self row) |
| 53 | `shot` | `gopi tool shot --url URL [--out PATH] [flags]` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 54 | `shot` | `gopi tool shot --self [--addr HOST:PORT] [--out PATH] [flags]` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 55 | `shot` | `gopi tool shot --self` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 56 | `shot` | `gopi tool shot --url https://example.com` | no — needs outbound network + a real Chromium render |
| 57 | `shot` | `gopi tool shot --url http://x --wait-selector "#chat-log"` | no — needs a live page carrying the selector |
| 58 | `shot` | `gopi tool shot --self --viewport 1440x900 --out web.png` | no — needs the web UI served AND a Chromium render |
| 59 | `code-search` | `gopi tool code-search <symbol> [--root DIR]` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 60 | `code-search` | `gopi tool code-search NewAgent` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 61 | `code-search` | `gopi tool code-search codeSearchScope --root internal/agent` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 62 | `archive` | `gopi task archive <branch> --reason <code> [--detail TEXT] [--override-completed]` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 63 | `archive` | `gopi task archive --check [--repo PATH]` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 64 | `archive` | `gopi task archive --reclaim [--repo PATH]` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 65 | `archive` | `gopi task archive --verify-commit <rev> [--repo PATH]` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 66 | `archive` | `gopi task archive --unreachable [--repo PATH]` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 67 | `archive` | `gopi task archive --rescue-tip <rev> --reason <code> [--repo PATH]` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 68 | `archive` | `gopi task archive feat/old-idea --reason superseded --detail "landed as feat/new-idea"` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 69 | `archive` | `gopi task archive fix/broken --reason compile-failure` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 70 | `archive` | `gopi task archive feat/forgotten --reason hand-mistake --override-completed` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 71 | `archive` | `gopi task archive --check` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 72 | `archive` | `gopi task archive --verify-commit e1701ac8` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 73 | `archive` | `gopi task archive --unreachable` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 74 | `archive` | `gopi task archive --rescue-tip 00fb325abef8 --reason hand-mistake --detail "orphaned tip from task/01a10e23"` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 75 | `worker` | `gopi worker` | no — drains the real ledger and runs task children (host action) |
| 76 | `worker` | `gopi worker --once` | no — drains the real ledger and runs task children (host action) |
| 77 | `worker` | `gopi worker [<task-id>]` | no — runs a task child against the real profile |
| 78 | `worker` | `gopi worker --resume <task-id>` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 79 | `worker` | `gopi worker --resume --branch B` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 80 | `worker` | `gopi worker --resume --worktree P` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 81 | `worker` | `gopi worker --daemon` | no — starts a detached resident process |
| 82 | `worker` | `gopi worker --status` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 83 | `worker` | `gopi worker --status --json` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 84 | `worker` | `gopi worker list` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 85 | `worker` | `gopi worker --stop` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 86 | `worker` | `gopi worker --stop --branch B` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 87 | `worker` | `gopi worker --stop --pid N` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 88 | `worker` | `gopi worker --restart` | no — stops and starts a detached resident process |
| 89 | `worker` | `gopi worker --start` | no — starts a detached resident process |
| 90 | `worker` | `gopi worker -h|--help` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 91 | `config` | `gopi config show [--settings PATH]` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 92 | `config` | `gopi config env [--write] [--path P]` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 93 | `config` | `gopi config setup [flags]` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 94 | `config` | `gopi config completion <shell>` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 95 | `config` | `gopi config -h` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 96 | `completion` | `gopi config completion bash` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 97 | `completion` | `gopi config completion bash > ~/.local/share/bash-completion/completions/gopi` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 98 | `completion` | `gopi config completion zsh` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 99 | `completion` | `gopi config completion fish` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 100 | `completion` | `gopi config completion fish > ~/.config/fish/completions/gopi.fish` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 101 | `scratch-gc` | `gopi tool scratch-gc` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 102 | `scratch-gc` | `gopi tool scratch-gc --dry-run` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 103 | `scratch-gc` | `gopi tool scratch-gc --explain` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 104 | `scratch-gc` | `gopi tool scratch-gc --explain --dry-run` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 105 | `scratch-gc` | `gopi tool scratch-gc tick` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 106 | `scratch-gc` | `gopi tool scratch-gc base` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 107 | `scratch-gc` | `gopi tool scratch-gc base --dry-run` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 108 | `scratch-gc` | `gopi tool scratch-gc trees` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 109 | `scratch-gc` | `gopi tool scratch-gc trees --dry-run` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 110 | `scratch-gc` | `gopi tool scratch-gc families` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 111 | `scratch-gc` | `gopi tool scratch-gc profile` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 112 | `scratch-gc` | `gopi tool scratch-gc profile --dry-run` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 113 | `scratch-gc` | `gopi tool scratch-gc profile --explain` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 114 | `scratch-gc` | `gopi tool scratch-gc profile --yes` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 115 | `scratch-gc` | `gopi tool scratch-gc sandboxes` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 116 | `scratch-gc` | `gopi tool scratch-gc sandboxes --dry-run` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 117 | `scratch-gc` | `gopi tool scratch-gc stubs` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 118 | `scratch-gc` | `gopi tool scratch-gc stubs --dry-run` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 119 | `scratch-gc` | `gopi tool scratch-gc stubs --yes` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 120 | `scratch-gc` | `gopi tool scratch-gc roots` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 121 | `scratch-gc` | `gopi tool scratch-gc roots --explain` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 122 | `scratch-gc` | `gopi tool scratch-gc roots --yes` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 123 | `scratch-gc` | `gopi tool scratch-gc roots --root DIR` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 124 | `scratch-gc` | `gopi tool scratch-gc gocache` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 125 | `scratch-gc` | `gopi tool scratch-gc gocache --path DIR` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 126 | `scratch-gc` | `gopi tool scratch-gc gocache --max-gib GiB` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 127 | `scratch-gc` | `gopi tool scratch-gc gocache --headroom-gib GiB` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 128 | `scratch-gc` | `gopi tool scratch-gc gocache --trim` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 129 | `scratch-gc` | `gopi tool scratch-gc deps` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 130 | `scratch-gc` | `gopi tool scratch-gc -h` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 131 | `scratch-gc` | `gopi tool scratch-gc trees --tree NAME` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 132 | `scratch-gc` | `gopi tool scratch-gc families --list` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 133 | `scratch-gc` | `gopi tool scratch-gc families --tree NAME` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 134 | `version` | `gopi version` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 135 | `(top-level)` | `gopi [options] "<prompt>"` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 136 | `(top-level)` | `gopi [options] input.txt` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 137 | `(top-level)` | `gopi [options] < input.txt` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 138 | `(top-level)` | `gopi [options] --prompt TEXT a.png` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 139 | `(top-level)` | `gopi -h | --help` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 140 | `(top-level)` | `gopi --help-env` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 141 | `(top-level)` | `gopi <command> -h` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 142 | `(top-level)` | `gopi version` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 143 | `(top-level)` | `gopi "<prompt>"` | no — a run needs provider credentials (asserted up to the credential refusal) |
| 144 | `(top-level)` | `gopi @PATH` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 145 | `(top-level)` | `gopi ./image.png` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 146 | `(top-level)` | `gopi ./notes.txt` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 147 | `(top-level)` | `gopi ./PROJECT.md` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 148 | `(top-level flags)` | `gopi --settings` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 149 | `(top-level flags)` | `gopi --home` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 150 | `(top-level flags)` | `gopi --prompt` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 151 | `(top-level flags)` | `gopi --model` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 152 | `(top-level flags)` | `gopi --api-key` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 153 | `(top-level flags)` | `gopi --base-url` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 154 | `(top-level flags)` | `gopi --vision-model` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 155 | `(top-level flags)` | `gopi --vision-base-url` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 156 | `(top-level flags)` | `gopi --reasoning-effort` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 157 | `(top-level flags)` | `gopi --thinking` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 158 | `(top-level flags)` | `gopi --hide-thinking` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 159 | `(top-level flags)` | `gopi --keep-thinking` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 160 | `(top-level flags)` | `gopi --max-tokens` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 161 | `(top-level flags)` | `gopi --context-window` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 162 | `(top-level flags)` | `gopi --timeout` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 163 | `(top-level flags)` | `gopi --compact` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 164 | `(top-level flags)` | `gopi --tool-timeout` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 165 | `(top-level flags)` | `gopi --bash-timeout` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 166 | `(top-level flags)` | `gopi --bash-idle` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 167 | `(top-level flags)` | `gopi --raw-log` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 168 | `(top-level flags)` | `gopi --db` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 169 | `(top-level flags)` | `gopi --tmpdir` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 170 | `(top-level flags)` | `gopi --output-language` | no — the flag is admitted by the parser (a bare flag run reaches the credential refusal, which is typed by the Usage gate TestTopLevelUsageShowsReplyLanguage); driving it as a FORM would need a live provider to observe the reply language it selects |
| 171 | `(top-level flags)` | `gopi --chat-ideal-geometry` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 172 | `(top-level flags)` | `gopi --memory-prefetch` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |
| 173 | `(top-level flags)` | `gopi --chat-surface-turns` | yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped` |

**173 advertised forms** across 15 verbs — 169 DISTINCT
forms, because a page may carry an example that another page also prints
(the two `screen_capture` examples appear on the `act` page, and `gopi
version` on the top-level page as well as its own). Of the 169 distinct
forms, **144 are typed** by a basic test and **25 are
declared blocked** with a named blocker; the two maps together account for
every one, which is exactly what the completeness gate asserts.

*(Counts re-derived by `bash scripts/g6-tools-maintenance-help-surface.sh
./bin/gopi` in task 01a1957a: `gopi tool scratch-gc --explain` became an
advertised form of its own when that flag stopped being purely additive (it now
IMPLIES `--dry-run`), so the scratch-gc group gained one row and every later row
number shifted down by one. The `tested?` column moved with it — the previous
revision's typed/blocked split was a stale snapshot of the two maps this
generator reads.)*
