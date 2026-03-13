mod app;
mod model;
mod process;
mod session;
mod ui;
#[allow(dead_code)]
mod warp;

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
    let json_mode = std::env::args().any(|a| a == "--json");

    if json_mode {
        let mut app = App::new();
        app.probe_tabs();
        app.refresh();
        let output = app.to_json();
        println!("{output}");
        return Ok(());
    }

    // Check accessibility
    if !warp::is_accessibility_trusted() {
        eprintln!("WARNING: Not trusted for Accessibility.");
        eprintln!("  System Settings > Privacy & Security > Accessibility");
        eprintln!("  Add your terminal app.\n");
    }

    // Setup terminal
    enable_raw_mode()?;
    let mut stdout = io::stdout();
    execute!(stdout, EnterAlternateScreen)?;
    let backend = CrosstermBackend::new(stdout);
    let mut terminal = Terminal::new(backend)?;

    let result = run_app(&mut terminal);

    // Restore terminal
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
    app.probe_tabs();
    app.refresh();

    let refresh_interval = Duration::from_secs(2);
    let mut last_refresh = Instant::now();

    loop {
        terminal.draw(|f| ui::render(f, &app))?;

        // Poll for events with 200ms timeout
        if event::poll(Duration::from_millis(200))? {
            if let Event::Key(key) = event::read()? {
                app.handle_key(key);
            }
        }

        if app.should_quit {
            return Ok(());
        }

        // Periodic refresh
        if last_refresh.elapsed() >= refresh_interval {
            app.refresh();
            last_refresh = Instant::now();
        }
    }
}
