# PurfecTerm Makefile

# Detect native platform
NATIVE_OS := $(shell go env GOOS)
NATIVE_ARCH := $(shell go env GOARCH)

.PHONY: build test clean fmt lint help

# Build all packages
build:
	@echo "Building purfecterm packages..."
	go build ./...
	@echo "Build complete"

# Build GTK widget (requires GTK3 development libraries)
# Linux: apt install libgtk-3-dev
# macOS: brew install gtk+3
build-gtk:
	@echo "Building purfecterm-gtk for $(NATIVE_OS)/$(NATIVE_ARCH)..."
	go build ./purfecterm-gtk
	@echo "Build complete"

# Build Qt widget (requires Qt5 development libraries)
# Linux: apt install qtbase5-dev
# macOS: brew install qt@5
build-qt:
	@echo "Building purfecterm-qt for $(NATIVE_OS)/$(NATIVE_ARCH)..."
	go build ./purfecterm-qt
	@echo "Build complete"

test:
	@echo "Running tests..."
	go test -v ./...
	@echo "Tests complete"

clean:
	@echo "Cleaning..."
	go clean ./...
	@echo "Clean complete"

fmt:
	@echo "Formatting code..."
	go fmt ./...
	@echo "Format complete"

lint:
	@echo "Running linter..."
	golangci-lint run ./...
	@echo "Lint complete"

help:
	@echo "PurfecTerm Makefile"
	@echo ""
	@echo "Targets:"
	@echo "  build      - Build all packages"
	@echo "  build-gtk  - Build GTK3 widget package"
	@echo "  build-qt   - Build Qt5 widget package"
	@echo "  test       - Run tests"
	@echo "  clean      - Clean build artifacts"
	@echo "  fmt        - Format code"
	@echo "  lint       - Run linter"
	@echo ""
	@echo "Requirements:"
	@echo "  GTK: Linux: libgtk-3-dev | macOS: brew install gtk+3"
	@echo "  Qt:  Linux: qtbase5-dev  | macOS: brew install qt@5"
