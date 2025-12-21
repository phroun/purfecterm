# PurfecTerm Makefile

# Detect native platform
NATIVE_OS := $(shell go env GOOS)
NATIVE_ARCH := $(shell go env GOARCH)

.PHONY: build test clean fmt lint help examples example-buffer example-gtk example-qt

# Build all packages
build:
	@echo "Building purfecterm packages..."
	go build ./...
	@echo "Build complete"

# Build GTK widget (requires GTK3 development libraries)
# Linux: apt install libgtk-3-dev
# macOS: brew install gtk+3
build-gtk:
	@echo "Building purfecterm/gtk for $(NATIVE_OS)/$(NATIVE_ARCH)..."
	go build ./gtk
	@echo "Build complete"

# Build Qt widget (requires Qt5 development libraries)
# Linux: apt install qtbase5-dev
# macOS: brew install qt@5
build-qt:
	@echo "Building purfecterm/qt for $(NATIVE_OS)/$(NATIVE_ARCH)..."
	go build ./qt
	@echo "Build complete"

# Build all examples
examples: example-buffer example-gtk example-qt
	@echo "All examples built"

# Build buffer-only example (no GUI dependencies)
example-buffer:
	@echo "Building buffer-only example..."
	go build -o examples/buffer-only/buffer-only ./examples/buffer-only
	@echo "Built: examples/buffer-only/buffer-only"

# Build GTK example (requires GTK3)
example-gtk:
	@echo "Building GTK example..."
	go build -o examples/gtk-basic/gtk-basic ./examples/gtk-basic
	@echo "Built: examples/gtk-basic/gtk-basic"

# Build Qt example (requires Qt5)
example-qt:
	@echo "Building Qt example..."
	go build -o examples/qt-basic/qt-basic ./examples/qt-basic
	@echo "Built: examples/qt-basic/qt-basic"

test:
	@echo "Running tests..."
	go test -v ./...
	@echo "Tests complete"

clean:
	@echo "Cleaning..."
	go clean ./...
	rm -f examples/buffer-only/buffer-only
	rm -f examples/gtk-basic/gtk-basic
	rm -f examples/qt-basic/qt-basic
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
	@echo "  build          - Build all packages"
	@echo "  build-gtk      - Build GTK3 widget package"
	@echo "  build-qt       - Build Qt5 widget package"
	@echo "  examples       - Build all examples"
	@echo "  example-buffer - Build buffer-only example (no GUI)"
	@echo "  example-gtk    - Build GTK3 example"
	@echo "  example-qt     - Build Qt5 example"
	@echo "  test           - Run tests"
	@echo "  clean          - Clean build artifacts"
	@echo "  fmt            - Format code"
	@echo "  lint           - Run linter"
	@echo ""
	@echo "Requirements:"
	@echo "  GTK: Linux: libgtk-3-dev | macOS: brew install gtk+3"
	@echo "  Qt:  Linux: qtbase5-dev  | macOS: brew install qt@5"
