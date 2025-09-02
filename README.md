# ShadowTrace

Bluetooth (BLE + Classic) proximity watcher for Linux using BlueZ via D-Bus.  
No native builds; works on Ubuntu and Raspberry Pi OS. Sends Telegram alerts when a device appears (or re-appears) and when it is marked lost after a timeout.

## Features
- Unified BLE + Classic scanning via BlueZ/D-Bus (`Transport=auto`)
- Telegram alerts (plain text)
- JSON persistence of device state
- Optional filters by device name and MAC

## Requirements (Ubuntu / Raspberry Pi OS)
```bash
sudo apt update
sudo apt install -y bluez dbus python3-venv libglib2.0-bin
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

## Configuration (env vars)
- `NAME_WHITELIST`: comma-separated substrings to include (optional)
- `IGNORE_MACS`: comma-separated MACs to ignore (AA:BB:CC:DD:EE:FF)
- `SCAN_INTERVAL_SECONDS` (default 20), `SCAN_WINDOW_SECONDS` (default 8)
- `SCAN_TRANSPORT`: `auto` (default), `le`, or `bredr`
- `CONTINUOUS_DISCOVERY`: keep scanning between cycles (default 1)
- `GONE_AFTER_SECONDS` (default 60)
- `STATE_FILE` (default `~/.shadowtrace_state.json`)
- `WIFI_HOSTS`: optional fallback presence by ICMP (e.g., `iphone@192.168.1.23,watch@watch.local,tablet.local`)

Troubleshooting tips
- If some BLE devices (e.g., phone/watch) are missed, try a longer window: `SCAN_WINDOW_SECONDS=15` and keep discovery on: `CONTINUOUS_DISCOVERY=1`.
- Force LE scan: set `SCAN_TRANSPORT=le`.
- Clear `NAME_WHITELIST` or ensure it matches the device names. Enable debug logging with `DEBUG=1` to see filter reasons.
- For phones that rarely advertise, add a `WIFI_HOSTS` entry and ensure the device responds to ICMP ping.
