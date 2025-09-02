# ShadowTrace

Bluetooth (BLE + Classic) proximity watcher for Linux using BlueZ via D-Bus.  
No native builds, works the same on Ubuntu and Raspberry Pi OS. Sends Telegram alerts when a device **appears** (or re-appears) and when it is **lost** after a configurable timeout.

## Features
- Unified BLE + Classic scanning using BlueZ/D-Bus (`Transport=auto`)
- Telegram alerts (plain text)
- JSON persistence of device state
- Optional filters by device name and MAC
- Systemd unit included

## Requirements (Ubuntu / Raspberry Pi OS)
```bash
sudo apt update
sudo apt install -y bluez dbus python3-venv libglib2.0-bin
sudo usermod -aG bluetooth "$USER"   # re-login after adding
