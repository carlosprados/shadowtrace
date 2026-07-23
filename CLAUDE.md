# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

ShadowTrace is a Linux Bluetooth watcher with two modes (`MODE` env var):

- **`watch` (default) — environment intrusion detection.** Scans the surrounding BLE
  environment, learns a baseline of habitual devices, and alerts (Telegram) on **unknown
  devices with strong, sustained signal** — i.e. physically near/inside — while writing a
  forensic event log. Answers "is there an unrecognised device around my place, and when?".
- **`presence` — legacy tracker.** Watches for devices and fires DETECTED/LOST alerts,
  fusing BLE with optional Wi-Fi ICMP, mDNS (avahi-browse) and ARP neighbour discovery.

Pure-Python, no native builds; targets Ubuntu and Raspberry Pi OS. Detects devices that
emit radio, not people — a phone that's off/airplane-mode is invisible; treat it as a
complementary/forensic layer, not a replacement for a camera or door sensor.

## Commands

Uses `uv` for everything; common tasks are wrapped in the `Justfile` (run `just` for the list).
The dev deps live in `[project.optional-dependencies]`, so installing them is `--extra dev`.

- `uv sync --extra dev` / `just dev` — install runtime + dev deps (pytest, ruff)
- `uv run pytest` / `just test` — run the suite
- `uv run pytest tests/test_watch.py::test_fingerprint_survives_mac_rotation` — single test
- `just watch-test` / `./scripts/watch-test.sh` — quick foreground watch-mode smoke test
  (short learn window, throwaway `/tmp` files, alerts to stdout). All knobs overridable via env.
- `uv run python main.py` / `just run` / `uv run shadowtrace` — run the loop (Ctrl+C to stop)
- `just lint` / `just lint-fix` / `just format` — ruff
- `just service-install` / `service-restart` / `service-logs-follow` — user systemd unit

Runtime requires Linux with BlueZ, a Bluetooth adapter, and system D-Bus. `presence` mode's
mDNS needs `avahi-utils`; Wi-Fi/ARP need `ip` and `ping`. Select the adapter with `BT_ADAPTER`
(e.g. `hci1`).

## Architecture

Essentially all logic lives in **`shadowtrace/app.py`**. `main.py` is a thin entrypoint that
calls `app.main()` and re-exports `now_utc`/`find_adapter_path` for tests. `app:cli` is the
`shadowtrace` console script. `app.main()` connects to the **system** D-Bus, resolves the
adapter (`find_adapter_path(objects, BT_ADAPTER)`), powers it on, then dispatches to
`watch_loop` or `presence_loop` by `MODE`.

**Shared BLE core**: both modes call `scan_ble(bus, adapter_path)`, which runs one discovery
window (`_run_discovery_window`, with `SetDiscoveryFilter` Transport + `DuplicateData=True`,
kept continuously discovering when `CONTINUOUS_DISCOVERY=1`) and then reads
`GetManagedObjects`, returning a raw observation per device that reported RSSI this window or
is `Connected`: `{mac, name, type, rssi, connected, company, uuids}`. `_val()` unwraps
dbus_next `Variant`s everywhere; D-Bus interfaces are fetched via `_get_interface_any` cast to
`Any` to keep type checkers quiet about dynamic `call_*` methods.

**Watch mode** (`watch_loop`):
- **Fingerprint** (`device_fingerprint`): a MAC-rotation-resistant identity built from
  `name` + manufacturer `company` id + sorted service `UUIDs`; falls back to `mac:<addr>` when
  the device exposes none. Advertised names that are *themselves* a MAC (`_looks_like_mac`,
  e.g. `24-EB-90-...`) are dropped from the fingerprint because they rotate with the MAC.
- **Baseline** (`~/.shadowtrace_baseline.json`, `load/save_baseline`): a learned, hand-editable
  allowlist keyed by fingerprint. During the learning window (`WATCH_LEARN_SECONDS`, persisted
  as `_meta.learn_until`) every strong device is learned and nothing alerts. `HOME_MACS` is an
  always-known allowlist for fixed-MAC devices. Disk writes happen **only on structural change**
  (new fingerprint or newly-seen MAC), not on every `last_seen`/`count` refresh — SD-friendly.
- **Proximity + hysteresis**: only sightings with `rssi >= WATCH_RSSI_MIN` (or
  `WATCH_RSSI_MIN_NIGHT` during `ALERT_HOURS`) count as near. A per-fingerprint track must reach
  `WATCH_CONFIRM_HITS` consecutive strong windows before it's "present" (filters passers-by); a
  present device is closed out after `WATCH_GONE_AFTER_SECONDS` of no strong sighting.
- **Alerts + forensics**: an unknown, confirmed, post-learning device triggers a Telegram alert
  (throttled by `ALERT_COOLDOWN_SECONDS`). Every appear/leave is appended to `EVENT_LOG`
  (`~/.shadowtrace_events.jsonl`, one JSON object per line) with timestamps, RSSI min/max and
  session duration. **This log is deliberately shaped as a dataset** for future offline anomaly
  detection (see below).

**Presence mode** (`presence_loop`): filters `scan_ble` output by `NAME_WHITELIST`/`IGNORE_MACS`,
merges `wifi_scan_once` (ICMP + mDNS) and `arp_scan_once` into one `seen_now` map keyed by
`mac`/`wifi:`/`mdns:`/`arp:`, diffs against the persisted `state`, and emits DETECTED/LOST vs
`GONE_AFTER`. State persists to `STATE_FILE` (atomic `.tmp` + `os.replace`), written only on
status transitions.

**Alerts are non-blocking**: `notify()` prints and sends Telegram via `asyncio.to_thread` so the
blocking `requests` call never stalls the scan loop. Timestamps are timezone-aware UTC
(`now_utc`); `_parse_dt` normalizes persisted ISO strings back to aware datetimes.

## Testing notes

Tests **stub `dbus_next`, `dotenv`, and `requests` in `sys.modules`** before importing `main`
(see `import_main_with_stubs` in `tests/test_find_adapter.py`) so the suite runs with no
Bluetooth stack or D-Bus. Reuse that helper; access package internals via `main.app.<name>`
(e.g. `main.app.device_fingerprint`). `tests/__init__.py` must exist for the relative imports to
resolve. pytest uses `asyncio_mode = auto` (`pytest.ini`), so `async def test_*` needs no marker.
Prefer testing pure helpers (fingerprint, adapter selection, state transitions) over the
D-Bus/subprocess edges.

## Conventions

- 4-space indent, line length ~100, ruff for lint/format. `snake_case` functions/modules,
  `UPPER_SNAKE` for module-level config constants.
- Keep I/O at the edges: isolate BlueZ/D-Bus and subprocess calls; keep diffing/parsing pure and
  testable. New transports follow the pattern — an async `*_scan_once` returning
  `{key: {name, rssi, type}}`, gated by an env flag, failing soft when tooling is absent.
- CI (`.github/workflows/ci.yml`) runs the suite via `uv` on pushes/PRs to `main`/`master`.

## Roadmap / notes

- **Wi-Fi monitor-mode sniffing** (probe requests) to detect nearby phones not on the LAN —
  needs a monitor-capable USB Wi-Fi adapter; not yet implemented.
- **Offline anomaly detection** over `EVENT_LOG` (see the ML discussion): the intended next step
  is unsupervised novelty detection (e.g. sklearn `IsolationForest`/`LocalOutlierFactor`) on
  temporal features (hour-of-day, weekday, RSSI, dwell time, concurrent-unknown count) to flag
  "known device at an odd hour" — not deep learning or RL, which don't fit the data volume/shape.
