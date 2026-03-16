#!/usr/bin/env bash
set -euo pipefail

RECON="$(cd "$(dirname "$0")/.." && pwd)/target/debug/recon"
PASS=0
FAIL=0
TOTAL=8

# Random 4-char ID to avoid collisions with real sessions
RID=$(head -c 100 /dev/urandom | LC_ALL=C tr -dc 'a-z0-9' | head -c 4)
S_NEW="e2e-${RID}-new"
S_INPUT="e2e-${RID}-input"
S_TWIN="e2e-${RID}-twin"
S_RESUME_ORIG="e2e-${RID}-res-orig"
S_RESUME_NEW="e2e-${RID}-res-new"
TMPDIR_NEW="/tmp/recon-e2e-${RID}"
TMPDIR_INPUT="/tmp/recon-e2e-${RID}-input"
TMPDIR_RESUME="/tmp/recon-e2e-${RID}-resume"
TMPFILE="/tmp/recon-e2e-${RID}-testfile.txt"

CLAUDE_MODEL="${CLAUDE_MODEL:-haiku}"
CLAUDE_EFFORT="${CLAUDE_EFFORT:-low}"
CLAUDE_FLAGS="--model $CLAUDE_MODEL --effort $CLAUDE_EFFORT"

echo "Test run ID: $RID (model=$CLAUDE_MODEL, effort=$CLAUDE_EFFORT)"

# --- Cleanup ---
cleanup() {
    # Kill all tmux sessions with our random prefix
    tmux list-sessions -F '#{session_name}' 2>/dev/null \
        | grep "^e2e-${RID}-" \
        | while read -r s; do tmux kill-session -t "$s" 2>/dev/null || true; done
    rm -rf "$TMPDIR_NEW" "$TMPDIR_INPUT" "$TMPDIR_RESUME" "$TMPFILE"
}
trap cleanup EXIT

# --- Preflight ---
if ! command -v jq &>/dev/null; then
    echo "FATAL: jq is required but not found"
    exit 1
fi

if ! command -v claude &>/dev/null; then
    echo "FATAL: claude CLI is required but not found"
    exit 1
fi

if [[ ! -x "$RECON" ]]; then
    echo "Building recon..."
    (cd "$(dirname "$0")/.." && cargo build --quiet)
fi

# Make sure tmux server is running
tmux start-server 2>/dev/null || true

# --- Helpers ---

create_session() {
    local name="$1" cwd="$2"
    mkdir -p "$cwd"
    tmux new-session -d -s "$name" -c "$cwd" "$(which claude) $CLAUDE_FLAGS"
}

send_to_session() {
    local name="$1" text="$2"
    tmux send-keys -t "$name" "$text" Enter
}

get_state() {
    local name="$1"
    "$RECON" json 2>/dev/null | jq -r \
        --arg name "$name" \
        '.sessions[] | select(.tmux_session == $name) | .status' \
    || echo ""
}

wait_for_state() {
    local name="$1" expected="$2" timeout="$3"
    local elapsed=0 state=""

    while (( elapsed < timeout )); do
        state="$(get_state "$name")"
        if [[ "$state" == "$expected" ]]; then
            return 0
        fi
        sleep 1
        (( elapsed++ )) || true
    done

    # Timeout — dump debug info
    echo "  TIMEOUT after ${timeout}s waiting for state '$expected' on session '$name'"
    echo "  Last seen state: '${state:-<not found>}'"
    echo "  Pane content:"
    tmux capture-pane -t "$name" -p -S -10 2>/dev/null | sed 's/^/    /' || echo "    <capture failed>"
    return 1
}

report() {
    local result="$1" label="$2"
    if [[ "$result" == "pass" ]]; then
        echo "[PASS] $label"
        (( PASS++ )) || true
    else
        echo "[FAIL] $label"
        (( FAIL++ )) || true
    fi
}

# --- Test 1: New state ---
create_session "$S_NEW" "$TMPDIR_NEW"

if wait_for_state "$S_NEW" "New" 15; then
    report pass "New state detected for $S_NEW"
else
    report fail "New state detected for $S_NEW"
fi

# --- Test 2: Working state ---
# Any prompt triggers Working during streaming. Use one that takes a few seconds.
# Wait for the TUI to be fully ready for input (status bar shows "? for shortcuts")
sleep 3
send_to_session "$S_NEW" "write a 500 word essay about the history of unix"

if wait_for_state "$S_NEW" "Working" 15; then
    report pass "Working state detected for $S_NEW"
else
    report fail "Working state detected for $S_NEW"
fi

# --- Test 3: Idle state ---
# After the essay response finishes, claude should return to idle
if wait_for_state "$S_NEW" "Idle" 60; then
    report pass "Idle state detected for $S_NEW"
else
    report fail "Idle state detected for $S_NEW"
fi

# --- Test 4: Token stability (same CWD, two sessions) ---
# Create a second session in the SAME directory to verify tokens don't swap
create_session "$S_TWIN" "$TMPDIR_NEW"
wait_for_state "$S_TWIN" "New" 15 >/dev/null 2>&1 || true

# Send a different prompt to the twin so it gets different token counts
sleep 3
send_to_session "$S_TWIN" "say exactly: hello world"
wait_for_state "$S_TWIN" "Idle" 20 >/dev/null 2>&1 || true

# Now both sessions share the same CWD. Poll multiple times and check tokens are stable.
tokens_stable=true
prev_new="" prev_twin=""
for i in $(seq 1 6); do
    json=$("$RECON" json 2>/dev/null)
    cur_new=$(echo "$json" | jq -r --arg n "$S_NEW" '.sessions[] | select(.tmux_session == $n) | .total_input_tokens')
    cur_twin=$(echo "$json" | jq -r --arg n "$S_TWIN" '.sessions[] | select(.tmux_session == $n) | .total_input_tokens')
    if [[ -n "$prev_new" && ("$cur_new" != "$prev_new" || "$cur_twin" != "$prev_twin") ]]; then
        echo "  Token swap detected: $S_NEW went $prev_new→$cur_new, $S_TWIN went $prev_twin→$cur_twin"
        tokens_stable=false
        break
    fi
    prev_new="$cur_new"
    prev_twin="$cur_twin"
    sleep 1
done

if $tokens_stable && [[ -n "$prev_new" && -n "$prev_twin" && "$prev_new" != "$prev_twin" ]]; then
    report pass "Token stability: $S_NEW=$prev_new, $S_TWIN=$prev_twin (same CWD, no swap)"
else
    if ! $tokens_stable; then
        report fail "Token stability: values swapped between sessions sharing CWD"
    else
        report fail "Token stability: could not verify (new=$prev_new twin=$prev_twin)"
    fi
fi

# --- Test 5: Sort by creation time (newest first) ---
# $S_TWIN was created after $S_NEW — it should appear first in the output
json=$("$RECON" json 2>/dev/null)
idx_new=$(echo "$json" | jq -r --arg n "$S_NEW" '.sessions | to_entries[] | select(.value.tmux_session == $n) | .key')
idx_twin=$(echo "$json" | jq -r --arg n "$S_TWIN" '.sessions | to_entries[] | select(.value.tmux_session == $n) | .key')

if [[ -n "$idx_new" && -n "$idx_twin" ]] && (( idx_twin < idx_new )); then
    report pass "Sort order: $S_TWIN (idx=$idx_twin) before $S_NEW (idx=$idx_new) — newest first"
else
    report fail "Sort order: expected $S_TWIN before $S_NEW (got idx_twin=$idx_twin idx_new=$idx_new)"
fi

# --- Test 6: Input state (permission prompt) ---
create_session "$S_INPUT" "$TMPDIR_INPUT"

# Wait for it to start
wait_for_state "$S_INPUT" "New" 15 >/dev/null 2>&1 || true

sleep 3
send_to_session "$S_INPUT" "please create a new file at $TMPFILE with the text hello"

if wait_for_state "$S_INPUT" "Input" 30; then
    report pass "Input state detected for $S_INPUT"
else
    report fail "Input state detected for $S_INPUT"
fi

# --- Test 7: Resume session shows original token count ---
# Wraps claude in bash so the pane stays alive after exit, letting us read the
# "Resume this session with: claude --resume <id>" message.
CLAUDE_PATH="$(which claude)"
mkdir -p "$TMPDIR_RESUME"
tmux new-session -d -s "$S_RESUME_ORIG" -c "$TMPDIR_RESUME" \
    "bash -c '$CLAUDE_PATH $CLAUDE_FLAGS 2>&1; exec bash'"

wait_for_state "$S_RESUME_ORIG" "New" 15 >/dev/null 2>&1 || true
sleep 3

# Do some work to accumulate tokens
send_to_session "$S_RESUME_ORIG" "say exactly the words: recon resume test"
wait_for_state "$S_RESUME_ORIG" "Idle" 30 >/dev/null 2>&1 || true

TOKENS_BEFORE=$("$RECON" json 2>/dev/null | jq -r \
    --arg n "$S_RESUME_ORIG" \
    '.sessions[] | select(.tmux_session == $n) | .total_input_tokens')

# Exit claude — it prints "Resume this session with: claude --resume <id>"
send_to_session "$S_RESUME_ORIG" "exit"
sleep 4

# Parse the original session-id from the pane exit output
ORIG_SESSION_ID=$(tmux capture-pane -t "$S_RESUME_ORIG" -p -S -200 2>/dev/null \
    | grep -oE 'claude --resume [a-zA-Z0-9-]+' | tail -1 | awk '{print $NF}' || true)

if [[ -z "$ORIG_SESSION_ID" ]]; then
    echo "  Could not parse resume session-id. Pane content:"
    tmux capture-pane -t "$S_RESUME_ORIG" -p -S -10 2>/dev/null | sed 's/^/    /'
    report fail "Resume: could not parse session-id from exit message (tokens_before=$TOKENS_BEFORE)"
else
    echo "  Original session-id: $ORIG_SESSION_ID (tokens before exit: $TOKENS_BEFORE)"

    # Resume via recon --resume (no-attach: creates detached session, switch-client skips if inside tmux with no client)
    # Use --name to control the session name for lookup
    "$RECON" resume --id "$ORIG_SESSION_ID" --name "$S_RESUME_NEW" --no-attach 2>/dev/null || true

    # Wait for the session file to be written and recon to refresh
    sleep 8

    TOKENS_RESUMED=$("$RECON" json 2>/dev/null | jq -r \
        --arg n "$S_RESUME_NEW" \
        '.sessions[] | select(.tmux_session == $n) | .total_input_tokens')

    if [[ -n "$TOKENS_RESUMED" ]] && \
       [[ "$TOKENS_RESUMED" =~ ^[0-9]+$ ]] && \
       (( TOKENS_RESUMED > 0 )); then
        report pass "Resume: $S_RESUME_NEW shows ${TOKENS_RESUMED} tokens (original had ${TOKENS_BEFORE})"
    else
        echo "  Original tokens: $TOKENS_BEFORE, resumed tokens: '$TOKENS_RESUMED'"
        "$RECON" json 2>/dev/null | jq -r --arg n "$S_RESUME_NEW" \
            '.sessions[] | select(.tmux_session == $n)' | sed 's/^/    /'
        report fail "Resume: expected non-zero tokens for resumed session"
    fi
fi

# --- Test 8: Resume idempotency (no-op if already running) ---
# The resumed session from Test 7 ($S_RESUME_NEW) should still be live.
# Resuming it again should NOT create a new tmux session.
SESSIONS_BEFORE=$(tmux list-sessions -F '#{session_name}' 2>/dev/null | grep "^e2e-${RID}-" | wc -l | tr -d ' ')

RESUME_OUTPUT=$("$RECON" resume --id "$ORIG_SESSION_ID" --name "$S_RESUME_NEW" --no-attach 2>&1 || true)

SESSIONS_AFTER=$(tmux list-sessions -F '#{session_name}' 2>/dev/null | grep "^e2e-${RID}-" | wc -l | tr -d ' ')

if (( SESSIONS_BEFORE == SESSIONS_AFTER )); then
    report pass "Resume idempotency: no new session created (before=$SESSIONS_BEFORE, after=$SESSIONS_AFTER)"
else
    report fail "Resume idempotency: session count changed (before=$SESSIONS_BEFORE, after=$SESSIONS_AFTER)"
fi

# --- Summary ---
echo ""
if (( FAIL == 0 )); then
    echo "All $TOTAL tests passed."
else
    echo "$PASS/$TOTAL tests passed, $FAIL failed."
    exit 1
fi
