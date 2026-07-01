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
- `playwright` — legacy flag, unused but still in schema
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
- `summary` — last conversation summary
- `active` — 1 if currently running in tmux, 0 if not
- `created_at` — ISO 8601 timestamp
- `deleted_at` — ISO 8601 timestamp, empty string means not deleted

Claude agent name is NOT stored in the database. It is always read from the JSONL `agent-name` record. The database should never cache data that Claude Code owns.

A claude_session is soft-deleted when:
- It is older than 10 minutes and its JSONL no longer exists (Claude Code prunes after 30 days)
- It is older than 10 minutes and its worktree no longer exists on disk
- The user presses `x` in the picker

All deletes are soft deletes (set `deleted_at` timestamp). Queries filter on `deleted_at = ''`. All pruning skips rows created less than 10 minutes ago (grace period for startup race conditions). Never duplicate pruning logic — `PruneDeadSessions` is the single source of truth for all pruning decisions.

### Session creation flow (`grecon new`)
1. User fills in: tmux display name, claude name, working directory, worktree checkbox
2. DB entry (workstream + tmux_session) is created FIRST — `tmux_id` is a new UUID, `display_name` is what the user typed
3. Tmux session is created with `tmux new-session -s <UUID>` — the UUID is the tmux session name
4. `tmux set-option -t <UUID> @display_name <name>` sets the cosmetic name
5. Claude is launched with `-n <claude_name>` and `--worktree` if selected
6. If worktree, `fixDefaultPath` polls until Claude settles into the worktree directory, then sets it as the tmux session's default path for new windows
7. Claude Code owns worktree data — grecon does NOT store worktree paths

### Session discovery flow (`DiscoverSessions` in server/session.go)
1. Lists all tmux panes and finds Claude processes via process tree
2. Matches PIDs to Claude session files in `~/.claude/sessions/{pid}.json` — if no match, skip (do NOT generate fake IDs)
3. Finds corresponding JSONL files in `~/.claude/projects/` by scanning all project directories for `{session-id}.jsonl`
4. Parses JSONL for model, tokens, status, subagents, wakeups, etc.

### Session visibility in the picker
`discoverTmuxSessions` (server/server.go) filters `DiscoverSessions` results to only include sessions whose `TmuxSession` (the UUID) exists in the database. The picker groups sessions by `TmuxSession` (UUID) but displays `TmuxDisplayName` as the header.

### Pruning (`PruneDeadSessions` in db/queries.go, runs every ~5 seconds)
- Soft-deletes claude_sessions older than 10 min whose JSONL no longer exists
- Soft-deletes claude_sessions older than 10 min whose worktree no longer exists on disk (checks `worktree-state` record in the JSONL — `worktreeSession: null` means explicitly deleted)
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
- Claude agent name is read from the JSONL `agent-name` record on every poll, not cached in the DB
- Claude Code prunes session data after 30 days (`cleanupPeriodDays`)

### Reconciliation (`reconcileDBWithLive` in server/server.go)
- Adds new live sessions to the DB if not already tracked (only real Claude session IDs)
- Marks sessions active/inactive based on whether they're running in tmux
- Appends inactive/deleted DB sessions to the picker list so they remain visible
- Populates `TmuxDisplayName` on all sessions from the DB (single source for display names)
- Reads claude agent name from JSONL for each session (never from DB)

### Reactivation
- Before reactivating, checks that the JSONL still exists — returns "session no longer exists" if not
- Finds CWD from the JSONL, validates it exists
- Creates a new tmux session or window with `claude --resume <session-id>`

### Error handling
- `grecon new` surfaces errors via cobra's RunE (prints to stderr on exit)
- The picker TUI quits and prints errors to stderr when Enter fails (e.g., reactivation fails, session no longer exists)
- All server command handlers return structured errors via `CommandResponse{OK: false, Error: "..."}`

## Testing

174 tests across 3 packages using Afero `MemMapFs` for filesystem faking, a `CommandRunner` interface for tmux/process commands, real SQLite with `:memory:` for the database, and bubbletea model driving for TUI integration tests.

### Test infrastructure
- **`server/env.go`** — `Env` struct with `afero.Fs`, `CommandRunner` interface (Run/Output/RunWithStdin), injected `Clock`, and `Home` path. `RealEnv()` for production, test helpers for tests.
- **`db/testing.go`** — `OpenTestDB()` returns an in-memory SQLite with all migrations applied
- **`server/testutil_test.go`** — `fakeCmd` (records Run/Output/RunWithStdin calls), `testEnv()` helper, JSONL writing helpers
- **`client/ui_test.go`** and **`client/integration_test.go`** — drive the TUI by constructing `tuiModel`, sending `tea.KeyMsg`, and asserting on state and rendered view. No server or external dependencies needed.

### Running tests
```
go test ./... -timeout 30s
```

### Key test files
- `db/queries_test.go` — pruning, grace periods, worktree detection, soft deletes, JSONL existence
- `server/reconcile_test.go` — session addition, deduplication, active/inactive, display name propagation
- `server/cleanup_test.go` — pane killing for soft-deleted sessions
- `server/discover_test.go` — processPaneLines, determineStatus, debounceStatus, ValidateCWD, background tasks
- `server/parse_test.go` — parseJSONL token accumulation, model extraction, file size caching, line overflow
- `server/restore_test.go` — session restoration logic
- `server/summary_test.go` — activity extraction, tool descriptions, summary generation
- `server/network_test.go` — frame decoding, subscription lifecycle
- `server/commands_test.go` — command handler error paths, fixDefaultPath, reactivation sanity checks
- `server/filter_test.go` — discoverTmuxSessions DB filter
- `server/bg_test.go` — cleanupPendingCalls, isSpinner
- `server/serialize_test.go` — frame serialization
- `client/ui_test.go` — n key form switching, Esc return, filter guard
- `client/integration_test.go` — full TUI integration: session display, grouping, vim navigation (j/k/G/gg/M/ctrl-d/ctrl-u), bounds clamping, Enter/x/n/q keys, filter search, status display, empty list safety

## Code layout

- `main.go` — cobra command definitions
- `client/` — TUI (app.go, ui.go), session creation form (new_session.go), tmux helpers (tmux.go)
- `server/` — background server (server.go), session discovery (session.go), command handling (commands.go), session restore (restore.go), summary generation (summary.go), environment abstraction (env.go)
- `db/` — SQLite schema/migrations (migrate.go), all queries (queries.go), connection management (db.go), test helpers (testing.go)
