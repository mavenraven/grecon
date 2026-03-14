use std::process::Command;

use crate::session;

/// Switch to a tmux session (inside tmux) or attach to it (outside tmux).
pub fn switch_to_session(name: &str) {
    let inside_tmux = std::env::var("TMUX").is_ok();
    if inside_tmux {
        let _ = Command::new("tmux")
            .args(["switch-client", "-t", name])
            .status();
    } else {
        let _ = Command::new("tmux")
            .args(["attach-session", "-t", name])
            .status();
    }
}

/// Launch claude in a new tmux session with the given name and working directory.
/// Returns the session name on success.
pub fn create_session(name: &str, cwd: &str) -> Result<String, String> {
    let base_name = sanitize_session_name(name);

    // Always create a new session — append -2, -3, etc. if name taken
    let session_name = if !session_exists(&base_name) {
        base_name.clone()
    } else {
        let mut n = 2;
        loop {
            let candidate = format!("{base_name}-{n}");
            if !session_exists(&candidate) {
                break candidate;
            }
            n += 1;
        }
    };

    let claude_path = which_claude().unwrap_or_else(|| "claude".to_string());
    let status = Command::new("tmux")
        .args([
            "new-session",
            "-d",
            "-s",
            &session_name,
            "-c",
            cwd,
            &claude_path,
        ])
        .status()
        .map_err(|e| format!("Failed to create tmux session: {e}"))?;

    if !status.success() {
        return Err("tmux new-session failed".to_string());
    }

    set_resume_on_exit_hook(&session_name);
    Ok(session_name)
}

/// Resume a claude session in a new tmux session.
pub fn resume_session(session_id: &str, name: Option<&str>) -> Result<String, String> {
    let tmux_name = name
        .map(|n| n.to_string())
        .unwrap_or_else(|| session_id[..6.min(session_id.len())].to_string());

    // Use the original session's cwd so we start in the right project directory.
    // Fall back to current directory if not found.
    let cwd = session::find_session_cwd(session_id)
        .or_else(|| std::env::current_dir().map(|p| p.to_string_lossy().to_string()).ok())
        .unwrap_or_else(|| ".".to_string());

    let base_name = sanitize_session_name(&tmux_name);
    let session_name = if !session_exists(&base_name) {
        base_name.clone()
    } else {
        let mut n = 2;
        loop {
            let candidate = format!("{base_name}-{n}");
            if !session_exists(&candidate) {
                break candidate;
            }
            n += 1;
        }
    };

    let claude_path = which_claude().unwrap_or_else(|| "claude".to_string());
    // Store the original session-id in the tmux session environment so recon can
    // find the right JSONL without parsing process command lines.
    let env_var = format!("RECON_RESUMED_FROM={session_id}");
    let status = Command::new("tmux")
        .args([
            "new-session",
            "-d",
            "-s",
            &session_name,
            "-c",
            &cwd,
            "-e",
            &env_var,
            &claude_path,
            "--resume",
            session_id,
        ])
        .status()
        .map_err(|e| format!("Failed to create tmux session: {e}"))?;

    if !status.success() {
        return Err("tmux new-session failed".to_string());
    }

    set_resume_on_exit_hook(&session_name);
    Ok(session_name)
}

/// Get default session name and cwd for a new session.
pub fn default_new_session_info() -> (String, String) {
    let cwd = std::env::current_dir()
        .map(|p| p.to_string_lossy().to_string())
        .unwrap_or_else(|_| ".".to_string());

    let name = std::path::Path::new(&cwd)
        .file_name()
        .map(|n| n.to_string_lossy().to_string())
        .unwrap_or_else(|| "claude".to_string());

    (name, cwd)
}

fn session_exists(name: &str) -> bool {
    Command::new("tmux")
        .args(["has-session", "-t", name])
        .output()
        .map(|o| o.status.success())
        .unwrap_or(false)
}

fn which_claude() -> Option<String> {
    let output = Command::new("which").arg("claude").output().ok()?;
    let path = String::from_utf8_lossy(&output.stdout).trim().to_string();
    if path.is_empty() { None } else { Some(path) }
}

/// Sanitize a string for use as a tmux session name (no dots or colons).
fn sanitize_session_name(name: &str) -> String {
    name.replace('.', "-").replace(':', "-")
}

/// When the pane (claude process) exits, read the session-id from
/// ~/.claude/sessions/<PID>.json and display "recon --resume <id>" in the
/// tmux status bar of whichever session the user lands on next.
fn set_resume_on_exit_hook(session_name: &str) {
    // #{pane_pid} is expanded by tmux when the hook fires.
    let hook_cmd = "run-shell '\
        SID=$(jq -r .sessionId ~/.claude/sessions/#{pane_pid}.json 2>/dev/null); \
        [ -n \"$SID\" ] && tmux display-message -d 0 \"recon --resume $SID\"\
    '";
    let _ = Command::new("tmux")
        .args(["set-hook", "-t", session_name, "pane-exited", hook_cmd])
        .status();
}

