#!/usr/bin/env bash
set -euo pipefail

log_path="${1:-aio-server/logs/backend-server.log}"

if [[ ! -f "$log_path" ]]; then
  echo "log not found: $log_path" >&2
  exit 1
fi

extract_numbers() {
  local pattern="$1"
  if command -v rg >/dev/null 2>&1; then
    rg --no-messages -o "$pattern" "$log_path" | rg -o "[0-9]+" || true
    return 0
  fi
  if command -v grep >/dev/null 2>&1; then
    grep -oE "$pattern" "$log_path" | grep -oE "[0-9]+" || true
    return 0
  fi
  echo "missing search tool: rg or grep" >&2
  return 1
}

report() {
  local label="$1"
  local pattern="$2"
  local tmp
  tmp="$(mktemp)"
  extract_numbers "$pattern" > "$tmp"
  python3 - "$label" "$tmp" <<'PY'
import sys
from pathlib import Path

label = sys.argv[1]
vals = [int(x) for x in Path(sys.argv[2]).read_text().split()]
if not vals:
    print(f"{label}: count=0")
    sys.exit(0)
vals.sort()
n = len(vals)
avg = sum(vals) / n
p50 = vals[int((n - 1) * 0.50)]
p90 = vals[int((n - 1) * 0.90)]
p95 = vals[int((n - 1) * 0.95)]
print(
    f"{label}: count={n} min={vals[0]} p50={p50} p90={p90} p95={p95} "
    f"max={vals[-1]} avg={avg:.1f}"
)
PY
  rm -f "$tmp"
}

echo "Latency report: $log_path"
report "tts_cloud_first_frame_decode_ms" "tts云端->首帧解码完成耗时: [0-9]+ ms"
report "e2e_first_frame_ms" "asr->llm->tts首帧 整体 耗时: [0-9]+ ms"
report "seg_asr_end_to_llm_start_ms" "asr_end->llm_start=[0-9]+ms"
report "seg_llm_start_to_llm_first_token_ms" "llm_start->llm_first_token=[0-9]+ms"
report "seg_llm_start_to_llm_first_chunk_ms" "llm_start->llm_first_chunk=[0-9]+ms"
report "seg_llm_start_to_llm_first_text_ms" "llm_start->llm_first_text=[0-9]+ms"
report "seg_llm_first_text_to_tts_first_ms" "llm_first_text->tts_first=[0-9]+ms"
report "seg_llm_start_to_tts_first_ms" "llm_start->tts_first=[0-9]+ms"
report "seg_tts_first_to_decoder_ready_ms" "tts_first->decoder_ready=[0-9]+ms"
