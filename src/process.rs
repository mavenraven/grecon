use std::collections::HashMap;
use std::sync::Mutex;

#[derive(Debug, Clone)]
pub struct ClaudeProcess {
    pub pid: i32,
    pub tty: String,
    pub stat: String,
    pub session_id: Option<String>,
    pub cwd: Option<String>,
}

static CWD_CACHE: Mutex<Option<HashMap<i32, String>>> = Mutex::new(None);

fn get_cached_cwd(pid: i32) -> Option<String> {
    let cache = CWD_CACHE.lock().unwrap();
    cache.as_ref()?.get(&pid).cloned()
}

fn set_cached_cwd(pid: i32, cwd: String) {
    let mut cache = CWD_CACHE.lock().unwrap();
    if cache.is_none() {
        *cache = Some(HashMap::new());
    }
    cache.as_mut().unwrap().insert(pid, cwd);
}

/// Discover all running claude processes.
pub fn discover_claude_processes() -> Vec<ClaudeProcess> {
    let output = match std::process::Command::new("ps")
        .args(["-eo", "pid,tty,stat,args"])
        .output()
    {
        Ok(o) => o,
        Err(_) => return vec![],
    };

    let stdout = String::from_utf8_lossy(&output.stdout);
    let mut procs = Vec::new();

    for line in stdout.lines().skip(1) {
        let parts: Vec<&str> = line.split_whitespace().collect();
        if parts.len() < 4 {
            continue;
        }

        let args_joined = parts[3..].join(" ");

        if !is_claude_binary(&args_joined) {
            continue;
        }

        let pid: i32 = match parts[0].parse() {
            Ok(p) => p,
            Err(_) => continue,
        };
        let tty = parts[1].to_string();
        let stat = parts[2].to_string();

        let session_id = extract_session_id(&args_joined);
        let cwd = get_process_cwd(pid);

        procs.push(ClaudeProcess {
            pid,
            tty,
            stat,
            session_id,
            cwd,
        });
    }

    procs.sort_by(|a, b| a.tty.cmp(&b.tty));
    procs
}

fn is_claude_binary(args: &str) -> bool {
    if args.contains("node_modules/.bin/claude") {
        return true;
    }
    let first_arg = args.split_whitespace().next().unwrap_or("");
    if first_arg.ends_with("/claude") || first_arg == "claude" {
        return !first_arg.contains("claude-");
    }
    false
}

fn extract_session_id(args: &str) -> Option<String> {
    let parts: Vec<&str> = args.split_whitespace().collect();
    for i in 0..parts.len().saturating_sub(1) {
        if parts[i] == "--resume" || parts[i] == "-r" {
            return Some(parts[i + 1].to_string());
        }
    }
    None
}

/// Get the CWD of a process via lsof, with caching.
fn get_process_cwd(pid: i32) -> Option<String> {
    if let Some(cwd) = get_cached_cwd(pid) {
        return Some(cwd);
    }

    let output = std::process::Command::new("lsof")
        .args(["-a", "-p", &pid.to_string(), "-d", "cwd", "-Fn"])
        .output()
        .ok()?;

    let stdout = String::from_utf8_lossy(&output.stdout);
    for line in stdout.lines() {
        if let Some(path) = line.strip_prefix('n') {
            if path.starts_with('/') {
                set_cached_cwd(pid, path.to_string());
                return Some(path.to_string());
            }
        }
    }
    None
}
