use std::fs;
use std::path::PathBuf;

use crate::ipc::{self, IpcEvent, LaunchConfig};
use crate::monitor;
use crate::process::ProcessSupervisor;
use crate::steam;

/// Каталог данных песочницы. Для snap-Steam берётся из того же `.../snap/steam/common`, что и
/// найденный `steam_paths.root` — иначе при запуске sfarm от root путь уезжает в `/root/snap/...`.
pub fn sandbox_dir(id: u64, steam_paths: &steam::SteamPaths) -> PathBuf {
    if let Some(common) = steam::snap_steam_common_dir(&steam_paths.root) {
        if common.is_dir() {
            return common.join(format!("sfarm-{}", id));
        }
    }
    let home = std::env::var("HOME").unwrap_or_else(|_| "/tmp".into());
    let snap_path = PathBuf::from(format!("{}/snap/steam/common/sfarm-{}", home, id));
    if PathBuf::from(format!("{}/snap/steam/common", home)).is_dir() {
        snap_path
    } else {
        PathBuf::from(format!("/tmp/sfarm-{}", id))
    }
}

fn resolve_sandbox_base(id: u64) -> PathBuf {
    match steam::find_steam() {
        Some(sp) => sandbox_dir(id, &sp),
        None => PathBuf::from(format!("/tmp/sfarm-{}", id)),
    }
}

fn setup_dirs(cfg: &LaunchConfig, steam_paths: &steam::SteamPaths) -> Result<PathBuf, Box<dyn std::error::Error>> {
    let base = sandbox_dir(cfg.id, steam_paths);
    if base.exists() {
        fs::remove_dir_all(&base)?;
    }

    fs::create_dir_all(base.join("home/.local/share"))?;
    fs::create_dir_all(base.join("home/.steam"))?;
    fs::create_dir_all(base.join("xdg"))?;

    // steam.sh / snap вызывают grep к user-dirs.dirs и лезут в Desktop — без этого root/snap падает на 2-й песочнице.
    fs::create_dir_all(base.join("home/.config"))?;
    fs::write(
        base.join("home/.config/user-dirs.dirs"),
        r#"# Written by sfarm-sandbox
XDG_DESKTOP_DIR="$HOME/Desktop"
XDG_DOWNLOAD_DIR="$HOME/Downloads"
XDG_TEMPLATES_DIR="$HOME/Templates"
XDG_PUBLICSHARE_DIR="$HOME/Public"
XDG_DOCUMENTS_DIR="$HOME/Documents"
XDG_MUSIC_DIR="$HOME/Music"
XDG_PICTURES_DIR="$HOME/Pictures"
XDG_VIDEOS_DIR="$HOME/Videos"
"#,
    )?;
    for d in ["Desktop", "Downloads", "Documents"] {
        fs::create_dir_all(base.join("home").join(d))?;
    }

    // Unique machine-id for HWID spoofing
    let machine_id = uuid::Uuid::new_v4().simple().to_string();
    fs::write(base.join("machine-id"), &machine_id)?;

    // Dummy zenity to bypass steam.sh's blocking error dialogs
    let fake_bin = base.join("bin");
    fs::create_dir_all(&fake_bin)?;
    fs::write(fake_bin.join("zenity"), "#!/bin/sh\nexit 0\n")?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        fs::set_permissions(fake_bin.join("zenity"), fs::Permissions::from_mode(0o755))?;
    }

    // Steam directory structure is created by the snap inner_script
    // which builds a shadow dir with a steamwebhelper wrapper.
    // For non-snap fallback, symlinks are set up in start_via_direct().

    Ok(base)
}

fn cleanup(id: u64) {
    let _ = fs::remove_dir_all(resolve_sandbox_base(id));
}

pub async fn run(cfg: LaunchConfig) -> Result<(), Box<dyn std::error::Error>> {
    let steam_paths = steam::find_steam().ok_or(
        "Steam installation not found: install & log in once; snap (Ubuntu Software) uses \
         ~/snap/steam/common/.local/share/Steam — set SFARM_STEAM_ROOT to that path if sfarm runs as root",
    )?;

    eprintln!(
        "[sandbox-{}] Steam root: {}, binary: {}, snap: {}",
        cfg.id,
        steam_paths.root.display(),
        steam_paths.steam_binary.display(),
        if steam::snap_steam_common_dir(&steam_paths.root).map(|p| p.is_dir()).unwrap_or(false) {
            "yes"
        } else {
            "no"
        },
    );

    let base = setup_dirs(&cfg, &steam_paths)?;

    // Write supervisor PID for external stop command
    fs::write(base.join("pid"), std::process::id().to_string())?;

    let mut supervisor = ProcessSupervisor::new(cfg.clone(), base.clone(), steam_paths);

    supervisor.start_xvfb().await?;
    supervisor.start_vnc().await?;

    ipc::emit(&IpcEvent::Started {
        pid: std::process::id(),
        vnc_port: cfg.vnc_port,
    });

    supervisor.start_game().await?;

    ipc::emit(&IpcEvent::GameRunning {
        app_id: steam::game_app_id(&cfg.game),
    });

    let game_pid = supervisor.game_pid();
    let monitor_handle = tokio::spawn(async move {
        monitor::run(game_pid).await;
    });

    // Watch Steam logs for debugging
    let log_base = base.join("home/.local/share/Steam/logs");
    let sandbox_id = cfg.id;
    tokio::spawn(async move {
        tokio::time::sleep(std::time::Duration::from_secs(3)).await;
        for log_name in ["bootstrap_log.txt", "connection_log.txt"] {
            let log_path = log_base.join(log_name);
            if log_path.exists() {
                if let Ok(content) = fs::read_to_string(&log_path) {
                    let lines: Vec<&str> = content.lines().collect();
                    let start = if lines.len() > 20 { lines.len() - 20 } else { 0 };
                    for line in &lines[start..] {
                        if !line.trim().is_empty() {
                            eprintln!("[steam-log-{}] [{}] {}", sandbox_id, log_name, line);
                        }
                    }
                }
            }
        }
    });

    // Wait for game to exit or termination signal
    let exit_code;
    let mut sigterm = tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())
        .expect("failed to install SIGTERM handler");

    tokio::select! {
        code = supervisor.wait() => {
            exit_code = code;
        }
        _ = tokio::signal::ctrl_c() => {
            eprintln!("[sandbox-{}] Received SIGINT, shutting down", cfg.id);
            exit_code = 130;
        }
        _ = sigterm.recv() => {
            eprintln!("[sandbox-{}] Received SIGTERM, shutting down", cfg.id);
            exit_code = 143;
        }
    }

    monitor_handle.abort();
    ipc::emit(&IpcEvent::Exited { code: exit_code });

    supervisor.shutdown().await;
    cleanup(cfg.id);

    Ok(())
}

pub fn stop(id: u64) {
    let pid_file = resolve_sandbox_base(id).join("pid");
    let pid: i32 = match fs::read_to_string(&pid_file) {
        Ok(pid_str) => match pid_str.trim().parse::<i32>() {
            Ok(p) => p,
            Err(_) => {
                eprintln!("Bad PID file for sandbox {}", id);
                return;
            }
        },
        Err(_) => {
            eprintln!("No PID file found for sandbox {}", id);
            return;
        }
    };

    match nix::sys::signal::kill(
        nix::unistd::Pid::from_raw(pid),
        nix::sys::signal::Signal::SIGTERM,
    ) {
        Ok(_) => eprintln!("Sent SIGTERM to sandbox {} (pid {})", id, pid),
        Err(e) => eprintln!("Failed to signal sandbox {} (pid {}): {}", id, pid, e),
    }

    // If the supervisor never reaches shutdown() (stuck I/O), force-kill after a grace period.
    std::thread::spawn(move || {
        std::thread::sleep(std::time::Duration::from_secs(20));
        if nix::sys::signal::kill(nix::unistd::Pid::from_raw(pid), None).is_ok() {
            let _ = nix::sys::signal::kill(
                nix::unistd::Pid::from_raw(pid),
                nix::sys::signal::Signal::SIGKILL,
            );
            eprintln!("SIGKILL sandbox supervisor {} (pid {}) — still alive after SIGTERM", id, pid);
        }
    });
}

pub fn status() {
    let mut entries = Vec::new();
    let Ok(dir) = fs::read_dir("/tmp") else {
        println!("[]");
        return;
    };

    for entry in dir.flatten() {
        let name = entry.file_name();
        let Some(name_str) = name.to_str() else { continue };
        let Some(id_str) = name_str.strip_prefix("sfarm-") else { continue };
        let Ok(id) = id_str.parse::<u64>() else { continue };

        let pid_file = entry.path().join("pid");
        let alive = fs::read_to_string(&pid_file)
            .ok()
            .and_then(|s| s.trim().parse::<i32>().ok())
            .map(|pid| {
                nix::sys::signal::kill(nix::unistd::Pid::from_raw(pid), None).is_ok()
            })
            .unwrap_or(false);

        entries.push(serde_json::json!({
            "id": id,
            "alive": alive,
        }));
    }

    println!("{}", serde_json::to_string(&entries).unwrap_or_else(|_| "[]".into()));
}
