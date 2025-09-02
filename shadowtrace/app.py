import asyncio
import json
import os
import time
from datetime import datetime, timedelta
from typing import Dict, Any, cast

from dbus_next.aio.message_bus import MessageBus
from dbus_next.constants import BusType
from dbus_next.signature import Variant
from dotenv import load_dotenv
import requests
from asyncio.subprocess import DEVNULL, PIPE

# ---------- Configuration ----------
load_dotenv()

APP_NAME = os.getenv("APP_NAME", "ShadowTrace")
LOCATION_TAG = os.getenv("LOCATION_TAG", "").strip()

TELEGRAM_BOT_TOKEN = os.getenv("TELEGRAM_BOT_TOKEN", "").strip()
TELEGRAM_CHAT_ID = os.getenv("TELEGRAM_CHAT_ID", "").strip()

SCAN_INTERVAL = int(os.getenv("SCAN_INTERVAL_SECONDS", "20"))  # full cycle duration
SCAN_WINDOW = int(os.getenv("SCAN_WINDOW_SECONDS", "8"))  # discovery window per cycle
SCAN_TRANSPORT = os.getenv("SCAN_TRANSPORT", "auto").lower()  # auto|le|bredr
GONE_AFTER = int(os.getenv("GONE_AFTER_SECONDS", "60"))  # if unseen for this time => LOST

STATE_FILE = os.getenv("STATE_FILE", os.path.expanduser("~/.shadowtrace_state.json"))
NAME_WHITELIST = [s.strip() for s in os.getenv("NAME_WHITELIST", "").split(",") if s.strip()]
IGNORE_MACS = {s.strip().upper() for s in os.getenv("IGNORE_MACS", "").split(",") if s.strip()}

DEBUG = os.getenv("DEBUG", "0").strip() not in ("", "0", "false", "False")
CONTINUOUS_DISCOVERY = os.getenv("CONTINUOUS_DISCOVERY", "1").strip() not in ("", "0", "false", "False")
MDNS_DISCOVERY = os.getenv("MDNS_DISCOVERY", "1").strip() not in ("", "0", "false", "False")

# Wi‑Fi presence config: comma-separated entries. Each entry can be "name@host" or just "host".
_WIFI_HOSTS_RAW = [s.strip() for s in os.getenv("WIFI_HOSTS", "").split(",") if s.strip()]
WIFI_HOSTS = []
for entry in _WIFI_HOSTS_RAW:
    if "@" in entry:
        name, host = entry.split("@", 1)
        WIFI_HOSTS.append({"name": name.strip(), "host": host.strip()})
    else:
        WIFI_HOSTS.append({"name": entry, "host": entry})

_running = True
state: Dict[str, Any] = {}  # mac -> {...}


# ---------- Utilities ----------
def now_utc() -> datetime:
    return datetime.utcnow()


def tag_prefix() -> str:
    return f"{APP_NAME}{' ' + LOCATION_TAG if LOCATION_TAG else ''}"


def ensure_parent_dir(path: str) -> None:
    d = os.path.dirname(path)
    if d and not os.path.exists(d):
        os.makedirs(d, exist_ok=True)


def load_state() -> Dict[str, Any]:
    if not os.path.exists(STATE_FILE):
        return {}
    try:
        with open(STATE_FILE, "r", encoding="utf-8") as f:
            data = json.load(f)
        # normalize timestamps
        for v in data.values():
            if isinstance(v.get("last_seen"), str):
                try:
                    v["last_seen"] = datetime.fromisoformat(v["last_seen"])
                except Exception:
                    v["last_seen"] = now_utc()
        return data
    except Exception:
        return {}


def save_state() -> None:
    ensure_parent_dir(STATE_FILE)
    data = {}
    for mac, info in state.items():
        data[mac] = dict(info)
        if isinstance(data[mac].get("last_seen"), datetime):
            data[mac]["last_seen"] = data[mac]["last_seen"].isoformat()
    tmp = STATE_FILE + ".tmp"
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent=2)
    os.replace(tmp, STATE_FILE)


def send_telegram(text: str) -> None:
    """Send a plain-text Telegram message. If not configured, print to stdout."""
    if not TELEGRAM_BOT_TOKEN or not TELEGRAM_CHAT_ID:
        print("[WARN] Telegram not configured. Message would be:\n", text)
        return
    url = f"https://api.telegram.org/bot{TELEGRAM_BOT_TOKEN}/sendMessage"
    payload = {"chat_id": TELEGRAM_CHAT_ID, "text": text}
    try:
        r = requests.post(url, json=payload, timeout=10)
        if r.status_code != 200:
            print("[ERROR] Telegram:", r.status_code, r.text)
    except Exception as e:
        print("[ERROR] Telegram:", e)


def match_whitelist(name: str) -> bool:
    """Return True if device name passes the optional whitelist."""
    if not NAME_WHITELIST:
        return True
    if not name:
        return False
    name_low = name.lower()
    return any(w.lower() in name_low for w in NAME_WHITELIST)


def infer_type(dev_props: Dict[str, Any]) -> str:
    """Heuristic: AddressType => BLE; Class => Classic; else Unknown."""
    if "AddressType" in dev_props:
        return "BLE"
    if "Class" in dev_props:
        return "Classic"
    return "Unknown"


def fmt_device_line(name: str, mac: str, dtype: str, rssi) -> str:
    rssi_txt = f" RSSI={rssi}dBm" if rssi is not None else ""
    return f"{name or 'unknown'} [{mac}] ({dtype}){rssi_txt}"


def debug(*args, **kwargs) -> None:
    if DEBUG:
        print("[DEBUG]", *args, **kwargs)


# ---------- Wi‑Fi presence helpers ----------
async def _ping_host(host: str, count: int = 1, timeout: int = 1) -> bool:
    """Return True if host responds to a single ICMP ping.
    Uses the system ping command with a short timeout.
    """
    # Prefer GNU ping options; fall back to a simpler form if needed
    cmd = ["ping", "-c", str(count), "-W", str(timeout), host]
    try:
        proc = await asyncio.create_subprocess_exec(*cmd, stdout=DEVNULL, stderr=DEVNULL)
        rc = await proc.wait()
        return rc == 0
    except Exception:
        # Fallback without -W
        try:
            proc = await asyncio.create_subprocess_exec("ping", "-c", str(count), host, stdout=DEVNULL, stderr=DEVNULL)
            rc = await proc.wait()
            return rc == 0
        except Exception as e:
            debug("ping exec failed:", e)
            return False


async def wifi_scan_once() -> dict:
    """Check configured WIFI_HOSTS for reachability via ICMP ping.
    Returns mapping key -> info dict similar to BLE seen structure.
    """
    seen: Dict[str, Dict[str, Any]] = {}
    if not WIFI_HOSTS:
        # fall through: still allow mDNS discovery below
        pass
    tasks = []
    for entry in WIFI_HOSTS:
        host = entry["host"]
        tasks.append(_ping_host(host))
    results = await asyncio.gather(*tasks, return_exceptions=True)
    for entry, ok in zip(WIFI_HOSTS, results):
        host = entry["host"]
        name = entry["name"]
        key = f"wifi:{host}"
        if isinstance(ok, Exception):
            debug("wifi check error:", host, ok)
            continue
        if ok:
            seen[key] = {"name": name, "rssi": None, "type": "WiFi"}
            debug("wifi seen:", f"{name} [{host}] (WiFi)")
        else:
            debug("wifi not reachable:", host)
    # Optional mDNS browse via avahi-browse (no prior IPs required)
    if MDNS_DISCOVERY:
        mdns = await mdns_scan_once()
        seen.update(mdns)
    return seen


async def mdns_scan_once(timeout: float = 5.0) -> dict:
    """Discover local devices announcing mDNS/Bonjour services using avahi-browse.
    Returns mapping key -> info dict (type: mDNS). Requires avahi-utils.
    """
    cmd = ["avahi-browse", "-artp", "-t"]
    try:
        proc = await asyncio.create_subprocess_exec(*cmd, stdout=PIPE, stderr=DEVNULL)
    except FileNotFoundError:
        debug("mdns: avahi-browse not found; skipping")
        return {}
    try:
        out, _ = await asyncio.wait_for(proc.communicate(), timeout=timeout)
    except asyncio.TimeoutError:
        debug("mdns: browse timeout")
        try:
            proc.kill()
        except Exception:
            pass
        return {}
    if not out:
        return {}
    seen: Dict[str, Dict[str, Any]] = {}
    for line in out.decode(errors="ignore").splitlines():
        if not line or line[0] not in "+=":
            continue
        parts = line.split(";")
        # event;ifindex;proto;name;type;domain;host;address;port;txt
        if len(parts) < 9:
            continue
        name = parts[3]
        host = parts[6]
        address = parts[7]
        key = f"mdns:{host or name}"
        seen[key] = {"name": name or host, "rssi": None, "type": "mDNS"}
        debug("mdns seen:", name, host, address)
    return seen


# ---------- BlueZ helpers (with casts to Any to silence type checkers) ----------
def _val(x, default=None):
    """Unwrap dbus_next Variant to plain value."""
    if isinstance(x, Variant):
        return x.value
    return x if x is not None else default

async def _get_interface_any(bus: MessageBus, service: str, path: str, interface: str) -> Any:
    """Return a D-Bus interface cast to Any so dynamic call_* methods don't upset static type checkers."""
    intros = await bus.introspect(service, path)
    obj = bus.get_proxy_object(service, path, intros)
    iface = obj.get_interface(interface)
    return cast(Any, iface)


async def get_managed_objects(bus: MessageBus) -> Dict[str, Any]:
    om = await _get_interface_any(bus, "org.bluez", "/", "org.freedesktop.DBus.ObjectManager")
    return await om.call_get_managed_objects()


def find_adapter_path(objects: Dict[str, Any]) -> str:
    for path, ifaces in objects.items():
        if "org.bluez.Adapter1" in ifaces:
            return path
    raise RuntimeError("No BlueZ adapter (hciX) found.")


async def get_adapter_ifaces(bus: MessageBus, adapter_path: str):
    adapter = await _get_interface_any(bus, "org.bluez", adapter_path, "org.bluez.Adapter1")
    props = await _get_interface_any(bus, "org.bluez", adapter_path, "org.freedesktop.DBus.Properties")
    return adapter, props


async def ensure_powered(bus: MessageBus, adapter_path: str):
    """Ensure adapter is powered on; try to enable it if not."""
    _adapter, props = await get_adapter_ifaces(bus, adapter_path)
    powered = await props.call_get("org.bluez.Adapter1", "Powered")
    if isinstance(powered, Variant):
        powered = powered.value
    if not powered:
        await props.call_set("org.bluez.Adapter1", "Powered", Variant("b", True))


async def is_discovering(bus: MessageBus, adapter_path: str) -> bool:
    _adapter, props = await get_adapter_ifaces(bus, adapter_path)
    discovering = await props.call_get("org.bluez.Adapter1", "Discovering")
    if isinstance(discovering, Variant):
        discovering = discovering.value
    return bool(discovering)


async def scan_once(bus: MessageBus, adapter_path: str):
    """Perform one discovery window and return devices seen in this window."""
    adapter, _props = await get_adapter_ifaces(bus, adapter_path)

    # BLE + Classic; DuplicateData=True so RSSI updates trigger in the same window
    try:
        await adapter.call_set_discovery_filter(
            {
                "Transport": Variant("s", SCAN_TRANSPORT),
                "DuplicateData": Variant("b", True),
            }
        )
    except Exception as e:
        print("[WARN] SetDiscoveryFilter failed:", e)

    if CONTINUOUS_DISCOVERY:
        # Ensure discovery is on; don't stop it to avoid missing sparse beacons
        try:
            if not await is_discovering(bus, adapter_path):
                await adapter.call_start_discovery()
        except Exception as e:
            debug("start_discovery (continuous) failed:", e)
        await asyncio.sleep(SCAN_WINDOW)
    else:
        await adapter.call_start_discovery()
        try:
            await asyncio.sleep(SCAN_WINDOW)
        finally:
            await adapter.call_stop_discovery()

    objects = await get_managed_objects(bus)
    seen_now: Dict[str, Dict[str, Any]] = {}

    debug("Managed objects:", len(objects))
    for _path, ifaces in objects.items():
        dev = ifaces.get("org.bluez.Device1")
        if not dev:
            continue
        mac = _val(dev.get("Address"))
        if not mac:
            continue
        mac = str(mac).upper()
        if mac in IGNORE_MACS:
            debug(mac, "ignored by IGNORE_MACS")
            continue

        name = _val(dev.get("Name")) or _val(dev.get("Alias")) or ""
        if not match_whitelist(name):
            debug(mac, "filtered by NAME_WHITELIST; name=", repr(name))
            continue

        rssi = _val(dev.get("RSSI")) if "RSSI" in dev else None
        connected = bool(_val(dev.get("Connected", False)))
        # Consider "seen" if it reports RSSI in this window or is currently connected
        if (rssi is not None) or connected:
            dtype = infer_type(dev)
            seen_now[mac] = {
                "name": name,
                "rssi": rssi,
                "type": dtype,
            }
            debug("seen:", fmt_device_line(name, mac, dtype, rssi), "connected=", connected)
        else:
            dtype = infer_type(dev)
            uuids = _val(dev.get("UUIDs")) if "UUIDs" in dev else None
            debug(
                "skipped (no RSSI, not connected):",
                fmt_device_line(name, mac, dtype, rssi),
                "uuids=",
                uuids,
            )

    return seen_now


# ---------- Main loop ----------
async def main():
    global state, _running
    state = load_state()
    _running = True

    bus = await MessageBus(bus_type=BusType.SYSTEM).connect()
    objects = await get_managed_objects(bus)
    adapter_path = find_adapter_path(objects)
    await ensure_powered(bus, adapter_path)

    hello = f"▶️ {tag_prefix()} started. interval={SCAN_INTERVAL}s, window={SCAN_WINDOW}s, lost_after={GONE_AFTER}s"
    print(hello)
    send_telegram(hello)

    while _running:
        t0 = time.time()
        try:
            seen_now = await scan_once(bus, adapter_path)
        except Exception as e:
            print("[ERROR] Scan failed:", e)
            await asyncio.sleep(5)
            # try to refresh adapter in case it changed
            try:
                adapter_path = find_adapter_path(await get_managed_objects(bus))
            except Exception:
                pass
            continue

        # Merge BLE and optional Wi‑Fi presence
        wifi_now = await wifi_scan_once()
        seen_now.update(wifi_now)

        changed = False
        now = now_utc()

        # New / reappeared devices
        for mac, info in seen_now.items():
            prev = state.get(mac)
            if prev is None or prev.get("status") == "gone":
                state[mac] = {
                    "name": info["name"],
                    "type": info["type"],
                    "rssi": info["rssi"],
                    "last_seen": now,
                    "status": "present",
                }
                changed = True
                msg = f"🟢 {tag_prefix()} — DETECTED {fmt_device_line(info['name'], mac, info['type'], info['rssi'])}"
                print(msg)
                send_telegram(msg)
            else:
                prev["last_seen"] = now
                prev["rssi"] = info["rssi"]
                if info["name"]:
                    prev["name"] = info["name"]
                if not prev.get("type"):
                    prev["type"] = info["type"]
                changed = True  # timestamp updated

        # Lost devices
        for mac, prev in list(state.items()):
            last_seen = prev.get("last_seen")
            if not isinstance(last_seen, datetime):
                try:
                    last_seen = datetime.fromisoformat(last_seen)
                except Exception:
                    last_seen = now
            if prev.get("status") != "gone" and (now - last_seen) > timedelta(seconds=GONE_AFTER):
                prev["status"] = "gone"
                changed = True
                msg = f"🔴 {tag_prefix()} — LOST {fmt_device_line(prev.get('name'), mac, prev.get('type', 'Unknown'), prev.get('rssi'))}"
                print(msg)
                send_telegram(msg)

        if changed:
            save_state()

        sleep_left = max(0.0, SCAN_INTERVAL - (time.time() - t0))
        await asyncio.sleep(sleep_left)


def cli() -> None:
    """Console entry point for `shadowtrace` script."""
    asyncio.run(main())
