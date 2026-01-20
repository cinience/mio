#!/bin/bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"

has_flag() {
  local needle="$1"
  shift || true
  for arg in "$@"; do
    case "$arg" in
      "$needle"|"$needle="*)
        return 0
        ;;
    esac
  done
  return 1
}

MODE="voice-to-text"
if [[ $# -gt 0 ]]; then
  MODE="$1"
  shift
fi

case "$MODE" in
  voice-to-text|voicetotext|vt)
    args=("$@")
    if ! has_flag "--audio-dir" "${args[@]}"; then
      args=(--audio-dir ../xiaozhi-tester/examples "${args[@]}")
    fi
    go run ./examples/voice-to-text "${args[@]}"
    ;;
  text-to-voice|texttovoice|tv)
    args=("$@")
    if ! has_flag "--text" "${args[@]}"; then
      args=(--text "你好，世界，今天的日期是2030年10月20号" "${args[@]}")
    fi
    go run ./examples/text-to-voice "${args[@]}"
    ;;
  mic-stream|mic)
    go run ./examples/mic-stream "$@"
    ;;
  turn-detection|turn)
    go run ./examples/turn-detection "$@"
    ;;
  *)
    cat <<'USAGE'
Usage: ./cli.sh <mode> [flags]

Modes:
  voice-to-text   Stream PCM/WAV clips to the realtime server (default).
  text-to-voice   Generate speech audio via response.create.
  mic-stream      Pipe microphone PCM16 frames from stdin with server-side VAD.
  turn-detection  Replay a single audio file with automatic VAD commits.

Every mode forwards additional flags directly to the underlying Go example.
Examples:
  ./cli.sh voice-to-text --chunk 150ms
  ./cli.sh text-to-voice --voice verse --sample-rate 48000
  ./cli.sh mic-stream --vad-model ten --switch-model silero --switch-after 5s
  ./cli.sh turn-detection --file ./sample.wav --create-response
USAGE
    exit 1
    ;;
esac
