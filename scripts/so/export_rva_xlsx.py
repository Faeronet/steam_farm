#!/usr/bin/env python3
"""Экспорт so/rva.xlsx → so/rva_table.json (колонки imagebase_offset_hex, block, опционально label).

Колонки A (RVA hex), B (block), C (подпись/комментарий — для frida_rva_watch.py и поиска по «entity list»).

Примеры:
  python3 scripts/so/export_rva_xlsx.py .
  python3 scripts/so/export_rva_xlsx.py . so/rva.xlsx so/rva_table.json
  python3 scripts/so/export_rva_xlsx.py . so/rva1.xlsx so/rva_table_1.json
  python3 scripts/so/export_rva_xlsx.py . so/rva2.xlsx so/rva_table_2.json
"""
import json
import re
import sys
import zipfile
import xml.etree.ElementTree as ET

NS = {"m": "http://schemas.openxmlformats.org/spreadsheetml/2006/main"}


def inline_str(c):
    is_ = c.find("m:is", NS)
    if is_ is None:
        return ""
    t = is_.find("m:t", NS)
    return (t.text or "").strip() if t is not None else ""


def cell_col_letter(ref: str) -> str:
    if not ref:
        return ""
    return "".join(ch for ch in ref if ch.isalpha())


def main():
    root_dir = sys.argv[1] if len(sys.argv) > 1 else "."
    xlsx = sys.argv[2] if len(sys.argv) > 2 else f"{root_dir}/so/rva.xlsx"
    out_path = sys.argv[3] if len(sys.argv) > 3 else f"{root_dir}/so/rva_table.json"
    z = zipfile.ZipFile(xlsx)
    xmlroot = ET.fromstring(z.read("xl/worksheets/sheet1.xml"))
    z.close()
    entries = []
    for row in xmlroot.findall("m:sheetData/m:row", NS):
        if row.get("r") == "1":
            continue
        col_a, col_b, col_c = "", "", ""
        for c in row.findall("m:c", NS):
            ref = c.get("r") or ""
            letter = cell_col_letter(ref)
            if letter == "A":
                col_a = inline_str(c)
            elif letter == "B":
                col_b = inline_str(c)
            elif letter == "C":
                col_c = inline_str(c)
        if not col_a:
            continue
        hx = col_a.strip().lower().replace("0x", "").strip()
        if not re.fullmatch(r"[0-9a-f]+", hx):
            continue
        u = int(hx, 16)
        item = {"rva_hex": "0x%X" % u, "rva": u, "block": col_b.strip() or ""}
        if col_c.strip():
            item["label"] = col_c.strip()
        entries.append(item)
    doc = {
        "doc": "Экспорт из XLSX — qword-слоты (.data/.bss/.got). Колонка C: подпись для frida_rva_watch.py.",
        "doc_source": xlsx,
        "count": len(entries),
        "entries": entries,
    }
    with open(out_path, "w", encoding="utf-8") as f:
        json.dump(doc, f, ensure_ascii=False)
    print(out_path, "entries", len(entries))


if __name__ == "__main__":
    main()
