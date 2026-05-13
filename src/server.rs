use std::collections::HashMap;
use std::io::{Read, Write};
use std::os::unix::net::{UnixListener, UnixStream};
use std::path::PathBuf;
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::Duration;

use crate::session::{self, Session};

fn socket_path() -> PathBuf {
    dirs::home_dir()
        .unwrap_or_else(|| PathBuf::from("/tmp"))
        .join(".recon")
        .join("recon.sock")
}

pub fn run_server() {
    let path = socket_path();
    if let Some(parent) = path.parent() {
        let _ = std::fs::create_dir_all(parent);
    }
    let _ = std::fs::remove_file(&path);

    let listener = match UnixListener::bind(&path) {
        Ok(l) => l,
        Err(e) => {
            eprintln!("Failed to bind {}: {e}", path.display());
            std::process::exit(1);
        }
    };

    eprintln!("recon server listening on {}", path.display());

    let data: Arc<Mutex<Vec<u8>>> = Arc::new(Mutex::new(Vec::new()));

    // Polling thread
    let data_clone = Arc::clone(&data);
    thread::spawn(move || {
        let mut prev_sessions: HashMap<String, Session> = HashMap::new();
        loop {
            let sessions: Vec<Session> = session::discover_sessions(&prev_sessions)
                .into_iter()
                .filter(|s| s.tmux_session.is_some())
                .collect();

            prev_sessions = sessions
                .iter()
                .map(|s| (s.session_id.clone(), s.clone()))
                .collect();

            if let Ok(bytes) = serde_json::to_vec(&sessions) {
                let len = (bytes.len() as u32).to_be_bytes();
                let mut buf = Vec::with_capacity(4 + bytes.len());
                buf.extend_from_slice(&len);
                buf.extend_from_slice(&bytes);
                *data_clone.lock().unwrap() = buf;
            }

            thread::sleep(Duration::from_secs(2));
        }
    });

    // Accept connections
    for stream in listener.incoming() {
        match stream {
            Ok(mut conn) => {
                let snapshot = data.lock().unwrap().clone();
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
    conn.set_read_timeout(Some(Duration::from_millis(50))).ok()?;

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
