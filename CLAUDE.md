# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

ShadowTrace is a Linux presence watcher: it detects when devices appear/disappear near a
host and sends Telegram alerts. It fuses four independent discovery transports — Bluetooth
(BLE + Classic via BlueZ/D-Bus), Wi-Fi ICMP ping, mDNS/Bonjour (avahi-browse), and ARP
neighbour table — into one device state map. Pure-Python, no native builds; targets Ubuntu
and Raspberry Pi OS.

## Commands

Uses `uv` for everything. Common targets are wrapped in the `Makefile`:

- `uv sync` / `make sync` — install runtime deps into `.venv`
- `uv sync --group dev` / `make dev` — add pytest, ruff, black
- `uv run pytest` / `make test` — run the suite
- `uv run pytest tests/test_find_adapter.py::test_find_adapter_selects_adapter_path` — single test
- `uv run python main.py` / `make run` / `uv run shadowtrace` — run the scan loop (Ctrl+C to stop)
- `make lint` / `make lint-fix` / `make format` — ruff check / check --fix / format
- `make service-install` / `service-restart` / `service-uninstall` / `service-logs-follow` — manage the user systemd unit

Runtime requires Linux with BlueZ, a Bluetooth adapter, and system D-Bus access. mDNS
discovery needs `avahi-utils` (`avahi-browse`) on PATH; ARP/Wi-Fi need `ip` and `ping`.

## Architecture

Essentially all logic lives in **`shadowtrace/app.py`** (~530 lines). `main.py` is a thin
entrypoint that calls `app.main()` and re-exports `now_utc` and `find_adapter_path` for tests.
`shadowtrace/app:cli` is the `shadowtrace` console script.

**Single async loop** (`main()`): each cycle runs `scan_once` (BLE) then merges
`wifi_scan_once` (which also folds in mDNS), then `arp_scan_once` into one `seen_now` dict,
then diffs against the persisted `state`, emits DETECTED/LOST alerts, and sleeps to fill out
`SCAN_INTERVAL`. All four transports write into the same `state` map keyed by an identifier:
raw MAC for BLE, `wifi:<host>`, `mdns:<host>`, `arp:<mac>`. Each transport is gated by an env
flag and degrades silently if its CLI tool is missing (returns `{}`), so any subset can run.

**State model**: `state[key] = {name, type, rssi, last_seen (datetime), status}` where status
is `present` or `gone`. A device flips to DETECTED when it's new or was `gone`; it flips to LOST
when `last_seen` is older than `GONE_AFTER`. State is persisted to `STATE_FILE` (JSON, atomic
via `.tmp` + `os.replace`), with `last_seen` serialized as ISO strings and re-parsed on load.

**BlueZ interaction**: talks to `org.bluez` over the **system** bus with `dbus_next`. Discovery
uses `SetDiscoveryFilter` with `Transport` (`SCAN_TRANSPORT`) and `DuplicateData=True` so RSSI
refreshes fire within the window. With `CONTINUOUS_DISCOVERY=1` (default) it never stops
discovery between cycles to avoid missing sparse beacons. A device counts as "seen" only if it
reports RSSI this window **or** is currently `Connected`. `_val()` unwraps dbus `Variant`
objects everywhere; D-Bus interfaces are fetched via `_get_interface_any` cast to `Any` to keep
static type checkers quiet about dynamic `call_*` methods.

**Config** is entirely env-driven, loaded once at import via `python-dotenv` (`.env`, or
`~/.config/shadowtrace.env` for the systemd unit). See `.env.example` for the full list. Key
knobs: `SCAN_TRANSPORT` (auto|le|bredr), `SCAN_INTERVAL_SECONDS`, `SCAN_WINDOW_SECONDS`,
`GONE_AFTER_SECONDS`, `NAME_WHITELIST`/`IGNORE_MACS` filters, and the per-transport toggles
(`WIFI_HOSTS`, `MDNS_DISCOVERY`, `ARP_DISCOVERY`/`ARP_SWEEP`/`ARP_SUBNETS`). `DEBUG=1` logs
why devices were skipped/filtered. Telegram is optional — with no token/chat it prints alerts
to stdout instead.

## Testing notes

Tests **stub out `dbus_next`, `dotenv`, and `requests` in `sys.modules`** before importing
`main` (see `import_main_with_stubs` in `tests/test_find_adapter.py`) so the suite runs without
a Bluetooth stack or D-Bus. Reuse that helper for any test that imports the app. pytest is
configured with `asyncio_mode = auto` (see `pytest.ini`), so `async def test_*` needs no marker.
Prefer testing pure helpers (state transitions, adapter selection, parsers) rather than the
D-Bus/subprocess edges.

## Conventions

- 4-space indent, line length ~100, ruff for lint/format. `snake_case` functions/modules,
  `UPPER_SNAKE` for the module-level config constants.
- Keep I/O at the edges: isolate BlueZ/D-Bus and subprocess calls, keep diffing/parsing pure
  and testable. New transports should follow the existing pattern — an async `*_scan_once`
  returning `{key: {name, rssi, type}}`, gated by an env flag, failing soft when tooling is absent.
- CI (`.github/workflows/ci.yml`) runs the test suite via `uv` on pushes/PRs to `main`/`master`.
