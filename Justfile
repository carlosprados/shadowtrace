# ShadowTrace task runner — https://github.com/casey/just

binary      := "shadowtrace"
service_dir := env_var_or_default("SERVICE_DIR", home_directory() / ".config/systemd/user")
events      := home_directory() / ".shadowtrace_events.jsonl"
model       := home_directory() / ".shadowtrace_anomaly.json"
version     := `git describe --tags --always --dirty 2>/dev/null || echo dev`
ldflags     := "-s -w -X github.com/carlosprados/shadowtrace/cmd.Version=" + version

# List available recipes
default:
    @just --list

# Build the self-contained binary for this host
build:
    go build -ldflags "{{ldflags}}" -o {{binary}} .

# Cross-compile for a Raspberry Pi (arch: arm64 | armv7 | armv6)
build-pi arch="arm64":
    #!/usr/bin/env bash
    set -euo pipefail
    case "{{arch}}" in
      arm64) goarch=arm64; goarm="" ;;
      armv7) goarch=arm;   goarm=7  ;;
      armv6) goarch=arm;   goarm=6  ;;
      *) echo "arch must be arm64 | armv7 | armv6"; exit 1 ;;
    esac
    mkdir -p dist
    CGO_ENABLED=0 GOOS=linux GOARCH=$goarch GOARM=$goarm \
      go build -ldflags "{{ldflags}}" -o dist/shadowtrace-linux-{{arch}} .
    echo "built dist/shadowtrace-linux-{{arch}}"

# Cross-compile all Raspberry Pi variants
build-pi-all: (build-pi "arm64") (build-pi "armv7") (build-pi "armv6")

# Install the binary into GOBIN (~/go/bin by default)
install:
    go install -ldflags "{{ldflags}}" .

# Tidy go.mod / go.sum
tidy:
    go mod tidy

# Run without building, e.g. `just run scan --adapter hci0`
run *args:
    go run . {{args}}

# One-shot environment scan
scan *args:
    go run . scan {{args}}

# Run tests
test:
    go test ./...

# Static analysis
vet:
    go vet ./...

# Format
fmt:
    gofmt -w .

# Train the anomaly model in Python (uv auto-installs deps via PEP 723)
anomaly-train:
    uv run tools/train.py --events {{events}} --model {{model}}

# Score events with the trained model (Go inference)
anomaly-score *args:
    go run . anomaly score {{args}}

# Force-refresh the OUI vendor database
oui-update:
    go run . oui update

# Build + install the user systemd unit (adjust paths in shadowtrace.service first)
service-install: build
    mkdir -p {{service_dir}}
    cp shadowtrace.service {{service_dir}}/shadowtrace.service
    systemctl --user daemon-reload
    systemctl --user enable --now shadowtrace

service-restart:
    systemctl --user restart shadowtrace

service-uninstall:
    -systemctl --user disable --now shadowtrace
    rm -f {{service_dir}}/shadowtrace.service
    systemctl --user daemon-reload

service-status:
    systemctl --user status shadowtrace --no-pager

service-logs:
    journalctl --user -u shadowtrace -n 200 --no-pager

service-logs-follow:
    journalctl --user -u shadowtrace -f
