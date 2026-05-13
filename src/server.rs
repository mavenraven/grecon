use std::collections::HashMap;
use std::io::{Read, Write};
use std::os::unix::net::{UnixListener, UnixStream};
use std::path::PathBuf;
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::{Duration, Instant};

use crate::session::{self, Session};

fn socket_path() -> PathBuf {
    dirs::home_dir()
        .unwrap_or_else(|| PathBuf::from("/tmp"))
        .join(".recon")
        .join("recon.sock")
}

fn serialize_sessions(sessions: &[Session]) -> Vec<u8> {
    let bytes = serde_json::to_vec(sessions).unwrap_or_default();
    let len = (bytes.len() as u32).to_be_bytes();
    let mut buf = Vec::with_capacity(4 + bytes.len());
    buf.extend_from_slice(&len);
    buf.extend_from_slice(&bytes);
    buf
}

struct ServerState {
    prev_sessions: HashMap<String, Session>,
    cached_data: Vec<u8>,
    last_poll: Instant,
}

impl ServerState {
    fn poll(&mut self) {
        let sessions: Vec<Session> = session::discover_sessions(&self.prev_sessions)
            .into_iter()
            .filter(|s| s.tmux_session.is_some())
            .collect();
        self.prev_sessions = sessions
            .iter()
            .map(|s| (s.session_id.clone(), s.clone()))
            .collect();
        self.cached_data = serialize_sessions(&sessions);
        self.last_poll = Instant::now();
    }
}

pub fn run_server() {
    let path = socket_path();
    if let Some(parent) = path.parent() {
        let _ = std::fs::create_dir_all(parent);
    }
    let _ = std::fs::remove_file(&path);

    let mut state = ServerState {
        prev_sessions: HashMap::new(),
        cached_data: Vec::new(),
        last_poll: Instant::now(),
    };
    state.poll();
    let state = Arc::new(Mutex::new(state));

    let listener = match UnixListener::bind(&path) {
        Ok(l) => l,
        Err(e) => {
            eprintln!("Failed to bind {}: {e}", path.display());
            std::process::exit(1);
        }
    };

    eprintln!("recon server listening on {}", path.display());

    // Polling thread
    let state_clone = Arc::clone(&state);
    thread::spawn(move || {
        loop {
            thread::sleep(Duration::from_secs(2));
            state_clone.lock().unwrap().poll();
        }
    });

    // Accept connections — inline refresh if data is stale (OS throttled the timer)
    for stream in listener.incoming() {
        match stream {
            Ok(mut conn) => {
                let mut st = state.lock().unwrap();
                if st.last_poll.elapsed() > Duration::from_secs(3) {
                    st.poll();
                }
                let snapshot = st.cached_data.clone();
                drop(st);
                let _ = conn.write_all(&snapshot);
            }
            Err(_) => continue,
        }
    }
}

/// Try to get sessions from the server. Returns None if server is unavailable.
pub fn try_fetch() -> Option<Vec<Session>> {
    let path = socket_path();
    let mut conn = UnixStream::connect(&path).ok()?;
    conn.set_read_timeout(Some(Duration::from_millis(500))).ok()?;

    let mut len_buf = [0u8; 4];
    conn.read_exact(&mut len_buf).ok()?;
    let len = u32::from_be_bytes(len_buf) as usize;

    if len == 0 || len > 10_000_000 {
        return None;
    }

    let mut buf = vec![0u8; len];
    conn.read_exact(&mut buf).ok()?;
    serde_json::from_slice(&buf).ok()
}

/// Fetch sessions from the server, or exit with a message if unavailable.
pub fn require_fetch() -> Vec<Session> {
    match try_fetch() {
        Some(sessions) => sessions,
        None => {
            eprintln!("recon server is not running. Start it with: recon server");
            std::process::exit(1);
        }
    }
}
