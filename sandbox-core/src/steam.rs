use std::path::PathBuf;

pub struct SteamPaths {
    pub root: PathBuf,
    pub common: PathBuf,
    pub linux64: PathBuf,
    pub ubuntu12_32: PathBuf,
    pub steam_binary: PathBuf,
    pub ld_linux_32: Option<PathBuf>,
    /// Complete library search path (mirrors run.sh + steam.sh)
    pub lib_paths: Vec<PathBuf>,
}

/// Путь к корню Steam (где лежат `steamapps/common`, `ubuntu12_32/steam`).
/// Если задан — проверяется первым (удобно при запуске sandbox от root, а Steam у обычного пользователя).
const ENV_STEAM_ROOT: [&str; 2] = ["SFARM_STEAM_ROOT", "STEAM_ROOT"];

pub fn find_steam() -> Option<SteamPaths> {
    for key in ENV_STEAM_ROOT {
        if let Ok(s) = std::env::var(key) {
            let root = PathBuf::from(s.trim());
            if let Some(paths) = try_steam_root(&root) {
                return Some(paths);
            }
        }
    }

    let home = std::env::var("REAL_HOME")
        .or_else(|_| std::env::var("HOME"))
        .ok()?;
    let candidates = [
        format!("{}/snap/steam/common/.local/share/Steam", home),
        format!("{}/.local/share/Steam", home),
        format!("{}/.steam/steam", home),
        format!("{}/.steam/debian-installation", home),
        // Flatpak (com.valvesoftware.Steam)
        format!(
            "{}/.var/app/com.valvesoftware.Steam/.local/share/Steam",
            home
        ),
        format!("{}/.var/app/com.valvesoftware.Steam/data/Steam", home),
    ];

    for root_str in &candidates {
        let root = PathBuf::from(root_str);
        if let Some(paths) = try_steam_root(&root) {
            return Some(paths);
        }
    }
    None
}

fn try_steam_root(root: &PathBuf) -> Option<SteamPaths> {
    let root = root
        .canonicalize()
        .unwrap_or_else(|_| root.clone());
    let common = root.join("steamapps/common");
    if !common.is_dir() {
        return None;
    }
    let steam_binary = root.join("ubuntu12_32/steam");
    if !steam_binary.exists() {
        return None;
    }
    let (ld_linux_32, lib_paths) = build_runtime_paths(&root);
    Some(SteamPaths {
        linux64: root.join("linux64"),
        ubuntu12_32: root.join("ubuntu12_32"),
        common,
        steam_binary,
        ld_linux_32,
        lib_paths,
        root,
    })
}

fn build_runtime_paths(steam_root: &PathBuf) -> (Option<PathBuf>, Vec<PathBuf>) {
    let mut ld = None;
    let mut libs = Vec::new();
    let runtime = steam_root.join("ubuntu12_32/steam-runtime");

    // 1. Steam's own 32-bit + panorama (steam.sh: $STEAMROOT/$PLATFORM:$STEAMROOT/$PLATFORM/panorama)
    push_if_dir(&mut libs, steam_root.join("ubuntu12_32"));
    push_if_dir(&mut libs, steam_root.join("ubuntu12_32/panorama"));

    // 2. Steam Runtime pinned libs (run.sh: $STEAM_RUNTIME/pinned_libs_32:pinned_libs_64)
    push_if_dir(&mut libs, runtime.join("pinned_libs_32"));
    push_if_dir(&mut libs, runtime.join("pinned_libs_64"));

    // 3. Steam Runtime libcurl compat (run.sh: $STEAM_RUNTIME/libcurl_compat_32:libcurl_compat_64)
    push_if_dir(&mut libs, runtime.join("libcurl_compat_32"));
    push_if_dir(&mut libs, runtime.join("libcurl_compat_64"));

    // 4. Steam Runtime core libraries (run.sh: lib/{i386,x86_64}-linux-gnu, usr/lib/... etc)
    push_if_dir(&mut libs, runtime.join("lib/i386-linux-gnu"));
    push_if_dir(&mut libs, runtime.join("usr/lib/i386-linux-gnu"));
    push_if_dir(&mut libs, runtime.join("lib/x86_64-linux-gnu"));
    push_if_dir(&mut libs, runtime.join("usr/lib/x86_64-linux-gnu"));
    push_if_dir(&mut libs, runtime.join("lib"));
    push_if_dir(&mut libs, runtime.join("usr/lib"));

    // 5. Runtime sub-arch dirs (e.g. steam-runtime/i386/...)
    for arch in ["i386", "amd64"] {
        let arch_dir = runtime.join(arch);
        push_if_dir(&mut libs, arch_dir.join("lib/i386-linux-gnu"));
        push_if_dir(&mut libs, arch_dir.join("usr/lib/i386-linux-gnu"));
        push_if_dir(&mut libs, arch_dir.join("lib/x86_64-linux-gnu"));
        push_if_dir(&mut libs, arch_dir.join("usr/lib/x86_64-linux-gnu"));
        push_if_dir(&mut libs, arch_dir.join("usr/lib"));
    }

    // 6. System library paths (host OS)
    push_if_dir(&mut libs, PathBuf::from("/lib/x86_64-linux-gnu"));
    push_if_dir(&mut libs, PathBuf::from("/lib"));
    push_if_dir(&mut libs, PathBuf::from("/usr/lib/x86_64-linux-gnu"));
    push_if_dir(&mut libs, PathBuf::from("/usr/local/lib"));

    // 7. Find 32-bit dynamic linker
    let sys_ld = PathBuf::from("/lib/ld-linux.so.2");
    if sys_ld.exists() {
        ld = Some(sys_ld);
    } else {
        // Snap's bundled 32-bit loader
        for snap_dir in glob_snap_dirs() {
            let snap_ld = snap_dir.join("usr/lib/ld-linux.so.2");
            if snap_ld.exists() {
                ld = Some(snap_ld);
                // Also add snap library paths
                push_if_dir(&mut libs, snap_dir.join("usr/lib/i386-linux-gnu"));
                push_if_dir(&mut libs, snap_dir.join("usr/lib"));
                push_if_dir(&mut libs, snap_dir.join("lib/i386-linux-gnu"));
                push_if_dir(&mut libs, snap_dir.join("lib"));
                break;
            }
        }
    }

    (ld, libs)
}

fn push_if_dir(libs: &mut Vec<PathBuf>, p: PathBuf) {
    if p.is_dir() {
        if !libs.contains(&p) {
            libs.push(p);
        }
    }
}

fn glob_snap_dirs() -> Vec<PathBuf> {
    let mut dirs = Vec::new();
    let Ok(rd) = std::fs::read_dir("/snap/steam") else { return dirs };
    for entry in rd.flatten() {
        let p = entry.path();
        if p.is_dir() && entry.file_name().to_str().map(|s| s.chars().all(|c| c.is_ascii_digit())).unwrap_or(false) {
            dirs.push(p);
        }
    }
    dirs.sort_by(|a, b| b.cmp(a));
    dirs
}

pub fn game_app_id(game: &str) -> u32 {
    match game {
        "cs2" => 730,
        "dota2" => 570,
        _ => 0,
    }
}  

pub fn default_launch_opts(game: &str) -> &'static str {
    match game {
        "cs2" => concat!(
            "-novid -nojoy -low -console -w 1280 -h 720 +fps_max 30 ",
            "+cl_disablehtmlmotd 1 -nosound -nopreload +r_dynamic 0 +mat_queue_mode 0 ",
            "+con_enable 1 -condebug +r_player_visibility_mode 1 ",
            "+cl_radar_scale 0.30 +cl_radar_rotate 0 +cl_radar_always_centered 0 +cl_hud_radar_scale 1.15 ",
            "+cl_hud_radar_map_additive 0 +cl_hud_radar_blur_background 1 +cl_hud_radar_background_alpha 1"
        ),
        "dota2" => "-novid -nojoy -low -w 640 -h 480 +fps_max 10 -map dota -nosound -nopreload",
        _ => "",
    }
}
