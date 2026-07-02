# grecon

A live session picker for [Claude Code](https://claude.ai/claude-code) running in tmux.

Grecon discovers every Claude Code instance running in your tmux sessions and shows them in a single, fast picker. See what each agent is working on, which ones need input, and jump to them instantly.

Completely stateless — grecon reads tmux and Claude's own JSONL logs. It never writes anything to disk.

Works with any tmux + Claude Code workflow: one Claude session per tmux session, multiple Claude sessions in one tmux session, or a mix. Grecon doesn't care how you organize things — it finds every Claude process in every tmux pane and shows them all.

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

## Why grecon?

There are several tools for managing Claude Code sessions. Here's how grecon compares:

| | grecon | [Claude Squad](https://github.com/smtg-ai/claude-squad) | [recon](https://github.com/craftzdog/tmux-claude-session-manager) |
|---|---|---|---|
| **Approach** | Picker — find and switch | All-in-one — tmux abstraction | Popup picker |
| **Philosophy** | Companion to tmux | Replacement for tmux | Companion to tmux |
| **Startup** | Instant — server has data ready | Loads on launch | Scans on launch (1-2s delay) |
| **Terminal** | Full-size tmux panes | Tiny embedded window | Popup overlay |
| **Session mgmt** | No — use tmux directly | Yes — creates/manages sessions | Yes — creates sessions |
| **Worktree mgmt** | No — use git directly | Yes — creates worktrees | No |
| **Push branches** | No — use git directly | Yes | No |
| **Workflow** | Non-prescriptive — works with any tmux layout | Prescriptive — sessions live inside the tool | Tied to its own session model |
| **State** | None — completely stateless | Manages its own state | Manages its own state |
| **AI summaries** | Yes — Haiku-generated | No | No |
| **Background tasks** | Yes — tracks shell/monitor/wakeup | No | No |
| **Subagents** | Yes — shows spawned agents | No | No |

**grecon's opinion:** These are different categories of tool. Claude Squad is a terminal-based IDE for Claude — it manages sessions, worktrees, branches, and gives you an embedded terminal to work through. Grecon is a tmux picker that works well with Claude — like `fzf` for your running sessions. Your terminal is yours; grecon doesn't take it over. You open it, switch sessions, and close it. Your Claude sessions run in full-size tmux panes that you control. For session creation, use `claude` directly (or skills). For persistence, use tmux-resurrect.

## Features

- **AI summaries** — each session gets a one-line Haiku-generated summary of what the agent is doing
- **Live status** — see which agents are working, idle, or waiting for your input
- **Background tasks** — see Bash commands and Monitor tools running in the background, with live/dead status
- **Subagents** — see spawned sub-agents (workflows, teammates) nested under their parent
- **Wakeup timers** — live countdown for `ScheduleWakeup` calls (polling loops, scheduled checks)
- **Search** — filter sessions by name with `/`
- **Jump to input** — press `i` to jump straight to the next agent that needs your attention

- **Custom tags** — set `@grecon/*` tmux session options and they show up in the picker automatically
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

## Design decisions

**Grecon is a picker, not a platform.** One screen, no persistent state, no session creation or management. It reads tmux and Claude's JSONL logs — it never writes anything to disk.

**Anti-goals:**
- Additional screens or modes
- Persistent state of any kind (no database, no files on disk)
- Session creation, resume, or lifecycle management

**What might be added:** new stateless data in the existing picker — tmux session properties, working directory, etc. Just more data read from tmux or the JSONL. No new screens, no state.

**Instant startup:** The picker opens instantly because the server is always running and has fresh data ready. No waiting for discovery on launch — the TUI connects, gets the latest snapshot, and renders immediately. That's the whole reason for the client/server split.

**Time horizon:** This tool is meant to be useful for the next 12-24 months of AI tooling evolution, not the next 10 years. Someone will build something better. The goal is to help people now while Claude Code is tmux-based, not to build the perfect tool for a future that's moving too fast to predict.

### Custom tags

Set tmux user options prefixed with `@grecon/` to display custom metadata in the picker:

```bash
tmux set @grecon/env "production"
tmux set @grecon/team "platform"
```

Tags appear under the tmux session header in the picker. They're read-only from grecon's perspective — your launcher or workflow scripts set them, grecon just displays them.

**What about session persistence / resume?** Use these tmux plugins — they handle it better than any custom solution:

- [tmux-resurrect](https://github.com/tmux-plugins/tmux-resurrect) — saves and restores tmux session layouts
- [tmux-continuum](https://github.com/tmux-plugins/tmux-continuum) — auto-saves resurrect state periodically
- [tmux-assistant-resurrect](https://github.com/nicksp/tmux-assistant-resurrect) — hooks into resurrect to resume Claude Code (and other AI assistants) with the correct `--resume` flags

This stack handles persistence across reboots. Grecon handles the live picker. They compose cleanly — grecon sees whatever tmux-resurrect brings back.

## License

MIT
