import importlib
import sys
import types


def import_main_with_stubs():
    # Stub out dbus-next modules so importing main.py doesn't require the package.
    dbus_next = types.ModuleType("dbus_next")
    aio = types.ModuleType("dbus_next.aio")
    message_bus = types.ModuleType("dbus_next.aio.message_bus")
    constants = types.ModuleType("dbus_next.constants")
    signature = types.ModuleType("dbus_next.signature")

    class MessageBus:  # minimal placeholder
        pass

    class BusType:
        SYSTEM = object()

    class Variant:
        def __init__(self, _sig, value):
            self.value = value

    message_bus.MessageBus = MessageBus
    constants.BusType = BusType
    signature.Variant = Variant

    # Stub external libs used at import time
    dotenv = types.ModuleType("dotenv")
    def _noop():
        return None
    dotenv.load_dotenv = _noop

    requests = types.ModuleType("requests")
    def _post(*args, **kwargs):
        class R:
            status_code = 200
            text = ""
        return R()
    requests.post = _post

    sys.modules.setdefault("dbus_next", dbus_next)
    sys.modules.setdefault("dbus_next.aio", aio)
    sys.modules.setdefault("dbus_next.aio.message_bus", message_bus)
    sys.modules.setdefault("dbus_next.constants", constants)
    sys.modules.setdefault("dbus_next.signature", signature)
    sys.modules.setdefault("dotenv", dotenv)
    sys.modules.setdefault("requests", requests)

    return importlib.import_module("main")


def test_find_adapter_selects_adapter_path():
    main = import_main_with_stubs()
    objects = {
        "/org/bluez/hci0": {"org.bluez.Adapter1": {}},
        "/org/bluez/hci0/dev_XX": {},
    }
    assert main.find_adapter_path(objects) == "/org/bluez/hci0"


def test_find_adapter_raises_when_missing():
    main = import_main_with_stubs()
    with __import__("pytest").raises(RuntimeError):
        main.find_adapter_path({"/some/path": {}})
