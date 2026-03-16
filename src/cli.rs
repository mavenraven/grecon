use clap::{Parser, Subcommand};

/// Monitor and manage Claude Code sessions running in tmux
#[derive(Parser)]
#[command(name = "recon", version)]
pub struct Cli {
    #[command(subcommand)]
    pub command: Option<Command>,
}

#[derive(Subcommand)]
pub enum Command {
    /// Open the visual (tamagotchi) dashboard
    View,
    /// Interactive form to create a new tmux session
    New,
    /// Create a new claude session in the current directory
    Launch {
        /// Print only the session name (no attach)
        #[arg(long)]
        name_only: bool,
    },
    /// Jump directly to the next agent waiting for input
    Next,
    /// Resume a past session (interactive picker, or by ID)
    Resume {
        /// Session ID to resume directly (skips the picker)
        #[arg(long)]
        id: Option<String>,
        /// Custom tmux session name
        #[arg(long)]
        name: Option<String>,
        /// Don't attach to the session after resuming
        #[arg(long)]
        no_attach: bool,
    },
    /// Print all session state as JSON
    Json,
    /// Save all live sessions to disk for restoring later
    Park,
    /// Restore previously parked sessions
    Unpark,
}
