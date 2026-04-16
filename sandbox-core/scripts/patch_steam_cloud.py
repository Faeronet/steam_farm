#!/usr/bin/env python3
"""Вставить в localconfig.vdf для appid: "CloudEnabled" "0" — без модалки «Cloud Out of Date»."""
import os
import re
import sys


def find_block_end(s: str, open_brace: int) -> int:
    if open_brace >= len(s) or s[open_brace] != "{":
        return -1
    depth = 0
    for k in range(open_brace, len(s)):
        if s[k] == "{":
            depth += 1
        elif s[k] == "}":
            depth -= 1
            if depth == 0:
                return k
    return -1


def patch_localconfig(path: str, app: str) -> bool:
    try:
        with open(path, encoding="utf-8", errors="replace") as f:
            text = f.read()
    except OSError:
        return False
    needle = f'"{app}"'
    m = re.search(re.escape(needle) + r"\s*\{", text)
    if not m:
        return False
    j = m.end() - 1
    end = find_block_end(text, j)
    if end < 0:
        return False
    inner_start = j + 1
    inner_end = end
    inner = text[inner_start:inner_end]
    if re.search(r'"CloudEnabled"\s+"0"', inner):
        return False
    cloud_pat = r'"CloudEnabled"\s+"[^"]*"'
    if re.search(cloud_pat, inner):
        new_inner = re.sub(cloud_pat, '"CloudEnabled"\t\t"0"', inner, count=1)
    else:
        new_inner = '\n\t\t\t\t"CloudEnabled"\t\t"0"' + inner
    new_text = text[:inner_start] + new_inner + text[inner_end:]
    try:
        with open(path, "w", encoding="utf-8") as f:
            f.write(new_text)
    except OSError:
        return False
    print(f"[sfarm] CloudEnabled=0 for app {app} in {path}", file=sys.stderr)
    return True


def main() -> None:
    app = os.environ.get("COMPAT_APP_ID", "").strip()
    home = os.environ.get("HOME", "")
    if not app or app == "0" or not home:
        return
    root = os.path.join(home, ".local/share/Steam/userdata")
    if not os.path.isdir(root):
        return
    for ent in os.listdir(root):
        for name in ("localconfig.vdf", "sharedconfig.vdf"):
            path = os.path.join(root, ent, "config", name)
            if os.path.isfile(path):
                patch_localconfig(path, app)


if __name__ == "__main__":
    main()
