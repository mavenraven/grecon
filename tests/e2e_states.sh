#!/usr/bin/env bash
set -euo pipefail

RECON="$(cd "$(dirname "$0")/.." && pwd)/target/debug/recon"
PASS=0
FAIL=0
TOTAL=6

# Random 4-char ID to avoid collisions with real sessions
RID=$(head -c 100 /dev/urandom | LC_ALL=C tr -dc 'a-z0-9' | head -c 4)
S_NEW="e2e-${RID}-new"
S_INPUT="e2e-${RID}-input"
S_TWIN="e2e-${RID}-twin"
TMPDIR_NEW="/tmp/recon-e2e-${RID}"
TMPDIR_INPUT="/tmp/recon-e2e-${RID}-input"
TMPFILE="/tmp/recon-e2e-${RID}-testfile.txt"

echo "Test run ID: $RID"

# --- Cleanup ---
cleanup() {
    # Kill all tmux sessions with our random prefix
    tmux list-sessions -F '#{session_name}' 2>/dev/null \
        | grep "^e2e-${RID}-" \
        | while read -r s; do tmux kill-session -t "$s" 2>/dev/null || true; done
    rm -rf "$TMPDIR_NEW" "$TMPDIR_INPUT" "$TMPFILE"
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
    tmux new-session -d -s "$name" -c "$cwd" "$(which claude)"
}

send_to_session() {
    local name="$1" text="$2"
    tmux send-keys -t "$name" "$text" Enter
}

get_state() {
    local name="$1"
    "$RECON" --json 2>/dev/null | jq -r \
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
send_to_session "$S_NEW" "write a 200 word essay about the history of unix"

if wait_for_state "$S_NEW" "Working" 15; then
    report pass "Working state detected for $S_NEW"
else
    report fail "Working state detected for $S_NEW"
fi

# --- Test 3: Idle state ---
# After the essay response finishes, claude should return to idle
if wait_for_state "$S_NEW" "Idle" 30; then
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
    json=$("$RECON" --json 2>/dev/null)
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
json=$("$RECON" --json 2>/dev/null)
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

# --- Summary ---
echo ""
if (( FAIL == 0 )); then
    echo "All $TOTAL tests passed."
else
    echo "$PASS/$TOTAL tests passed, $FAIL failed."
    exit 1
fi
