//! Kill processes whose environ contains DISPLAY=:N ( Steam client, CS2, etc. ).
use nix::sys::signal::{kill, Signal};
use nix::unistd::Pid;

const SFARM_DISPLAY_PREFIX: &[u8] = b"SFARM_DISPLAY=";

fn is_linuxsteamrt_game_cmdline(cmdline: &[u8]) -> bool {
    const CS2: &[u8] = b"linuxsteamrt64/cs2";
    const DOTA: &[u8] = b"linuxsteamrt64/dota2";
    cmdline.windows(CS2.len()).any(|w| w == CS2) || cmdline.windows(DOTA.len()).any(|w| w == DOTA)
}

/// Убивает cs2/dota2 **без** `SFARM_DISPLAY` в environ — обычно это игра на основном столе (`:0`),
/// из‑за которой Steam в песочнице показывает «Only one instance of the game can be running».
/// Процессы других песочниц (с `SFARM_DISPLAY=…`) не трогаем.
/// Отключить: `SFARM_KILL_HOST_CS2=0` (или `off` / `false`).
pub fn kill_host_linuxsteamrt_games_without_sfarm_display() {
    if std::env::var_os("SFARM_KILL_HOST_CS2")
        .map(|v| v == "0" || v == "off" || v == "false")
        .unwrap_or(false)
    {
        return;
    }

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

        let cmdline_path = format!("/proc/{}/cmdline", pid);
        let Ok(cmdline) = std::fs::read(&cmdline_path) else {
            continue;
        };
        if !is_linuxsteamrt_game_cmdline(&cmdline) {
            continue;
        }

        let env_path = format!("/proc/{}/environ", pid);
        let Ok(data) = std::fs::read(&env_path) else {
            continue;
        };
        let mut has_sfarm = false;
        for chunk in data.split(|&b| b == 0) {
            if chunk.starts_with(SFARM_DISPLAY_PREFIX) {
                has_sfarm = true;
                break;
            }
        }
        if !has_sfarm {
            targets.push(pid);
        }
    }

    for pid in targets {
        let _ = kill(Pid::from_raw(pid), Signal::SIGKILL);
    }
}

/// Убить «зависшие» cs2/dota2 только для этого X-слота (по DISPLAY / SFARM_DISPLAY в environ).
/// Делается на хосте в Rust: inner_script snap часто идёт через `/bin/sh` (dash), без bash-фич.
pub fn kill_stale_linuxsteamrt_games_on_display(display: u16) {
    let Ok(entries) = std::fs::read_dir("/proc") else {
        return;
    };

    let disp = format!("DISPLAY=:{}", display);
    let disp_b = disp.as_bytes();
    let sfarm = format!("SFARM_DISPLAY={}", display);
    let sfarm_b = sfarm.as_bytes();

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

        let cmdline_path = format!("/proc/{}/cmdline", pid);
        let Ok(cmdline) = std::fs::read(&cmdline_path) else {
            continue;
        };
        if !is_linuxsteamrt_game_cmdline(&cmdline) {
            continue;
        }

        let env_path = format!("/proc/{}/environ", pid);
        let Ok(data) = std::fs::read(&env_path) else {
            continue;
        };
        let mut ok = false;
        for chunk in data.split(|&b| b == 0) {
            if chunk.starts_with(disp_b) || chunk.starts_with(sfarm_b) {
                ok = true;
                break;
            }
        }
        if !ok {
            continue;
        }
        targets.push(pid);
    }

    for pid in targets {
        let _ = kill(Pid::from_raw(pid), Signal::SIGKILL);
    }
}

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
