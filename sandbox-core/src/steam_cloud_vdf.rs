//! Правка localconfig.vdf: CloudEnabled=0 для appid (без модалки «Cloud Out of Date»).
//! Поиск блока `"<appid>" { ... }` по всему файлу (как scripts/patch_steam_cloud.py), затем вставка в блок "apps" при отсутствии.

use std::fs;
use std::path::Path;

fn find_matching_brace(text: &str, open_idx: usize) -> Option<usize> {
    let b = text.as_bytes();
    if open_idx >= b.len() || b[open_idx] != b'{' {
        return None;
    }
    let mut depth = 0i32;
    let mut i = open_idx;
    while i < b.len() {
        match b[i] {
            b'{' => depth += 1,
            b'}' => {
                depth -= 1;
                if depth == 0 {
                    return Some(i);
                }
            }
            _ => {}
        }
        i += 1;
    }
    None
}

fn find_apps_block(text: &str) -> Option<(usize, usize)> {
    let marker = "\"apps\"";
    let pos = text.find(marker)?;
    let after = text.get(pos + marker.len()..)?;
    let mut skip = 0usize;
    for (i, c) in after.char_indices() {
        if c.is_whitespace() {
            skip = i + c.len_utf8();
        } else {
            break;
        }
    }
    let rest = after.get(skip..)?;
    if !rest.starts_with('{') {
        return None;
    }
    let open = pos + marker.len() + skip;
    let close = find_matching_brace(text, open)?;
    Some((open, close))
}

/// Как Python: любое вхождение `"730"` с `{` далее в файле (не только под первым `"apps"`).
fn find_app_block_range(text: &str, inner_start: usize, inner_end: usize, app_id: u32) -> Option<(usize, usize)> {
    let needle = format!("\"{}\"", app_id);
    let region = &text[inner_start..inner_end];
    let mut offset = 0usize;
    while offset < region.len() {
        let Some(rel) = region[offset..].find(&needle) else {
            break;
        };
        let abs = inner_start + offset + rel;
        let after_key = abs + needle.len();
        let tail = text.get(after_key..inner_end)?;
        let mut j = 0usize;
        for (k, c) in tail.char_indices() {
            if c.is_whitespace() {
                j = k + c.len_utf8();
            } else {
                break;
            }
        }
        if tail.get(j..).unwrap_or("").starts_with('{') {
            let open_brace = after_key + j;
            let close = find_matching_brace(text, open_brace)?;
            return Some((open_brace, close));
        }
        offset += rel + needle.len();
    }
    None
}

fn insert_app_block(text: &mut String, apps_close: usize, app_id: u32) -> bool {
    let block = format!(
        "\n\t\t\t\t\"{}\"\n\t\t\t\t{{\n\t\t\t\t\t\"CloudEnabled\"\t\t\"0\"\n\t\t\t\t}}",
        app_id
    );
    text.insert_str(apps_close, &block);
    true
}

fn apply_patch_inner(text: &mut String, open_brace: usize, close_brace: usize) -> bool {
    let i0 = open_brace + 1;
    let i1 = close_brace;
    let inner = text.get(i0..i1).unwrap_or("");
    let Some(new_inner) = patch_app_inner(inner) else {
        return false;
    };
    text.replace_range(i0..i1, &new_inner);
    true
}

/// Возвращает новый inner блок приложения или None, если уже CloudEnabled "0".
fn patch_app_inner(inner: &str) -> Option<String> {
    let key = "\"CloudEnabled\"";
    if let Some(idx) = inner.find(key) {
        let mut p = idx + key.len();
        let b = inner.as_bytes();
        while p < b.len() && b[p].is_ascii_whitespace() {
            p += 1;
        }
        if p >= b.len() || b[p] != b'"' {
            return None;
        }
        p += 1;
        let val_start = p;
        let val_end = val_start + inner.get(val_start..)?.find('"')?;
        let val = &inner[val_start..val_end];
        if val == "0" {
            return None;
        }
        let mut s = String::with_capacity(inner.len());
        s.push_str(&inner[..val_start]);
        s.push('0');
        s.push_str(&inner[val_end..]);
        return Some(s);
    }
    Some(format!(
        "\n\t\t\t\t\t\"CloudEnabled\"\t\t\"0\"{}",
        inner
    ))
}

pub fn patch_localconfig_str(text: &mut String, app_id: u32) -> bool {
    let len = text.len();
    if let Some((open_brace, close_brace)) = find_app_block_range(text, 0, len, app_id) {
        return apply_patch_inner(text, open_brace, close_brace);
    }
    if let Some((_apps_open, apps_close)) = find_apps_block(text) {
        return insert_app_block(text, apps_close, app_id);
    }
    false
}

pub fn patch_localconfig_file(path: &Path, app_id: u32) -> Result<bool, String> {
    let mut text = fs::read_to_string(path).map_err(|e| e.to_string())?;
    let before = text.clone();
    if !patch_localconfig_str(&mut text, app_id) {
        return Ok(false);
    }
    if text == before {
        return Ok(false);
    }
    fs::write(path, &text).map_err(|e| e.to_string())?;
    eprintln!(
        "[sfarm] CloudEnabled=0 for app {} in {}",
        app_id,
        path.display()
    );
    Ok(true)
}

pub fn patch_all_userdata_cloud(home: &Path, app_id: u32) -> Result<(), String> {
    if app_id == 0 {
        return Ok(());
    }
    let ud = home.join(".local/share/Steam/userdata");
    if !ud.is_dir() {
        return Ok(());
    }
    for ent in fs::read_dir(&ud).map_err(|e| e.to_string())? {
        let ent = ent.map_err(|e| e.to_string())?;
        let base = ent.path();
        for name in ["config/localconfig.vdf", "config/sharedconfig.vdf"] {
            let p = base.join(name);
            if p.is_file() {
                let _ = patch_localconfig_file(&p, app_id);
            }
        }
    }
    Ok(())
}
