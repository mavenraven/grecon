# grecon

A live session picker for [Claude Code](https://claude.ai/claude-code) running in tmux.

![grecon picker](docs/screenshot.png)

## Why grecon?

If [Claude Squad](https://github.com/smtg-ai/claude-squad) or [recon](https://github.com/craftzdog/tmux-claude-session-manager) had fit my needs, I wouldn't have built grecon.

### grecon vs Claude Squad

These are different categories of tool. Claude Squad is a terminal-based IDE for Claude that abstracts over tmux. Grecon is a tmux picker — like `fzf` for your running Claude sessions.

- **Claude Squad takes over your terminal.** Your Claude sessions run in a tiny embedded window inside Claude Squad's UI. With grecon, your sessions run in full-size tmux panes that you control — grecon is just a picker you open, switch with, and close.
- **Claude Squad is prescriptive.** It manages session creation, git worktrees, branch pushing — sessions live inside the tool. Grecon is non-prescriptive — it works with whatever tmux layout you already have.
- **Claude Squad manages state.** It writes `~/.claude-squad/state.json` and maintains a `worktrees/` directory. This state can get out of sync with the actual tmux/git state. Grecon is completely stateless — it reads tmux and Claude's JSONL logs and never writes anything to disk.
- **Grecon has features Claude Squad doesn't.** AI-generated summaries (Haiku), live status (working/idle/input), background task tracking, subagent visibility, wakeup timer countdowns, custom tmux tags.

### grecon vs recon

Grecon is a fork of [recon](https://github.com/craftzdog/tmux-claude-session-manager) and shares its philosophy — both are companions to tmux, not replacements. The key differences:

- **Startup speed.** Recon scans tmux and parses JSONL on launch, which takes 1-2 seconds. Grecon runs a background server that polls continuously, so the picker opens instantly with data already ready.
- **AI summaries.** Grecon generates one-line Haiku summaries of what each agent is working on. Recon shows raw session info.
- **Live status.** Grecon detects whether each session is working, idle, or waiting for input. Recon doesn't.
- **Background tasks and subagents.** Grecon shows running background commands, monitor tools, spawned sub-agents, and wakeup timer countdowns.
- **Custom tags.** Grecon reads `@grecon/*` tmux session options and displays them in the picker.

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

### 3. Add a tmux keybinding

Add to `~/.tmux.conf`:

```bash
# prefix + g → open grecon picker
bind g run-shell 'tmux kill-session -t _grecon 2>/dev/null; \
  tmux new-session -d -s _grecon grecon; \
  tmux switch-client -t _grecon'
```

Reload with `tmux source ~/.tmux.conf`, then press your prefix + `g`.

### Custom tags

Set `@grecon/*` tmux options to display custom metadata in the picker:

```bash
tmux set @grecon/env "production"
tmux set @grecon/team "platform"
```

### Session persistence

For persisting sessions across reboots, use these tmux plugins:

- [tmux-resurrect](https://github.com/tmux-plugins/tmux-resurrect) — saves and restores tmux session layouts
- [tmux-continuum](https://github.com/tmux-plugins/tmux-continuum) — auto-saves resurrect state periodically
- [tmux-assistant-resurrect](https://github.com/nicksp/tmux-assistant-resurrect) — hooks into resurrect to resume Claude Code with the correct `--resume` flags

## License

MIT
