# Оффсеты CS2 для навигации по памяти

Файлы **`offsets.json`** и **`client_dll.json`** — вывод [a2x/cs2-dumper](https://github.com/a2x/cs2-dumper). Бот на Linux собирает из них `dwLocalPlayerPawn`, `m_vOldOrigin`, `m_angEyeAngles`, `m_vecAbsVelocity` без ручного `cs2_memory.json`.

Скачать актуальные:

```bash
make cs2-offsets
```

После обновления игры offsets устаревают — повторите команду.
