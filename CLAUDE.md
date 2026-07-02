# grecon

## Design philosophy

Grecon is a **live session picker** for Claude Code running in tmux. That's it.

- It shows what's currently in memory. It doesn't care about anything else.
- No session creation — use `claude` directly, or build a launcher as a separate project.
- No session persistence/restore — use tmux-resurrect for that.
- No persistent state — no SQLite, no files on disk, no resume cache.
- Claude session name comes from the JSONL (`agent-name` / `custom-title`). If absent, show `-`.
- Don't build features that require grecon to own state it doesn't control.
- This is a Claude Code-centric tmux picker. If something already exists (tmux-resurrect, launchers), use that — don't reimplement it.

## After pushing changes

Always kill the running `grecon server`, reinstall the binary (`go install .`), and restart the server (`grecon server &`) after pushing code changes.
