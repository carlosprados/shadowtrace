# ShadowTrace

Self-contained **Go** binary for Linux: a Bluetooth/Wi-Fi **environment intrusion
detector** and presence watcher using BlueZ over D-Bus, with a small Python side-car
for anomaly-model training. Sends Telegram alerts. No native builds; works on Ubuntu
and Raspberry Pi OS.

## Modes

Select with `--mode` / `MODE` (default `watch`):

- **`watch` — environment intrusion detection (default).** Scans the surrounding BLE
  environment, learns a baseline of habitual devices, and alerts on **unknown devices
  with strong, sustained signal** (near/inside), while writing a forensic event log.
- **`presence` — legacy tracker.** DETECTED/LOST alerts for specific devices, fusing
  BLE with optional Wi-Fi ICMP, mDNS and ARP discovery.

### What it can and cannot do
- Detects **devices that emit radio, not people**. A phone that is off or in airplane
  mode is invisible. It is a complementary/forensic layer, not a camera or door sensor.
- BLE crosses walls, so you **will** pick up neighbours. The `--rssi-min` threshold plus
  the learned baseline are the defences — expect to calibrate.
- Phones rotate their BLE MAC, so alerts say "an unknown device is near", not *who*. A
  best-effort fingerprint (name / manufacturer id / service UUIDs) groups a device across
  MAC rotations; identification (vendor/kind/model) is derived passively.

## Requirements

```bash
sudo apt install -y bluez dbus libglib2.0-bin avahi-utils   # avahi only for presence mDNS
sudo usermod -aG bluetooth "$USER"                          # re-login afterwards
```
Go 1.26+ to build. `uv` (optional) for the anomaly trainer.

## Build & run

```bash
just build            # or: go build -o shadowtrace .
./shadowtrace help    # discover everything

./shadowtrace scan --adapter hci0        # one-shot look at what's around
./shadowtrace watch --adapter hci1       # run the IDS loop (Ctrl+C to stop)
```

The CLI is fully self-documenting — every command has `--help` with an example:

```
shadowtrace
  scan       Run one scan window and print what's around right now
  watch      Run the environment intrusion-detection loop
  presence   Run the legacy presence tracker (DETECTED/LOST)
  baseline   list | show <fp> | forget <fp>
  events     tail | stats
  anomaly    score | train
  oui        update | info | lookup <mac>
  version
```

## Configuration

Flags, environment variables (legacy names, honoured by the systemd unit via
`~/.config/shadowtrace.env`) or defaults — in that precedence. Key ones:

| Flag | Env | Default | Meaning |
|------|-----|---------|---------|
| `--mode` | `MODE` | `watch` | `watch` or `presence` |
| `--adapter` | `BT_ADAPTER` | first | Bluetooth adapter, e.g. `hci1` |
| `--rssi-min` | `WATCH_RSSI_MIN` | `-70` | min RSSI to count as near |
| `--rssi-min-night` | `WATCH_RSSI_MIN_NIGHT` | =rssi-min | threshold during `--alert-hours` |
| `--confirm-hits` | `WATCH_CONFIRM_HITS` | `2` | strong windows before "present" |
| `--gone-after` | `WATCH_GONE_AFTER_SECONDS` | `120` | grace before "gone" |
| `--learn-seconds` | `WATCH_LEARN_SECONDS` | `86400` | learning window |
| `--alert-cooldown` | `ALERT_COOLDOWN_SECONDS` | `600` | min seconds between repeat alerts |
| `--alert-hours` | `ALERT_HOURS` | — | reinforced hours, e.g. `0-7` |
| `--home-macs` | `HOME_MACS` | — | MACs always known |
| `--baseline-file` | `BASELINE_FILE` | `~/.shadowtrace_baseline.json` | learned allowlist |
| `--event-log` | `EVENT_LOG` | `~/.shadowtrace_events.jsonl` | forensic log (JSONL) |
| `--window` / `--interval` | `SCAN_WINDOW_SECONDS` / `SCAN_INTERVAL_SECONDS` | `8` / `20` | scan timing |
| `--oui-file` | `OUI_FILE` | `~/.shadowtrace_oui.tsv` | vendor database cache |
| `--oui-url` | `OUI_URL` | Wireshark manuf | where to download it |
| `--oui-max-age-days` | `OUI_MAX_AGE_DAYS` | `30` | auto-refresh when older (0 = never) |
| `--oui-auto` | `OUI_AUTO_UPDATE` | `true` | auto-download when missing/stale |

Run `./shadowtrace watch --help` (and `presence --help`) for the full list, including
the presence-only Wi-Fi/mDNS/ARP flags. Telegram: set `TELEGRAM_BOT_TOKEN` /
`TELEGRAM_CHAT_ID` (empty = alerts print to stdout).

### Vendor database (OUI)
Devices with a public MAC are labelled with their hardware vendor from a locally
cached OUI database (Wireshark's `manuf`, ~40k prefixes). It downloads on first use
and auto-refreshes when older than `--oui-max-age-days`. Force a refresh anytime:

```bash
shadowtrace oui update        # download now
shadowtrace oui info          # cache path, age, prefix count
shadowtrace oui lookup F8:AB:E5:91:56:87
```

### Calibrating watch mode
1. Let it learn for `--learn-seconds` (default 24h) in place; nothing alerts, devices
   fill `--baseline-file`.
2. Prune it: `shadowtrace baseline list`, then `baseline forget <fp>` for anything that
   shouldn't be known (a recurring neighbour). The file is plain, hand-editable JSON.
3. Tune `--rssi-min` toward 0 (e.g. `-60`) for neighbour noise, or more negative for
   missed inside devices. Inspect ranges with `shadowtrace events tail` / `events stats`.
4. Optionally set `--alert-hours 0-7` with a more permissive `--rssi-min-night`.

## Anomaly detection (hybrid: Python trains, Go infers)

The event log is the training dataset. Training runs in Python (scikit-learn
IsolationForest) and exports a JSON model; scoring is Go inference.

```bash
uv run tools/train.py --events ~/.shadowtrace_events.jsonl \
                      --model ~/.shadowtrace_anomaly.json   # or: just anomaly-train
shadowtrace anomaly score --top 20                          # Go inference
```

`uv run` auto-installs scikit-learn/numpy from the trainer's PEP 723 header — no
Python project needed. It flags the unusual (e.g. a known device at an odd hour) that
the plain rules miss. Needs a few days of data before it is meaningful.

## Run as a service

```bash
mkdir -p ~/.config && cp .env.example ~/.config/shadowtrace.env   # edit: TELEGRAM_*, BT_ADAPTER, ...
# Adjust WorkingDirectory/ExecStart paths in shadowtrace.service to your checkout, then:
just service-install
just service-logs-follow
```

## Roadmap
- Wi-Fi monitor-mode sniffing (probe requests) to detect nearby phones not on your
  network — requires a monitor-capable USB Wi-Fi adapter.
- Optional active GATT enrichment (Device Information Service) for fixed-MAC devices.
