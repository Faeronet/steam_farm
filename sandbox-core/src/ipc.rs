use serde::{Deserialize, Serialize};
use std::io::Write;

#[derive(Debug, Serialize)]
#[serde(tag = "event")]
pub enum IpcEvent {
    #[serde(rename = "started")]
    Started { pid: u32, vnc_port: u16 },
    #[serde(rename = "steam_ready")]
    SteamReady,
    #[serde(rename = "game_running")]
    GameRunning { app_id: u32 },
    #[serde(rename = "stats")]
    Stats { cpu: f64, memory_mb: u64 },
    #[serde(rename = "exited")]
    Exited { code: i32 },
    #[serde(rename = "error")]
    Error { message: String },
} 

#[derive(Debug, Deserialize, Clone)]
pub struct LaunchConfig {
    pub id: u64,
    pub game: String,
    pub username: String,
    pub password: String,
    pub vnc_port: u16,
    pub display: u16,
    pub launch_opts: Option<String>,
}

pub fn emit(event: &IpcEvent) {
    if let Ok(json) = serde_json::to_string(event) {
        let stdout = std::io::stdout();
        let mut handle = stdout.lock();
        let _ = writeln!(handle, "{}", json);
        let _ = handle.flush();
    }
}
