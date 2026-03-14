use std::fs;
use std::io;
use std::path::PathBuf;
use std::time::Duration;

use crossterm::{
    event::{self, Event, KeyCode},
    execute,
    terminal::{disable_raw_mode, enable_raw_mode, EnterAlternateScreen, LeaveAlternateScreen},
};
use ratatui::{
    layout::{Constraint, Layout},
    prelude::CrosstermBackend,
    style::{Color, Modifier, Style},
    text::{Line, Span},
    widgets::{Block, Borders, Cell, Paragraph, Row, Table},
    Terminal,
};
use serde::{Deserialize, Serialize};

use crate::model;
use crate::session::Session;

const MAX_ENTRIES: usize = 10;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ResumeEntry {
    pub session_id: String,
    pub name: String,
    pub cwd: String,
    pub model: Option<String>,
    pub tokens: u64,
    pub exited_at: String,
}

pub struct ResumeHistory;

impl ResumeHistory {
    fn path() -> Option<PathBuf> {
        Some(dirs::home_dir()?.join(".recon").join("history.json"))
    }

    pub fn load() -> Vec<ResumeEntry> {
        let path = match Self::path() {
            Some(p) => p,
            None => return vec![],
        };
        let content = match fs::read_to_string(&path) {
            Ok(c) => c,
            Err(_) => return vec![],
        };
        serde_json::from_str(&content).unwrap_or_default()
    }

    pub fn append(session: &Session) {
        let path = match Self::path() {
            Some(p) => p,
            None => return,
        };

        if let Some(parent) = path.parent() {
            let _ = fs::create_dir_all(parent);
        }

        let mut entries = Self::load();

        // Don't add duplicates
        if entries.iter().any(|e| e.session_id == session.session_id) {
            return;
        }

        let now = chrono::Utc::now().to_rfc3339();
        entries.push(ResumeEntry {
            session_id: session.session_id.clone(),
            name: session.tmux_session.clone().unwrap_or_default(),
            cwd: session.cwd.clone(),
            model: session.model.clone(),
            tokens: session.total_input_tokens + session.total_output_tokens,
            exited_at: now,
        });

        // Keep last N
        if entries.len() > MAX_ENTRIES {
            entries = entries.split_off(entries.len() - MAX_ENTRIES);
        }

        let _ = fs::write(&path, serde_json::to_string_pretty(&entries).unwrap_or_default());
    }
}

/// Interactive TUI picker for resuming a past session.
/// Returns Some((session_id, name)) if the user picks one, None if they cancel.
pub fn run_resume_picker() -> io::Result<Option<(String, String)>> {
    let entries = ResumeHistory::load();
    if entries.is_empty() {
        eprintln!("No resumable sessions found.");
        return Ok(None);
    }

    // Show most recent first
    let entries: Vec<ResumeEntry> = entries.into_iter().rev().collect();

    enable_raw_mode()?;
    let mut stdout = io::stdout();
    execute!(stdout, EnterAlternateScreen)?;
    let backend = CrosstermBackend::new(stdout);
    let mut terminal = Terminal::new(backend)?;

    let mut selected = 0usize;
    let result;

    loop {
        terminal.draw(|f| {
            let chunks = Layout::vertical([Constraint::Min(1), Constraint::Length(1)])
                .split(f.area());

            let header = Row::new(vec![
                Cell::from(" # "),
                Cell::from("Session"),
                Cell::from("Directory"),
                Cell::from("Model"),
                Cell::from("Tokens"),
                Cell::from("Exited"),
            ])
            .style(Style::default().fg(Color::Cyan).add_modifier(Modifier::BOLD));

            let rows: Vec<Row> = entries
                .iter()
                .enumerate()
                .map(|(i, e)| {
                    let model_display = e
                        .model
                        .as_deref()
                        .map(|m| model::display_name(m).to_string())
                        .unwrap_or_else(|| "—".to_string());

                    let tokens = format!("{}k", e.tokens / 1000);

                    let dir = shorten_home(&e.cwd);

                    let exited = format_relative(&e.exited_at);

                    let row = Row::new(vec![
                        Cell::from(format!(" {} ", i + 1)),
                        Cell::from(e.name.clone()),
                        Cell::from(dir).style(Style::default().fg(Color::DarkGray)),
                        Cell::from(model_display),
                        Cell::from(tokens),
                        Cell::from(exited),
                    ]);

                    if i == selected {
                        row.style(Style::default().bg(Color::DarkGray))
                    } else {
                        row
                    }
                })
                .collect();

            let widths = [
                Constraint::Length(4),
                Constraint::Length(20),
                Constraint::Min(20),
                Constraint::Length(16),
                Constraint::Length(10),
                Constraint::Length(14),
            ];

            let table = Table::new(rows, widths).header(header).block(
                Block::default()
                    .borders(Borders::ALL)
                    .title(" Resume Session "),
            );
            f.render_widget(table, chunks[0]);

            let footer = Paragraph::new(Line::from(vec![
                Span::styled("j/k", Style::default().fg(Color::Cyan)),
                Span::raw(" navigate  "),
                Span::styled("Enter", Style::default().fg(Color::Cyan)),
                Span::raw(" resume  "),
                Span::styled("q/Esc", Style::default().fg(Color::Cyan)),
                Span::raw(" cancel"),
            ]));
            f.render_widget(footer, chunks[1]);
        })?;

        if event::poll(Duration::from_millis(200))? {
            if let Event::Key(key) = event::read()? {
                match key.code {
                    KeyCode::Char('q') | KeyCode::Esc => {
                        result = None;
                        break;
                    }
                    KeyCode::Char('j') | KeyCode::Down => {
                        if selected + 1 < entries.len() {
                            selected += 1;
                        }
                    }
                    KeyCode::Char('k') | KeyCode::Up => {
                        if selected > 0 {
                            selected -= 1;
                        }
                    }
                    KeyCode::Enter => {
                        let entry = &entries[selected];
                        result = Some((entry.session_id.clone(), entry.name.clone()));
                        break;
                    }
                    _ => {}
                }
            }
        }
    }

    disable_raw_mode()?;
    execute!(terminal.backend_mut(), LeaveAlternateScreen)?;
    terminal.show_cursor()?;

    Ok(result)
}

fn shorten_home(path: &str) -> String {
    if let Some(home) = dirs::home_dir() {
        let home_str = home.to_string_lossy();
        if let Some(rest) = path.strip_prefix(home_str.as_ref()) {
            return format!("~{rest}");
        }
    }
    path.to_string()
}

fn format_relative(ts: &str) -> String {
    use chrono::{DateTime, Utc};
    let parsed = ts.parse::<DateTime<Utc>>();
    match parsed {
        Ok(dt) => {
            let diff = Utc::now() - dt;
            if diff.num_minutes() < 1 {
                "just now".to_string()
            } else if diff.num_minutes() < 60 {
                format!("{}m ago", diff.num_minutes())
            } else if diff.num_hours() < 24 {
                format!("{}h ago", diff.num_hours())
            } else {
                format!("{}d ago", diff.num_days())
            }
        }
        Err(_) => ts.to_string(),
    }
}
