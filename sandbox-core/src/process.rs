use std::path::PathBuf;
use std::process::Stdio;
use std::time::Duration;
use tokio::io::{AsyncBufReadExt, BufReader};
use tokio::process::{Child, Command};

use crate::ipc::LaunchConfig;
use crate::proc_kill;
use crate::steam::{self, SteamPaths};
use nix::sys::signal::Signal;

fn self_dir() -> PathBuf {
    std::env::current_exe()
        .ok()
        .and_then(|p| p.parent().map(|d| d.to_path_buf()))
        .unwrap_or_else(|| PathBuf::from("."))
}

fn shell_escape_single(s: &str) -> String {
    format!("'{}'", s.replace('\'', "'\"'\"'"))
}

fn find_sandbox_bin() -> String {
    let local = self_dir().join("sfarm-sandbox");
    if local.is_file() {
        return local.display().to_string();
    }
    if let Ok(exe) = std::env::current_exe() {
        if exe.file_name().and_then(|n| n.to_str()) == Some("sfarm-sandbox") {
            return exe.display().to_string();
        }
    }
    find_bin("sfarm-sandbox")
}

/// Встроенный скрипт: правит localconfig.vdf — CloudEnabled=0 для COMPAT_APP_ID (Rust; python — запасной путь).
fn steam_cloud_patch_heredoc(sandbox_bin: &str) -> String {
    let q = shell_escape_single(sandbox_bin);
    let py = include_str!("../scripts/patch_steam_cloud.py");
    format!(
        r#"
# SFARM: отключить Steam Cloud для игры (appid=COMPAT_APP_ID) — без модалки «Cloud Out of Date».
if [ -n "$COMPAT_APP_ID" ] && [ "$COMPAT_APP_ID" != "0" ] && [ -f {q} ]; then
  {q} patch-steam-cloud --home "$HOME" --app-id "$COMPAT_APP_ID" 2>/dev/null || true
elif [ -n "$COMPAT_APP_ID" ] && [ "$COMPAT_APP_ID" != "0" ] && command -v python3 >/dev/null 2>&1; then
  python3 <<'PYEOF'
{py}
PYEOF
fi
"#,
        q = q,
        py = py
    )
}

fn find_bin(name: &str) -> String {
    let local = self_dir().join(name);
    if local.is_file() {
        return local.display().to_string();
    }
    // systemd/cron иногда дают урезанный PATH без /usr/bin — execvp не находит x11vnc/Xvfb.
    for dir in ["/usr/bin", "/usr/local/bin", "/bin"] {
        let p = PathBuf::from(dir).join(name);
        if p.is_file() {
            return p.display().to_string();
        }
    }
    name.to_string()
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

        // Kill any orphaned Xvfb on the same display from a previous run
        let _ = std::process::Command::new("pkill")
            .args(["-9", "-f", &format!("Xvfb :{}", display_num)])
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .status();
        tokio::time::sleep(std::time::Duration::from_millis(200)).await;

        let lock_file = format!("/tmp/.X{}-lock", display_num);
        let socket_file = format!("/tmp/.X11-unix/X{}", display_num);
        let _ = std::fs::remove_file(&lock_file);
        let _ = std::fs::remove_file(&socket_file);

        // Минимальные образы / пустой /tmp: каталог для unix-сокета X11.
        let x11_unix = std::path::Path::new("/tmp/.X11-unix");
        if !x11_unix.exists() {
            std::fs::create_dir_all(x11_unix)
                .map_err(|e| format!("create /tmp/.X11-unix: {}", e))?;
        }

        // -listen tcp + -ac без -auth: snap Steam в своём namespace не видит unix-сокет в /tmp хоста и
        // подключается по TCP; с -auth сервер требует MIT-cookie и даёт «Authorization required…», а cookie
        // из файла в песочнице с snap не совпадает. -ac отключает контроль доступа — клиент без cookie.
        let xvfb_args = [
            display.as_str(),
            "-screen",
            "0",
            "1280x720x24",
            "-ac",
            "+extension",
            "GLX",
            "-listen",
            "tcp",
            "-noreset",
        ];

        let mut child = Command::new(&xvfb_bin)
            .args(&xvfb_args)
            .stdout(Stdio::null())
            .stderr(Stdio::piped())
            .spawn()
            .map_err(|e| {
                let hint = if e.raw_os_error() == Some(2) {
                    " — установите пакет: apt install xvfb"
                } else {
                    ""
                };
                format!("Failed to start Xvfb (tried '{}'): {}{}", xvfb_bin, e, hint)
            })?;

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

        // Ждём unix-сокет; если Xvfb сразу падает (нет lib, неверный бинарь) — ошибка, а не «тихий» успех.
        let socket_path = format!("/tmp/.X11-unix/X{}", self.cfg.display);
        for _ in 0..50 {
            if let Ok(Some(status)) = child.try_wait() {
                return Err(
                    format!(
                        "Xvfb exited before display :{} was ready (status={}); check [xvfb-{}] stderr above",
                        self.cfg.display, status, self.cfg.id
                    )
                    .into(),
                );
            }
            if std::path::Path::new(&socket_path).exists() {
                eprintln!("[sandbox-{}] Xvfb display :{} ready (unix socket)", self.cfg.id, self.cfg.display);
                self.xvfb = Some(child);
                return Ok(());
            }
            tokio::time::sleep(std::time::Duration::from_millis(100)).await;
        }
        let _ = child.kill().await;
        Err(
            format!(
                "X11 unix socket {} did not appear within 5s — Xvfb likely failed (binary: {})",
                socket_path, xvfb_bin
            )
            .into(),
        )
    }

    pub async fn start_vnc(&mut self) -> Result<(), Box<dyn std::error::Error>> {
        let vnc_bin = find_bin("x11vnc");
        let display = format!(":{}", self.cfg.display);
        let port = self.cfg.vnc_port.to_string();

        // Kill any orphaned x11vnc on the same port
        let _ = std::process::Command::new("sh")
            .args(["-c", &format!("fuser -k {}/tcp 2>/dev/null || true", self.cfg.vnc_port)])
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .status();

        // Не подмешивать bin/lib к системному x11vnc — несовместимые .so дают мгновенный exit.
        // Не использовать -rfbaddr: в x11vnc 0.9.16 (libvnc) на части сборок это даёт
        // «*** unrecognized option(s) *** -rfbaddr» и exit 1 — песочница не стартует.
        // Без -localhost x11vnc слушает все интерфейсы (норм для Docker -p и для 127.0.0.1 на хосте).
        // -noshm: при переполнении IPC /dev/shm x11vnc падает с «shmget: No space left on device»
        // (это лимит shm, не диск). Polling через XGetImage без MIT-SHM медленнее, но стабильнее.
        // -noscr: без RECORD/scrollcopyrect — иначе на части хостов/клиентов VNC «мигает» и полосит экран.
        // -nowireframe/-nowcr: иначе wireframe + CopyRect — на noVNC часто мигание и артефакты.
        // -defer: слегка сгладить поток прямоугольников (меньше белых вспышек при tight+медленной сети).
        let mut child = Command::new(&vnc_bin)
            .args([
                "-display", &display,
                "-rfbport", &port,
                "-nopw", "-forever", "-shared",
                "-noxdamage",
                "-noshm",
                "-noscr",
                "-nowireframe",
                "-nowcr",
                "-defer",
                "28",
            ])
            .env_remove("LD_LIBRARY_PATH")
            .env_remove("XAUTHORITY")
            .stdout(Stdio::null())
            .stderr(Stdio::piped())
            .spawn()
            .map_err(|e| {
                let hint = if e.raw_os_error() == Some(2) {
                    " — установите пакет: apt install x11vnc (и xvfb: apt install xvfb)"
                } else {
                    ""
                };
                format!("Failed to start x11vnc (tried '{}'): {}{}", vnc_bin, e, hint)
            })?;

        let id = self.cfg.id;
        let vnc_port = self.cfg.vnc_port;
        if let Some(stderr) = child.stderr.take() {
            tokio::spawn(async move {
                let reader = BufReader::new(stderr);
                let mut lines = reader.lines();
                while let Ok(Some(line)) = lines.next_line().await {
                    eprintln!("[vnc-{}] {}", id, line);
                }
            });
        }

        tokio::time::sleep(Duration::from_millis(300)).await;
        if let Ok(Some(status)) = child.try_wait() {
            return Err(
                format!(
                    "x11vnc exited immediately (status={}); see [vnc-{}] stderr (display :{} port {})",
                    status, self.cfg.id, self.cfg.display, vnc_port
                )
                .into(),
            );
        }

        let addr = format!("127.0.0.1:{}", vnc_port);
        let mut accepted = false;
        for _ in 0..50 {
            if tokio::net::TcpStream::connect(&addr).await.is_ok() {
                accepted = true;
                break;
            }
            if let Ok(Some(st)) = child.try_wait() {
                return Err(
                    format!(
                        "x11vnc died before listening (status={}); see [vnc-{}] stderr",
                        st, self.cfg.id
                    )
                    .into(),
                );
            }
            tokio::time::sleep(Duration::from_millis(100)).await;
        }
        if !accepted {
            let _ = child.kill().await;
            return Err(
                format!(
                    "x11vnc did not accept TCP on {} within ~5s; see [vnc-{}] stderr",
                    addr, self.cfg.id
                )
                .into(),
            );
        }

        eprintln!(
            "[sandbox-{}] x11vnc listening on {} (display :{})",
            self.cfg.id, addr, self.cfg.display
        );
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
        let steam_args = self.build_steam_args();
        let sandbox_home = &self.base.join("home");

        let disp_for_kill = self.cfg.display;
        tokio::task::spawn_blocking(move || {
            // Сначала «чужой» CS2 на хосте (:0 и т.д.) без SFARM_DISPLAY — иначе Steam в VNC: «Only one instance…»
            proc_kill::kill_host_linuxsteamrt_games_without_sfarm_display();
            proc_kill::kill_stale_linuxsteamrt_games_on_display(disp_for_kill);
        })
        .await
        .ok();

        let args_str = steam_args.iter()
            .map(|a| format!("'{}'", a.replace('\'', "'\\''")))
            .collect::<Vec<_>>()
            .join(" ");

        // TCP: snap не видит unix-сокет Xvfb на хосте в /tmp; `127.0.0.1:N` → порт 6000+N. Вместе с Xvfb `-ac` без `-auth`.
        let display_tcp = format!("127.0.0.1:{}", self.cfg.display);

        // steam_real/snap_common — из найденного Steam (find_steam), не из $HOME супервизора:
        // иначе при root + Steam у steam-farm путь был /root/snap/... и steam.sh не находился.
        let steam_real = self.steam_paths.root.display().to_string();
        let snap_common = steam::snap_steam_common_dir(&self.steam_paths.root)
            .unwrap_or_else(|| {
                PathBuf::from(
                    std::env::var("HOME")
                        .map(|h| format!("{}/snap/steam/common", h))
                        .unwrap_or_else(|_| "/tmp".into()),
                )
            })
            .display()
            .to_string();

        let compat_app_id = steam::game_app_id(&self.cfg.game);
        let try_fuse_cs2_overlay = self.cfg.game == "cs2";
        let common_folder = steam::steamapps_common_folder_name(&self.cfg.game).unwrap_or("");

        // Shell script that runs inside the snap container:
        // 1. Sets HOME to the sandbox directory (accessible via snap's shared data)
        // 2. Creates Steam symlinks
        // 3. Sets DISPLAY to the Xvfb instance
        // 4. Runs steam.sh with bootstrap-skip flags

        let inner_script = format!(
            r#"
export HOME='{snap_home}'
export SFARM_DISPLAY='{display_num}'
unset XAUTHORITY
export DISPLAY='{display_tcp}'
export STEAM_DISABLE_BROWSER_SANDBOX=1
export CEF_DISABLE_SANDBOX=1
export SDL_VIDEODRIVER=x11

# NVIDIA Optimus (PRIME Render Offload): force discrete GPU for games.
export __NV_PRIME_RENDER_OFFLOAD=1
export __NV_PRIME_RENDER_OFFLOAD_PROVIDER=NVIDIA-G0
export __GLX_VENDOR_LIBRARY_NAME=nvidia
export __VK_LAYER_NV_optimus=NVIDIA_only

# Vulkan ICD: use snap-provided NVIDIA ICD (host path is inaccessible inside snap)
if [ -f /var/lib/snapd/lib/vulkan/icd.d/nvidia_icd.json ]; then
    export VK_ICD_FILENAMES=/var/lib/snapd/lib/vulkan/icd.d/nvidia_icd.json
    export VK_DRIVER_FILES=/var/lib/snapd/lib/vulkan/icd.d/nvidia_icd.json
fi

# Ensure NVIDIA GPU libraries are reachable by games (Vulkan, GL).
if [ -d /var/lib/snapd/lib/gl ]; then
    export LD_LIBRARY_PATH="/var/lib/snapd/lib/gl:/var/lib/snapd/lib/gl32${{LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}}"
fi

# Provide a dbus session bus via dbus-launch if available, else set a dummy socket
if command -v dbus-daemon >/dev/null 2>&1; then
    eval "$(dbus-launch --sh-syntax 2>/dev/null)" || true
fi
if [ -z "$DBUS_SESSION_BUS_ADDRESS" ]; then
    export DBUS_SESSION_BUS_ADDRESS=unix:path=/dev/null
fi

STEAM_REAL='{steam_real}'
STEAM_LOCAL="$HOME/.local/share/Steam"

# Отдельный TMP для каждой песочницы — иначе lock-файлы в общем /tmp дают «Only one instance…»
mkdir -p "$HOME/.sfarm-tmp"
export TMPDIR="$HOME/.sfarm-tmp"
export TMP="$TMPDIR"
export TEMP="$TMPDIR"
export TEMPDIR="$TMPDIR"

export COMPAT_APP_ID='{compat_app_id}'
COMMON_FOLDER='{common_folder}'
TRY_FUSE_CS2_OVERLAY='{try_fuse_cs2_overlay}'

mkdir -p "$STEAM_LOCAL"

# Copy steam.sh so STEAMROOT resolves to shadow dir; symlink everything else
# config, steamapps, userdata, registry — отдельно (не делить с другой песочницей).
for item in "$STEAM_REAL"/*; do
    name=$(basename "$item")
    [ "$name" = "ubuntu12_32" ] && continue
    [ "$name" = "ubuntu12_64" ] && continue
    [ "$name" = "config" ] && continue
    [ "$name" = "steamapps" ] && continue
    [ "$name" = "userdata" ] && continue
    [ "$name" = "registry.vdf" ] && continue
    [ "$name" = "registry.vdf.bak" ] && continue
    [ "$name" = "steam.pid" ] && continue
    if [ "$name" = "steam.sh" ]; then
        cp "$item" "$STEAM_LOCAL/$name"
        chmod +x "$STEAM_LOCAL/$name"
    else
        ln -sfn "$item" "$STEAM_LOCAL/$name" 2>/dev/null
    fi
done

# Состояние Steam на диске — своя копия на инстанс (симлинк на хост → «Only one instance of the game»).
if [ -d "$STEAM_REAL/userdata" ]; then
    cp -a "$STEAM_REAL/userdata" "$STEAM_LOCAL/userdata"
fi
for f in registry.vdf registry.vdf.bak; do
    if [ -f "$STEAM_REAL/$f" ]; then cp -a "$STEAM_REAL/$f" "$STEAM_LOCAL/$f"; fi
done
rm -f "$STEAM_LOCAL/steam.pid" 2>/dev/null

# Shadow steamapps: copy appmanifest files (own game state), symlink the rest.
# compatdata: общий симлинк на хост → два клиента делят prefix/locks → «Only one instance…».
# Для известных игр (compat_app_id) копируем только каталог appid, остальное — симлинки.
mkdir -p "$STEAM_LOCAL/steamapps"
for item in "$STEAM_REAL/steamapps"/*; do
    [ -e "$item" ] || continue
    name=$(basename "$item")
    case "$name" in
        appmanifest_*.acf|libraryfolders.vdf)
            cp "$item" "$STEAM_LOCAL/steamapps/$name" 2>/dev/null
            ;;
        compatdata)
            if [ -n "$COMPAT_APP_ID" ] && [ "$COMPAT_APP_ID" != "0" ]; then
                :
            else
                ln -sfn "$item" "$STEAM_LOCAL/steamapps/$name" 2>/dev/null
            fi
            ;;
        common)
            if [ -n "$COMMON_FOLDER" ] && [ -n "$COMPAT_APP_ID" ] && [ "$COMPAT_APP_ID" != "0" ]; then
                mkdir -p "$STEAM_LOCAL/steamapps/common"
                for sub in "$item"/*; do
                    [ -e "$sub" ] || continue
                    sname=$(basename "$sub")
                    M="$STEAM_LOCAL/steamapps/common/$sname"
                    if [ "$sname" = "$COMMON_FOLDER" ] && [ -d "$sub" ]; then
                        if [ "$TRY_FUSE_CS2_OVERLAY" = "1" ] && command -v fuse-overlayfs >/dev/null 2>&1; then
                            U="$HOME/.sfarm-cs2-overlay-upper"
                            W="$HOME/.sfarm-cs2-overlay-work"
                            mkdir -p "$U" "$W"
                            rm -rf "$M" 2>/dev/null
                            mkdir -p "$M"
                            if fuse-overlayfs -o "lowerdir=$sub,upperdir=$U,workdir=$W" "$M" 2>/dev/null; then
                                :
                            else
                                ln -sfn "$sub" "$M"
                            fi
                        else
                            ln -sfn "$sub" "$M"
                        fi
                    else
                        ln -sfn "$sub" "$M" 2>/dev/null
                    fi
                done
            else
                ln -sfn "$item" "$STEAM_LOCAL/steamapps/$name" 2>/dev/null
            fi
            ;;
        *)
            ln -sfn "$item" "$STEAM_LOCAL/steamapps/$name" 2>/dev/null
            ;;
    esac
done

if [ -n "$COMPAT_APP_ID" ] && [ "$COMPAT_APP_ID" != "0" ] && [ -d "$STEAM_REAL/steamapps/compatdata" ]; then
    rm -rf "$STEAM_LOCAL/steamapps/compatdata" 2>/dev/null
    mkdir -p "$STEAM_LOCAL/steamapps/compatdata"
    for citem in "$STEAM_REAL/steamapps/compatdata"/*; do
        [ -e "$citem" ] || continue
        cname=$(basename "$citem")
        if [ "$cname" = "$COMPAT_APP_ID" ]; then
            cp -a "$citem" "$STEAM_LOCAL/steamapps/compatdata/$cname"
        else
            ln -sfn "$citem" "$STEAM_LOCAL/steamapps/compatdata/$cname" 2>/dev/null
        fi
    done
    if [ ! -e "$STEAM_LOCAL/steamapps/compatdata/$COMPAT_APP_ID" ]; then
        mkdir -p "$STEAM_LOCAL/steamapps/compatdata/$COMPAT_APP_ID"
    fi
fi

# Reset all game StateFlags to 4 ("fully installed, not running")
for m in "$STEAM_LOCAL/steamapps"/appmanifest_*.acf; do
    [ -f "$m" ] && sed -i 's/"StateFlags"[[:space:]]*"[0-9]*"/"StateFlags"\t\t"4"/' "$m"
done

# Shadow config: копии файлов (не симлинки) — иначе два клиента делят localconfig/SingletonLock.
mkdir -p "$STEAM_LOCAL/config"
for item in "$STEAM_REAL/config"/*; do
    name=$(basename "$item")
    [ "$name" = "htmlcache" ] && continue
    cp -a "$item" "$STEAM_LOCAL/config/$name" 2>/dev/null
done
mkdir -p "$STEAM_LOCAL/config/htmlcache"

# Shadow ubuntu12_32 — COPY the steam binary so /proc/self/exe resolves here;
# symlink everything else
mkdir -p "$STEAM_LOCAL/ubuntu12_32"
for item in "$STEAM_REAL/ubuntu12_32"/*; do
    name=$(basename "$item")
    if [ "$name" = "steam" ]; then
        cp "$item" "$STEAM_LOCAL/ubuntu12_32/$name"
        chmod +x "$STEAM_LOCAL/ubuntu12_32/$name"
    else
        ln -sfn "$item" "$STEAM_LOCAL/ubuntu12_32/$name" 2>/dev/null
    fi
done

# Shadow ubuntu12_64 — symlink all except steamwebhelper_sniper_wrap.sh
mkdir -p "$STEAM_LOCAL/ubuntu12_64"
for item in "$STEAM_REAL/ubuntu12_64"/*; do
    name=$(basename "$item")
    [ "$name" = "steamwebhelper_sniper_wrap.sh" ] && continue
    ln -sfn "$item" "$STEAM_LOCAL/ubuntu12_64/$name" 2>/dev/null
done

# Custom steamwebhelper_sniper_wrap.sh: software rendering + no-sandbox for CEF only
cat > "$STEAM_LOCAL/ubuntu12_64/steamwebhelper_sniper_wrap.sh" << 'WRAPEOF'
#!/bin/bash
export LD_LIBRARY_PATH=.${{LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}}
export LIBGL_ALWAYS_SOFTWARE=1
export GALLIUM_DRIVER=llvmpipe
export MESA_GL_VERSION_OVERRIDE=4.5
export MESA_GLSL_VERSION_OVERRIDE=450
export MESA_LOADER_DRIVER_OVERRIDE=swrast
echo "<6>exec ./steamwebhelper (sandbox-wrapped) $*"
echo "<remaining-lines-assume-level=7>"
exec ./steamwebhelper \
    --no-sandbox \
    --disable-dev-shm-usage \
    "$@"
WRAPEOF
chmod +x "$STEAM_LOCAL/ubuntu12_64/steamwebhelper_sniper_wrap.sh"

# .steam symlinks
mkdir -p "$HOME/.steam"
ln -sfn "$STEAM_LOCAL" "$HOME/.steam/steam"
ln -sfn "$STEAM_LOCAL" "$HOME/.steam/root"

# Share GPU shader caches from host Steam to avoid recompilation on each launch
SNAP_COMMON='{snap_common}'
mkdir -p "$HOME/.cache"
for dir in mesa_shader_cache nvidia fontconfig; do
    [ -d "$SNAP_COMMON/.cache/$dir" ] && ln -sfn "$SNAP_COMMON/.cache/$dir" "$HOME/.cache/$dir"
done
[ -d "$SNAP_COMMON/.nv" ] && ln -sfn "$SNAP_COMMON/.nv" "$HOME/.nv"

# Dummy zenity to suppress blocking dialogs
mkdir -p "$HOME/bin"
printf '#!/bin/sh\nexit 0\n' > "$HOME/bin/zenity"
chmod +x "$HOME/bin/zenity"
export PATH="$HOME/bin:$PATH"

# Зависшие cs2/dota2 на этом DISPLAY уже убраны супервизором (Rust), до snap — не через sh,
# иначе snap часто выполняет -c через dash и ломается на local/read -d ''.

{patch_cloud}

"$STEAM_LOCAL/steam.sh" {args} &
STEAM_PID=$!
# Keep the shell alive so the sandbox supervisor can track it.
# steam.sh forks the real client and exits; we must stay alive
# until killed externally (SIGTERM from supervisor).
wait $STEAM_PID 2>/dev/null
# If Steam exited, keep alive so Xvfb/x11vnc stay running
# for the bot to use. Sleep until killed.
while true; do sleep 60; done
"#,
            display_tcp = display_tcp,
            display_num = self.cfg.display,
            snap_home = sandbox_home.display(),
            snap_common = snap_common,
            steam_real = steam_real,
            compat_app_id = compat_app_id,
            common_folder = common_folder,
            try_fuse_cs2_overlay = if try_fuse_cs2_overlay { "1" } else { "0" },
            patch_cloud = steam_cloud_patch_heredoc(&find_sandbox_bin()),
            args = args_str,
        );

        eprintln!(
            "[sandbox-{}] Launching Steam via snap (DISPLAY={} unix :{})",
            self.cfg.id, display_tcp, self.cfg.display
        );

        let mut cmd = Command::new("snap");
        cmd.args(["run", "--shell", "steam", "-c", &inner_script]);
        let home = sandbox_home.display().to_string();
        let xdg_cfg = self.base.join("home/.config").display().to_string();
        let xdg_data = self.base.join("home/.local/share").display().to_string();
        cmd.env("HOME", &home)
            .env("XDG_CONFIG_HOME", &xdg_cfg)
            .env("XDG_DATA_HOME", &xdg_data)
            .env("XDG_RUNTIME_DIR", self.base.join("xdg").display().to_string())
            .env("SFARM_DISPLAY", self.cfg.display.to_string())
            .env_remove("XAUTHORITY");
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
        let disp_for_kill = self.cfg.display;
        tokio::task::spawn_blocking(move || {
            proc_kill::kill_host_linuxsteamrt_games_without_sfarm_display();
            proc_kill::kill_stale_linuxsteamrt_games_on_display(disp_for_kill);
        })
        .await
        .ok();

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
                "#!/bin/sh\nunset XAUTHORITY\nexport LD_LIBRARY_PATH='{lib}'\n'{ld}' --library-path '{lib}' '{bin}' {args}\n",
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
                .env("SFARM_DISPLAY", self.cfg.display.to_string())
                .env("XDG_RUNTIME_DIR", &xdg_runtime)
                .env("DBUS_SESSION_BUS_ADDRESS", "disabled")
                .env("PATH", &new_path)
                .env("REAL_HOME", std::env::var("HOME").unwrap_or_default())
                .env("LD_LIBRARY_PATH", &lib_path_str)
                .env("STEAMROOT", &self.steam_paths.root)
                .env("LIBGL_ALWAYS_SOFTWARE", "1")
                .env_remove("XAUTHORITY");
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
            .env("SFARM_DISPLAY", self.cfg.display.to_string())
            .env("DBUS_SESSION_BUS_ADDRESS", "disabled")
            .env("PATH", &new_path)
            .env_remove("XAUTHORITY");
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
        // Hard-stop everything on this X display first (Steam/CS2 often survive killing only the snap shell).
        let display = self.cfg.display;
        let _ = tokio::task::spawn_blocking(move || {
            proc_kill::signal_clients_on_display(display, Signal::SIGTERM);
            std::thread::sleep(std::time::Duration::from_millis(700));
            proc_kill::signal_clients_on_display(display, Signal::SIGKILL);
        })
        .await;

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
