#!/usr/bin/env bash
# Скачивает все страницы GET /rva-probe в один JSON-массив probes (нужны sfarm-desktop + CS2 + бот).
# По умолчанию /rva-probe разрешён только после управляемого спавна в матче; вне матца: FORCE=1 или SFARM_CS2_MEM_MATCH_GATE=0 на десктопе.
# Пример: SFARM_URL=http://127.0.0.1:17355 BLOCK=.data ./scripts/so/fetch_rva_probe_all.sh
# Результат: so/rva_probe_dump.json (meta + все probes подряд)

set -euo pipefail
BASE="${SFARM_URL:-http://127.0.0.1:17355}"
BLOCK="${BLOCK:-}" # .data | .bss | .got | пусто = все
LIMIT="${LIMIT:-2000}"
FORCE="${FORCE:-}" # 1 — добавить ?force=1 (обход ворот матча)
OUT="${OUT:-so/rva_probe_dump.json}"
TOKEN_HEADER=()
if [[ -n "${SFARM_CS2_MEM_DEBUG_TOKEN:-}" ]]; then
  TOKEN_HEADER=(-H "Authorization: Bearer ${SFARM_CS2_MEM_DEBUG_TOKEN}")
fi

qb=""
if [[ -n "$BLOCK" ]]; then
  qb="block=$(printf %s "$BLOCK" | jq -sRr @uri)"
fi

offset=0
all='[]'
meta='{}'
while true; do
  url="${BASE}/rva-probe?limit=${LIMIT}&offset=${offset}"
  if [[ -n "$qb" ]]; then url="${url}&${qb}"; fi
  if [[ "$FORCE" == "1" ]]; then url="${url}&force=1"; fi
  chunk=$(curl -fsS --connect-timeout 2 "${TOKEN_HEADER[@]}" "$url") || {
    echo "curl failed (desktop running? mem-debug on? game+bot?)" >&2
    exit 1
  }
  ok=$(echo "$chunk" | jq -r '.ok')
  if [[ "$ok" != "true" ]]; then
    echo "$chunk" | jq . >&2
    exit 1
  fi
  if [[ "$offset" -eq 0 ]]; then
    meta=$(echo "$chunk" | jq '{ts_ms, pid, client_base, table_path, table_total_entries, filter_block, matched_entries}')
  fi
  n=$(echo "$chunk" | jq '.probes | length')
  part=$(echo "$chunk" | jq '.probes')
  all=$(jq -n --argjson a "$all" --argjson p "$part" '$a + $p')
  matched=$(echo "$chunk" | jq '.matched_entries')
  offset=$((offset + n))
  if [[ "$n" -eq 0 ]] || [[ "$offset" -ge "$matched" ]]; then
    break
  fi
done

jq -n --argjson meta "$meta" --argjson probes "$all" '{meta: $meta, probe_count: ($probes | length), probes: $probes}' > "$OUT"
echo "Wrote $OUT ($(jq '.probe_count' < "$OUT") probes)"
