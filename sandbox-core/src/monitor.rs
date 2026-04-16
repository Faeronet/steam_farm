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
    // After ") state": ppid(1) pgrp(2) session(3) tty(4) tpgid(5) flags(6)
    // minflt(7) cminflt(8) majflt(9) cmajflt(10) utime(11) stime(12)
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

pub async fn run(game_pid: Option<u32>) {
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

    loop {
        tokio::time::sleep(Duration::from_secs(5)).await;

        // Collect all descendants of both the game PID and our supervisor PID
        // to catch processes that may have been reparented
        let mut all_pids = collect_descendant_pids(supervisor_pid);
        if !all_pids.contains(&pid) {
            let game_pids = collect_descendant_pids(pid);
            for p in game_pids {
                if !all_pids.contains(&p) {
                    all_pids.push(p);
                }
            }
        }

        // Exclude the supervisor itself and its direct helper processes (Xvfb, x11vnc)
        // by only counting processes that are not the supervisor
        let monitored: Vec<u32> = all_pids.into_iter()
            .filter(|&p| p != supervisor_pid)
            .collect();

        // Сообщаем PID cs2 в desktop: у пользователя pgrep не видит root/hidepid.
        let mut cs2_child: Option<u32> = None;
        for &p in &monitored {
            let Ok(content) = fs::read_to_string(format!("/proc/{}/comm", p)) else {
                continue;
            };
            if content.trim() == "cs2" {
                cs2_child = Some(p);
                break;
            }
        }
        if cs2_child != last_cs2_emit {
            if let Some(pid) = cs2_child {
                ipc::emit(&IpcEvent::Cs2Pid { pid });
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
        // Сумма utime+stime по процессам даёт «проценты по ядрам» (до N×100%). Делим на число
        // онлайн-CPU — отображаем долю машины 0…100%, как ожидают в UI.
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
