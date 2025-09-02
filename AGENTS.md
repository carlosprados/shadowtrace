# Repository Guidelines

## Project Structure & Module Organization
- Entry: `main.py` (thin wrapper calling `shadowtrace/app.py`).
- Package: `shadowtrace/` with core logic in `app.py`.
- Config: `pyproject.toml` and `uv.lock`.
- Virtual env: `.venv/` (managed by `uv`).
- Tests: `tests/` with `test_*.py` naming.

## Build, Test, and Development Commands
- Install deps (recommended): `uv sync` — creates `.venv` and installs from `pyproject.toml`.
- Dev deps: `uv sync --group dev` — installs pytest and tools.
- Run the app: `uv run python main.py` or `make run` — starts Bluetooth scan loop.
- Test: `uv run pytest` or `make test` — runs unit tests.
- Add a package: `uv add <pkg>` or `make add PKG=package` — records in `pyproject.toml` and locks.

Notes: Requires Linux with BlueZ, a Bluetooth adapter, and access to the system D‑Bus. You may need to run under a user with Bluetooth permissions.

## Coding Style & Naming Conventions
- Indentation: 4 spaces; line length ~100.
- Naming: modules `snake_case.py`; functions/vars `snake_case`; constants `UPPER_SNAKE`.
- Type hints: prefer annotations and docstrings for public functions.
- Structure: keep I/O at the edge; isolate BlueZ/D‑Bus calls; separate pure logic for easy testing.

## Testing Guidelines
- Framework: pytest (suggested). Place tests in `tests/` with `test_*.py`.
- Running: `pytest -q` or `uv run pytest -q`.
- Coverage: target ≥80% for new logic; add tests for device state transitions (present → gone) and adapter discovery errors.

## Commit & Pull Request Guidelines
- Commits: clear, imperative subject (e.g., "Add BlueZ scan filter") and focused diffs.
- Branches: short-lived feature branches; reference issues in commit bodies when relevant.
- PRs: include purpose, summary of changes, how to run/verify, and any screenshots or logs. Link related issues. Keep PRs small and reviewable.

CI: GitHub Actions runs tests on pushes and PRs to `main`/`master` using `uv`.

## Security & Configuration Tips
- Secrets: load via environment or `.env` (supported by `python-dotenv`); do not commit `.env`.
- System access: avoid requiring `sudo`; prefer adding user to Bluetooth groups or using udev/rules where applicable.
