# grecon

A tmux-native dashboard for managing [Claude Code](https://claude.ai/claude-code) sessions.

Run multiple Claude Code instances in tmux, then manage them all from one place — see what each agent is working on, which ones need your attention, switch between them, kill or spawn new ones, and resume past sessions.

```
┌ grecon — Claude Code Sessions ──────────────────────────────────────────────────────────────────────┐
│  #  Session          Project                Directory          Status  Model       Context  Activity │
│  1  api-refactor     myapp::feat/auth       ~/repos/myapp      ● Input Opus 4.6    45k/1M   2m ago   │
│  2  debug-pipeline   infra::main            ~/repos/infra      ● Work  Sonnet 4.6  12k/200k < 1m     │
│  3  write-tests      myapp::feat/auth       ~/repos/myapp      ● Work  Haiku 4.5   8k/200k  < 1m     │
│  4  code-review      webapp::pr-452         ~/repos/webapp     ● Idle  Sonnet 4.6  90k/200k 5m ago   │
│  5  scratch          recon::main            ~/repos/recon      ● Idle  Opus 4.6    3k/1M    10m ago  │
└─────────────────────────────────────────────────────────────────────────────────────────────────────┘
j/k navigate  Enter switch  x kill  / search  i next input  q quit
```

- **Input** rows are highlighted — these sessions are blocked waiting for your approval
- **Working** sessions are actively streaming or running tools
- **Idle** sessions are done and waiting for your next prompt

## How it works

grecon runs a background server that polls tmux every 2 seconds. The TUI connects to this server over a Unix socket.

```
tmux list-panes (#{pane_pid})  →  PID → tmux session name
~/.claude/sessions/{PID}.json  →  PID → JSONL session ID
~/.claude/projects/*/*.jsonl   →  session ID → tokens, model, timestamps
tmux capture-pane              →  tmux session → status (last line of pane)
```

Status detection reads the Claude Code status bar at the bottom of each tmux pane:

| Status bar text | State |
|---|---|
| `esc to interrupt` | **Working** |
| `Esc to cancel` | **Input** |
| anything else | **Idle** |

## tmux config

```bash
# prefix + g → switch to grecon (creates session if needed)
bind g run-shell 'tmux kill-session -t _grecon 2>/dev/null; tmux new-session -d -s _grecon grecon; tmux switch-client -t _grecon'

# prefix + f → jump to next session waiting for input
bind f run-shell 'grecon next'
```

## License

MIT
