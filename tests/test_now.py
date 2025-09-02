import importlib

from .test_find_adapter import import_main_with_stubs


def test_now_returns_recent_utc():
    main = import_main_with_stubs()
    before = __import__("datetime").datetime.utcnow()
    ts = main.now_utc()
    after = __import__("datetime").datetime.utcnow()
    assert before <= ts <= after
