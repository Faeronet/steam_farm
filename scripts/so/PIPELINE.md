# libclient: задачи по этапам

Цель — получить рабочие **RVA для Linux `libclient.so`** (`dw_*`, поля pawn и т.д.) и проверить их в **sfarm-desktop**.

**Как поля конфига распределены по подсистемам фермы** (навигация, ESP, sigscan): см. пакет `autoplay` — файл **`internal/engine/cs2/autoplay/mem_read_tasks.go`** (`MemReadTask`, `JSONKeysForTask`).

---

## Задача A — статическая таблица RVA (без игры)

| Что | Как |
|-----|-----|
| Обновить Excel → JSON | `make rva-table` или `python3 scripts/so/export_rva_xlsx.py .` |
| Результат | `so/rva_table.json` (слоты `.data` / `.bss` / `.got`) |

**Зависимости:** файл `so/rva.xlsx` (таблица Imagebase RVA из Ghidra/аналог).

---

## Задача B — дампер a2x (Windows-числа для подсказок, не Linux RVA)

| Что | Как |
|-----|-----|
| Подтянуть `offsets.json` + `client_dll.json` | `make cs2-offsets` |
| Результат | `config/cs2_dumper/*.json` — **RVA под `client.dll` (Windows)**; с таблицей libclient **по числу rva не совпадают** |

Используется движком для полей структур (`m_v_old_origin`, …) и подсказок; **глобалы libclient под Linux** отсюда напрямую не копируются.

---

## Задача C — живой снимок памяти CS2 (Frida)

| Что | Как |
|-----|-----|
| Окружение | `python3 -m venv .venv-frida && .venv-frida/bin/pip install frida frida-tools` |
| Запущен **cs2**, тот же uid | `PID=$(pgrep -n cs2)` |
| Полный прогон таблицы | `.venv-frida/bin/python scripts/so/frida_rva_table_probe.py "$PID" --limit 70000 --batch 256 --log-file /tmp/frida_rva.log > /tmp/rva_probe_full.tsv` |

**Результат:** TSV с колонками `rva_hex`, `raw_u64`, `ptr_ok`, секция `block`.

**Зависимости:** Задача A (`rva_table.json`), запущенный CS2.

---

## Задача D — отбор кандидатов по снимку

| Что | Как |
|-----|-----|
| Анализ TSV | `python3 scripts/so/find_libclient_globals.py /tmp/rva_probe_full.tsv --offsets-json config/cs2_dumper/offsets.json --out-json so/libclient_globals_candidates.json` |

**Результат:** указатели **внутрь образа** libclient по слотам (в т.ч. `.data`/`.got`); **имён `dw_entity_list` и т.п. скрипт не ставит** — это только карта кандидатов.

**Зависимости:** Задача C.

---

## Задача E — точные RVA (основной путь в продукте)

| Что | Как |
|-----|-----|
| Sigscan по `.text` | Встроен в `tryStartLinuxMemDriver` (после управляемого спавна, если не отключены ворота) |
| Логи | панель SigScanner в UI, опционально `/tmp/sfarm_sigscan.log` |

**Зависимости:** `sfarm-desktop`, бот, матч; конфиг без полного набора `dw_*` (или обнулённые глобалы под Linux).

---

## Задача F — ручной `libclient_offsets.json`

| Что | Как |
|-----|-----|
| Шаблон | `so/libclient_offsets.example.json` → скопировать в `config/libclient_offsets.json` или `so/libclient_offsets.json` |
| Заполнение | Сверка кандидатов из Задачи D + реверс / сравнение с sigscan / другой источник по **твоему** билду |

**Зависимости:** Задачи D и/или E, свой `build_id` при необходимости (`readelf -n libclient.so`).

---

## Задача G — проверка без Frida (HTTP mem-debug)

| Что | Как |
|-----|-----|
| Тот же `rva_table.json`, что и у desktop | `SFARM_URL=http://127.0.0.1:17355 ./scripts/so/fetch_rva_probe_all.sh` (нужны десктоп, CS2, бот; в матче или `FORCE=1`) |

**Зависимости:** Задача A, запущенный mem-debug порт.

---

## Порядок «от нуля до офсетов»

1. A → (B по желанию)  
2. C → D (карта кандидатов)  
3. E (sigscan) **или** F (ручной merge из кандидатов)  
4. G — опциональная перекрёстная проверка с десктопом  

Frida и sigscan **дополняют** друг друга: Frida даёт **что лежит в слотах**, sigscan даёт **RVA глобалов по сигнатурам** под текущий `libclient.so`.
