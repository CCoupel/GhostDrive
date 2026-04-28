# GhostDrive Plugin SDK — Makefile
# Builds the echo plugin and any custom plugins in this directory.
#
# Usage:
#   make build   — compile echo.exe for Windows AMD64
#   make clean   — remove build artifacts

GOOS   := windows
GOARCH := amd64
CGO    := 0

.PHONY: build clean

build:
	@echo "Building echo plugin for $(GOOS)/$(GOARCH)..."
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO) \
		go build -tags ignore -ldflags="-s -w" -o echo.exe ./echo/
	@echo "Done: echo.exe"

clean:
	@rm -f echo.exe
	@echo "Cleaned."
