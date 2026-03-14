mod app;
mod history;
mod model;
mod new_session;
mod session;
mod tmux;
mod ui;

use std::io;
use std::time::{Duration, Instant};

use crossterm::{
    event::{self, Event},
    execute,
    terminal::{disable_raw_mode, enable_raw_mode, EnterAlternateScreen, LeaveAlternateScreen},
};
use ratatui::prelude::CrosstermBackend;
use ratatui::Terminal;

use app::App;

fn main() -> io::Result<()> {
    let args: Vec<String> = std::env::args().collect();
    let cmd = args.get(1).map(|s| s.as_str());

    match cmd {
        Some("new") => {
            let result = new_session::run_new_session_form()?;
            if let Some(name) = result {
                tmux::switch_to_session(&name);
            }
            return Ok(());
        }
        Some("launch") => {
            let name_only = args.iter().any(|a| a == "--name-only");
            let (default_name, cwd) = tmux::default_new_session_info();
            match tmux::create_session(&default_name, &cwd) {
                Ok(name) => {
                    if name_only {
                        print!("{name}");
                    } else {
                        tmux::switch_to_session(&name);
                        eprintln!("Session: {name}");
                    }
                }
                Err(e) => {
                    eprintln!("Error: {e}");
                    std::process::exit(1);
                }
            }
            return Ok(());
        }
        Some("resume") => {
            // Interactive resume picker from history
            let result = history::run_resume_picker()?;
            if let Some((session_id, name)) = result {
                match tmux::resume_session(&session_id, Some(&name)) {
                    Ok(sess) => {
                        tmux::switch_to_session(&sess);
                        eprintln!("Resumed in session: {sess}");
                    }
                    Err(e) => {
                        eprintln!("Error: {e}");
                        std::process::exit(1);
                    }
                }
            }
            return Ok(());
        }
        Some("--resume") => {
            let session_id = match args.get(2) {
                Some(id) => id,
                None => {
                    eprintln!("Usage: recon --resume <session-id> [--name <name>]");
                    std::process::exit(1);
                }
            };
            let name = args.iter().position(|a| a == "--name")
                .and_then(|i| args.get(i + 1))
                .map(|s| s.as_str());
            match tmux::resume_session(session_id, name) {
                Ok(sess) => {
                    tmux::switch_to_session(&sess);
                    eprintln!("Resumed in session: {sess}");
                }
                Err(e) => {
                    eprintln!("Error: {e}");
                    std::process::exit(1);
                }
            }
            return Ok(());
        }
        Some("--json") => {
            let mut app = App::new();
            app.refresh();
            println!("{}", app.to_json());
            return Ok(());
        }
        _ => {}
    }

    // Default: TUI dashboard
    enable_raw_mode()?;
    let mut stdout = io::stdout();
    execute!(stdout, EnterAlternateScreen)?;
    let backend = CrosstermBackend::new(stdout);
    let mut terminal = Terminal::new(backend)?;

    let result = run_app(&mut terminal);

    disable_raw_mode()?;
    execute!(terminal.backend_mut(), LeaveAlternateScreen)?;
    terminal.show_cursor()?;

    if let Err(e) = result {
        eprintln!("Error: {e}");
    }

    Ok(())
}

fn run_app(terminal: &mut Terminal<CrosstermBackend<io::Stdout>>) -> io::Result<()> {
    let mut app = App::new();
    app.refresh();

    let refresh_interval = Duration::from_secs(2);
    let mut last_refresh = Instant::now();

    loop {
        terminal.draw(|f| ui::render(f, &app))?;

        if event::poll(Duration::from_millis(200))? {
            if let Event::Key(key) = event::read()? {
                app.handle_key(key);
            }
        }

        if app.should_quit {
            return Ok(());
        }

        if last_refresh.elapsed() >= refresh_interval {
            app.refresh();
            last_refresh = Instant::now();
        }
    }
}
