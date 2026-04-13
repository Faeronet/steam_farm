#!/usr/bin/env python3
"""
Ищет кандидатов в глобалы libclient.so по результату frida_rva_table_probe.py (TSV).

Важно:
  • config/cs2_dumper/offsets.json — RVA для Windows client.dll; совпадения по колонке rva
    с таблицей libclient **почти не будет** (другой бинарник).
  • Реальные Linux dw_* даёт sigscan (.text) или ручной libclient_offsets.json.
  • Этот скрипт отбирает слоты .data/.got, где qword — указатель внутри [base, base+size),
    плюс сводку по ptr_ok.

Пример:
  python3 scripts/so/find_libclient_globals.py /tmp/rva_probe_full.tsv \\
      --offsets-json config/cs2_dumper/offsets.json \\
      --out-json so/libclient_globals_candidates.json
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path

# Поля, которые заполняет sigscan / config (для отчёта и grep по имени)
WANT_KEYS = [
    "dw_local_player_pawn",
    "dw_entity_list",
    "dw_local_player_controller",
    "dw_view_matrix",
    "dw_game_entity_system",
    "dw_game_entity_system_highest_index",
    "dw_global_vars",
    "dw_game_rules",
    "dw_csgo_input",
    "dw_view_render",
    "dw_glow_manager",
    "dw_sensitivity",
]


def parse_tsv_header(line: str) -> tuple[int | None, int | None]:
    base_m = re.search(r"base=(0x[0-9a-fA-F]+)", line)
    size_m = re.search(r"size=(\d+)", line)
    base = int(base_m.group(1), 16) if base_m else None
    size = int(size_m.group(1)) if size_m else None
    return base, size


def load_windows_offsets(path: Path) -> dict[str, int]:
    root = json.loads(path.read_text(encoding="utf-8"))
    cd = root.get("client.dll") or {}
    return {k: int(v) for k, v in cd.items() if isinstance(v, int)}


def main() -> int:
    ap = argparse.ArgumentParser(description="Поиск кандидатов глобалов libclient по TSV Frida")
    ap.add_argument("tsv", type=Path, help="Выход frida_rva_table_probe.py")
    ap.add_argument(
        "--offsets-json",
        type=Path,
        default=None,
        help="config/cs2_dumper/offsets.json — проверка совпадения rva (Windows)",
    )
    ap.add_argument(
        "--default-size",
        type=int,
        default=75618824,
        help="Размер модуля, если нет в заголовке TSV",
    )
    ap.add_argument("--out-json", type=Path, default=None, help="JSON с кандидатами")
    ap.add_argument("--max-internal", type=int, default=80, help="Макс. строк «указатель внутрь модуля»")
    args = ap.parse_args()

    if not args.tsv.is_file():
        print(f"Нет файла: {args.tsv}", file=sys.stderr)
        return 1

    lines = args.tsv.read_text(encoding="utf-8", errors="replace").splitlines()
    base, size = None, None
    for ln in lines[:5]:
        if ln.startswith("#") and "base=" in ln:
            base, size = parse_tsv_header(ln)
            break
    if base is None:
        print("Не найден base= в первых строках TSV", file=sys.stderr)
        return 1
    if size is None:
        size = args.default_size

    end = base + size

    rva_set: set[int] = set()
    rows: list[dict] = []
    for ln in lines:
        if not ln or ln.startswith("#"):
            continue
        if ln.startswith("rva_hex"):
            continue
        parts = ln.split("\t")
        if len(parts) < 6:
            continue
        rva_hex, rva_s, block, raw_s, ptr_s, *_rest = parts[0], parts[1], parts[2], parts[3], parts[4]
        err = parts[6] if len(parts) > 6 else ""
        if err.strip():
            continue
        try:
            rva = int(rva_s)
        except ValueError:
            continue
        rva_set.add(rva)
        if not raw_s.startswith("0x"):
            continue
        raw = int(raw_s, 16)
        ptr_ok = ptr_s.strip() == "True"
        rows.append(
            {
                "rva_hex": rva_hex,
                "rva": rva,
                "block": block,
                "raw_u64": raw,
                "raw_hex": raw_s,
                "ptr_ok": ptr_ok,
            }
        )

    # Совпадения Windows RVA с колонкой rva (редко)
    win_hits: list[tuple[str, int]] = []
    if args.offsets_json and args.offsets_json.is_file():
        win = load_windows_offsets(args.offsets_json)
        for name, rv in sorted(win.items(), key=lambda x: -x[1]):
            if rv in rva_set:
                win_hits.append((name, rv))

    internal: list[dict] = []
    for r in rows:
        if not r["ptr_ok"]:
            continue
        v = r["raw_u64"]
        if base <= v < end:
            internal.append(
                {
                    **r,
                    "points_to_rva": v - base,
                    "points_to_hex": hex(v - base),
                }
            )

    internal.sort(key=lambda x: (x["block"], x["rva"]))

    by_block: dict[str, int] = {}
    for x in internal:
        by_block[x["block"]] = by_block.get(x["block"], 0) + 1
    data_got = [x for x in internal if x["block"] in (".data", ".got")]

    report = {
        "doc": "Кандидаты: qword в .data/.got/.bss с ptr_ok, значение указывает внутрь загрузочного образа libclient.",
        "module_base_hex": hex(base),
        "module_size": size,
        "module_end_hex": hex(end),
        "tsv": str(args.tsv.resolve()),
        "total_rows": len(rows),
        "windows_rva_exact_hits_in_table": [{"name": n, "rva": v} for n, v in win_hits],
        "windows_offsets_note": "offsets.json client.dll — RVA под Windows; для Linux используйте sigscan или libclient_offsets.json.",
        "internal_pointer_slots": internal[: args.max_internal],
        "internal_pointer_count_by_block": by_block,
        "internal_pointer_slots_data_got": data_got[: args.max_internal],
        "wanted_field_names": WANT_KEYS,
    }

    print("=== libclient (Frida TSV) ===")
    print(f"base={hex(base)} size={size} end={hex(end)}")
    print(f"строк в TSV: {len(rows)}")
    print(f"совпадений rva с client.dll из offsets.json: {len(win_hits)}")
    if not win_hits:
        print("  (ожидаемо 0: таблица — libclient.so, offsets.json — Windows client.dll)")
    else:
        for n, v in win_hits:
            print(f"  {n}  rva={v}")
    print(f"слотов ptr_ok с указателем внутрь [base,end): {len(internal)}")
    print("  по секциям:", ", ".join(f"{k}={v}" for k, v in sorted(by_block.items())))
    print(f"  из них .data/.got: {len(data_got)} (глобалы чаще в .data/.got; .bss — куча объектов)")
    print("--- первые записи (.data/.got, указатель внутрь модуля) ---")
    for x in data_got[: min(25, len(data_got))]:
        print(
            f"  {x['rva_hex']} {x['block']:6}  raw={x['raw_hex']}  -> points_to_rva={x['points_to_hex']}"
        )
    print("--- первые записи (все секции) ---")
    for x in internal[: min(25, len(internal))]:
        print(
            f"  {x['rva_hex']} {x['block']:6}  raw={x['raw_hex']}  -> points_to_rva={x['points_to_hex']}"
        )

    if args.out_json:
        args.out_json.parent.mkdir(parents=True, exist_ok=True)
        args.out_json.write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
        print(f"\nJSON: {args.out_json}")

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
