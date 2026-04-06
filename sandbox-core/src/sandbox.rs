use std::fs;
use std::os::unix::fs as unix_fs;
use std::path::PathBuf;

use crate::ipc::{self, IpcEvent, LaunchConfig};
use crate::monitor;
use crate::process::ProcessSupervisor;
use crate::steam;

pub fn sandbox_dir(id: u64) -> PathBuf {
    // Use $HOME/snap/steam/common/sfarm-{id} if snap Steam is installed,
    // so the sandbox dir is accessible from inside the snap container.
    // Fall back to /tmp/sfarm-{id} otherwise.
    let home = std::env::var("HOME").unwrap_or_else(|_| "/tmp".into());
    let snap_path = PathBuf::from(format!("{}/snap/steam/common/sfarm-{}", home, id));
    if PathBuf::from(format!("{}/snap/steam/common", home)).is_dir() {
        snap_path
    } else {
        PathBuf::from(format!("/tmp/sfarm-{}", id))
    }
}

fn setup_dirs(cfg: &LaunchConfig, steam_paths: &steam::SteamPaths) -> Result<PathBuf, Box<dyn std::error::Error>> {
    let base = sandbox_dir(cfg.id);
    if base.exists() {
        fs::remove_dir_all(&base)?;
    }

    fs::create_dir_all(base.join("home/.local/share"))?;
    fs::create_dir_all(base.join("home/.steam"))?;
    fs::create_dir_all(base.join("xdg"))?;

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

    // Symlink the host Steam directory (for non-snap fallback path)
    let sandbox_steam = base.join("home/.local/share/Steam");
    unix_fs::symlink(&steam_paths.root, &sandbox_steam)?;
    unix_fs::symlink(&steam_paths.root, base.join("home/.steam/steam"))?;

    if steam_paths.linux64.exists() {
        unix_fs::symlink(&steam_paths.linux64, base.join("home/.steam/sdk64"))?;
    }
    if steam_paths.ubuntu12_32.exists() {
        unix_fs::symlink(&steam_paths.ubuntu12_32, base.join("home/.steam/sdk32"))?;
    }

    Ok(base)
}

fn cleanup(id: u64) {
    let _ = fs::remove_dir_all(sandbox_dir(id));
}

pub async fn run(cfg: LaunchConfig) -> Result<(), Box<dyn std::error::Error>> {
    let steam_paths = steam::find_steam().ok_or("Steam installation not found on this system")?;

    eprintln!(
        "[sandbox-{}] Steam root: {}, binary: {}, snap: {}",
        cfg.id,
        steam_paths.root.display(),
        steam_paths.steam_binary.display(),
        if PathBuf::from(format!("{}/snap/steam/common", std::env::var("HOME").unwrap_or_default())).is_dir() { "yes" } else { "no" },
    );

    let base = setup_dirs(&cfg, &steam_paths)?;

    // Write supervisor PID for external stop command
    fs::write(base.join("pid"), std::process::id().to_string())?;

    let mut supervisor = ProcessSupervisor::new(cfg.clone(), base, steam_paths);

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
    let pid_file = sandbox_dir(id).join("pid");
    match fs::read_to_string(&pid_file) {
        Ok(pid_str) => {
            if let Ok(pid) = pid_str.trim().parse::<i32>() {
                match nix::sys::signal::kill(
                    nix::unistd::Pid::from_raw(pid),
                    nix::sys::signal::Signal::SIGTERM,
                ) {
                    Ok(_) => eprintln!("Sent SIGTERM to sandbox {} (pid {})", id, pid),
                    Err(e) => eprintln!("Failed to signal sandbox {} (pid {}): {}", id, pid, e),
                }
            }
        }
        Err(_) => eprintln!("No PID file found for sandbox {}", id),
    }
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
