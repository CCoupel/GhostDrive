# GhostDrive Plugin SDK — Makefile
# Builds the echo plugin binary for one or both target platforms.
#
# Usage:
#   make build        — compile echo.exe for Windows AMD64
#   make build-linux  — compile echo for Linux AMD64
#   make build-all    — compile both (echo.exe + echo)
#   make clean        — remove all build artefacts

GOARCH := amd64
CGO    := 0

.PHONY: build build-linux build-all clean

build:
	@echo "Building echo plugin for windows/$(GOARCH)..."
	GOOS=windows GOARCH=$(GOARCH) CGO_ENABLED=$(CGO) \
		go build -tags ignore -ldflags="-s -w" -o echo.exe ./echo/
	@echo "Done: echo.exe"

build-linux:
	@echo "Building echo plugin for linux/$(GOARCH)..."
	GOOS=linux GOARCH=$(GOARCH) CGO_ENABLED=$(CGO) \
		go build -tags ignore -ldflags="-s -w" -o echo ./echo/
	@echo "Done: echo"

build-all: build build-linux
	@echo "All targets built."

clean:
	@rm -f echo.exe echo
	@echo "Cleaned."
