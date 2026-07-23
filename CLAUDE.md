# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

ShadowTrace is a self-contained **Go** binary: a Bluetooth/Wi-Fi environment
intrusion detector and presence watcher, with a small Python side-car for ML
training. Two modes, selected by `--mode` / `MODE`:

- **watch (default)** — scans the surrounding BLE environment, learns a baseline of
  habitual devices, and alerts (Telegram) on unknown devices with strong, sustained
  signal (near/inside), writing a forensic event log that doubles as the ML dataset.
- **presence** — legacy tracker firing DETECTED/LOST, fusing BLE with optional
  Wi-Fi ICMP, mDNS (avahi-browse) and ARP discovery.

It detects devices that emit radio, not people (a phone that is off/airplane is
invisible). Complementary/forensic layer, not a camera replacement.

The CLI is built with **Cobra + Viper** and is fully self-documenting: every command
has a Long description and an Example, so `shadowtrace help` and `--help` teach the
whole tool. When in doubt about behaviour, read the command's `--help` first.

## Commands (development)

Tasks live in the `Justfile` (`just` to list). Go 1.26+.

- `just build` — build `./shadowtrace` for this host; `just build-pi [arm64|armv7|armv6]` cross-compiles for a Raspberry Pi
- `just test` / `go test ./...` — unit tests
- `go test ./internal/identify -run TestFingerprint` — a single test
- `just run scan --adapter hci0` — run without building
- `just vet`, `just fmt`, `just tidy`
- `just anomaly-train` — `uv run tools/train.py ...` (Python trainer, PEP 723 auto-deps)
- `just service-install` / `service-restart` / `service-logs-follow` — user systemd unit

Runtime needs Linux with BlueZ + a Bluetooth adapter + system D-Bus. Select the
adapter with `--adapter` / `BT_ADAPTER` (e.g. hci1). Presence mDNS needs
`avahi-browse`; Wi-Fi/ARP need `ip`/`ping`.

## Architecture

`main.go` → `cmd.Execute()`. Packages:

- **cmd/** — Cobra tree. `root.go` declares every persistent flag, binds each to a
  Viper key **and** to its legacy env var (`WATCH_RSSI_MIN`, `BT_ADAPTER`, …) via
  `BindEnv`, so `~/.config/shadowtrace.env` keeps working unchanged. Precedence:
  flag > env > default. Subcommands: `watch`, `presence`, `scan` (one-shot, read-only
  environment dump — great for discovery/calibration), `baseline` (list/show/forget),
  `events` (tail/stats), `anomaly` (score/train), `oui` (update/info/lookup),
  `version`, plus Cobra `completion`.
- **internal/config** — `Config` struct + `Load(*viper.Viper)`. Flag keys are the
  `Key*` constants; `~`-expansion and list parsing live here.
- **internal/model** — shared types: `Observation` (one device this window, optional
  fields as pointers) and `Identity` (vendor/kind/model/note).
- **internal/scan** — `bluetooth.go` drives BlueZ over `godbus/dbus/v5`
  (SetDiscoveryFilter + StartDiscovery + GetManagedObjects), `variants.go` unwraps
  D-Bus variants (RSSI int16, ManufacturerData a{qv}, ServiceData a{sv}, …),
  `net.go` runs the Wi-Fi/mDNS/ARP transports via `os/exec`.
- **internal/identify** — `Fingerprint` (MAC-rotation-resistant key: non-MAC name +
  company id + union of UUIDs/ServiceData UUIDs; else `mac:<addr>`) and `Identify`
  (vendor via SIG company id / OUI, kind via icon/appearance, model via Fast Pair id,
  Apple continuity type, or a printable string embedded in service data).
- **internal/store** — atomic JSON persistence: `Baseline` (`_meta` + fingerprint
  entries, hand-editable), the JSONL `Event` log (appear/leave), presence `State`.
- **internal/oui** — MAC→vendor database (Wireshark `manuf`), with `EnsureFresh`
  (download when missing/stale/forced, non-fatal on failure) and a nil-safe `Vendor`
  lookup. Injected into `identify.Identify` as a `VendorFunc` so identify stays pure.
- **internal/engine** — `Watch` (proximity threshold + `ConfirmHits` hysteresis +
  learning window + cooldown + night hours; writes appear/leave; alerts unknowns) and
  `Presence`. Both consume `scan.Scanner` and a `notify.Notifier`. `Watch` keeps a
  `macFP` anchor: a device's fingerprint is fixed for its session (BlueZ enriches a
  device's props over cycles, which would otherwise drift the fingerprint and double-
  count one device); MAC rotation still produces a fresh fingerprint for regrouping.
- **internal/notify** — Telegram (net/http) + stdout.
- **internal/anomaly** — Isolation Forest **inference** in Go from a JSON model.

### Hybrid ML

Training is Python, inference is Go. `tools/train.py` (run via `uv run tools/train.py`
— PEP 723 header auto-installs scikit-learn/numpy, no pyproject needed) fits a
`StandardScaler + IsolationForest` over the event log and exports the tree ensemble
+ scaler + offset to JSON. `internal/anomaly/forest.go` loads that JSON and
reproduces sklearn's `decision_function` (path-length scoring). The feature vector
`[hour_sin, hour_cos, dow_sin, dow_cos, rssi, known]` and the Monday=0 weekday
convention **must stay identical** between `tools/train.py` and `forest.go`, or scores
diverge. `shadowtrace anomaly score` is Go inference; `anomaly train` shells out to the
Python trainer.

## Testing

`go test ./...`. Pure-logic tests only (no D-Bus/subprocess): `identify` (fingerprint
stability, identification), `scan` (`findAdapterPath`), `anomaly` (path-length math
against the sklearn formula, weekday encoding), `config` (Load/fallbacks). Keep the
D-Bus and exec edges thin and untested; test the pure functions they feed.

## Conventions

- Standard Go layout; `internal/` for non-exported packages. `gofmt`, `go vet` clean.
- Keep I/O at the edges (BlueZ/D-Bus, os/exec, net/http); keep fingerprint/identify/
  anomaly pure and tested.
- Every new command needs a Long description + Example (self-documenting is a
  first-class requirement here).
- Config: add a flag in `cmd/root.go` (flag + `BindPFlag` + `BindEnv` legacy name),
  a `Key*` const and a field in `internal/config`.
- CI (`.github/workflows/ci.yml`) runs `go vet`, `go build` and `go test ./...` on push/PR.

## Roadmap

- Wi-Fi monitor-mode sniffing (probe requests) to detect nearby phones not on the LAN
  — needs a monitor-capable USB adapter; not yet implemented.
- Optional active GATT enrichment (Device Information Service) for fixed-MAC devices.
