use std::collections::HashMap;
use std::fs;
use std::io::{BufRead, BufReader, Seek, SeekFrom};
use std::path::{Path, PathBuf};

use serde::Deserialize;

use crate::model;
use crate::process::ClaudeProcess;

#[derive(Debug, Clone, PartialEq)]
pub enum SessionStatus {
    Working,
    Idle,
    Input,
}

impl SessionStatus {
    pub fn label(&self) -> &str {
        match self {
            SessionStatus::Working => "Working",
            SessionStatus::Idle => "Idle",
            SessionStatus::Input => "Input",
        }
    }
}

#[derive(Debug, Clone)]
#[allow(dead_code)]
pub struct Session {
    pub session_id: String,
    pub project_name: String,
    pub tab_title: Option<String>,
    pub tab_number: Option<u8>,
    pub model: Option<String>,
    pub total_input_tokens: u64,
    pub total_output_tokens: u64,
    pub status: SessionStatus,
    pub pid: i32,
    pub tty: String,
    pub last_activity: Option<String>,
    pub jsonl_path: Option<PathBuf>,
    pub last_file_size: u64,
}

impl Session {
    pub fn token_display(&self) -> String {
        let used = self.total_input_tokens + self.total_output_tokens;
        let window = self
            .model
            .as_deref()
            .map(model::context_window)
            .unwrap_or(200_000);
        format!("{}k / {}k", used / 1000, window / 1000)
    }

    pub fn token_ratio(&self) -> f64 {
        let used = self.total_input_tokens + self.total_output_tokens;
        let window = self
            .model
            .as_deref()
            .map(model::context_window)
            .unwrap_or(200_000);
        if window == 0 {
            return 0.0;
        }
        used as f64 / window as f64
    }

    pub fn model_display(&self, effort: &str) -> String {
        match &self.model {
            Some(m) => model::format_with_effort(m, effort),
            None => "—".to_string(),
        }
    }
}

/// Resolve sessions from discovered processes.
pub fn resolve_sessions(
    processes: &[ClaudeProcess],
    prev_sessions: &HashMap<String, Session>,
) -> Vec<Session> {
    let claude_dir = match dirs::home_dir() {
        Some(h) => h.join(".claude").join("projects"),
        None => return vec![],
    };

    let mut sessions = Vec::new();

    for proc in processes {
        let cwd = match &proc.cwd {
            Some(c) => c.clone(),
            None => continue,
        };

        let encoded = encode_project_path(&cwd);
        let project_dir = claude_dir.join(&encoded);
        let project_name = shorten_path(&cwd);

        // Find the JSONL file
        let jsonl_path = find_jsonl(&project_dir, proc.session_id.as_deref());

        // Determine session_id
        let session_id = proc
            .session_id
            .clone()
            .or_else(|| {
                jsonl_path.as_ref().and_then(|p| {
                    p.file_stem().map(|s| s.to_string_lossy().to_string())
                })
            })
            .unwrap_or_else(|| format!("pid-{}", proc.pid));

        // Check if we have a previous session to do incremental parsing
        let prev = prev_sessions.get(&session_id);
        let prev_file_size = prev.map(|s| s.last_file_size).unwrap_or(0);
        let prev_input = prev.map(|s| s.total_input_tokens).unwrap_or(0);
        let prev_output = prev.map(|s| s.total_output_tokens).unwrap_or(0);
        let prev_model = prev.and_then(|s| s.model.clone());

        // Parse JSONL
        let (input_tokens, output_tokens, model_id, last_activity, file_size) =
            match &jsonl_path {
                Some(path) => parse_jsonl(path, prev_file_size, prev_input, prev_output, prev_model),
                None => (0, 0, None, None, 0),
            };

        // Determine status from ps stat
        let status = determine_status(&proc.stat, &jsonl_path, file_size);

        sessions.push(Session {
            session_id,
            project_name,
            tab_title: None,
            tab_number: None,
            model: model_id,
            total_input_tokens: input_tokens,
            total_output_tokens: output_tokens,
            status,
            pid: proc.pid,
            tty: proc.tty.clone(),
            last_activity,
            jsonl_path,
            last_file_size: file_size,
        });
    }

    sessions
}

/// Shorten a path by replacing the home directory with ~.
fn shorten_path(path: &str) -> String {
    if let Some(home) = dirs::home_dir() {
        let home_str = home.to_string_lossy();
        if let Some(rest) = path.strip_prefix(home_str.as_ref()) {
            return format!("~{rest}");
        }
    }
    path.to_string()
}

/// Encode a CWD path the same way Claude does for project directories.
/// `/Users/gavra/repos/yaba` -> `-Users-gavra-repos-yaba`
/// Also replaces `.` (removed) and `_` (to `-`).
fn encode_project_path(path: &str) -> String {
    path.replace('/', "-").replace('.', "-").replace('_', "-")
}

/// Find the best matching JSONL file in a project directory.
fn find_jsonl(project_dir: &Path, session_id: Option<&str>) -> Option<PathBuf> {
    if !project_dir.exists() {
        return None;
    }

    // If we have a session ID, look for exact match
    if let Some(id) = session_id {
        let direct = project_dir.join(format!("{id}.jsonl"));
        if direct.exists() {
            return Some(direct);
        }
        // Also check in subdirectories (session_id/session_id.jsonl pattern doesn't exist,
        // but the file is directly in the project dir)
    }

    // Otherwise find the most recently modified JSONL
    let mut best: Option<(PathBuf, std::time::SystemTime)> = None;
    if let Ok(entries) = fs::read_dir(project_dir) {
        for entry in entries.flatten() {
            let path = entry.path();
            if path.extension().map(|e| e == "jsonl").unwrap_or(false) {
                if let Ok(meta) = path.metadata() {
                    if let Ok(modified) = meta.modified() {
                        if best.as_ref().map(|(_, t)| modified > *t).unwrap_or(true) {
                            best = Some((path, modified));
                        }
                    }
                }
            }
        }
    }

    best.map(|(p, _)| p)
}

/// Minimal serde structs for JSONL parsing.
#[derive(Deserialize)]
struct JsonlEntry {
    #[serde(default)]
    r#type: String,
    #[serde(default)]
    subtype: Option<String>,
    #[serde(default)]
    message: Option<MessageEntry>,
    #[serde(default)]
    timestamp: Option<String>,
}

#[derive(Deserialize)]
struct MessageEntry {
    #[serde(default)]
    model: Option<String>,
    #[serde(default)]
    usage: Option<UsageEntry>,
}

#[derive(Deserialize)]
struct UsageEntry {
    #[serde(default)]
    input_tokens: u64,
    #[serde(default)]
    output_tokens: u64,
    #[serde(default)]
    cache_creation_input_tokens: u64,
    #[serde(default)]
    cache_read_input_tokens: u64,
}

/// Parse JSONL file, incrementally if possible.
/// Returns (total_input, total_output, model, last_activity, file_size).
fn parse_jsonl(
    path: &Path,
    prev_file_size: u64,
    prev_input: u64,
    prev_output: u64,
    prev_model: Option<String>,
) -> (u64, u64, Option<String>, Option<String>, u64) {
    let file = match fs::File::open(path) {
        Ok(f) => f,
        Err(_) => return (prev_input, prev_output, prev_model, None, 0),
    };

    let file_size = file.metadata().map(|m| m.len()).unwrap_or(0);

    // If file hasn't changed, return previous values
    if file_size == prev_file_size && prev_file_size > 0 {
        return (prev_input, prev_output, prev_model, None, file_size);
    }

    let mut reader = BufReader::new(file);
    let mut total_input = prev_input;
    let mut total_output = prev_output;
    let mut model = prev_model;
    let mut last_activity = None;
    let mut last_type = String::new();
    let mut last_subtype: Option<String> = None;

    // Seek to where we left off for incremental reads
    if prev_file_size > 0 {
        let _ = reader.seek(SeekFrom::Start(prev_file_size));
    } else {
        total_input = 0;
        total_output = 0;
        model = None;
    }

    let mut line = String::new();
    loop {
        line.clear();
        match reader.read_line(&mut line) {
            Ok(0) => break,
            Ok(_) => {}
            Err(_) => break,
        }

        let trimmed = line.trim();
        if trimmed.is_empty() {
            continue;
        }

        // Quick check: only parse lines that might have useful data
        if !trimmed.contains("\"type\"") {
            continue;
        }

        // Track type for status detection
        if trimmed.contains("\"type\":\"assistant\"") {
            if let Ok(entry) = serde_json::from_str::<JsonlEntry>(trimmed) {
                last_type = entry.r#type;
                last_subtype = entry.subtype;
                if let Some(ts) = entry.timestamp {
                    last_activity = Some(ts);
                }
                if let Some(msg) = entry.message {
                    if let Some(m) = msg.model {
                        model = Some(m);
                    }
                    if let Some(usage) = msg.usage {
                        total_input += usage.input_tokens
                            + usage.cache_creation_input_tokens
                            + usage.cache_read_input_tokens;
                        total_output += usage.output_tokens;
                    }
                }
            }
        } else if trimmed.contains("\"type\":\"user\"") || trimmed.contains("\"type\":\"system\"") {
            if let Ok(entry) = serde_json::from_str::<JsonlEntry>(trimmed) {
                last_type = entry.r#type;
                last_subtype = entry.subtype;
                if let Some(ts) = entry.timestamp {
                    last_activity = Some(ts);
                }
            }
        }
    }

    // Store last_type/last_subtype info in last_activity for status detection
    // We'll encode it as a suffix — hacky but avoids changing the return type
    let _ = (last_type, last_subtype); // used in determine_status via separate path

    (total_input, total_output, model, last_activity, file_size)
}

/// Determine session status from process state.
fn determine_status(stat: &str, jsonl_path: &Option<PathBuf>, _file_size: u64) -> SessionStatus {
    // R+ means the process is actively running (Working)
    if stat.contains('R') {
        return SessionStatus::Working;
    }

    // S+ means sleeping — check if awaiting input
    if stat.contains('S') {
        // Check the last entry type in the JSONL
        if let Some(path) = jsonl_path {
            if let Some(last_type) = read_last_entry_type(path) {
                if last_type == "turn_duration" {
                    return SessionStatus::Input;
                }
            }
        }
    }

    SessionStatus::Idle
}

/// Read the last meaningful entry type from JSONL (reads from end).
fn read_last_entry_type(path: &Path) -> Option<String> {
    let content = fs::read_to_string(path).ok()?;
    // Read from the end to find the last entry
    for line in content.lines().rev() {
        let trimmed = line.trim();
        if trimmed.is_empty() {
            continue;
        }
        if trimmed.contains("\"subtype\":\"turn_duration\"") {
            return Some("turn_duration".to_string());
        }
        if trimmed.contains("\"type\":\"user\"") {
            return Some("user".to_string());
        }
        if trimmed.contains("\"type\":\"assistant\"") {
            return Some("assistant".to_string());
        }
        if trimmed.contains("\"type\":\"system\"") {
            // Could be turn_duration or something else
            if trimmed.contains("\"subtype\":\"turn_duration\"") {
                return Some("turn_duration".to_string());
            }
            return Some("system".to_string());
        }
    }
    None
}
