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

### Database schema (3 tables)

#### workstreams
- `id` — auto-increment primary key
- `created_at` — ISO 8601 timestamp
- `deleted_at` — ISO 8601 timestamp, empty string means not deleted

A workstream is soft-deleted when it has no remaining tmux_sessions.

#### tmux_sessions
- `id` — auto-increment primary key
- `workstream_id` — foreign key to workstreams
- `tmux_id` — UUID, the actual tmux session name. Generated once at creation, never changes. Used for all tmux commands (`new-session -s`, `kill-session -t`, `switch-client -t`, etc.)
- `display_name` — human-friendly name shown in the picker and set as `@display_name` tmux user option via `tmux set-option -t <tmux_id> @display_name <name>`. Cosmetic only — never used for lookups or tmux operations.
- `created_at` — ISO 8601 timestamp
- `deleted_at` — ISO 8601 timestamp, empty string means not deleted

A tmux_session is soft-deleted when it is older than 10 minutes and has no claude_sessions.

#### claude_sessions
- `id` — auto-increment primary key
- `workstream_id` — foreign key to workstreams
- `session_id` — Claude Code's session UUID (from `~/.claude/sessions/{pid}.json`). Must be a real Claude session ID — never generate fake IDs.
- `display_name` — Claude agent name (from `-n` flag or JSONL `agent-name` record)
- `summary` — last conversation summary
- `active` — 1 if currently running in tmux, 0 if not
- `created_at` — ISO 8601 timestamp
- `deleted_at` — ISO 8601 timestamp, empty string means not deleted

A claude_session is soft-deleted when:
- It is older than 10 minutes and its JSONL no longer exists (Claude Code prunes after 30 days)
- It is older than 10 minutes and its worktree no longer exists on disk
- The user presses `x` in the picker

All deletes are soft deletes (set `deleted_at` timestamp). Queries filter on `deleted_at = ''`. All pruning skips rows created less than 10 minutes ago (grace period for startup race conditions).

### Session creation flow (`grecon new`)
1. User fills in: tmux display name, claude name, working directory, worktree checkbox
2. DB entry (workstream + tmux_session) is created FIRST — `tmux_id` is a new UUID, `display_name` is what the user typed
3. Tmux session is created with `tmux new-session -s <UUID>` — the UUID is the tmux session name
4. `tmux set-option -t <UUID> @display_name <name>` sets the cosmetic name
5. Claude is launched with `-n <claude_name>` and `--worktree` if selected
6. Claude Code owns worktree data — grecon does NOT store worktree paths

### Session discovery flow (`DiscoverSessions` in server/session.go)
1. Lists all tmux panes and finds Claude processes via process tree
2. Matches PIDs to Claude session files in `~/.claude/sessions/{pid}.json` — if no match, skip (do NOT generate fake IDs)
3. Finds corresponding JSONL files in `~/.claude/projects/` by scanning all project directories for `{session-id}.jsonl`
4. Parses JSONL for model, tokens, status, subagents, wakeups, etc.

### Session visibility in the picker
`discoverTmuxSessions` (server/server.go) filters `DiscoverSessions` results to only include sessions whose `TmuxSession` (the UUID) exists in the database. The picker groups sessions by `TmuxSession` (UUID) but displays `TmuxDisplayName` as the header.

### Pruning (`PruneDeadSessions` in db/queries.go, runs every ~5 seconds)
- Soft-deletes claude_sessions older than 10 min whose JSONL no longer exists
- Soft-deletes claude_sessions older than 10 min whose worktree no longer exists on disk (checks `worktree-state` record in the JSONL)
- Soft-deletes tmux_sessions/workstreams older than 10 min with zero remaining claude_sessions that aren't live in tmux

### Soft-delete cleanup (`cleanupSoftDeleted` in server/server.go, runs every 500ms)
- Kills tmux panes for claude_sessions that have been soft-deleted (e.g., user pressed `x`)
- Does NOT prune workstreams — that's `PruneDeadSessions`' job only (single source of truth)

### Claude Code data ownership
- Claude Code stores session data in `~/.claude/projects/{encoded-path}/{session-id}.jsonl`
- JSONL files contain `worktree-state` records with `originalCwd`, `worktreePath`, etc.
- When a worktree is deleted, Claude writes `worktreeSession: null` in the JSONL
- Session PID metadata lives in `~/.claude/sessions/{pid}.json` (includes cwd, sessionId, status)
- grecon reads this data but never writes to it — Claude Code is the source of truth

### Reconciliation (`reconcileDBWithLive` in server/server.go)
- Adds new live sessions to the DB if not already tracked (only real Claude session IDs)
- Marks sessions active/inactive based on whether they're running in tmux
- Appends inactive/deleted DB sessions to the picker list so they remain visible
- Populates `TmuxDisplayName` on all sessions from the DB (single source for display names)

### Error handling
- `grecon new` surfaces errors via cobra's RunE (prints to stderr on exit)
- The picker TUI quits and prints errors to stderr when Enter fails (e.g., reactivation fails)

## Code layout

- `main.go` — cobra command definitions
- `client/` — TUI (app.go, ui.go), session creation form (new_session.go), tmux helpers (tmux.go)
- `server/` — background server (server.go), session discovery (session.go), command handling (commands.go), session restore (restore.go)
- `db/` — SQLite schema/migrations (migrate.go), all queries (queries.go), connection management (db.go)
