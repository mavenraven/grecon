use std::collections::HashMap;

use crossterm::event::{KeyCode, KeyEvent};

use crate::process;
use crate::session::{self, Session};
use crate::warp;

pub struct App {
    pub sessions: Vec<Session>,
    pub selected: usize,
    pub effort_level: String,
    pub should_quit: bool,
    prev_sessions: HashMap<String, Session>,
    /// Map from Warp tab title -> Cmd+N position (1-indexed).
    /// Probed once on startup.
    tab_map: Vec<(u8, String)>,
    /// Which tab recon is running in (to return to after probing).
    recon_tab: Option<u8>,
}

impl App {
    pub fn new() -> Self {
        let effort_level = read_effort_level().unwrap_or_else(|| "medium".to_string());
        App {
            sessions: Vec::new(),
            selected: 0,
            effort_level,
            should_quit: false,
            prev_sessions: HashMap::new(),
            tab_map: Vec::new(),
            recon_tab: None,
        }
    }

    /// Probe Warp tabs to build tab_title -> tab_number mapping.
    /// Call once on startup before entering the TUI event loop.
    pub fn probe_tabs(&mut self) {
        // First, figure out which tab we're in now by reading window title
        let current_title = read_warp_window_title();

        // Probe all tabs
        // We'll return to tab 1 initially, then find our tab after probing
        self.tab_map = warp::probe_tab_titles(1);

        // Find which tab has our title so we can return to it
        if let Some(title) = &current_title {
            for (pos, t) in &self.tab_map {
                if t == title {
                    self.recon_tab = Some(*pos);
                    warp::switch_to_tab_number(*pos);
                    break;
                }
            }
        }
    }

    pub fn refresh(&mut self) {
        let procs = process::discover_claude_processes();
        let mut sessions = session::resolve_sessions(&procs, &self.prev_sessions);

        self.prev_sessions = sessions
            .iter()
            .map(|s| (s.session_id.clone(), s.clone()))
            .collect();

        // Match sessions to probed tab titles.
        // Match by checking if the tab title contains the last path component of the CWD,
        // or if the CWD path ends with part of the tab title.
        for session in sessions.iter_mut() {
            for (pos, title) in &self.tab_map {
                if tab_matches_session(title, &session.project_name) {
                    session.tab_number = Some(*pos);
                    session.tab_title = Some(title.clone());
                    break;
                }
            }
        }

        self.sessions = sessions;

        if self.selected >= self.sessions.len() && !self.sessions.is_empty() {
            self.selected = self.sessions.len() - 1;
        }
    }

    pub fn handle_key(&mut self, key: KeyEvent) {
        match key.code {
            KeyCode::Char('q') | KeyCode::Esc => self.should_quit = true,
            KeyCode::Char('j') | KeyCode::Down => {
                if !self.sessions.is_empty() {
                    self.selected = (self.selected + 1).min(self.sessions.len() - 1);
                }
            }
            KeyCode::Char('k') | KeyCode::Up => {
                if self.selected > 0 {
                    self.selected -= 1;
                }
            }
            KeyCode::Enter => {
                if let Some(session) = self.sessions.get(self.selected) {
                    if let Some(tab) = session.tab_number {
                        warp::switch_to_tab_number(tab);
                    }
                }
            }
            KeyCode::Char('r') => {
                self.refresh();
            }
            KeyCode::Char(c) if c.is_ascii_digit() && c != '0' => {
                let n = c as u8 - b'0';
                warp::switch_to_tab_number(n);
            }
            _ => {}
        }
    }

    pub fn to_json(&self) -> String {
        let sessions: Vec<serde_json::Value> = self
            .sessions
            .iter()
            .enumerate()
            .map(|(i, s)| {
                serde_json::json!({
                    "index": i + 1,
                    "session_id": s.session_id,
                    "project_name": s.project_name,
                    "tab_title": s.tab_title,
                    "tab_number": s.tab_number,
                    "model": s.model,
                    "model_display": s.model_display(&self.effort_level),
                    "total_input_tokens": s.total_input_tokens,
                    "total_output_tokens": s.total_output_tokens,
                    "tokens_display": s.token_display(),
                    "token_ratio": s.token_ratio(),
                    "status": s.status.label(),
                    "pid": s.pid,
                    "tty": s.tty,
                    "last_activity": s.last_activity,
                })
            })
            .collect();

        serde_json::to_string_pretty(&serde_json::json!({
            "sessions": sessions,
            "effort_level": self.effort_level,
            "tab_map": self.tab_map.iter()
                .map(|(pos, title)| serde_json::json!({"position": pos, "title": title}))
                .collect::<Vec<_>>(),
        }))
        .unwrap_or_else(|_| "{}".to_string())
    }
}

/// Check if a Warp tab title matches a session's project name.
fn tab_matches_session(tab_title: &str, project_name: &str) -> bool {
    let title_lower = tab_title.to_lowercase();
    let name_lower = project_name.to_lowercase();

    // Exact match
    if title_lower == name_lower {
        return true;
    }

    // Direct containment either way
    if title_lower.contains(&name_lower) || name_lower.contains(&title_lower) {
        return true;
    }

    // Extract last path component from project_name (e.g., "~/repos/solo" -> "solo")
    let last_component = project_name
        .rsplit('/')
        .next()
        .unwrap_or(project_name)
        .to_lowercase();

    if title_lower.contains(&last_component) {
        return true;
    }

    // Split both into words/tokens and check for significant overlap.
    // "WEBRTC E2E" should match "optimal_webrtc_e2e"
    let title_words = tokenize(&title_lower);
    let name_words = tokenize(&name_lower);

    if !title_words.is_empty() {
        let matches = title_words
            .iter()
            .filter(|w| name_words.iter().any(|nw| nw.contains(w.as_str()) || w.contains(nw.as_str())))
            .count();
        // All title words must match something in the name
        if matches == title_words.len() {
            return true;
        }
    }

    false
}

/// Split a string into lowercase word tokens (split on spaces, underscores, hyphens, slashes, dots).
fn tokenize(s: &str) -> Vec<String> {
    s.split(|c: char| c == ' ' || c == '_' || c == '-' || c == '/' || c == '.')
        .filter(|w| !w.is_empty() && w.len() > 1) // skip single-char tokens like "~"
        .map(|w| w.to_string())
        .collect()
}

fn read_warp_window_title() -> Option<String> {
    let output = std::process::Command::new("osascript")
        .args([
            "-e",
            r#"tell application "System Events" to tell process "Warp" to name of window 1"#,
        ])
        .output()
        .ok()?;
    let title = String::from_utf8_lossy(&output.stdout).trim().to_string();
    if title.is_empty() {
        None
    } else {
        Some(title)
    }
}

fn read_effort_level() -> Option<String> {
    let home = dirs::home_dir()?;
    let path = home.join(".claude").join("settings.json");
    let content = std::fs::read_to_string(path).ok()?;
    let v: serde_json::Value = serde_json::from_str(&content).ok()?;
    v.get("effortLevel")?.as_str().map(|s| s.to_string())
}
