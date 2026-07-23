import pytest

from .test_find_adapter import import_main_with_stubs


@pytest.fixture()
def app():
    return import_main_with_stubs().app


def test_fingerprint_survives_mac_rotation(app):
    """Same name/company/uuids => same fingerprint even if the MAC changes."""
    fp1 = app.device_fingerprint("Pixel", 224, ["0000fe9f-0000"], "AA:BB:CC:00:00:01")
    fp2 = app.device_fingerprint("Pixel", 224, ["0000fe9f-0000"], "FF:EE:DD:00:00:02")
    assert fp1 == fp2
    assert fp1.startswith("fp:")


def test_fingerprint_uuids_order_independent(app):
    a = app.device_fingerprint("x", None, ["b", "a"], "AA")
    b = app.device_fingerprint("x", None, ["a", "b"], "AA")
    assert a == b


def test_fingerprint_falls_back_to_mac(app):
    """Anonymous advertisement (no name/company/uuids) keys on the raw MAC."""
    fp = app.device_fingerprint("", None, None, "AA:BB:CC:DD:EE:FF")
    assert fp == "mac:AA:BB:CC:DD:EE:FF"


def test_fingerprint_ignores_mac_shaped_name(app):
    """A name that is really a MAC must not enter the fingerprint (it rotates)."""
    fp = app.device_fingerprint("24-EB-90-67-E7-AA", None, ["svc"], "24:EB:90:67:E7:AA")
    assert fp == "fp:u=svc"
    assert app._looks_like_mac("24-EB-90-67-E7-AA")
    assert app._looks_like_mac("AA:BB:CC:DD:EE:FF")
    assert not app._looks_like_mac("MP1_FDE349")


def test_find_adapter_prefers_named(app):
    objects = {
        "/org/bluez/hci0": {"org.bluez.Adapter1": {}},
        "/org/bluez/hci1": {"org.bluez.Adapter1": {}},
    }
    assert app.find_adapter_path(objects, "hci1") == "/org/bluez/hci1"


def test_find_adapter_prefer_missing_falls_back(app):
    objects = {"/org/bluez/hci0": {"org.bluez.Adapter1": {}}}
    assert app.find_adapter_path(objects, "hci9") == "/org/bluez/hci0"
