import asyncio
import json
import os
import re
import time
from datetime import datetime, timedelta, timezone
from typing import Dict, Any, Optional, List, cast

from dbus_next.aio.message_bus import MessageBus
from dbus_next.constants import BusType
from dbus_next.signature import Variant
from dotenv import load_dotenv
import requests
from asyncio.subprocess import DEVNULL, PIPE
import ipaddress

# ---------- Configuration ----------
load_dotenv()

APP_NAME = os.getenv("APP_NAME", "ShadowTrace")
LOCATION_TAG = os.getenv("LOCATION_TAG", "").strip()

# Operating mode: "watch" (environment IDS) or "presence" (legacy presence tracker).
MODE = os.getenv("MODE", "watch").strip().lower()

TELEGRAM_BOT_TOKEN = os.getenv("TELEGRAM_BOT_TOKEN", "").strip()
TELEGRAM_CHAT_ID = os.getenv("TELEGRAM_CHAT_ID", "").strip()

# ---- Shared scan config ----
SCAN_INTERVAL = int(os.getenv("SCAN_INTERVAL_SECONDS", "20"))  # full cycle duration
SCAN_WINDOW = int(os.getenv("SCAN_WINDOW_SECONDS", "8"))  # discovery window per cycle
SCAN_TRANSPORT = os.getenv("SCAN_TRANSPORT", "auto").lower()  # auto|le|bredr
# Preferred Bluetooth adapter (e.g. "hci1"); empty => first available.
BT_ADAPTER = (os.getenv("BT_ADAPTER", "") or os.getenv("WATCH_ADAPTER", "")).strip()

DEBUG = os.getenv("DEBUG", "0").strip() not in ("", "0", "false", "False")
CONTINUOUS_DISCOVERY = os.getenv("CONTINUOUS_DISCOVERY", "1").strip() not in ("", "0", "false", "False")

# ---- Watch mode config (environment IDS) ----
# Only sightings at/above this RSSI count as "near / inside". Weaker = farther (neighbours).
WATCH_RSSI_MIN = int(os.getenv("WATCH_RSSI_MIN", "-70"))
# During ALERT_HOURS an (optionally) more permissive threshold is used to catch more.
WATCH_RSSI_MIN_NIGHT = int(os.getenv("WATCH_RSSI_MIN_NIGHT", str(WATCH_RSSI_MIN)))
# Consecutive strong windows required before a device is treated as "really present".
WATCH_CONFIRM_HITS = int(os.getenv("WATCH_CONFIRM_HITS", "2"))
# Seconds without a strong sighting before a present device is considered gone.
WATCH_GONE_AFTER = int(os.getenv("WATCH_GONE_AFTER_SECONDS", "120"))
# Learning window: every strong device seen during this period is added to the baseline.
WATCH_LEARN_SECONDS = int(os.getenv("WATCH_LEARN_SECONDS", "86400"))
# Minimum seconds between repeated alerts for the same device.
ALERT_COOLDOWN = int(os.getenv("ALERT_COOLDOWN_SECONDS", "600"))
# Reinforced hours "start-end" (24h), e.g. "0-7" or "22-6". Empty => disabled.
ALERT_HOURS = os.getenv("ALERT_HOURS", "").strip()
# MACs that are always known/home regardless of baseline (fixed-MAC devices).
HOME_MACS = {s.strip().upper() for s in os.getenv("HOME_MACS", "").split(",") if s.strip()}

BASELINE_FILE = os.path.expanduser(os.getenv("BASELINE_FILE", "~/.shadowtrace_baseline.json"))
EVENT_LOG = os.path.expanduser(os.getenv("EVENT_LOG", "~/.shadowtrace_events.jsonl"))

# ---- Presence mode config (legacy) ----
GONE_AFTER = int(os.getenv("GONE_AFTER_SECONDS", "60"))  # if unseen for this time => LOST
STATE_FILE = os.path.expanduser(os.getenv("STATE_FILE", "~/.shadowtrace_state.json"))
NAME_WHITELIST = [s.strip() for s in os.getenv("NAME_WHITELIST", "").split(",") if s.strip()]
IGNORE_MACS = {s.strip().upper() for s in os.getenv("IGNORE_MACS", "").split(",") if s.strip()}
MDNS_DISCOVERY = os.getenv("MDNS_DISCOVERY", "1").strip() not in ("", "0", "false", "False")
ARP_DISCOVERY = os.getenv("ARP_DISCOVERY", "0").strip() not in ("", "0", "false", "False")
ARP_SUBNETS = [s.strip() for s in os.getenv("ARP_SUBNETS", "").split(",") if s.strip()]
ARP_SWEEP = os.getenv("ARP_SWEEP", "0").strip() not in ("", "0", "false", "False")
ARP_SWEEP_LIMIT = int(os.getenv("ARP_SWEEP_LIMIT", "256"))
ARP_TIMEOUT_MS = int(os.getenv("ARP_TIMEOUT_MS", "500"))

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
state: Dict[str, Any] = {}  # presence mode: mac -> {...}


# ---------- Utilities ----------
def now_utc() -> datetime:
    """Timezone-aware UTC now."""
    return datetime.now(timezone.utc)


def _parse_dt(value: Any) -> Optional[datetime]:
    if isinstance(value, datetime):
        return value if value.tzinfo else value.replace(tzinfo=timezone.utc)
    if isinstance(value, str):
        try:
            dt = datetime.fromisoformat(value)
            return dt if dt.tzinfo else dt.replace(tzinfo=timezone.utc)
        except Exception:
            return None
    return None


def tag_prefix() -> str:
    return f"{APP_NAME}{' ' + LOCATION_TAG if LOCATION_TAG else ''}"


def ensure_parent_dir(path: str) -> None:
    d = os.path.dirname(path)
    if d and not os.path.exists(d):
        os.makedirs(d, exist_ok=True)


def send_telegram(text: str) -> None:
    """Send a plain-text Telegram message. If not configured, print to stdout.

    Blocking (uses requests); callers on the event loop must wrap it with
    asyncio.to_thread to avoid stalling the scan loop.
    """
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


async def notify(text: str) -> None:
    """Non-blocking alert: prints and sends to Telegram off the event loop."""
    print(text)
    await asyncio.to_thread(send_telegram, text)


def match_whitelist(name: str) -> bool:
    """Return True if device name passes the optional whitelist (presence mode)."""
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


# ---------- Wi‑Fi presence helpers (presence mode) ----------
async def _ping_host(host: str, count: int = 1, timeout: int = 1) -> bool:
    """Return True if host responds to a single ICMP ping."""
    cmd = ["ping", "-c", str(count), "-W", str(timeout), host]
    try:
        proc = await asyncio.create_subprocess_exec(*cmd, stdout=DEVNULL, stderr=DEVNULL)
        rc = await proc.wait()
        return rc == 0
    except Exception:
        try:
            proc = await asyncio.create_subprocess_exec("ping", "-c", str(count), host, stdout=DEVNULL, stderr=DEVNULL)
            rc = await proc.wait()
            return rc == 0
        except Exception as e:
            debug("ping exec failed:", e)
            return False


async def wifi_scan_once() -> dict:
    """Check configured WIFI_HOSTS via ICMP; also fold in mDNS discovery."""
    seen: Dict[str, Dict[str, Any]] = {}
    tasks = [_ping_host(entry["host"]) for entry in WIFI_HOSTS]
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
    if MDNS_DISCOVERY:
        seen.update(await mdns_scan_once())
    return seen


async def mdns_scan_once(timeout: float = 5.0) -> dict:
    """Discover local mDNS/Bonjour services via avahi-browse. Requires avahi-utils."""
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
        if len(parts) < 9:
            continue
        name = parts[3]
        host = parts[6]
        address = parts[7]
        key = f"mdns:{host or name}"
        seen[key] = {"name": name or host, "rssi": None, "type": "mDNS"}
        debug("mdns seen:", name, host, address)
    return seen


# ---------- ARP/neighbour discovery (presence mode) ----------
async def _ip_json(cmd: List[str]) -> Any:
    try:
        proc = await asyncio.create_subprocess_exec(*cmd, stdout=PIPE, stderr=DEVNULL)
        out, _ = await asyncio.wait_for(proc.communicate(), timeout=3)
        if not out:
            return None
        return json.loads(out.decode())
    except Exception as e:
        debug("ip json failed:", cmd, e)
        return None


async def _auto_local_subnets() -> List[str]:
    routes = await _ip_json(["ip", "-j", "route", "show", "scope", "link"])
    subnets: List[str] = []
    if isinstance(routes, list):
        for r in routes:
            dst = r.get("dst")
            if dst and "/" in dst and dst != "default":
                try:
                    net = ipaddress.ip_network(dst, strict=False)
                    if net.version == 4 and 16 <= net.prefixlen <= 30:
                        subnets.append(dst)
                except Exception:
                    continue
    return subnets


async def _arp_table() -> List[dict]:
    neigh = await _ip_json(["ip", "-j", "neigh"])
    return neigh if isinstance(neigh, list) else []


async def _arp_sweep(subnets: List[str], limit: int, timeout_ms: int) -> None:
    ips: List[str] = []
    for cidr in subnets:
        try:
            net = ipaddress.ip_network(cidr, strict=False)
            for ip in net.hosts():
                ips.append(str(ip))
                if len(ips) >= limit:
                    break
        except Exception:
            continue
        if len(ips) >= limit:
            break
    if not ips:
        return
    sem = asyncio.Semaphore(64)

    async def ping_limited(ip: str):
        async with sem:
            return await _ping_host(ip, count=1, timeout=max(1, timeout_ms // 1000))

    await asyncio.gather(*[ping_limited(ip) for ip in ips], return_exceptions=True)


async def arp_scan_once() -> dict:
    seen: Dict[str, Dict[str, Any]] = {}
    if not ARP_DISCOVERY:
        return seen
    subnets = ARP_SUBNETS or await _auto_local_subnets()
    if ARP_SWEEP and subnets:
        await _arp_sweep(subnets, ARP_SWEEP_LIMIT, ARP_TIMEOUT_MS)
    neigh = await _arp_table()
    for n in neigh:
        ip = n.get("dst") or n.get("to") or n.get("ip")
        mac = (n.get("lladdr") or "").upper()
        nstate = (n.get("state") or "").upper()
        if not ip or not mac:
            continue
        if nstate in {"FAILED", "INCOMPLETE"}:
            continue
        key = f"arp:{mac}"
        seen[key] = {"name": ip, "rssi": None, "type": "ARP"}
        debug("arp seen:", ip, mac, nstate, n.get("dev"))
    return seen


# ---------- BlueZ helpers ----------
def _val(x, default=None):
    """Unwrap dbus_next Variant to plain value."""
    if isinstance(x, Variant):
        return x.value
    return x if x is not None else default


async def _get_interface_any(bus: MessageBus, service: str, path: str, interface: str) -> Any:
    intros = await bus.introspect(service, path)
    obj = bus.get_proxy_object(service, path, intros)
    iface = obj.get_interface(interface)
    return cast(Any, iface)


async def get_managed_objects(bus: MessageBus) -> Dict[str, Any]:
    om = await _get_interface_any(bus, "org.bluez", "/", "org.freedesktop.DBus.ObjectManager")
    return await om.call_get_managed_objects()


def find_adapter_path(objects: Dict[str, Any], prefer: str = "") -> str:
    """Return a BlueZ adapter path. If `prefer` (e.g. 'hci1') matches, return it."""
    paths = [p for p, ifaces in objects.items() if "org.bluez.Adapter1" in ifaces]
    if not paths:
        raise RuntimeError("No BlueZ adapter (hciX) found.")
    if prefer:
        for p in paths:
            if p.rstrip("/").endswith(prefer):
                return p
        debug(f"preferred adapter {prefer!r} not found; using {paths[0]}")
    return paths[0]


async def get_adapter_ifaces(bus: MessageBus, adapter_path: str):
    adapter = await _get_interface_any(bus, "org.bluez", adapter_path, "org.bluez.Adapter1")
    props = await _get_interface_any(bus, "org.bluez", adapter_path, "org.freedesktop.DBus.Properties")
    return adapter, props


async def ensure_powered(bus: MessageBus, adapter_path: str):
    _adapter, props = await get_adapter_ifaces(bus, adapter_path)
    powered = _val(await props.call_get("org.bluez.Adapter1", "Powered"))
    if not powered:
        await props.call_set("org.bluez.Adapter1", "Powered", Variant("b", True))


async def is_discovering(bus: MessageBus, adapter_path: str) -> bool:
    _adapter, props = await get_adapter_ifaces(bus, adapter_path)
    return bool(_val(await props.call_get("org.bluez.Adapter1", "Discovering")))


def _manufacturer_company(dev: Dict[str, Any]) -> Optional[int]:
    """First Bluetooth SIG company id from ManufacturerData, if any."""
    md = _val(dev.get("ManufacturerData")) if "ManufacturerData" in dev else None
    if isinstance(md, dict) and md:
        try:
            return sorted(int(k) for k in md.keys())[0]
        except Exception:
            return None
    return None


_MAC_RE = re.compile(r"^([0-9A-Fa-f]{2}[:-]){5}[0-9A-Fa-f]{2}$")


def _looks_like_mac(name: str) -> bool:
    """True if the advertised name is really just a MAC address (some devices do
    this). Such names rotate with the MAC and are useless as a stable identity."""
    return bool(name) and bool(_MAC_RE.match(name.strip()))


def device_fingerprint(name: str, company: Optional[int], uuids: Optional[List[str]], mac: str) -> str:
    """Stable-ish identity that survives MAC rotation when the device exposes
    a name, a manufacturer id, or service UUIDs. Falls back to MAC otherwise."""
    parts: List[str] = []
    if name and not _looks_like_mac(name):
        parts.append(f"n={name}")
    if company is not None:
        parts.append(f"c={company}")
    if uuids:
        parts.append("u=" + ",".join(sorted(uuids)))
    if parts:
        return "fp:" + "|".join(parts)
    return "mac:" + mac


async def _run_discovery_window(bus: MessageBus, adapter_path: str) -> None:
    """Run one discovery window on the adapter (shared by both modes)."""
    adapter, _props = await get_adapter_ifaces(bus, adapter_path)
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


async def scan_ble(bus: MessageBus, adapter_path: str) -> List[Dict[str, Any]]:
    """One discovery window; return raw observations for every BLE/Classic device
    that reported RSSI this window or is currently connected."""
    await _run_discovery_window(bus, adapter_path)
    objects = await get_managed_objects(bus)
    out: List[Dict[str, Any]] = []
    debug("Managed objects:", len(objects))
    for _path, ifaces in objects.items():
        dev = ifaces.get("org.bluez.Device1")
        if not dev:
            continue
        mac = _val(dev.get("Address"))
        if not mac:
            continue
        mac = str(mac).upper()
        rssi = _val(dev.get("RSSI")) if "RSSI" in dev else None
        connected = bool(_val(dev.get("Connected", False)))
        if rssi is None and not connected:
            continue
        name = _val(dev.get("Name")) or _val(dev.get("Alias")) or ""
        uuids = _val(dev.get("UUIDs")) if "UUIDs" in dev else None
        out.append(
            {
                "mac": mac,
                "name": name,
                "type": infer_type(dev),
                "rssi": rssi,
                "connected": connected,
                "company": _manufacturer_company(dev),
                "uuids": uuids if isinstance(uuids, list) else None,
            }
        )
    return out


# ---------- Presence-mode state persistence ----------
def load_state() -> Dict[str, Any]:
    if not os.path.exists(STATE_FILE):
        return {}
    try:
        with open(STATE_FILE, "r", encoding="utf-8") as f:
            data = json.load(f)
        for v in data.values():
            dt = _parse_dt(v.get("last_seen"))
            v["last_seen"] = dt or now_utc()
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


# ---------- Watch-mode persistence ----------
def load_baseline() -> Dict[str, Any]:
    if not os.path.exists(BASELINE_FILE):
        return {"_meta": {}, "fingerprints": {}}
    try:
        with open(BASELINE_FILE, "r", encoding="utf-8") as f:
            data = json.load(f)
        data.setdefault("_meta", {})
        data.setdefault("fingerprints", {})
        return data
    except Exception:
        return {"_meta": {}, "fingerprints": {}}


def save_baseline(baseline: Dict[str, Any]) -> None:
    ensure_parent_dir(BASELINE_FILE)
    tmp = BASELINE_FILE + ".tmp"
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump(baseline, f, ensure_ascii=False, indent=2)
    os.replace(tmp, BASELINE_FILE)


def log_event(event: Dict[str, Any]) -> None:
    """Append one forensic event as a JSON line."""
    ensure_parent_dir(EVENT_LOG)
    try:
        with open(EVENT_LOG, "a", encoding="utf-8") as f:
            f.write(json.dumps(event, ensure_ascii=False) + "\n")
    except Exception as e:
        debug("event log write failed:", e)


def _in_alert_hours(dt: datetime) -> bool:
    if not ALERT_HOURS or "-" not in ALERT_HOURS:
        return False
    try:
        start_s, end_s = ALERT_HOURS.split("-", 1)
        start, end = int(start_s), int(end_s)
    except Exception:
        return False
    h = dt.astimezone().hour
    if start == end:
        return False
    if start < end:
        return start <= h < end
    return h >= start or h < end  # wraps midnight


# ---------- Watch loop (environment IDS) ----------
async def watch_loop(bus: MessageBus, adapter_path: str) -> None:
    global _running
    baseline = load_baseline()
    fingerprints: Dict[str, Any] = baseline["fingerprints"]

    now = now_utc()
    learn_until = _parse_dt(baseline["_meta"].get("learn_until"))
    if learn_until is None:
        learn_until = now + timedelta(seconds=WATCH_LEARN_SECONDS)
        baseline["_meta"]["learn_until"] = learn_until.isoformat()
        baseline["_meta"]["created"] = now.isoformat()
        save_baseline(baseline)

    tracks: Dict[str, Any] = {}  # fingerprint -> live tracking state

    hello = (
        f"👁️ {tag_prefix()} WATCH started on {adapter_path.split('/')[-1]}. "
        f"rssi_min={WATCH_RSSI_MIN}dBm, confirm={WATCH_CONFIRM_HITS}, "
        f"known={len(fingerprints)}, learning_until={learn_until.astimezone().isoformat(timespec='minutes')}"
    )
    await notify(hello)

    while _running:
        t0 = time.time()
        try:
            observations = await scan_ble(bus, adapter_path)
        except Exception as e:
            print("[ERROR] Scan failed:", e)
            await asyncio.sleep(5)
            try:
                adapter_path = find_adapter_path(await get_managed_objects(bus), BT_ADAPTER)
            except Exception:
                pass
            continue

        now = now_utc()
        learning = now < learn_until
        threshold = WATCH_RSSI_MIN_NIGHT if _in_alert_hours(now) else WATCH_RSSI_MIN
        baseline_changed = False

        seen_fps = set()
        for obs in observations:
            rssi = obs["rssi"]
            if rssi is None or rssi < threshold:
                continue  # too far / no signal: not "near"
            fp = device_fingerprint(obs["name"], obs["company"], obs["uuids"], obs["mac"])
            seen_fps.add(fp)
            known = fp in fingerprints or obs["mac"] in HOME_MACS

            # Forensic log entry for every strong sighting of a new session handled below.
            if learning or known:
                # Learn / refresh baseline entry. Only structural changes (a new
                # fingerprint or a newly-seen MAC) trigger a disk write; last_seen/count
                # are kept in memory and flushed on the next structural change.
                entry = fingerprints.get(fp)
                is_new_fp = entry is None
                if entry is None:
                    entry = {
                        "name": "",
                        "type": obs["type"],
                        "macs": [],
                        "first_seen": now.isoformat(),
                        "count": 0,
                    }
                new_mac = obs["mac"] not in entry["macs"]
                if new_mac:
                    entry["macs"].append(obs["mac"])
                if obs["name"] and not _looks_like_mac(obs["name"]) and not entry.get("name"):
                    entry["name"] = obs["name"]
                entry["last_seen"] = now.isoformat()
                entry["count"] = int(entry.get("count", 0)) + 1
                if is_new_fp:
                    fingerprints[fp] = entry
                if is_new_fp or new_mac:
                    baseline_changed = True

            tr = tracks.get(fp)
            if tr is None:
                tr = {
                    "hits": 0,
                    "first_seen": now,
                    "max_rssi": rssi,
                    "min_rssi": rssi,
                    "alerted_at": None,
                    "in_session": False,
                    "name": obs["name"],
                    "mac": obs["mac"],
                    "type": obs["type"],
                    "known": known,
                }
                tracks[fp] = tr
            tr["hits"] += 1
            tr["last_seen"] = now
            tr["max_rssi"] = max(tr["max_rssi"], rssi)
            tr["min_rssi"] = min(tr["min_rssi"], rssi)
            tr["last_rssi"] = rssi
            if obs["name"]:
                tr["name"] = obs["name"]
            tr["known"] = known

            # Confirmed, sustained presence?
            if tr["hits"] >= WATCH_CONFIRM_HITS and not tr["in_session"]:
                tr["in_session"] = True
                log_event(
                    {
                        "ts": now.isoformat(),
                        "event": "appear",
                        "fp": fp,
                        "name": tr["name"],
                        "mac": obs["mac"],
                        "type": obs["type"],
                        "rssi": rssi,
                        "known": known,
                        "learning": learning,
                    }
                )
                if not known and not learning:
                    cooldown_ok = (
                        tr["alerted_at"] is None
                        or (now - tr["alerted_at"]).total_seconds() >= ALERT_COOLDOWN
                    )
                    if cooldown_ok:
                        tr["alerted_at"] = now
                        night = " 🌙" if _in_alert_hours(now) else ""
                        msg = (
                            f"🚨 {tag_prefix()} — UNKNOWN nearby{night} "
                            f"{fmt_device_line(tr['name'], obs['mac'], obs['type'], rssi)}"
                        )
                        await notify(msg)

        # Close sessions for devices no longer seen strongly.
        for fp, tr in list(tracks.items()):
            if fp in seen_fps:
                continue
            gone_for = (now - tr["last_seen"]).total_seconds()
            if gone_for > WATCH_GONE_AFTER:
                if tr["in_session"]:
                    dur = int((tr["last_seen"] - tr["first_seen"]).total_seconds())
                    log_event(
                        {
                            "ts": now.isoformat(),
                            "event": "leave",
                            "fp": fp,
                            "name": tr["name"],
                            "mac": tr["mac"],
                            "type": tr["type"],
                            "rssi_max": tr["max_rssi"],
                            "rssi_min": tr["min_rssi"],
                            "duration_s": dur,
                            "known": tr["known"],
                        }
                    )
                del tracks[fp]

        if learning and now >= learn_until:
            baseline["_meta"]["learn_done"] = now.isoformat()
            baseline_changed = True
            await notify(
                f"✅ {tag_prefix()} — learning finished. {len(fingerprints)} known devices. "
                f"Now alerting on unknowns near (RSSI ≥ {WATCH_RSSI_MIN}dBm)."
            )

        if baseline_changed:
            save_baseline(baseline)

        sleep_left = max(0.0, SCAN_INTERVAL - (time.time() - t0))
        await asyncio.sleep(sleep_left)


# ---------- Presence loop (legacy) ----------
async def presence_scan_once(bus: MessageBus, adapter_path: str) -> Dict[str, Dict[str, Any]]:
    seen_now: Dict[str, Dict[str, Any]] = {}
    for obs in await scan_ble(bus, adapter_path):
        mac = obs["mac"]
        if mac in IGNORE_MACS:
            debug(mac, "ignored by IGNORE_MACS")
            continue
        if not match_whitelist(obs["name"]):
            debug(mac, "filtered by NAME_WHITELIST; name=", repr(obs["name"]))
            continue
        seen_now[mac] = {"name": obs["name"], "rssi": obs["rssi"], "type": obs["type"]}
    return seen_now


async def presence_loop(bus: MessageBus, adapter_path: str) -> None:
    global state, _running
    state = load_state()

    hello = f"▶️ {tag_prefix()} started. interval={SCAN_INTERVAL}s, window={SCAN_WINDOW}s, lost_after={GONE_AFTER}s"
    await notify(hello)

    while _running:
        t0 = time.time()
        try:
            seen_now = await presence_scan_once(bus, adapter_path)
        except Exception as e:
            print("[ERROR] Scan failed:", e)
            await asyncio.sleep(5)
            try:
                adapter_path = find_adapter_path(await get_managed_objects(bus), BT_ADAPTER)
            except Exception:
                pass
            continue

        seen_now.update(await wifi_scan_once())
        seen_now.update(await arp_scan_once())

        changed = False
        now = now_utc()

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
                await notify(
                    f"🟢 {tag_prefix()} — DETECTED {fmt_device_line(info['name'], mac, info['type'], info['rssi'])}"
                )
            else:
                prev["last_seen"] = now
                prev["rssi"] = info["rssi"]
                if info["name"]:
                    prev["name"] = info["name"]
                if not prev.get("type"):
                    prev["type"] = info["type"]

        for mac, prev in list(state.items()):
            last_seen = _parse_dt(prev.get("last_seen")) or now
            if prev.get("status") != "gone" and (now - last_seen) > timedelta(seconds=GONE_AFTER):
                prev["status"] = "gone"
                changed = True
                await notify(
                    f"🔴 {tag_prefix()} — LOST {fmt_device_line(prev.get('name'), mac, prev.get('type', 'Unknown'), prev.get('rssi'))}"
                )

        if changed:
            save_state()

        sleep_left = max(0.0, SCAN_INTERVAL - (time.time() - t0))
        await asyncio.sleep(sleep_left)


# ---------- Main ----------
async def main():
    global _running
    _running = True

    bus = await MessageBus(bus_type=BusType.SYSTEM).connect()
    objects = await get_managed_objects(bus)
    adapter_path = find_adapter_path(objects, BT_ADAPTER)
    await ensure_powered(bus, adapter_path)

    if MODE == "presence":
        await presence_loop(bus, adapter_path)
    else:
        await watch_loop(bus, adapter_path)


def cli() -> None:
    """Console entry point for `shadowtrace` script."""
    asyncio.run(main())
