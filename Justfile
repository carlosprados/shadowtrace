# ShadowTrace task runner — https://github.com/casey/just

service_dir := env_var_or_default("SERVICE_DIR", home_directory() / ".config/systemd/user")

# List available recipes
default:
    @just --list

# Install runtime deps into .venv
sync:
    uv sync

# Install runtime + dev deps (pytest, ruff)
dev:
    uv sync --extra dev

# Run the test suite
test:
    uv run pytest

# Run the app (uses MODE from env/.env; default is watch)
run:
    uv run python main.py

# Quick foreground watch-mode smoke test (throwaway files, alerts to stdout)
watch-test:
    ./scripts/watch-test.sh

# Add a dependency, e.g. `just add requests`
add pkg:
    uv add {{pkg}}

# Format code with ruff
format:
    uv run ruff format .

# Lint with ruff
lint:
    uv run ruff check .

# Lint with autofix
lint-fix:
    uv run ruff check --fix .

# Install + enable the user systemd unit
service-install:
    mkdir -p {{service_dir}}
    cp shadowtrace.service {{service_dir}}/shadowtrace.service
    systemctl --user daemon-reload
    systemctl --user enable --now shadowtrace

# Restart the user systemd unit
service-restart:
    systemctl --user restart shadowtrace

# Disable + remove the user systemd unit
service-uninstall:
    -systemctl --user disable --now shadowtrace
    rm -f {{service_dir}}/shadowtrace.service
    systemctl --user daemon-reload

# Show unit status
service-status:
    systemctl --user status shadowtrace --no-pager

# Show recent unit logs
service-logs:
    journalctl --user -u shadowtrace -n 200 --no-pager

# Follow unit logs
service-logs-follow:
    journalctl --user -u shadowtrace -f
