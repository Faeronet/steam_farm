use std::path::PathBuf;
use std::process::Stdio;
use tokio::io::{AsyncBufReadExt, BufReader};
use tokio::process::{Child, Command};

use crate::ipc::LaunchConfig;
use crate::steam::{self, SteamPaths};

fn self_dir() -> PathBuf {
    std::env::current_exe()
        .ok()
        .and_then(|p| p.parent().map(|d| d.to_path_buf()))
        .unwrap_or_else(|| PathBuf::from("."))
}

fn find_bin(name: &str) -> String {
    let local = self_dir().join(name);
    if local.exists() {
        return local.display().to_string();
    }
    name.to_string()
}

fn lib_path() -> String {
    let local_lib = self_dir().join("lib");
    if local_lib.is_dir() {
        let existing = std::env::var("LD_LIBRARY_PATH").unwrap_or_default();
        if existing.is_empty() {
            local_lib.display().to_string()
        } else {
            format!("{}:{}", local_lib.display(), existing)
        }
    } else {
        std::env::var("LD_LIBRARY_PATH").unwrap_or_default()
    }
}

fn snap_steam_available() -> bool {
    std::process::Command::new("snap")
        .args(["info", "steam"])
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .status()
        .map(|s| s.success())
        .unwrap_or(false)
}

pub struct ProcessSupervisor {
    cfg: LaunchConfig,
    base: PathBuf,
    steam_paths: SteamPaths,
    xvfb: Option<Child>,
    vnc: Option<Child>,
    game: Option<Child>,
}

impl ProcessSupervisor {
    pub fn new(cfg: LaunchConfig, base: PathBuf, steam_paths: SteamPaths) -> Self {
        Self {
            cfg,
            base,
            steam_paths,
            xvfb: None,
            vnc: None,
            game: None,
        }
    }

    pub async fn start_xvfb(&mut self) -> Result<(), Box<dyn std::error::Error>> {
        let xvfb_bin = find_bin("Xvfb");
        let display_num = self.cfg.display;
        let display = format!(":{}", display_num);

        // Clean up stale X11 lock files from previous runs
        let lock_file = format!("/tmp/.X{}-lock", display_num);
        let socket_file = format!("/tmp/.X11-unix/X{}", display_num);
        let _ = std::fs::remove_file(&lock_file);
        let _ = std::fs::remove_file(&socket_file);

        let mut child = Command::new(&xvfb_bin)
            .args([&display, "-screen", "0", "640x480x24", "-ac", "-nolisten", "tcp"])
            .stdout(Stdio::null())
            .stderr(Stdio::piped())
            .spawn()
            .map_err(|e| format!("Failed to start Xvfb (tried '{}'): {}", xvfb_bin, e))?;

        let id = self.cfg.id;
        if let Some(stderr) = child.stderr.take() {
            tokio::spawn(async move {
                let reader = BufReader::new(stderr);
                let mut lines = reader.lines();
                while let Ok(Some(line)) = lines.next_line().await {
                    eprintln!("[xvfb-{}] {}", id, line);
                }
            });
        }

        self.xvfb = Some(child);

        // Wait for X11 socket to appear
        let socket_path = format!("/tmp/.X11-unix/X{}", self.cfg.display);
        for _ in 0..20 {
            if std::path::Path::new(&socket_path).exists() {
                eprintln!("[sandbox-{}] Xvfb display :{} ready", self.cfg.id, self.cfg.display);
                return Ok(());
            }
            tokio::time::sleep(std::time::Duration::from_millis(100)).await;
        }
        eprintln!("[sandbox-{}] WARNING: X11 socket {} not found after 2s", self.cfg.id, socket_path);
        Ok(())
    }

    pub async fn start_vnc(&mut self) -> Result<(), Box<dyn std::error::Error>> {
        let vnc_bin = find_bin("x11vnc");
        let display = format!(":{}", self.cfg.display);
        let port = self.cfg.vnc_port.to_string();
        let ld = lib_path();

        let mut child = Command::new(&vnc_bin)
            .args([
                "-display", &display,
                "-rfbport", &port,
                "-nopw", "-forever", "-shared",
                "-noxdamage",
            ])
            .env("LD_LIBRARY_PATH", &ld)
            .stdout(Stdio::null())
            .stderr(Stdio::piped())
            .spawn()
            .map_err(|e| format!("Failed to start x11vnc (tried '{}'): {}", vnc_bin, e))?;

        let id = self.cfg.id;
        let vnc_port = self.cfg.vnc_port;
        if let Some(stderr) = child.stderr.take() {
            tokio::spawn(async move {
                let reader = BufReader::new(stderr);
                let mut lines = reader.lines();
                while let Ok(Some(line)) = lines.next_line().await {
                    if line.contains("error") || line.contains("Error") || line.contains("PORT") || line.contains("listen") {
                        eprintln!("[vnc-{}] {}", id, line);
                    }
                }
            });
        }

        eprintln!("[sandbox-{}] x11vnc started on port {}", self.cfg.id, vnc_port);
        self.vnc = Some(child);
        Ok(())
    }

    fn build_steam_args(&self) -> Vec<String> {
        let app_id = steam::game_app_id(&self.cfg.game);
        let launch_opts = self.cfg.launch_opts.clone()
            .unwrap_or_else(|| steam::default_launch_opts(&self.cfg.game).to_string());

        let mut args = vec![
            "-skipinitialbootstrap".into(),
            "-nobootstrapperupdate".into(),
            "-noverifyfiles".into(),
            "-skipsystemcheck".into(),
            "-no-child-update-ui".into(),
            "-no-browser".into(),
            "-nochatui".into(),
            "-nofriendsui".into(),
            "-silent".into(),
            "-login".into(),
            self.cfg.username.clone(),
            self.cfg.password.clone(),
            "-applaunch".into(),
            app_id.to_string(),
        ];
        args.extend(launch_opts.split_whitespace().map(String::from));
        args
    }

    pub async fn start_game(&mut self) -> Result<(), Box<dyn std::error::Error>> {
        if snap_steam_available() {
            self.start_via_snap().await
        } else {
            self.start_via_direct().await
        }
    }

    async fn start_via_snap(&mut self) -> Result<(), Box<dyn std::error::Error>> {
        let display = format!(":{}", self.cfg.display);
        let steam_args = self.build_steam_args();
        let sandbox_home = &self.base.join("home");

        let args_str = steam_args.iter()
            .map(|a| format!("'{}'", a.replace('\'', "'\\''")))
            .collect::<Vec<_>>()
            .join(" ");

        let snap_user_common = std::env::var("HOME")
            .map(|h| format!("{}/snap/steam/common", h))
            .unwrap_or_else(|_| String::from("/home/user/snap/steam/common"));

        // Shell script that runs inside the snap container:
        // 1. Sets HOME to the sandbox directory (accessible via snap's shared data)
        // 2. Creates Steam symlinks
        // 3. Sets DISPLAY to the Xvfb instance
        // 4. Runs steam.sh with bootstrap-skip flags
        let inner_script = format!(
            r#"
export DISPLAY='{display}'
export HOME='{snap_home}'
export STEAM_DISABLE_BROWSER_SANDBOX=1
export STEAM_DISABLE_GPU=1
export DBUS_SESSION_BUS_ADDRESS=disabled
export STEAMWEBHELPER_ARGS="--disable-gpu --disable-gpu-compositing --no-sandbox --disable-software-rasterizer"
mkdir -p "$HOME/.local/share" "$HOME/.steam" "$HOME/bin"
ln -sfn '{steam_root}' "$HOME/.local/share/Steam"
ln -sfn '{steam_root}' "$HOME/.steam/steam"
printf '#!/bin/sh\nexit 0\n' > "$HOME/bin/zenity"
chmod +x "$HOME/bin/zenity"
export PATH="$HOME/bin:$PATH"
exec "$HOME/.local/share/Steam/steam.sh" {args}
"#,
            display = display,
            snap_home = sandbox_home.display(),
            steam_root = snap_user_common + "/.local/share/Steam",
            args = args_str,
        );

        eprintln!("[sandbox-{}] Launching Steam via snap (display {})", self.cfg.id, display);

        let mut cmd = Command::new("snap");
        cmd.args(["run", "--shell", "steam", "-c", &inner_script]);
        cmd.stdout(Stdio::null())
            .stderr(Stdio::piped());

        let mut child = cmd.spawn()
            .map_err(|e| format!("Failed to start Steam via snap: {}", e))?;

        let id = self.cfg.id;
        if let Some(stderr) = child.stderr.take() {
            tokio::spawn(async move {
                let reader = BufReader::new(stderr);
                let mut lines = reader.lines();
                while let Ok(Some(line)) = lines.next_line().await {
                    eprintln!("[steam-{}] {}", id, line);
                }
            });
        }

        eprintln!("[sandbox-{}] Steam snap launched (pid {:?})", self.cfg.id, child.id());
        self.game = Some(child);
        Ok(())
    }

    async fn start_via_direct(&mut self) -> Result<(), Box<dyn std::error::Error>> {
        let display = format!(":{}", self.cfg.display);
        let steam_args = self.build_steam_args();
        let sandbox_home = self.base.join("home");
        let xdg_runtime = self.base.join("xdg");
        let fake_bin = self.base.join("bin");
        let sys_path = std::env::var("PATH").unwrap_or_default();
        let new_path = format!("{}:{}", fake_bin.display(), sys_path);

        let lib_path_str = self.steam_paths.lib_paths.iter()
            .map(|p| p.display().to_string())
            .collect::<Vec<_>>()
            .join(":");

        if let Some(ref ld_linux) = self.steam_paths.ld_linux_32 {
            let script_path = self.base.join("launch_steam.sh");
            let args_str = steam_args.iter()
                .map(|a| format!("'{}'", a.replace('\'', "'\\''")))
                .collect::<Vec<_>>()
                .join(" ");

            let script = format!(
                "#!/bin/sh\nexport LD_LIBRARY_PATH='{lib}'\n'{ld}' --library-path '{lib}' '{bin}' {args}\n",
                ld = ld_linux.display(),
                lib = lib_path_str,
                bin = self.steam_paths.steam_binary.display(),
                args = args_str,
            );
            std::fs::write(&script_path, &script)?;
            #[cfg(unix)]
            {
                use std::os::unix::fs::PermissionsExt;
                std::fs::set_permissions(&script_path, std::fs::Permissions::from_mode(0o755))?;
            }

            eprintln!("[sandbox-{}] Launching Steam via ld-linux (fallback)", self.cfg.id);

            let mut cmd = Command::new(script_path.display().to_string());
            cmd.env("HOME", &sandbox_home)
                .env("DISPLAY", &display)
                .env("XDG_RUNTIME_DIR", &xdg_runtime)
                .env("DBUS_SESSION_BUS_ADDRESS", "disabled")
                .env("PATH", &new_path)
                .env("REAL_HOME", std::env::var("HOME").unwrap_or_default())
                .env("LD_LIBRARY_PATH", &lib_path_str)
                .env("STEAMROOT", &self.steam_paths.root)
                .env("LIBGL_ALWAYS_SOFTWARE", "1");
            cmd.stdout(Stdio::null()).stderr(Stdio::piped());

            let mut child = cmd.spawn()
                .map_err(|e| format!("Failed to start Steam: {}", e))?;

            let id = self.cfg.id;
            if let Some(stderr) = child.stderr.take() {
                tokio::spawn(async move {
                    let reader = BufReader::new(stderr);
                    let mut lines = reader.lines();
                    while let Ok(Some(line)) = lines.next_line().await {
                        eprintln!("[steam-{}] {}", id, line);
                    }
                });
            }

            self.game = Some(child);
            return Ok(());
        }

        // Last resort: steam command
        let steam_sh = self.steam_paths.root.join("steam.sh");
        let steam_cmd = if steam_sh.exists() {
            steam_sh.display().to_string()
        } else {
            "steam".to_string()
        };

        let mut cmd = Command::new(&steam_cmd);
        cmd.args(&steam_args);
        cmd.env("HOME", &sandbox_home)
            .env("DISPLAY", &display)
            .env("DBUS_SESSION_BUS_ADDRESS", "disabled")
            .env("PATH", &new_path);
        cmd.stdout(Stdio::null()).stderr(Stdio::piped());

        let mut child = cmd.spawn()
            .map_err(|e| format!("Failed to start Steam: {}", e))?;

        let id = self.cfg.id;
        if let Some(stderr) = child.stderr.take() {
            tokio::spawn(async move {
                let reader = BufReader::new(stderr);
                let mut lines = reader.lines();
                while let Ok(Some(line)) = lines.next_line().await {
                    eprintln!("[steam-{}] {}", id, line);
                }
            });
        }

        self.game = Some(child);
        Ok(())
    }

    pub fn game_pid(&self) -> Option<u32> {
        self.game.as_ref().and_then(|c| c.id())
    }

    pub async fn wait(&mut self) -> i32 {
        if let Some(ref mut game) = self.game {
            match game.wait().await {
                Ok(status) => status.code().unwrap_or(-1),
                Err(_) => -1,
            }
        } else {
            -1
        }
    }

    pub async fn shutdown(&mut self) {
        for child_opt in [&mut self.game, &mut self.vnc, &mut self.xvfb] {
            if let Some(ref mut child) = child_opt {
                let _ = child.kill().await;
                let _ = child.wait().await;
            }
        }
        // Clean up X11 files
        let _ = std::fs::remove_file(format!("/tmp/.X{}-lock", self.cfg.display));
        let _ = std::fs::remove_file(format!("/tmp/.X11-unix/X{}", self.cfg.display));
    }
}
