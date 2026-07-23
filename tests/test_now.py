from datetime import datetime, timezone

from .test_find_adapter import import_main_with_stubs


def test_now_returns_recent_utc():
    main = import_main_with_stubs()
    before = datetime.now(timezone.utc)
    ts = main.now_utc()
    after = datetime.now(timezone.utc)
    assert ts.tzinfo is not None
    assert before <= ts <= after
