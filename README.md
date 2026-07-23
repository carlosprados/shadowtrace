# ShadowTrace

Bluetooth (BLE + Classic) proximity watcher for Linux using BlueZ via D-Bus.  
No native builds; works on Ubuntu and Raspberry Pi OS. Sends Telegram alerts.

## Modes

Select with `MODE` (default `watch`):

- **`watch` — environment intrusion detection (default).** Scans the surrounding BLE
  environment, learns a baseline of habitual devices, and alerts on **unknown devices with
  strong, sustained signal** (i.e. physically near/inside), while writing a forensic event
  log. Built to answer "is there an unrecognised device around my place, and when?".
- **`presence` — legacy tracker.** Watches for specific devices and fires DETECTED/LOST
  alerts, optionally fusing Wi‑Fi/mDNS/ARP. See the Presence section below.

### What watch mode can and cannot do
- It detects **devices that emit radio, not people**. Someone entering with their phone off
  or in airplane mode is invisible to it. Treat it as a complementary/forensic layer, not a
  replacement for a door sensor or camera.
- BLE crosses walls, so you **will** pick up neighbours. The `WATCH_RSSI_MIN` threshold plus
  the learned baseline are the only defences against that noise — expect to calibrate.
- Phones rotate their BLE MAC, so alerts say "an unknown device is near", not *who*. A
  best-effort fingerprint (name / manufacturer id / service UUIDs) groups a device across
  MAC rotations when it exposes any of those.

## Features
- Unified BLE + Classic scanning via BlueZ/D-Bus (`Transport=auto`), adapter selectable via `BT_ADAPTER`
- Watch mode: RSSI proximity threshold, auto-learned editable baseline, sustained-presence
  confirmation, reinforced night hours, forensic JSONL event log, anti-spam cooldown
- Telegram alerts (plain text), sent off the scan loop (non-blocking)
- JSON persistence of state/baseline

## Requirements (Ubuntu / Raspberry Pi OS)
```bash
sudo apt update
sudo apt install -y bluez dbus python3-venv libglib2.0-bin avahi-utils
sudo usermod -aG bluetooth "$USER"  # re-login after adding
```
Install uv (package/deb, or via script):
```bash
curl -LsSf https://astral.sh/uv/install.sh | sh
exec "$SHELL" -l  # reload PATH so ~/.local/bin is active
```

## Setup & Run (uv)
```bash
uv sync --group dev
export TELEGRAM_BOT_TOKEN=... TELEGRAM_CHAT_ID=...
uv run shadowtrace
```

Note: Press Ctrl+C to stop.

## How To Use
- Local run (CLI):
  - Copy env template: `cp .env.example .env` and fill values (or export vars in your shell).
  - Install deps: `uv sync` (add `--group dev` for tests).
  - Start: `uv run shadowtrace` or `uv run python main.py`.

- As a user systemd service:
  - Create an env file: `mkdir -p ~/.config && cp .env.example ~/.config/shadowtrace.env` and edit values.
  - Install and enable via Makefile: `make service-install`.
  - Restart service after changes: `make service-restart`.
  - Uninstall service: `make service-uninstall`.
  - Alternatively (manual):
    - `mkdir -p ~/.config/systemd/user && cp shadowtrace.service ~/.config/systemd/user/`
    - `systemctl --user daemon-reload && systemctl --user enable --now shadowtrace`
  - Note: if your project path is not `~/shadowtrace`, update `WorkingDirectory` in the unit before installing.
  - Logs: `journalctl --user -u shadowtrace -f`.

## Makefile Targets
- `make sync`: install runtime deps into `.venv` via uv
- `make dev`: install dev deps (pytest, etc.)
- `make run`: run the app (`uv run python main.py`)
- `make test`: run tests (`uv run pytest`)
- `make add PKG=x`: add a dependency (records in `pyproject.toml`)
- `make format`: format code with Ruff (`uv run ruff format .`)
- `make lint`: lint with Ruff (`uv run ruff check .`)
- `make lint-fix`: lint with auto-fix (`uv run ruff check --fix .`)
- `make service-install`: install + enable the user systemd unit
- `make service-restart`: restart the user unit
- `make service-uninstall`: disable and remove the unit
- `make service-status`: show the unit status
- `make service-logs`: show the last 200 log lines
- `make service-logs-follow`: follow unit logs in real time

## Watch mode configuration (env vars)
- `MODE`: `watch` (default) or `presence`
- `BT_ADAPTER`: preferred Bluetooth adapter (e.g. `hci1`); empty = first available
- `WATCH_RSSI_MIN` (default −70): minimum RSSI to count as "near/inside" (weaker = farther)
- `WATCH_RSSI_MIN_NIGHT`: threshold used during `ALERT_HOURS` (defaults to `WATCH_RSSI_MIN`)
- `WATCH_CONFIRM_HITS` (default 2): consecutive strong windows before a device counts as present
- `WATCH_GONE_AFTER_SECONDS` (default 120): grace before a present device is marked gone
- `WATCH_LEARN_SECONDS` (default 86400): learning window that populates the baseline
- `ALERT_COOLDOWN_SECONDS` (default 600): min seconds between repeated alerts for one device
- `ALERT_HOURS`: reinforced hours `start-end` (24h local), e.g. `0-7`; empty = disabled
- `HOME_MACS`: comma-separated MACs always treated as known (fixed-MAC devices)
- `BASELINE_FILE` (default `~/.shadowtrace_baseline.json`): learned, hand-editable allowlist
- `EVENT_LOG` (default `~/.shadowtrace_events.jsonl`): forensic log (one JSON object per line)

### Calibrating watch mode
1. Run it for `WATCH_LEARN_SECONDS` (default 24h) so it learns everything habitual as known.
   Nothing alerts during learning; devices are added to `BASELINE_FILE`.
2. After learning, edit `BASELINE_FILE` to drop anything you don't want treated as known
   (a recurring neighbour, say). It's plain JSON keyed by fingerprint.
3. Tune `WATCH_RSSI_MIN` toward 0 (e.g. −60) if you get neighbour noise, or more negative
   (e.g. −80) if you miss devices you know are inside. Review `EVENT_LOG` to see RSSI ranges.
4. Optionally set `ALERT_HOURS` (e.g. `0-7`) with a more permissive `WATCH_RSSI_MIN_NIGHT`
   so an empty house is watched more aggressively at night.

Roadmap: Wi‑Fi monitor-mode sniffing (probe requests) to detect nearby phones not on your
network — requires a monitor-capable USB Wi‑Fi adapter; not yet implemented.

## Presence mode configuration (env vars)
- `NAME_WHITELIST`: comma-separated substrings to include (optional)
- `IGNORE_MACS`: comma-separated MACs to ignore (AA:BB:CC:DD:EE:FF)
- `SCAN_INTERVAL_SECONDS` (default 20), `SCAN_WINDOW_SECONDS` (default 8)
- `SCAN_TRANSPORT`: `auto` (default), `le`, or `bredr`
- `CONTINUOUS_DISCOVERY`: keep scanning between cycles (default 1)
- `GONE_AFTER_SECONDS` (default 60)
- `STATE_FILE` (default `~/.shadowtrace_state.json`)
- `WIFI_HOSTS`: optional fallback presence by ICMP (e.g., `iphone@192.168.1.23,watch@watch.local,tablet.local`)
- `MDNS_DISCOVERY`: enable mDNS/Bonjour discovery without knowing IPs (requires `avahi-browse`)
- `ARP_DISCOVERY`: detect devices from ARP/neighbour table (no prior IPs)
- `ARP_SUBNETS`: optional CIDR list to sweep (e.g., `192.168.1.0/24,10.0.0.0/24`); auto-detected if empty
- `ARP_SWEEP`: ping sweep subnets to populate neighbour entries (off by default)
- `ARP_SWEEP_LIMIT`: cap the number of hosts to ping per cycle (default 256)
- `ARP_TIMEOUT_MS`: timeout per ping (default 500)

Troubleshooting tips
- If some BLE devices (e.g., phone/watch) are missed, try a longer window: `SCAN_WINDOW_SECONDS=15` and keep discovery on: `CONTINUOUS_DISCOVERY=1`.
- Force LE scan: set `SCAN_TRANSPORT=le`.
- Clear `NAME_WHITELIST` or ensure it matches the device names. Enable debug logging with `DEBUG=1` to see filter reasons.
- For phones that rarely advertise, add a `WIFI_HOSTS` entry and ensure the device responds to ICMP ping.
- Enable mDNS discovery (`MDNS_DISCOVERY=1`) to detect devices announcing Bonjour services (e.g., iPhones) without prior IP knowledge.
- Enable ARP discovery: `ARP_DISCOVERY=1` (optionally with `ARP_SWEEP=1` and `ARP_SUBNETS` to be more aggressive). This can detect phones present on your LAN even if they don’t advertise BLE.
