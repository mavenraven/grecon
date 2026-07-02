# grecon

A live session picker for [Claude Code](https://claude.ai/claude-code) running in tmux.

Grecon discovers every Claude Code instance running in your tmux sessions and shows them in a single, fast picker. See what each agent is working on, which ones need input, and jump to them instantly.

Completely stateless — grecon reads tmux and Claude's own JSONL logs. It never writes anything to disk.

```
+- grecon -- Claude Code Sessions ----------------------------------------+
| Name                      Status    Summary                             |
| api-refactor                                                            |
| +- calm-river             * Work    Adding retry logic to deploy step   |
| |  +- shell               * Run     Run full test suite                 |
| |  +- monitor             * Run     Test suite completion               |
| +- bright-fox             * Idle    Refactored auth middleware          |
| webapp                                                                  |
| +- quick-elk              * Input   Waiting for approval to delete...   |
| |  +- wakeup in 4m20s     * Sleep   checking CI build                   |
| infra                                                                   |
| +- bold-hawk              * Work    Migrating config to new schema      |
| |  +- shell               * Run     Running database migration          |
+-------------------------------------------------------------------------+
 j/k navigate  Enter switch  x kill  / search  i next input  q quit
```

## Features

- **AI summaries** — each session gets a one-line Haiku-generated summary of what the agent is doing
- **Live status** — see which agents are working, idle, or waiting for your input
- **Background tasks** — see Bash commands and Monitor tools running in the background, with live/dead status
- **Subagents** — see spawned sub-agents (workflows, teammates) nested under their parent
- **Wakeup timers** — live countdown for `ScheduleWakeup` calls (polling loops, scheduled checks)
- **Search** — filter sessions by name with `/`
- **Jump to input** — press `i` to jump straight to the next agent that needs your attention

All of this is derived from tmux pane content and Claude's JSONL logs. Zero state.

## Getting started

### 1. Install

```bash
go install github.com/mavenraven/grecon@latest
```

Requires Go 1.21+ and tmux.

### 2. Start the server

```bash
grecon server &
```

The server polls tmux every ~500ms, parses JSONL logs, generates summaries, and streams results to connected clients.

### 3. Add a tmux keybinding

Add to `~/.tmux.conf`:

```bash
# prefix + g → open grecon picker
bind g run-shell 'tmux kill-session -t _grecon 2>/dev/null; \
  tmux new-session -d -s _grecon grecon; \
  tmux switch-client -t _grecon'
```

Reload with `tmux source ~/.tmux.conf`, then press your prefix + `g`.

### Keyboard shortcuts

| Key | Action |
|---|---|
| `j` / `k` | Navigate up/down |
| `Enter` | Switch to selected session |
| `x` | Kill selected pane |
| `/` | Search/filter sessions |
| `i` or `Tab` | Jump to next session waiting for input |
| `q` | Quit |

## How it works

Grecon is two things: a background server and a TUI client, connected over a Unix socket.

The server polls every ~500ms:

```
tmux list-panes (pane_pid)     →  find Claude processes in tmux
~/.claude/sessions/{PID}.json  →  map PID to JSONL session ID
~/.claude/projects/*/*.jsonl   →  parse tokens, model, status, background tasks
tmux capture-pane              →  read the status bar for Working/Idle/Input
```

Summaries are generated lazily by calling Haiku when JSONL content changes.

The TUI subscribes to the server and gets instant updates. That's it — no database, no files on disk, no state to get out of sync.

## What about session creation / resume?

Grecon is just a picker. For creating sessions, use `claude` directly (or build a launcher). For persisting sessions across reboots, use [tmux-resurrect](https://github.com/tmux-plugins/tmux-resurrect). Grecon is designed to compose with these tools, not replace them.

## License

MIT
