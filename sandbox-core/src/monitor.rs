use std::fs;
use std::time::{Duration, Instant};

use crate::ipc::{self, IpcEvent};

struct ProcStat {
    utime: u64,
    stime: u64,
}

fn read_proc_stat(pid: u32) -> Option<ProcStat> {
    let content = fs::read_to_string(format!("/proc/{}/stat", pid)).ok()?;
    let close_paren = content.rfind(')')?;
    let fields: Vec<&str> = content[close_paren + 2..].split_whitespace().collect();
    let utime = fields.get(11)?.parse().ok()?;
    let stime = fields.get(12)?.parse().ok()?;
    Some(ProcStat { utime, stime })
}

fn read_memory_kb(pid: u32) -> u64 {
    let Ok(content) = fs::read_to_string(format!("/proc/{}/status", pid)) else {
        return 0;
    };
    for line in content.lines() {
        if let Some(rest) = line.strip_prefix("VmRSS:") {
            if let Some(kb_str) = rest.trim().strip_suffix("kB") {
                return kb_str.trim().parse().unwrap_or(0);
            }
        }
    }
    0
}

fn collect_descendant_pids(root: u32) -> Vec<u32> {
    let mut result = vec![root];
    let mut queue = vec![root];

    while let Some(parent) = queue.pop() {
        let Ok(dir) = fs::read_dir("/proc") else { break };
        for entry in dir.flatten() {
            let name = entry.file_name();
            let Some(name_str) = name.to_str() else { continue };
            let Ok(pid) = name_str.parse::<u32>() else { continue };

            let Ok(stat) = fs::read_to_string(format!("/proc/{}/stat", pid)) else { continue };
            let Some(close) = stat.rfind(')') else { continue };
            let fields: Vec<&str> = stat[close + 2..].split_whitespace().collect();
            if let Some(ppid_str) = fields.get(1) {
                if ppid_str.parse::<u32>().ok() == Some(parent) && !result.contains(&pid) {
                    result.push(pid);
                    queue.push(pid);
                }
            }
        }
    }
    result
}

/// comm==cs2 или путь linuxsteamrt64/cs2 в cmdline (snap может не класть потомка в дерево супервизора).
fn is_cs2_process(pid: u32) -> bool {
    let Ok(comm) = fs::read_to_string(format!("/proc/{}/comm", pid)) else {
        return false;
    };
    if comm.trim() == "cs2" {
        return true;
    }
    let Ok(cmd) = fs::read(format!("/proc/{}/cmdline", pid)) else {
        return false;
    };
    let lossy = String::from_utf8_lossy(&cmd);
    if lossy.contains("linuxsteamrt64/cs2") {
        return true;
    }
    // Репаковки / proot
    lossy.contains("Counter-Strike Global Offensive") && lossy.contains("cs2")
}

/// Совпадает с Go environMatchesDisplay: SFARM_DISPLAY, DISPLAY=:N, DISPLAY=:N.0
fn environ_matches_display(pid: u32, display: u16) -> bool {
    let Ok(data) = fs::read(format!("/proc/{}/environ", pid)) else {
        return false;
    };
    for needle in [
        format!("SFARM_DISPLAY={}\0", display),
        format!("DISPLAY=:{}\0", display),
        format!("DISPLAY=:{}.0\0", display),
    ] {
        let n = needle.as_bytes();
        if data.len() >= n.len() && data.windows(n.len()).any(|w| w == n) {
            return true;
        }
    }
    false
}

fn all_numeric_pids() -> Vec<u32> {
    let mut out = Vec::new();
    let Ok(dir) = fs::read_dir("/proc") else {
        return out;
    };
    for e in dir.flatten() {
        let name = e.file_name();
        let Some(s) = name.to_str() else { continue };
        if let Ok(pid) = s.parse::<u32>() {
            out.push(pid);
        }
    }
    out.sort_unstable();
    out
}

/// В maps уже есть клиентская libclient (не ранний лаунчер без .so).
fn maps_has_game_libclient(pid: u32) -> bool {
    let Ok(s) = fs::read_to_string(format!("/proc/{}/maps", pid)) else {
        return false;
    };
    for line in s.lines() {
        let l = line.to_lowercase();
        if l.contains("steamclient") || l.contains("panorama") {
            continue;
        }
        if !l.contains("libclient") {
            continue;
        }
        if l.contains("linuxsteamrt64") {
            return true;
        }
        if l.contains("libclient.so")
            && (l.contains("counter-strike") || l.contains("csgo") || l.contains("steamapps"))
        {
            return true;
        }
    }
    false
}

fn collect_cs2_candidates(monitored: &[u32], display: u16) -> Vec<u32> {
    let mut out = Vec::new();
    let mut seen = std::collections::HashSet::new();
    for &p in monitored {
        if is_cs2_process(p) && seen.insert(p) {
            out.push(p);
        }
    }
    for p in all_numeric_pids() {
        if seen.contains(&p) {
            continue;
        }
        if is_cs2_process(p) && environ_matches_display(p, display) && seen.insert(p) {
            out.push(p);
        }
    }
    out
}

/// Только PID, где в `/proc/.../maps` уже есть игровой libclient (не ранний cs2 без .so).
/// Без fallback по RSS: иначе в IPC уходит лаунчер, desktop долбится в maps без libclient до таймаута.
fn pick_best_cs2_pid(candidates: &[u32]) -> Option<u32> {
    let with_so: Vec<u32> = candidates
        .iter()
        .copied()
        .filter(|&p| maps_has_game_libclient(p))
        .collect();
    if with_so.is_empty() {
        return None;
    }
    with_so.into_iter().max_by_key(|p| read_memory_kb(*p))
}

pub async fn run(game_pid: Option<u32>, display: u16) {
    let pid = match game_pid {
        Some(p) if p > 0 => p,
        _ => return,
    };

    let supervisor_pid = std::process::id();

    let clock_ticks = unsafe { libc::sysconf(libc::_SC_CLK_TCK) } as f64;
    if clock_ticks <= 0.0 {
        return;
    }

    let mut prev_total: u64 = 0;
    let mut prev_time = Instant::now();

    if let Some(stat) = read_proc_stat(pid) {
        prev_total = stat.utime + stat.stime;
    }

    let mut last_cs2_emit: Option<u32> = None;
    let mut loop_idx: u32 = 0;

    loop {
        // Первая итерация почти сразу; дальше чаще — чтобы перейти на PID с libclient.so в maps.
        let delay = if loop_idx == 0 {
            Duration::from_millis(300)
        } else if loop_idx < 120 {
            Duration::from_secs(1)
        } else {
            Duration::from_secs(5)
        };
        tokio::time::sleep(delay).await;
        loop_idx = loop_idx.saturating_add(1);

        let mut all_pids = collect_descendant_pids(supervisor_pid);
        if !all_pids.contains(&pid) {
            let game_pids = collect_descendant_pids(pid);
            for p in game_pids {
                if !all_pids.contains(&p) {
                    all_pids.push(p);
                }
            }
        }

        let monitored: Vec<u32> = all_pids
            .into_iter()
            .filter(|&p| p != supervisor_pid)
            .collect();

        let cands = collect_cs2_candidates(&monitored, display);
        let cs2_child = pick_best_cs2_pid(&cands);

        if cs2_child != last_cs2_emit {
            if let Some(cpid) = cs2_child {
                ipc::emit(&IpcEvent::Cs2Pid { pid: cpid });
            } else if last_cs2_emit.is_some() {
                ipc::emit(&IpcEvent::Cs2Pid { pid: 0 });
            }
            last_cs2_emit = cs2_child;
        }

        if monitored.is_empty() {
            if fs::metadata(format!("/proc/{}", pid)).is_err() {
                break;
            }
            ipc::emit(&IpcEvent::Stats { cpu: 0.0, memory_mb: 0 });
            continue;
        }

        let mut total_ticks: u64 = 0;
        let mut total_mem_kb: u64 = 0;
        for &p in &monitored {
            if let Some(stat) = read_proc_stat(p) {
                total_ticks += stat.utime + stat.stime;
            }
            total_mem_kb += read_memory_kb(p);
        }

        let now = Instant::now();
        let delta_secs = now.duration_since(prev_time).as_secs_f64();
        let ncpu = std::thread::available_parallelism()
            .map(|n| n.get() as f64)
            .unwrap_or(1.0)
            .max(1.0);
        let cpu = if delta_secs > 0.0 {
            let delta_ticks = total_ticks.saturating_sub(prev_total) as f64;
            ((delta_ticks / clock_ticks / delta_secs) * 100.0) / ncpu
        } else {
            0.0
        };

        ipc::emit(&IpcEvent::Stats {
            cpu,
            memory_mb: total_mem_kb / 1024,
        });

        prev_total = total_ticks;
        prev_time = now;
    }
}
