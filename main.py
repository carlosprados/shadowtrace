#!/usr/bin/env python3
"""Entrypoint for ShadowTrace; delegates to package module."""

import asyncio
import signal

from shadowtrace import app

# Re-export symbols used by tests or external integrations
now_utc = app.now_utc
find_adapter_path = app.find_adapter_path


def _handle_stop(*_):
    print("Termination signal received. Exiting…")


def main() -> None:
    signal.signal(signal.SIGINT, _handle_stop)
    signal.signal(signal.SIGTERM, _handle_stop)
    asyncio.run(app.main())


if __name__ == "__main__":
    main()

