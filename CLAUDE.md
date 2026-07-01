# grecon

A TUI for monitoring and managing Claude Code sessions running in tmux.

## After pushing changes

Always kill the running `grecon server`, reinstall the binary (`go install .`), and restart the server (`grecon server &`) after pushing code changes.

## Architecture

- **Go CLI** using cobra for commands, bubbletea for TUI
- **Server** runs in background, polls tmux every 500ms to discover live Claude sessions
- **SQLite database** at `~/.grecon/grecon.db` tracks workstreams, tmux sessions, and claude sessions
- **Client** connects to server via unix socket, renders the picker TUI

### Key commands
- `grecon` (no args) — opens the picker TUI
- `grecon new` — interactive form to create a new tmux session running Claude
- `grecon server` — starts the background server
- `grecon launch` — non-interactive session creation

### Session discovery flow (`DiscoverSessions` in server/session.go)
1. Lists all tmux panes and finds Claude processes via process tree
2. Matches PIDs to Claude session files in `~/.claude/sessions/{pid}.json`
3. Finds corresponding JSONL files in `~/.claude/projects/` by scanning all project directories for `{session-id}.jsonl`
4. Parses JSONL for model, tokens, status, subagents, wakeups, etc.

### Session visibility in the picker
`discoverTmuxSessions` (server/server.go) filters `DiscoverSessions` results to only include sessions whose tmux name exists in the database. A session without a DB entry is invisible in the picker.

### Database schema (3 tables)
- **workstreams** — top-level grouping (id, created_at, deleted_at)
- **tmux_sessions** — links a workstream to a tmux session name (workstream_id, tmux_id, display_name, created_at, deleted_at)
- **claude_sessions** — individual Claude sessions within a workstream (workstream_id, session_id, display_name, summary, active, created_at, deleted_at)

All deletes are soft deletes (set `deleted_at` timestamp). Queries filter on `deleted_at = ''`.

### Session creation flow (`grecon new`)
1. DB entry (workstream + tmux_session) is created FIRST
2. Then the tmux session is created with `claude` (and `--worktree` if selected)
3. Claude Code owns worktree data — grecon does NOT store worktree paths. The worktree checkbox just passes `--worktree` to the Claude CLI.

### Pruning (`PruneDeadSessions` in db/queries.go, runs every ~5 seconds)
- Soft-deletes claude_sessions whose JSONL no longer exists (Claude Code prunes JSONLs after 30 days via `cleanupPeriodDays`)
- Soft-deletes claude_sessions whose worktree no longer exists on disk (checks `worktree-state` record in the JSONL)
- Soft-deletes empty workstreams (no remaining claude_sessions) that aren't live in tmux
- Skips workstreams created less than 10 minutes ago (prevents race condition where DB entry is pruned before tmux session starts)

### Claude Code data ownership
- Claude Code stores session data in `~/.claude/projects/{encoded-path}/{session-id}.jsonl`
- JSONL files contain `worktree-state` records with `originalCwd`, `worktreePath`, etc.
- When a worktree is deleted, Claude writes `worktreeSession: null` in the JSONL
- Session PID metadata lives in `~/.claude/sessions/{pid}.json` (includes cwd, sessionId, status)
- grecon reads this data but never writes to it — Claude Code is the source of truth

### Reconciliation (`reconcileDBWithLive` in server/server.go)
- Adds new live sessions to the DB if not already tracked
- Marks sessions active/inactive based on whether they're running in tmux
- Appends inactive/deleted DB sessions to the picker list so they remain visible

### Error handling
- `grecon new` surfaces errors via cobra's RunE (prints to stderr on exit)
- The picker TUI quits and prints errors to stderr when Enter fails (e.g., reactivation fails)

## Code layout

- `main.go` — cobra command definitions
- `client/` — TUI (app.go, ui.go), session creation form (new_session.go), tmux helpers (tmux.go)
- `server/` — background server (server.go), session discovery (session.go), command handling (commands.go), session restore (restore.go)
- `db/` — SQLite schema/migrations (migrate.go), all queries (queries.go), connection management (db.go)
