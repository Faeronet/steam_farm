#!/usr/bin/env python3
"""
Frida: чтение qword по таблице RVA (so/rva_table.json и др.) на протяжении минут — не один прогон.

Пока вы играете, скрипт повторяет опрос: фокус-строки (по ключевым словам в label / именам из offsets.json)
каждый тик, остальную таблицу — чанками по кругу. В конце — сводка по устойчивости значений (mode, % совпадений).

Зависимости: .venv-frida с frida, frida-tools.

Примеры:
  FRIDA_PID=$(pgrep -n cs2) python3 scripts/so/frida_rva_watch.py "$FRIDA_PID" --duration 300 --table so/rva_table.json \\
    --log-file /tmp/frida_rva_watch.log --out-tsv /tmp/frida_rva_watch_summary.tsv

  # таблицы rva1 / rva2 после экспорта:
  # python3 scripts/so/export_rva_xlsx.py . so/rva1.xlsx so/rva_table_1.json
  # python3 scripts/so/frida_rva_watch.py $(pgrep -n cs2) --table so/rva_table_1.json --duration 300

Ключевые слова (подстроки в label, без учёта регистра): entity, pawn, controller, matrix, game ent, …
и опечатки вроде «энтитилист» — см. DEFAULT_KEYWORDS.
"""
from __future__ import annotations

import argparse
import json
import sys
import time
from collections import Counter
from pathlib import Path

try:
    import frida
except ImportError:
    print("pip install frida frida-tools", file=sys.stderr)
    raise

# Как в cs2mem_linux.go ptrOK
def ptr_ok(u: int) -> bool:
    return 0x10000 <= u < 0x7FFFFFFFFFFF


FRIDA_AGENT = r"""
(function () {
  'use strict';
  var base = null;
  var mod = null;
  var nameHint = %s;

  function findMod() {
    if (mod) return;
    var m = Process.findModuleByName('libclient.so');
    if (m) { mod = m; base = m.base; return; }
    var all = Process.enumerateModules();
    for (var i = 0; i < all.length; i++) {
      var n = all[i].name;
      if (!n) continue;
      if (n.indexOf(nameHint) >= 0 && (n.indexOf('.so') >= 0 || n.indexOf('client') >= 0)) {
        mod = all[i];
        base = mod.base;
        return;
      }
    }
    throw new Error('libclient not found (hint: ' + nameHint + ')');
  }

  function addrFromRvaHex(hexStr) {
    findMod();
    var off = ptr(hexStr);
    return base.add(off);
  }

  rpc.exports = {
    meta: function () {
      findMod();
      var end = base.add(mod.size);
      return {
        module: mod.name,
        path: mod.path,
        base: base.toString(),
        size: mod.size,
        end: end.toString(),
      };
    },
    probeBatchHex: function (hexList) {
      findMod();
      var out = [];
      for (var i = 0; i < hexList.length; i++) {
        try {
          var a = addrFromRvaHex(hexList[i]);
          var v = a.readU64();
          out.push({ ok: true, v: v.toString() });
        } catch (e) {
          out.push({ ok: false, err: e.message || String(e) });
        }
      }
      return out;
    },
  };
})();
"""


def load_rva_table(path: Path) -> dict:
    with open(path, "rb") as f:
        return json.load(f)


def load_client_dll_offsets(path: Path) -> dict[str, int]:
    with open(path, encoding="utf-8") as f:
        root = json.load(f)
    cd = root.get("client.dll") or root.get("client.dll".lower())
    if not isinstance(cd, dict):
        return {}
    out = {}
    for k, v in cd.items():
        if isinstance(v, int) and v >= 0:
            out[str(k)] = v
    return out


DEFAULT_KEYWORDS = (
    "entity",
    "entitylist",
    "entity_list",
    "dw_entity",
    "entlist",
    "local player",
    "localplayer",
    "pawn",
    "dw_pawn",
    "controller",
    "viewmatrix",
    "view matrix",
    "matrix",
    "gameentity",
    "gameentitysystem",
    "ges",
    "global",
    "input",
    # опечатки / транслит
    "энтити",
    "энтитилист",
    "ентитилист",
    "ентити",
)


def normalize_label(s: str) -> str:
    return "".join(s.lower().split())


def label_matches_keywords(label: str, keywords: tuple[str, ...]) -> bool:
    if not (label or "").strip():
        return False
    low = label.lower()
    n = normalize_label(label)
    for kw in keywords:
        k = kw.strip().lower()
        if not k:
            continue
        if k in low:
            return True
        kn = normalize_label(kw)
        if kw and kn in n:
            return True
    return False


def entry_hex(e: dict) -> str:
    return str(e.get("rva_hex") or hex(int(e["rva"]))).strip()


def rva_to_names_from_offsets(hi: dict[str, int]) -> dict[int, list[str]]:
    m: dict[int, list[str]] = {}
    for name, rv in hi.items():
        m.setdefault(rv, []).append(name)
    return m


def build_focus_indices(
    entries: list[dict],
    keywords: tuple[str, ...],
    rva_to_names: dict[int, list[str]],
    name_substrings: tuple[str, ...],
) -> set[int]:
    """Индексы строк: совпадение label с keywords или имя из dumper в rva_to_names."""
    focus: set[int] = set()
    for i, e in enumerate(entries):
        rva = int(e["rva"])
        label = str(e.get("label") or "")
        if label_matches_keywords(label, keywords):
            focus.add(i)
            continue
        if rva in rva_to_names:
            names = ",".join(rva_to_names[rva]).lower()
            for sub in name_substrings:
                if sub and sub in names:
                    focus.add(i)
                    break
    return focus


def probe_batch(script, chunk: list[dict], batch: int):
    """Yields (rva, entry, u64|None, err|None)."""
    i = 0
    while i < len(chunk):
        part = chunk[i : i + batch]
        hex_list = [entry_hex(e) for e in part]
        raw = script.exports_sync.probe_batch_hex(hex_list)
        for j, e in enumerate(part):
            row = raw[j]
            rva = int(e["rva"])
            if not row.get("ok"):
                yield rva, e, None, row.get("err") or "?"
                continue
            u = int(row["v"])
            yield rva, e, u, None
        i += batch


def main() -> int:
    ap = argparse.ArgumentParser(description="Frida: длительный watch qword по таблице RVA (CS2)")
    ap.add_argument("pid", type=int, help="PID процесса cs2")
    ap.add_argument(
        "--table",
        type=Path,
        default=None,
        help="JSON таблица (по умолчанию repo/so/rva_table.json)",
    )
    ap.add_argument("--duration", type=float, default=300.0, help="Секунд наблюдения (по умолчанию 300 = 5 мин)")
    ap.add_argument(
        "--interval",
        type=float,
        default=2.5,
        help="Минимум секунд между циклами «фокус + чанк» (после завершения чтения)",
    )
    ap.add_argument("--chunk-size", type=int, default=2500, help="Сколько строк таблицы за один проход ротации")
    ap.add_argument("--batch", type=int, default=128, help="Размер RPC-батча Frida")
    ap.add_argument(
        "--keywords",
        type=str,
        default="",
        help="Дополнительные подстроки для фокуса (через запятую), кроме встроенных",
    )
    ap.add_argument(
        "--offsets-json",
        type=Path,
        default=None,
        help="config/cs2_dumper/offsets.json — подсветка RVAs из dumper (имена для фокуса)",
    )
    ap.add_argument(
        "--offsets-name-substr",
        type=str,
        default="entity,localplayer,controller,view,matrix,gameentity",
        help="Подстроки имён в offsets (через запятую) для авто-фокуса по RVAs",
    )
    ap.add_argument("--module-substr", default="libclient", help="Подстрока имени модуля")
    ap.add_argument("--log-file", type=Path, default=None, help="Доп. лог")
    ap.add_argument("--out-tsv", type=Path, default=None, help="Итоговая сводка по фокус-строкам")
    ap.add_argument("--verbose", action="store_true", help="Прогресс в stderr")
    args = ap.parse_args()

    repo = Path(__file__).resolve().parents[2]
    table_path = args.table or (repo / "so" / "rva_table.json")
    if not table_path.is_file():
        print(f"Нет таблицы: {table_path}", file=sys.stderr)
        return 1

    tab = load_rva_table(table_path)
    entries = tab.get("entries") or []
    if not entries:
        print("Пустая таблица entries", file=sys.stderr)
        return 1

    extra_kw = tuple(x.strip() for x in args.keywords.split(",") if x.strip())
    keywords: tuple[str, ...] = tuple(dict.fromkeys(DEFAULT_KEYWORDS + extra_kw))

    hi: dict[str, int] = {}
    off_path = args.offsets_json or (repo / "config" / "cs2_dumper" / "offsets.json")
    if off_path.is_file():
        hi = load_client_dll_offsets(off_path)
    rva_to_names = rva_to_names_from_offsets(hi)
    name_substrings = tuple(x.strip().lower() for x in args.offsets_name_substr.split(",") if x.strip())

    focus_idx = build_focus_indices(entries, keywords, rva_to_names, name_substrings)
    focus_entries = [entries[i] for i in sorted(focus_idx)]

    log_fp = None
    if args.log_file:
        args.log_file.parent.mkdir(parents=True, exist_ok=True)
        log_fp = open(args.log_file, "a", encoding="utf-8")
        log_fp.write(
            f"\n--- frida_rva_watch {time.strftime('%Y-%m-%d %H:%M:%S')} pid={args.pid} table={table_path} ---\n"
        )
        log_fp.write(f"entries={len(entries)} focus={len(focus_entries)} duration={args.duration}s\n")

    def log(msg: str) -> None:
        if log_fp:
            log_fp.write(msg + "\n")
            log_fp.flush()
        if args.verbose:
            print(msg, file=sys.stderr)

    hint_js = json.dumps(args.module_substr)
    session = frida.attach(args.pid)
    script = session.create_script(FRIDA_AGENT % hint_js)
    script.load()
    meta = script.exports_sync.meta()
    log(f"meta: {meta}")

    # rva -> список прочитанных u64 (только фокус)
    focus_vals: dict[int, list[int]] = {}
    focus_rvas = {int(e["rva"]) for e in focus_entries}

    for rva in focus_rvas:
        focus_vals[rva] = []

    chunk_size = max(100, min(args.chunk_size, len(entries)))
    batch = max(1, min(args.batch, 512))
    rot = 0
    tick = 0
    t0 = time.time()
    deadline = t0 + args.duration

    print(
        f"# watch pid={args.pid} table={table_path} entries={len(entries)} focus={len(focus_entries)} duration={args.duration}s interval>={args.interval}s chunk={chunk_size}",
        flush=True,
    )
    if focus_entries:
        print("# focus rows (label / dumper name):", flush=True)
        for e in focus_entries[:80]:
            rva = int(e["rva"])
            lab = e.get("label") or ""
            hits = ",".join(rva_to_names.get(rva, []))
            print(f"#   rva={e.get('rva_hex')} label={lab!r} dumper={hits}", flush=True)
        if len(focus_entries) > 80:
            print(f"#   ... +{len(focus_entries) - 80} more", flush=True)
    else:
        print(
            "# нет фокус-строк: добавьте колонку C (label) в xlsx или проверьте --keywords / offsets.json",
            flush=True,
        )

    def run_chunk(chunk: list[dict], record_focus: bool) -> None:
        for rva, _e, u, _err in probe_batch(script, chunk, batch):
            if record_focus and rva in focus_vals and u is not None:
                focus_vals[rva].append(u)

    while time.time() < deadline:
        loop_start = time.time()
        tick += 1

        # 1) фокус каждый цикл
        if focus_entries:
            run_chunk(focus_entries, True)

        # 2) ротация по всей таблице
        start = (rot * chunk_size) % len(entries)
        out = []
        for k in range(chunk_size):
            out.append(entries[(start + k) % len(entries)])
        run_chunk(out, False)
        rot += 1

        elapsed = time.time() - loop_start
        if args.verbose or tick % 10 == 1:
            log(f"tick={tick} loop_s={elapsed:.2f} focus_samples={sum(len(v) for v in focus_vals.values())}")

        sleep = args.interval - elapsed
        if sleep > 0:
            time.sleep(sleep)

    # сводка по фокусу
    lines_out: list[str] = []
    lines_out.append(
        "rva_hex\trva\tlabel\tn_samples\tn_ptr_ok\tmode_hex\tmode_pct\tunique_n\tdumper_names"
    )
    for e in sorted(focus_entries, key=lambda x: int(x["rva"])):
        rva = int(e["rva"])
        vals = focus_vals.get(rva, [])
        n = len(vals)
        if n == 0:
            lines_out.append(
                f'{e.get("rva_hex")}\t{rva}\t{(e.get("label") or "")
                .replace(chr(9), " ")}\t0\t0\t\t\t0\t{",".join(rva_to_names.get(rva, []))}'
            )
            continue
        n_ptr = sum(1 for v in vals if ptr_ok(v))
        ctr = Counter(vals)
        mode_v, mode_c = ctr.most_common(1)[0]
        mode_pct = 100.0 * mode_c / n
        uniq = len(ctr)
        lab = (e.get("label") or "").replace("\t", " ")
        lines_out.append(
            f'{e.get("rva_hex")}\t{rva}\t{lab}\t{n}\t{n_ptr}\t0x{mode_v:x}\t{mode_pct:.1f}\t{uniq}\t{",".join(rva_to_names.get(rva, []))}'
        )

    print("\n# --- summary (focus rows) ---", flush=True)
    for line in lines_out:
        print(line, flush=True)

    if args.out_tsv:
        args.out_tsv.parent.mkdir(parents=True, exist_ok=True)
        args.out_tsv.write_text("\n".join(lines_out) + "\n", encoding="utf-8")
        print(f"# wrote {args.out_tsv}", flush=True)

    if log_fp:
        log_fp.close()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
