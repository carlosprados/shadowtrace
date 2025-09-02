.PHONY: help sync dev test run add format lint lint-fix service-install service-restart service-uninstall service-status service-logs service-logs-follow

help:
	@echo "Common targets:"
	@echo "  make sync      # install deps (incl. dev if specified)"
	@echo "  make dev       # install dev deps"
	@echo "  make test      # run pytest"
	@echo "  make run       # run the app"
	@echo "  make add PKG=x # add a new dependency"
	@echo "  make format    # format code with ruff"
	@echo "  make lint      # lint code with ruff"
	@echo "  make lint-fix  # lint + auto-fix with ruff"
	@echo "  make service-install  # install+enable user systemd unit"
	@echo "  make service-restart  # restart user systemd unit"
	@echo "  make service-uninstall# disable+remove user systemd unit"
	@echo "  make service-status   # show unit status"
	@echo "  make service-logs     # show recent unit logs"
	@echo "  make service-logs-follow # follow unit logs"

sync:
	uv sync

dev:
	uv sync --group dev

test:
	uv run pytest

run:
	uv run python main.py

add:
	uv add $(PKG)

format:
	uv run ruff format .

lint:
	uv run ruff check .

lint-fix:
	uv run ruff check --fix .

# Install and enable the user systemd service
SERVICE_DIR ?= $(HOME)/.config/systemd/user
service-install:
	mkdir -p $(SERVICE_DIR)
	cp shadowtrace.service $(SERVICE_DIR)/shadowtrace.service
	systemctl --user daemon-reload
	systemctl --user enable --now shadowtrace

service-restart:
	systemctl --user restart shadowtrace

service-uninstall:
	- systemctl --user disable --now shadowtrace
	rm -f $(SERVICE_DIR)/shadowtrace.service
	systemctl --user daemon-reload

service-status:
	systemctl --user status shadowtrace --no-pager

service-logs:
	journalctl --user -u shadowtrace -n 200 --no-pager

service-logs-follow:
	journalctl --user -u shadowtrace -f
