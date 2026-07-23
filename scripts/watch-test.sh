#!/usr/bin/env bash
#
# watch-test.sh — quick foreground smoke test of ShadowTrace watch mode.
#
# Learns for a short window, then alerts on nearby unknown devices via stdout
# (or Telegram if TELEGRAM_* are set). Uses throwaway files under /tmp so it
# never touches your real baseline/event log. Ctrl+C to stop.
#
# Every value can be overridden from the environment, e.g.:
#   BT_ADAPTER=hci0 WATCH_RSSI_MIN=-75 ./scripts/watch-test.sh
#   WATCH_LEARN_SECONDS=60 ./scripts/watch-test.sh   # learn 1 min, then alert on newcomers
#
# For the real 24h run use ~/.config/shadowtrace.env + `make service-install`.

set -euo pipefail
cd "$(dirname "$0")/.."

export PYTHONUNBUFFERED=1  # stream output live even through a pipe

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
    sed -n '/^# watch-test/,/^$/p' "$0" | sed 's/^# \{0,1\}//'
    exit 0
fi

export MODE="${MODE:-watch}"
export BT_ADAPTER="${BT_ADAPTER:-hci1}"          # dedicated adapter; try hci0 if nothing shows
export WATCH_RSSI_MIN="${WATCH_RSSI_MIN:--90}"   # low so the test captures almost everything
export WATCH_CONFIRM_HITS="${WATCH_CONFIRM_HITS:-1}"
export WATCH_LEARN_SECONDS="${WATCH_LEARN_SECONDS:-5}"  # short so alerts start fast
export SCAN_WINDOW_SECONDS="${SCAN_WINDOW_SECONDS:-6}"
export SCAN_INTERVAL_SECONDS="${SCAN_INTERVAL_SECONDS:-8}"
export BASELINE_FILE="${BASELINE_FILE:-/tmp/st_baseline.json}"
export EVENT_LOG="${EVENT_LOG:-/tmp/st_events.jsonl}"

echo "ShadowTrace watch test"
echo "  adapter=${BT_ADAPTER}  rssi_min=${WATCH_RSSI_MIN}dBm  learn=${WATCH_LEARN_SECONDS}s"
echo "  baseline=${BASELINE_FILE}  events=${EVENT_LOG}"
if [[ -z "${TELEGRAM_BOT_TOKEN:-}" || -z "${TELEGRAM_CHAT_ID:-}" ]]; then
    echo "  Telegram not set -> alerts print to stdout"
fi
echo "  Ctrl+C to stop"
echo

exec uv run python main.py
