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

pub fn find_steam() -> Option<SteamPaths> {
    let home = std::env::var("REAL_HOME")
        .or_else(|_| std::env::var("HOME"))
        .ok()?;
    let candidates = [
        format!("{}/snap/steam/common/.local/share/Steam", home),
        format!("{}/.local/share/Steam", home),
        format!("{}/.steam/steam", home),
        format!("{}/.steam/debian-installation", home),
    ];

    for root_str in &candidates {
        let root = PathBuf::from(root_str);
        let common = root.join("steamapps/common");
        if !common.is_dir() {
            continue;
        }

        let steam_binary = root.join("ubuntu12_32/steam");
        if !steam_binary.exists() {
            continue;
        }

        let (ld_linux_32, lib_paths) = build_runtime_paths(&root);

        return Some(SteamPaths {
            linux64: root.join("linux64"),
            ubuntu12_32: root.join("ubuntu12_32"),
            common,
            steam_binary,
            ld_linux_32,
            lib_paths,
            root,
        });
    }
    None
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
        "cs2" => "-novid -nojoy -low -vulkan -w 640 -h 480 +fps_max 15 +cl_disablehtmlmotd 1 -nosound",
        "dota2" => "-novid -nojoy -low -vulkan -w 640 -h 480 +fps_max 15 -map dota -nosound",
        _ => "",
    }
}
