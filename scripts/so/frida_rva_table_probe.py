#!/usr/bin/env python3
"""
Чтение qword по таблице из so/rva.xlsx (экспорт → so/rva_table.json) в процессе CS2.

  frida-trace — для перехвата функций; здесь frida.attach + Memory.readU64 по RVA (как /rva-probe).

Зависимости:
  python3 -m venv .venv-frida && .venv-frida/bin/pip install frida frida-tools

Логи:
  По умолчанию только stdout (TSV). Подробности и ошибки Frida — в stderr или в файл:
    --log-file /tmp/frida_rva_probe.log

Пример:
  pgrep -n cs2
  python3 scripts/so/frida_rva_table_probe.py "$(pgrep -n cs2)" --block .data --limit 30 --log-file /tmp/frida_rva_probe.log

Запускать от того же uid, что и CS2; ptrace_scope: echo 0 | sudo tee /proc/sys/kernel/yama/ptrace_scope
"""

from __future__ import annotations

import argparse
import json
import sys
import time
from pathlib import Path

try:
    import frida
except ImportError:
    print("pip install frida frida-tools", file=sys.stderr)
    raise

# Как в cs2mem_linux.go ptrOK
def ptr_ok(u: int) -> bool:
    return 0x10000 <= u < 0x7FFFFFFFFFFF


# RVA передаём hex-строками: так RPC Frida не портит большие int в JS.
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
      var testHex = '0x42EC660';
      var testAddr = addrFromRvaHex(testHex);
      var testOk = false;
      var testErr = '';
      try {
        testAddr.readU64();
        testOk = true;
      } catch (e) {
        testErr = e.message || String(e);
      }
      return {
        module: mod.name,
        path: mod.path,
        base: base.toString(),
        size: mod.size,
        end: end.toString(),
        test_rva: testHex,
        test_addr: testAddr.toString(),
        test_read_ok: testOk,
        test_read_err: testErr,
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
    with open(path, 'rb') as f:
        return json.load(f)


def load_client_dll_offsets(path: Path) -> dict[str, int]:
    with open(path, encoding='utf-8') as f:
        root = json.load(f)
    cd = root.get('client.dll') or root.get('client.dll'.lower())
    if not isinstance(cd, dict):
        return {}
    out = {}
    for k, v in cd.items():
        if isinstance(v, int) and v >= 0:
            out[str(k)] = v
    return out


def main() -> int:
    ap = argparse.ArgumentParser(description='Frida: qword probe по so/rva_table.json (CS2 / libclient.so)')
    ap.add_argument('pid', type=int, help='PID процесса cs2')
    ap.add_argument('--table', type=Path, default=None, help='rva_table.json')
    ap.add_argument('--block', default='', help='Фильтр секции: .data, .bss, …')
    ap.add_argument('--offset', type=int, default=0, help='Сдвиг по отфильтрованному списку')
    ap.add_argument('--limit', type=int, default=300, help='Макс. строк')
    ap.add_argument('--batch', type=int, default=128, help='Размер батча')
    ap.add_argument('--only-ptr', action='store_true', help='Только ptr_ok qword')
    ap.add_argument('--highlight-offsets', type=Path, default=None, help='offsets.json для колонки dumper_hit')
    ap.add_argument('--module-substr', default='libclient', help='Подстрока имени модуля')
    ap.add_argument(
        '--log-file',
        type=Path,
        default=None,
        help='Дополнительно писать диагностику (meta, первые ошибки чтения) в файл',
    )
    ap.add_argument('--verbose', action='store_true', help='Дублировать диагностику в stderr')
    args = ap.parse_args()

    log_fp = None
    if args.log_file:
        args.log_file.parent.mkdir(parents=True, exist_ok=True)
        log_fp = open(args.log_file, 'a', encoding='utf-8')
        log_fp.write(f'\n--- frida_rva_table_probe {time.strftime("%Y-%m-%d %H:%M:%S")} pid={args.pid} ---\n')

    def log(msg: str) -> None:
        if log_fp:
            log_fp.write(msg + '\n')
            log_fp.flush()
        if args.verbose:
            print(msg, file=sys.stderr)

    repo = Path(__file__).resolve().parents[2]
    table_path = args.table or (repo / 'so' / 'rva_table.json')
    if not table_path.is_file():
        print(f'Нет таблицы: {table_path}', file=sys.stderr)
        return 1

    tab = load_rva_table(table_path)
    entries = tab.get('entries') or []
    block_f = (args.block or '').strip().lower()
    filtered = [e for e in entries if not block_f or str(e.get('block') or '').lower() == block_f]

    hi = {}
    if args.highlight_offsets and args.highlight_offsets.is_file():
        hi = load_client_dll_offsets(args.highlight_offsets)
    rva_to_names: dict[int, list[str]] = {}
    for name, rv in hi.items():
        rva_to_names.setdefault(rv, []).append(name)

    tail = filtered[args.offset : args.offset + args.limit]
    if not tail:
        print('Нет строк (проверь --block / --offset).', file=sys.stderr)
        return 1

    hint_js = json.dumps(args.module_substr)
    session = frida.attach(args.pid)
    script = session.create_script(FRIDA_AGENT % hint_js)
    script.load()
    meta = script.exports_sync.meta()

    log(f'meta: module={meta.get("module")} path={meta.get("path")}')
    log(f'meta: base={meta.get("base")} size={meta.get("size")} end={meta.get("end")}')
    log(f'meta: test .data first slot rva={meta.get("test_rva")} addr={meta.get("test_addr")} ok={meta.get("test_read_ok")} err={meta.get("test_read_err")!r}')

    if not meta.get('test_read_ok'):
        err = meta.get('test_read_err') or 'unknown'
        print(
            f'# WARNING: пробное чтение по RVA {meta.get("test_rva")} не удалось: {err}',
            file=sys.stderr,
        )
        print(
            '# Проверь: тот ли PID у cs2, тот же uid, ptrace_scope=0; модуль должен совпадать с so/libclient.so по билду.',
            file=sys.stderr,
        )

    print(f'# pid={args.pid} module={meta.get("module")} base={meta.get("base")} size={meta.get("size")} table={table_path}', flush=True)
    print(f'# slice offset={args.offset} limit={args.limit} block={args.block or "*"} total_filtered={len(filtered)}', flush=True)
    print('rva_hex\trva\tblock\traw_u64\tptr_ok\tdumper_hit\terr', flush=True)

    batch = max(1, min(args.batch, 512))
    err_logged = 0
    i = 0
    while i < len(tail):
        chunk = tail[i : i + batch]
        hex_list = [str(c.get('rva_hex') or hex(int(c['rva']))).strip() for c in chunk]
        raw = script.exports_sync.probe_batch_hex(hex_list)  # Frida: probeBatchHex
        for j, e in enumerate(chunk):
            row = raw[j]
            if not row.get('ok'):
                err = row.get('err') or '?'
                if err_logged < 5:
                    log(f'read_err rva={e.get("rva_hex")}: {err}')
                    err_logged += 1
                print(
                    f'{e.get("rva_hex")}\t{e["rva"]}\t{e.get("block","")}\t\t\t\t{err}',
                    flush=True,
                )
                continue
            u = int(row['v'])
            ok = ptr_ok(u)
            if args.only_ptr and not ok:
                continue
            hit = ''
            if e['rva'] in rva_to_names:
                hit = ','.join(rva_to_names[e['rva']])
            print(
                f'{e.get("rva_hex")}\t{e["rva"]}\t{e.get("block","")}\t0x{u:x}\t{ok}\t{hit}\t',
                flush=True,
            )
        i += batch

    if log_fp:
        log_fp.close()
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
