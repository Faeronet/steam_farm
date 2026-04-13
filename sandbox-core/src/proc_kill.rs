//! Kill processes whose environ contains DISPLAY=:N ( Steam client, CS2, etc. ).
use nix::sys::signal::{kill, Signal};
use nix::unistd::Pid;

pub fn signal_clients_on_display(display: u16, signal: Signal) {
    let prefix = format!("DISPLAY=:{}", display);
    let prefix_bytes = prefix.as_bytes();

    let Ok(entries) = std::fs::read_dir("/proc") else {
        return;
    };

    let mut targets: Vec<i32> = Vec::new();
    for ent in entries.flatten() {
        let name = ent.file_name();
        let Some(s) = name.to_str() else {
            continue;
        };
        let Ok(pid) = s.parse::<i32>() else {
            continue;
        };
        if pid <= 1 {
            continue;
        }

        let env_path = format!("/proc/{}/environ", pid);
        let Ok(data) = std::fs::read(&env_path) else {
            continue;
        };

        for chunk in data.split(|&b| b == 0) {
            if chunk.starts_with(prefix_bytes) {
                targets.push(pid);
                break;
            }
        }
    }

    for pid in targets {
        let _ = kill(Pid::from_raw(pid), signal);
    }
}
