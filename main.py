#!/usr/bin/env python3
"""Entrypoint for ShadowTrace; delegates to package module."""

import asyncio

from shadowtrace import app

# Re-export symbols used by tests or external integrations
now_utc = app.now_utc
find_adapter_path = app.find_adapter_path


def main() -> None:
    try:
        asyncio.run(app.main())
    except KeyboardInterrupt:
        # Allow Ctrl+C to terminate immediately
        print("Termination signal received. Exiting…")


if __name__ == "__main__":
    main()
